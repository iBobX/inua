package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/clipboard"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/hyprconf"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/notify"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

// Dictation (voice input into any window): the bar's microphone, the
// shortcut behind it, the Dictation page of the settings. Recording and transcription are the voxtype
// daemon's; inua drives it and says what it is doing. voiceRequest answers
// `inua call voice …`, which scripts and the settings can drive too.
func (s *shellUI) voiceRequest(fn string, args []string) ipc.Response {
	if !s.cfg.Voice.Enable {
		return ipc.Response{Error: "dictation is off (Settings → Dictation)"}
	}
	v := s.svc.dictation
	if v == nil {
		return ipc.Response{Error: "dictation is not available"}
	}
	var err error
	switch fn {
	case "toggle":
		err = v.Toggle()
	case "start":
		err = v.Start()
	case "stop":
		err = v.Stop()
	case "cancel":
		err = v.Cancel()
	case "type": // voxtype's words (its post-process command): typed as the AI's are
		text := strings.TrimSpace(strings.Join(args, " "))
		if text != "" {
			go func() {
				if err := s.svc.typer.typeText(text); err != nil {
					gtkx.Post(func() { s.dictationFailed(err) })
				}
			}()
		}
	default:
		return ipc.Response{Error: "unknown function voice " + fn}
	}
	if err != nil {
		return ipc.Response{Error: err.Error()}
	}
	return ipc.Response{OK: true}
}

func (s *shellUI) bindVoiceHandlers() {
	// The bar's microphone records when the daemon is there, else opens
	// the page that says what is missing.
	s.onVoiceButton = func(gtk.Button) {
		if !s.sys.Voice.Available {
			s.openSettings()
			s.showSettingsPage("voice")
			return
		}
		if r := s.voiceRequest("toggle", nil); r.Error != "" {
			log.Printf("voice: %s", r.Error)
		}
	}
}

// applyVoiceState follows the daemon's state into the bars' microphones.
func (s *shellUI) applyVoiceState() {
	st := s.sys.Voice
	for _, b := range s.bars {
		b.voiceIcon.SetFromIconName(shell.VoiceIcon(st))
		b.voiceIcon.SetTooltipText(shell.VoiceTooltip(st))
		toggle(&b.voice.Widget, "recording", st.Available && st.Status == "recording")
		toggle(&b.voice.Widget, "transcribing", st.Available && st.Status == "transcribing")
	}
	s.dictationState(st)
	s.showVoice()
	s.handOverVoxtype(st)
	// While it records, Escape drops it and its keys end it.
	if recording := st.Available && st.Status == "recording"; recording != s.svc.inSubmap {
		s.svc.inSubmap = recording
		if recording {
			s.dispatchSubmap(hyprconf.DictationSubmap)
		} else {
			s.dispatchSubmap("reset")
		}
	}
}

// dictationEngine turns speech into text: voxtype's daemon (system.Voice)
// or the AI profile for voice input (system.Dictation).
type dictationEngine interface {
	Run(ctx context.Context, onChange func(system.VoiceState))
	Start() error
	Stop() error
	Toggle() error
	Cancel() error
}

