package ui

import (
	"cmp"
	"context"
	"log"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/hyprconf"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/ipc"
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
	v := s.svc.voice
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
	s.showVoice()
}

// followVoice follows the voxtype daemon while dictation is on, and
// stops reading its state when it is turned off.
func (s *shellUI) followVoice() {
	v := &s.svc
	on := s.cfg.Voice.Enable
	if on == (v.voiceStop != nil) {
		return
	}
	if !on {
		v.voiceStop()
		v.voiceStop = nil
		s.sys.Voice = system.VoiceState{}
		s.applyVoiceState()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.voiceStop = cancel
	go v.voice.Run(ctx, func(st system.VoiceState) {
		s.update(func(x *shell.System) {
			if ctx.Err() == nil { // not a poll from before it was turned off
				x.Voice = st
			}
		})
	})
}

// voiceSettings is the Dictation page's part of the settings window.
type voiceSettings struct {
	page        gtk.Box
	enable, ptt gtk.Switch
	pttRow      gtk.Box
	engine      gtk.Box // the ENGINE section, while dictation is on
	// The daemon's row: what it is doing, or what is missing, with the
	// one thing to do about it.
	state, detail         gtk.Label
	install, start, setup gtk.Button
	modelRow              gtk.Box
	model, status         gtk.Label
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
		s.onVoiceSetup = func(gtk.Button) { s.voiceSetupModel() }
		s.onVoiceInstall = func(gtk.Button) {
			if err := desktop.Spawn([]string{"xdg-open", voxtypeSite}, ""); err != nil {
				v.setStatus(err.Error())
			}
		}
		s.onVoiceStart = func(gtk.Button) { s.voiceStartDaemon() }
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
	gtkx.Append(&v.page, &sec.Widget)

	sec, body = displaySection(i18n.T("ENGINE"), i18n.T("voxtype listens and turns speech into text on this machine"))
	v.engine = *sec
	row, state, detail := titledRow()
	v.state, v.detail = *state, *detail
	v.install = button(row, i18n.T("voxtype.io"), &s.onVoiceInstall)
	v.start = button(row, i18n.T("Start"), &s.onVoiceStart)
	gtkx.Append(body, &row.Widget)
	row, model := displayRow(i18n.T("Model"), " ")
	v.model, v.modelRow = *model, *row
	v.setup = button(row, i18n.T("Change"), &s.onVoiceSetup) // voxtype's setup, in a terminal
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

// showVoice fills the page from the config and the daemon: push to talk
// and the engine hidden while dictation is off; else the daemon's
// state, or what is missing and the way to it.
func (s *shellUI) showVoice() {
	st := &s.settings.voice
	if s.settings.win == nil || st.page.GoPointer() == 0 {
		return
	}
	v, on := s.sys.Voice, s.cfg.Voice.Enable
	st.pttRow.SetVisible(on)
	st.engine.SetVisible(on)
	if !on {
		return
	}
	var title, detail string
	switch {
	case v.Status == system.VoiceMissing:
		title, detail = i18n.T("voxtype is not installed"), i18n.T("Install it from its site, then turn on its daemon")
	case v.Status == system.VoiceStopped:
		title, detail = i18n.T("The daemon is not running"), i18n.T("voxtype is installed; its user service is stopped")
	case !v.Available:
		title, detail = i18n.T("Looking for voxtype…"), " "
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
	st.install.SetVisible(v.Status == system.VoiceMissing)
	st.start.SetVisible(v.Status == system.VoiceStopped)
	st.modelRow.SetVisible(v.Available)
	st.model.SetText(cmp.Or(v.Model, i18n.T("Unknown")))
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
