package ui

import (
	"cmp"
	"context"
	"log"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

// Voice input: the bar's microphone, the shortcut behind it, the voice
// page of the settings. Recording and transcription are the voxtype
// daemon's; inua drives it and says what it is doing. voiceRequest answers
// `inua call voice …`, which scripts and the settings can drive too.
func (s *shellUI) voiceRequest(fn string) ipc.Response {
	if !s.cfg.Voice.Enable {
		return ipc.Response{Error: "voice input is off"}
	}
	v := s.svc.voice
	if v == nil {
		return ipc.Response{Error: "voice input is not available"}
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
	s.onVoiceButton = func(gtk.Button) {
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

// followVoice follows the voxtype daemon while voice input is on, and
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

// voiceSettings is the Voice page's part of the settings window.
type voiceSettings struct {
	page                 gtk.Box
	enable, ptt          gtk.Switch
	state, model, status gtk.Label
	stateRow, modelRow   gtk.Box
	offRow               gtk.Box
}

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
	}
	sw := func(body *gtk.Box, name, title, sub string) gtk.Switch {
		row, _ := displayRow(title, sub)
		y := gtk.NewSwitch()
		y.SetValign(gtk.AlignCenterValue)
		y.SetName(name)
		y.ConnectStateSet(&s.onVoiceSwitch)
		gtkx.Append(row, &y.Widget)
		gtkx.Append(body, &row.Widget)
		return *y
	}

	sec, body := displaySection(i18n.T("VOICE"), i18n.T("Talk, and the words are typed where you are"))
	v.enable = sw(body, "voice_enable", i18n.T("Voice input"), i18n.T("A microphone in the bar; a shortcut holds to talk"))
	v.ptt = sw(body, "voice_ptt", i18n.T("Push to talk"), i18n.T("The shortcut records while it is held"))
	gtkx.Append(&v.page, &sec.Widget)

	sec, body = displaySection(i18n.T("ENGINE"), i18n.T("The voxtype daemon does the listening"))
	row, state := displayRow(i18n.T("State"), " ")
	v.state, v.stateRow = *state, *row
	gtkx.Append(body, &row.Widget)
	row, model := displayRow(i18n.T("Model"), " ")
	v.model, v.modelRow = *model, *row
	gtkx.Append(body, &row.Widget)
	row, _ = displayRow(i18n.T("Models"), i18n.T("voxtype's setup lists and downloads them"))
	b := gtk.NewButton()
	b.AddCssClass("outlined")
	b.SetValign(gtk.AlignCenterValue)
	b.SetLabel(i18n.T("Open voxtype"))
	b.ConnectClicked(&s.onVoiceSetup)
	gtkx.Append(row, &b.Widget)
	gtkx.Append(body, &row.Widget)
	v.status = *gtk.NewLabel("")
	v.status.AddCssClass("sub")
	v.status.SetXalign(0)
	v.status.SetWrap(true)
	gtkx.Append(body, &v.status.Widget)
	row, _ = displayRow("", "")
	v.offRow = *row
	note := gtk.NewLabel(i18n.T("Voxtype is not installed, or its daemon is not running."))
	note.AddCssClass("sub")
	note.SetXalign(0)
	note.SetWrap(true)
	gtkx.Append(row, &note.Widget)
	gtkx.Append(body, &row.Widget)
	gtkx.Append(&v.page, &sec.Widget)
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
	v.ptt.SetSensitive(s.cfg.Voice.Enable)
	s.showVoice()
}

// setVoiceButton shows the bar's microphone, or hides it while the voice
// input is off.
func (s *shellUI) setVoiceButton() {
	for _, b := range s.bars {
		b.voice.SetVisible(s.cfg.Voice.Enable)
	}
}

// showVoice fills the settings page with the daemon's state and model;
// while voice input is off, nothing is read and none shows.
func (s *shellUI) showVoice() {
	st := &s.settings.voice
	if s.settings.win == nil || st.page.GoPointer() == 0 {
		return
	}
	v, on := s.sys.Voice, s.cfg.Voice.Enable
	st.state.SetText(voiceStateText(v))
	st.model.SetText(cmp.Or(v.Model, i18n.T("Unknown")))
	st.stateRow.SetVisible(on && v.Available)
	st.modelRow.SetVisible(on && v.Available)
	st.offRow.SetVisible(on && !v.Available)
}

// voiceStateText is the daemon's state in a word for the settings.
func voiceStateText(v system.VoiceState) string {
	switch {
	case !v.Available:
		return i18n.T("Not available")
	case v.Status == "recording":
		return i18n.T("Listening")
	case v.Status == "transcribing":
		return i18n.T("Transcribing")
	}
	return i18n.T("Ready")
}

// voiceSetupModel opens voxtype's interactive model setup in a terminal.
func (s *shellUI) voiceSetupModel() {
	st := &s.settings.voice
	term, dashE := desktop.Terminal(s.cfg.TerminalCommand())
	if err := system.SetupModelCommand(term, dashE).Start(); err != nil {
		st.status.SetText(i18n.Tf("Could not start voxtype's setup: %s", err.Error()))
	}
}