// followVoice runs the engine dictation is set to while it is on, a new
// one when the engine or the AI's profile changes, and none while it is
// off, so nothing is read then.
func (s *shellUI) followVoice() {
	v := &s.svc
	var want, profile string
	if s.cfg.Voice.Enable {
		want = s.cfg.Voice.Engine
		if want == "ai" {
			if s.cfg.Assistant.Enable {
				profile = s.cfg.Assistant.TranscriptionProfile()
			}
			want += ":" + profile
		}
	}
	if want == v.voiceRun {
		return
	}
	if v.voiceStop != nil {
		v.voiceStop()
		v.voiceStop, v.dictation = nil, nil
	}
	v.voiceRun, v.voxtypeSet = want, false
	v.typer = dictationTyper{history: s.clipboard.path,
		hypr:  func() hyprNow { return hyprNow{s.client, s.hyprLua} },
		heard: func(text string) { gtkx.Post(func() { s.dictationHeard(text) }) }}
	s.sys.Voice = system.VoiceState{}
	if want == "" {
		s.applyVoiceState()
		return
	}
	var e dictationEngine = v.voice
	if s.cfg.Voice.Engine == "ai" {
		e = &system.Dictation{Profile: profile, Assistant: "inua-assistant", Type: v.typer.typeText,
			Failed: func(err error) { gtkx.Post(func() { s.dictationFailed(err) }) }}
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.dictation, v.voiceStop = e, cancel
	s.applyVoiceState()
	go e.Run(ctx, func(st system.VoiceState) {
		s.update(func(x *shell.System) {
			if ctx.Err() != nil { // from an engine since stopped
				return
			}
			if !st.Available && x.Voice.Available && time.Now().Before(v.voxtypeQuiet) {
				return // inua restarting voxtype: back in a moment
			}
			x.Voice = st
		})
	})
}

// handOverVoxtype has voxtype, once it is up, hand its words to inua to
// type (`inua call voice type -`) rather than type them itself, which it
// cannot without wtype or the session's Wayland in its service.
func (s *shellUI) handOverVoxtype(st system.VoiceState) {
	v := &s.svc
	if v.voxtypeSet || !st.Available || s.cfg.Voice.Engine != "voxtype" {
		return
	}
	helper, err := inuaHelper()
	if err != nil {
		log.Printf("dictation: no inua beside inua-shell to hand voxtype's words to: %v", err)
		return
	}
	v.voxtypeSet = true
	v.voxtypeQuiet = time.Now().Add(15 * time.Second) // it may restart it
	command := system.VoxtypeHandsOver(helper, s.opts.Instance)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if changed, err := system.ConfigureVoxtype(ctx, command); err != nil {
			log.Printf("dictation: voxtype's hand-over: %v", err)
		} else if changed {
			log.Printf("dictation: voxtype hands its words to %s", command)
		}
	}()
}

// dictationNote says why a dictation came to nothing, as a notification.
func (s *shellUI) dictationNote(err error) {
	if s.notes.srv == nil {
		log.Printf("dictation: %v", err)
		return
	}
	s.postNote(notify.Notification{
		ID: s.notes.srv.NewID(), App: "inua", Icon: "microphone-slash-symbolic",
		Summary: i18n.T("Dictation did not work"), Body: notify.Markup(err.Error()), Urgency: notify.Normal, Time: time.Now(),
	})
}

// voiceSettings is the Dictation page's part of the settings window.
type voiceSettings struct {
	page        gtk.Box
	enable, ptt gtk.Switch
	pttRow      gtk.Box
	showRow     gtk.Box
	showDrop    gtk.DropDown
	engine      gtk.Box // the ENGINE section, while dictation is on
	engineDrop  gtk.DropDown
	// The daemon's row: what it is doing, or what is missing, with the
	// one thing to do about it.
	state, detail               gtk.Label
	install, site, start, setup gtk.Button
	// busy is set while voxtype installs, changes model or gets its other
	// engines: busyTitle says which, progress where it is.
	busy                bool
	busyTitle, progress string
	pick                voxtypePicker
	// The model (voxtype's) or the profile (the AI's), and how the words
	// reach the window.
	modelRow          gtk.Box
	modelTitle, model gtk.Label
	typingRow         gtk.Box
	typing, status    gtk.Label
	aiSettings        gtk.Button
}

// voiceShowLabels name config.VoiceShows in the Show drop-down.
func voiceShowLabels() []string {
	return []string{i18n.T("Under the window you type in"), i18n.T("In the screen's corner"), i18n.T("In the bar only")}
}

// voiceEngineLabels name config.VoiceEngines in the Engine drop-down.
func voiceEngineLabels() []string {
	return []string{i18n.T("voxtype, on this machine"), i18n.T("An AI profile (Voice input in Settings → AI)")}
}

// voxtypeSite is where voxtype is installed from.
const voxtypeSite = "https://voxtype.io"

