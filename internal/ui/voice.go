package ui

import (
	"context"
	"log"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/config"
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
	for _, b := range s.bars {
		b.voiceIcon.SetFromIconName(shell.VoiceIcon(s.sys.Voice))
		b.voiceIcon.SetTooltipText(shell.VoiceTooltip(s.sys.Voice))
	}
}

// voiceSettings is the Voice page's part of the settings window.
type voiceSettings struct {
	page                 gtk.Box
	enable, ptt          gtk.Switch
	state, model, status gtk.Label
	stateRow, offRow     gtk.Box
	download             map[string]*gtk.Button
	setup                gtk.Button
}

// getVoiceSettings makes the page's sections in its box.
func (s *shellUI) getVoiceSettings(ui *gtkx.Builder) {
	v := &s.settings.voice
	ui.Get("voice_page", &v.page)
	v.download = map[string]*gtk.Button{}
	if s.onVoiceSwitch == nil {
		s.onVoiceSwitch = func(sw gtk.Switch, on bool) bool {
			if s.settings.filling {
				return false
			}
			c := s.cfg
			switch sw.GetName() {
			case "voice_enable":
				c.Voice.Enable = on
				if on {
					s.ensureVoiceShortcut(&c)
				}
			case "voice_ptt":
				c.Voice.PushToTalk = on
			default:
				return false
			}
			s.changeSettings(c)
			return false // let the switch move
		}
		s.onVoiceModel = func(b gtk.Button) {
			for _, m := range voiceModels {
				if m.ID == b.GetName() {
					s.voiceDownload(m)
					return
				}
			}
			if b.GetName() == "voice_setup" {
				s.voiceSetupModel()
			}
		}
	}
	sw := func(body *gtk.Box, name, title, sub string) gtk.Switch {
		row, text := displayRow(title, sub)
		y := gtk.NewSwitch()
		y.SetValign(gtk.AlignCenterValue)
		y.SetName(name)
		y.ConnectStateSet(&s.onVoiceSwitch)
		gtkx.Append(row, &y.Widget)
		gtkx.Append(body, &row.Widget)
		_ = text
		return *y
	}

	sec, body := displaySection(i18n.T("VOICE"), i18n.T("Talk, and the words are typed where you are"))
	v.enable = sw(body, "voice_enable", i18n.T("Voice input"), i18n.T("A microphone in the bar; a shortcut holds to talk"))
	v.ptt = sw(body, "voice_ptt", i18n.T("Push to talk"), i18n.T("The shortcut records while it is held"))
	gtkx.Append(&v.page, &sec.Widget)

	sec, body = displaySection(i18n.T("ENGINE"), i18n.T("The voxtype daemon does the listening"))
	row, state := displayRow(i18n.T("State"), " ")
	v.state = *state
	v.stateRow = *row
	gtkx.Append(body, &row.Widget)
	row, model := displayRow(i18n.T("Model"), " ")
	v.model = *model
	gtkx.Append(body, &row.Widget)
	for _, m := range voiceModels {
		row, _ := displayRow(m.Label, m.Size)
		b := gtk.NewButton()
		b.SetName(m.ID)
		b.AddCssClass("outlined")
		dl := i18n.T("Download")
		if s.svc.voice != nil && s.svc.voice.ModelExists(m.ID) {
			dl = i18n.T("Downloaded")
			b.SetSensitive(false)
		}
		b.SetLabel(dl)
		b.ConnectClicked(&s.onVoiceModel)
		gtkx.Append(row, &b.Widget)
		v.download[m.ID] = b
		gtkx.Append(body, &row.Widget)
	}
	row, _ = displayRow(i18n.T("Other models"), i18n.T("voxtype's own setup lists more of them"))
	v.setup.SetName("voice_setup")
	v.setup.AddCssClass("outlined")
	v.setup.SetLabel(i18n.T("Open voxtype"))
	v.setup.ConnectClicked(&s.onVoiceModel)
	gtkx.Append(row, &v.setup.Widget)
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
	s.showVoice()
}

// setVoiceButton shows the bar's microphone, or hides it while the voice
// input is off.
func (s *shellUI) setVoiceButton() {
	for _, b := range s.bars {
		b.voice.SetVisible(s.cfg.Voice.Enable)
	}
}

// showVoice fills the settings page: the daemon's state, the model, and
// what the shortcut is.
func (s *shellUI) showVoice() {
	st := &s.settings.voice
	v := s.sys.Voice
	st.state.SetText(voiceStateText(v))
	st.model.SetText(cmpOr(v.Model, i18n.T("Unknown")))
	st.stateRow.SetVisible(v.Available)
	st.offRow.SetVisible(!v.Available)
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

// voiceDownloading is the model being downloaded, if any; the button that
// starts one says so while it runs.
var voiceDownloading string

// voiceDownload starts a model download in the background and reports
// what happened as the page's status line.
func (s *shellUI) voiceDownload(m system.VoiceModel) {
	if voiceDownloading != "" {
		return
	}
	voiceDownloading = m.ID
	st := &s.settings.voice
	st.status.SetText(i18n.Tf("Downloading %s…", m.Label))
	st.download[m.ID].SetSensitive(false)
	go func() {
		err := s.svc.voice.DownloadModel(context.Background(), m.Engine, m.ID, nil)
		if restart := s.svc.voice.RestartDaemon(); restart != nil {
			log.Printf("voice: restart the daemon: %v", restart)
		}
		gtkx.Post(func() {
			voiceDownloading = ""
			st.download[m.ID].SetSensitive(true)
			switch {
			case err != nil:
				st.status.SetText(err.Error())
			default:
				st.status.SetText(i18n.Tf("%s is ready.", m.Label))
			}
			s.showVoice()
		})
	}()
}

// voiceSetupModel opens voxtype's interactive model setup in a terminal,
// the only path that downloads the ONNX engines' models.
func (s *shellUI) voiceSetupModel() {
	term, dashE := desktop.Terminal(s.cfg.TerminalCommand())
	if err := system.SetupModelCommand(term, dashE).Start(); err != nil {
		s.setStatus(i18n.Tf("Could not start voxtype's setup: %s", err.Error()), true)
	}
}

// voiceModels are the settings page's list, in order.
var voiceModels = system.VoiceModels

// cmpOr is strings.TrimSpace(a) or b when that is empty.
func cmpOr(a, b string) string {
	if a == "" {
		return b
	}
	return a
}

// ensureVoiceShortcut gives the voice action a default shortcut the first
// time the input is turned on, so the button and the key both work.
func (s *shellUI) ensureVoiceShortcut(c *config.Config) {
	for _, sc := range c.Shortcuts {
		if sc.Action == "voice" {
			return
		}
	}
	c.Shortcuts = append(c.Shortcuts, config.Shortcut{Action: "voice", Keys: "SUPER+SHIFT+V"})
}
