package ui

import (
	"bytes"
	"cmp"
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

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
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
func (s *shellUI) voiceRequest(fn string) ipc.Response {
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
		if r := s.voiceRequest("toggle"); r.Error != "" {
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
	v.voiceRun = want
	s.sys.Voice = system.VoiceState{}
	if want == "" {
		s.applyVoiceState()
		return
	}
	var e dictationEngine = v.voice
	if s.cfg.Voice.Engine == "ai" {
		e = &system.Dictation{Profile: profile, Assistant: "inua-assistant", Type: s.typeDictation,
			Failed: func(err error) { gtkx.Post(func() { s.dictationFailed(err) }) }}
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.dictation, v.voiceStop = e, cancel
	s.applyVoiceState()
	go e.Run(ctx, func(st system.VoiceState) {
		s.update(func(x *shell.System) {
			if ctx.Err() == nil { // not from an engine since stopped
				x.Voice = st
			}
		})
	})
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
	// installing is set while InstallVoxtype runs; progress says where.
	installing bool
	progress   string
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
		s.onVoiceSetup = func(gtk.Button) {
			if s.cfg.Voice.Engine == "ai" {
				s.showSettingsPage("ai")
				return
			}
			s.voiceSetupModel()
		}
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
	v.setup = button(row, i18n.T("Change"), &s.onVoiceSetup) // voxtype's setup, or Settings → AI
	gtkx.Append(body, &row.Widget)
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
	case st.installing:
		title, detail = i18n.T("Installing voxtype…"), st.progress
	case v.Status == system.VoiceNoProfile:
		title = i18n.T("No profile turns speech into text")
		detail = i18n.T("Turn on Voice for a profile in Settings → AI, and choose it for Voice input")
	case ai && v.Status == system.VoiceMissing:
		title, detail = i18n.T("pw-cat is not installed"), i18n.T("Dictation records with PipeWire's pw-cat (pipewire-bin)")
	case v.Status == system.VoiceMissing:
		title = i18n.T("voxtype is not installed")
		detail = i18n.Tf("Install downloads voxtype %s from its GitHub releases into ~/.local/bin, checks it, and starts it with an English speech model (Change picks another)", system.VoxtypeVersion)
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
	voxtype := !ai && !st.installing
	st.install.SetVisible(voxtype && v.Status == system.VoiceMissing)
	st.site.SetVisible(voxtype && v.Status == system.VoiceMissing)
	st.start.SetVisible(voxtype && v.Status == system.VoiceStopped)
	st.aiSettings.SetVisible(ai && v.Status == system.VoiceNoProfile)
	st.engineDrop.SetSensitive(!st.installing)

	st.modelRow.SetVisible(v.Available)
	st.modelTitle.SetText(i18n.T("Model"))
	st.model.SetText(cmp.Or(v.Model, i18n.T("Unknown")))
	if ai {
		st.modelTitle.SetText(i18n.T("Profile"))
		st.model.SetText(i18n.Tf("%s; the recording goes to its provider", v.Model))
	}
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
	if st.installing {
		return
	}
	st.installing, st.progress = true, i18n.T("Downloading…")
	st.setStatus("")
	s.showVoice()
	go func() {
		err := system.InstallVoxtype(context.Background(), func(step system.VoxtypeStep, done float64) {
			var text string
			switch step {
			case system.VoxtypeDownloading:
				text = i18n.Tf("Downloading voxtype %s… %s", system.VoxtypeVersion, shell.Percent(done))
			case system.VoxtypeModel:
				text = i18n.T("Downloading its speech model…")
			case system.VoxtypeService:
				text = i18n.T("Starting its service…")
			}
			gtkx.Post(func() {
				st.progress = text
				s.showVoice()
			})
		})
		gtkx.Post(func() {
			st.installing = false
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

// voiceSetupModel opens voxtype's interactive model setup in a terminal.
func (s *shellUI) voiceSetupModel() {
	st := &s.settings.voice
	term, dashE := desktop.Terminal(s.cfg.TerminalCommand())
	if err := system.SetupModelCommand(term, dashE).Start(); err != nil {
		st.setStatus(i18n.Tf("Could not start voxtype's setup: %s", err.Error()))
	}
}

// typeDictation puts dictation's words into the focused window: typed by
// wtype when there is one; else pasted, as the clipboard history pastes
// (Ctrl+V, Ctrl+Shift+V in a terminal), and the clipboard's text put back.
// It runs on the engine's goroutine.
func (s *shellUI) typeDictation(text string) error {
	gtkx.Post(func() { s.dictationHeard(text) })
	if _, err := exec.LookPath("wtype"); err == nil {
		return exec.Command("wtype", "--", text).Run()
	}
	client, lua := s.client, s.hyprLua
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
	copyText := func(t []byte) error {
		cmd := exec.Command("wl-copy", "--type", "text/plain;charset=utf-8")
		cmd.Stdin = bytes.NewReader(t)
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