// getVoiceSettings makes the page's sections in its box.
func (s *shellUI) getVoiceSettings(ui *gtkx.Builder) {
	v := &s.settings.voice
	ui.Get("voice_page", &v.page)
	if s.onVoiceSwitch == nil {
		s.onVoiceSwitch = func(sw gtk.Switch, on bool) bool {
			if s.settings.filling {
				return false
			}
			c := s.cfg
			switch sw.GetName() {
			case "voice_enable":
				c.Voice.Enable = on
				c.AddVoiceShortcut()
			case "voice_ptt":
				c.Voice.PushToTalk = on
			default:
				return false
			}
			s.changeSettings(c)
			return false // let the switch move
		}
		s.onVoiceSetup = func(gtk.Button) { s.showSettingsPage("ai") }
		s.onVoiceInstall = func(gtk.Button) { s.voiceInstall() }
		s.onVoiceSite = func(gtk.Button) {
			if err := desktop.Spawn([]string{"xdg-open", voxtypeSite}, ""); err != nil {
				v.setStatus(err.Error())
			}
		}
		s.onVoiceStart = func(gtk.Button) { s.voiceStartDaemon() }
		s.onVoiceAI = func(gtk.Button) { s.showSettingsPage("ai") }
		s.onVoiceShow = func(uintptr, uintptr, uintptr) {
			i := int(v.showDrop.GetSelected())
			if s.settings.filling || i < 0 || i >= len(config.VoiceShows) || config.VoiceShows[i] == s.cfg.Voice.Show {
				return
			}
			c := s.cfg
			c.Voice.Show = config.VoiceShows[i]
			s.changeSettings(c)
		}
		s.onVoiceEngine = func(uintptr, uintptr, uintptr) {
			i := int(v.engineDrop.GetSelected())
			if s.settings.filling || i < 0 || i >= len(config.VoiceEngines) || config.VoiceEngines[i] == s.cfg.Voice.Engine {
				return
			}
			c := s.cfg
			c.Voice.Engine = config.VoiceEngines[i]
			v.setStatus("")
			s.changeSettings(c)
		}
	}
	sw := func(body *gtk.Box, name, title, sub string) (gtk.Switch, gtk.Box) {
		row, _ := displayRow(title, sub)
		y := gtk.NewSwitch()
		y.SetValign(gtk.AlignCenterValue)
		y.SetName(name)
		y.ConnectStateSet(&s.onVoiceSwitch)
		gtkx.Append(row, &y.Widget)
		gtkx.Append(body, &row.Widget)
		return *y, *row
	}
	button := func(row *gtk.Box, label string, f *func(gtk.Button)) gtk.Button {
		b := gtk.NewButton()
		b.AddCssClass("outlined")
		b.SetValign(gtk.AlignCenterValue)
		b.SetLabel(label)
		b.ConnectClicked(f)
		gtkx.Append(row, &b.Widget)
		return *b
	}

	sec, body := displaySection(i18n.T("DICTATION"), i18n.T("Talk, and the words are typed where you are"))
	v.enable, _ = sw(body, "voice_enable", i18n.T("Dictation"), i18n.T("A microphone in the bar, and a shortcut to talk"))
	v.ptt, v.pttRow = sw(body, "voice_ptt", i18n.T("Push to talk"), i18n.T("The shortcut records while it is held; off, it starts and stops"))
	row, _ := displayRow(i18n.T("Show it"), i18n.T("A card with the waveform, then the words; the bar's microphone shows it too"))
	v.showRow = *row
	v.showDrop = *gtk.NewDropDownFromStrings(voiceShowLabels())
	v.showDrop.SetValign(gtk.AlignCenterValue)
	gtkx.ConnectNotify(&v.showDrop, "selected", &s.onVoiceShow)
	gtkx.Append(row, &v.showDrop.Widget)
	gtkx.Append(body, &row.Widget)
	gtkx.Append(&v.page, &sec.Widget)

	sec, body = displaySection(i18n.T("ENGINE"), i18n.T("What turns your speech into text"))
	v.engine = *sec
	row, _ = displayRow(i18n.T("Engine"), i18n.T("voxtype keeps the recording on this machine; an AI profile sends it to its provider"))
	v.engineDrop = *gtk.NewDropDownFromStrings(voiceEngineLabels())
	v.engineDrop.SetValign(gtk.AlignCenterValue)
	gtkx.ConnectNotify(&v.engineDrop, "selected", &s.onVoiceEngine)
	gtkx.Append(row, &v.engineDrop.Widget)
	gtkx.Append(body, &row.Widget)
	row, state, detail := titledRow()
	v.state, v.detail = *state, *detail
	v.site = button(row, i18n.T("voxtype.io"), &s.onVoiceSite)
	v.install = button(row, i18n.T("Install"), &s.onVoiceInstall)
	v.start = button(row, i18n.T("Start"), &s.onVoiceStart)
	v.aiSettings = button(row, i18n.T("AI Settings"), &s.onVoiceAI)
	gtkx.Append(body, &row.Widget)
	row, title, model := titledRow()
	v.modelRow, v.modelTitle, v.model = *row, *title, *model
	v.setup = button(row, i18n.T("Change"), &s.onVoiceSetup) // Settings → AI
	gtkx.Append(body, &row.Widget)
	s.buildVoxtypePicker(body, button)
	row, typing := displayRow(i18n.T("Typing"), " ")
	v.typingRow, v.typing = *row, *typing
	gtkx.Append(body, &row.Widget)
	v.status = *gtk.NewLabel("")
	v.status.AddCssClass("row-error")
	v.status.AddCssClass("flush")
	v.status.SetXalign(0)
	v.status.SetWrap(true)
	v.status.SetSelectable(true)
	v.status.SetVisible(false)
	gtkx.Append(body, &v.status.Widget)
	gtkx.Append(&v.page, &sec.Widget)
}

// titledRow is a displayRow whose title changes too: both its labels.
func titledRow() (row *gtk.Box, title, sub *gtk.Label) {
	row = gtk.NewBox(gtk.OrientationHorizontalValue, 16)
	row.AddCssClass("row")
	text := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	text.SetHexpand(true)
	text.SetValign(gtk.AlignCenterValue)
	title, sub = gtk.NewLabel(""), gtk.NewLabel("")
	title.AddCssClass("title")
	sub.AddCssClass("sub")
	for _, l := range []*gtk.Label{title, sub} {
		l.SetXalign(0)
		l.SetWrap(true)
		gtkx.Append(text, &l.Widget)
	}
	gtkx.Append(row, &text.Widget)
	return row, title, sub
}

// fillVoiceSettings sets the page's controls from the config; showVoice
// follows the daemon.
func (s *shellUI) fillVoiceSettings() {
	v := &s.settings.voice
	if v.page.GoPointer() == 0 {
		return
	}
	v.enable.SetActive(s.cfg.Voice.Enable)
	v.ptt.SetActive(s.cfg.Voice.PushToTalk)
	s.showVoice()
}

// setVoiceButton shows the bar's microphone, or hides it while dictation
// is off.
func (s *shellUI) setVoiceButton() {
	for _, b := range s.bars {
		b.voice.SetVisible(s.cfg.Voice.Enable)
	}
}

// showVoice fills the page from the config and the engine: push to talk
// and the engine hidden while dictation is off; else what the engine is
// doing, or what is missing and the one thing to do about it.
func (s *shellUI) showVoice() {
	st := &s.settings.voice
	if s.settings.win == nil || st.page.GoPointer() == 0 {
		return
	}
	v, on, ai := s.sys.Voice, s.cfg.Voice.Enable, s.cfg.Voice.Engine == "ai"
	st.pttRow.SetVisible(on)
	st.showRow.SetVisible(on)
	st.showDrop.SetSelected(uint32(max(0, slices.Index(config.VoiceShows, s.cfg.Voice.Show))))
	st.engine.SetVisible(on)
	if !on {
		return
	}
	st.engineDrop.SetSelected(uint32(max(0, slices.Index(config.VoiceEngines, s.cfg.Voice.Engine))))
	var title, detail string
	switch {
	case st.busy:
		title, detail = st.busyTitle, st.progress
	case v.Status == system.VoiceNoProfile:
		title = i18n.T("No profile turns speech into text")
		detail = i18n.T("Turn on Voice for a profile in Settings → AI, and choose it for Voice input")
	case ai && v.Status == system.VoiceMissing:
		title, detail = i18n.T("pw-cat is not installed"), i18n.T("Dictation records with PipeWire's pw-cat (pipewire-bin)")
	case v.Status == system.VoiceMissing:
		title = i18n.T("voxtype is not installed")
		detail = i18n.Tf("Install downloads voxtype %s from its GitHub releases into ~/.local/bin, checks it, and starts it with a speech model for your language; others are a choice away", system.VoxtypeVersion)
	case v.Status == system.VoiceStopped:
		title, detail = i18n.T("The daemon is not running"), i18n.T("voxtype is installed; its user service is stopped")
	case !v.Available:
		title, detail = i18n.T("Looking…"), " "
	default:
		title = voiceStateText(v)
		switch keys := s.voiceKeys(); {
		case keys == "":
			detail = i18n.T("Click the microphone in the bar, and talk")
		case s.cfg.Voice.PushToTalk:
			detail = i18n.Tf("Click the microphone in the bar, or hold %s, and talk", keys)
		default:
			detail = i18n.Tf("Click the microphone in the bar, or press %s, and talk", keys)
		}
	}
	st.state.SetText(title)
	st.detail.SetText(detail)
	voxtype := !ai && !st.busy
	st.install.SetVisible(voxtype && v.Status == system.VoiceMissing)
	st.site.SetVisible(voxtype && v.Status == system.VoiceMissing)
	st.start.SetVisible(voxtype && v.Status == system.VoiceStopped)
	st.aiSettings.SetVisible(ai && v.Status == system.VoiceNoProfile)
	st.engineDrop.SetSensitive(!st.busy)

	st.modelRow.SetVisible(ai && v.Available) // the profile; voxtype's model is the picker's
	st.modelTitle.SetText(i18n.T("Profile"))
	st.model.SetText(i18n.Tf("%s; the recording goes to its provider", v.Model))
	s.showVoxtypePicker(!ai && v.Available)
	st.typingRow.SetVisible(v.Available)
	if _, err := exec.LookPath("wtype"); err == nil {
		st.typing.SetText(i18n.T("wtype types the words into the window"))
	} else {
		st.typing.SetText(i18n.T("The words are pasted through the clipboard; with wtype installed, they are typed"))
	}
	if v.Available {
		st.setStatus("")
	}
}

// setStatus shows what went wrong under the engine's row, or nothing.
func (v *voiceSettings) setStatus(text string) {
	v.status.SetText(text)
	v.status.SetVisible(text != "")
}

// voiceKeys is dictation's shortcut as shown, "" for none.
func (s *shellUI) voiceKeys() string {
	for _, sc := range s.cfg.Shortcuts {
		if sc.Action == "voice" {
			return hyprconf.Label(sc.Keys)
		}
	}
	return ""
}

// voiceStateText is the daemon's state in a word for the settings.
func voiceStateText(v system.VoiceState) string {
	switch v.Status {
	case "recording":
		return i18n.T("Listening")
	case "transcribing":
		return i18n.T("Transcribing")
	}
	return i18n.T("Ready")
}

// voiceInstall installs voxtype (system.InstallVoxtype), saying each step
// on the page; the poll shows it come up.
func (s *shellUI) voiceInstall() {
	st := &s.settings.voice
	if st.busy {
		return
	}
	st.busy, st.busyTitle, st.progress = true, i18n.T("Installing voxtype…"), i18n.T("Downloading…")
	st.setStatus("")
	s.showVoice()
	lang := i18n.Lang()
	go func() {
		err := system.InstallVoxtype(context.Background(), lang, func(step system.VoxtypeStep, done float64) {
			var text string
			switch {
			case step == system.VoxtypeDownloading:
				text = i18n.Tf("Downloading voxtype %s… %s", system.VoxtypeVersion, shell.Percent(done))
			case step == system.VoxtypeModelDownloading && done >= 0:
				text = i18n.Tf("Downloading a speech model… %s", shell.Percent(done))
			case step == system.VoxtypeModelDownloading:
				text = i18n.T("Downloading a speech model…")
			case step == system.VoxtypeService:
				text = i18n.T("Starting its service…")
			}
			gtkx.Post(func() {
				st.progress = text
				s.showVoice()
			})
		})
		gtkx.Post(func() {
			st.busy, st.pick.loaded = false, false
			if err != nil {
				st.setStatus(i18n.Tf("voxtype was not installed: %s", err.Error()))
			}
			s.showVoice()
		})
	}()
}

// voiceStartDaemon starts voxtype's user service; the poll shows it come
// up, and its error shows under the row.
func (s *shellUI) voiceStartDaemon() {
	st := &s.settings.voice
	st.start.SetSensitive(false)
	st.setStatus("")
	go func() {
		err := s.svc.voice.StartDaemon()
		gtkx.Post(func() {
			st.start.SetSensitive(true)
			if err != nil {
				st.setStatus(i18n.Tf("voxtype did not start: %s", err.Error()))
			}
		})
	}()
}

// spaced ends a dictation with a space, so the next one does not run on,
// unless it ends in a space already or in a script written without them
// (Chinese, Japanese).
func spaced(text string) string {
	r, _ := utf8.DecodeLastRuneInString(text)
	if text == "" || unicode.IsSpace(r) || unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana) ||
		r >= 0x3000 && r <= 0x303f || r >= 0xff00 && r <= 0xffef { // CJK and full-width punctuation
		return text
	}
	return text + " "
}

// dictationTyper puts dictation's words into the focused window: typed by
// wtype when there is one; else pasted, as the clipboard history pastes
// (Ctrl+V, Ctrl+Shift+V in a terminal), and the clipboard's text put back,
// neither copy kept in the history. Made on the main thread, typeText runs
// on the engine's goroutine.
type dictationTyper struct {
	history string         // the clipboard history's file
	hypr    func() hyprNow // read on the main thread, when it types
	heard   func(text string)
}

// hyprNow is the shell's Hyprland client (nil without Hyprland) and
// whether it takes 0.55's Lua dispatchers, both known only after start-up.
type hyprNow struct {
	client *hypr.Client
	lua    bool
}

func (t dictationTyper) typeText(text string) error {
	t.heard(text)
	text = spaced(text)
	if _, err := exec.LookPath("wtype"); err == nil {
		return exec.Command("wtype", "--", text).Run()
	}
	h, err := gtkx.Call(time.Second, t.hypr)
	if err != nil {
		return err
	}
	client, lua := h.client, h.lua
	if client == nil {
		return errors.New(i18n.T("dictation types with wtype, which is not installed"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	reply, err := client.Request(ctx, "j/activewindow")
	if err != nil {
		return err
	}
	var w struct{ Address, Class string }
	if json.Unmarshal(reply, &w) != nil || w.Address == "" {
		return errors.New(i18n.T("no window has the focus to type into"))
	}
	old, hadText := exec.Command("wl-paste", "--no-newline", "--type", "text/plain").Output()
	copyText := func(b []byte) error {
		// Not into the clipboard history: its capture skips the next one.
		if t.history != "" {
			clipboard.SkipFirst(t.history)
		}
		cmd := exec.Command("wl-copy", "--type", "text/plain;charset=utf-8")
		cmd.Stdin = bytes.NewReader(b)
		// wl-copy's daemon must not inherit a pipe waited on by Cmd.Run.
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		return cmd.Run()
	}
	if err := copyText([]byte(text)); err != nil {
		return err
	}
	mods := "CTRL"
	if class := strings.ToLower(w.Class); slices.ContainsFunc(terminalClasses, func(t string) bool { return strings.Contains(class, t) }) {
		mods = "CTRL SHIFT"
	}
	if lua {
		err = client.LuaDispatch(ctx, fmt.Sprintf(`hl.dsp.send_shortcut({ mods = %q, key = "V", window = %q })`, mods, "address:"+w.Address))
	} else {
		err = client.Dispatch(ctx, fmt.Sprintf("sendshortcut %s, V, address:%s", mods, w.Address))
	}
	if hadText == nil {
		time.Sleep(400 * time.Millisecond) // the window has read the clipboard
		copyText(old)
	}
	return err
}
