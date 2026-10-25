package ui

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

// voxtypeModels is the Dictation page's list of voxtype's speech models:
// an engine to show (Whisper, SenseVoice …) and a row a model, its size,
// its languages and whether it is downloaded or in use, with Download,
// Use and Delete; a download's progress shows in its row. On a build with
// Whisper alone, a row offers the one with every engine.
type voxtypeModels struct {
	head, list, moreRow gtk.Box
	hint                gtk.Label
	filter              gtk.DropDown
	more                gtk.Button
	catalog             system.VoxtypeCatalog
	loaded, loading     bool
	engines             []string // the filter's, as voxtype names them
	busy, progress      string   // the model being got (an Arg), how far
	upgrading           bool
}

// voxtypeEngineNames are voxtype's engines as their makers write them.
var voxtypeEngineNames = map[string]string{
	"whisper": "Whisper", "parakeet": "Parakeet", "moonshine": "Moonshine", "sensevoice": "SenseVoice",
	"paraformer": "Paraformer", "dolphin": "Dolphin", "omnilingual": "Omnilingual", "cohere": "Cohere", "openvino": "OpenVINO",
}

// voxtypeEngineHints say who made each engine and what it is good at.
var voxtypeEngineHints = map[string]string{
	"whisper":     i18n.M("OpenAI's open Whisper: many languages; the .en models English alone"),
	"parakeet":    i18n.M("NVIDIA's Parakeet: English, or 25 European languages (v3)"),
	"moonshine":   i18n.M("Small and quick, a model a language"),
	"sensevoice":  i18n.M("Alibaba's SenseVoice: Chinese, Cantonese, English, Japanese and Korean"),
	"paraformer":  i18n.M("Alibaba's Paraformer: Chinese or English"),
	"dolphin":     i18n.M("Dolphin: English and Chinese"),
	"omnilingual": i18n.M("Meta's Omnilingual: more than 1,600 languages"),
	"cohere":      i18n.M("Cohere's Transcribe: 14 languages"),
	"openvino":    i18n.M("Whisper on Intel's processors and NPUs"),
}

func (s *shellUI) buildVoxtypeModels(body *gtk.Box) {
	m := &s.settings.voice.models
	if s.onVoxtypeFilter == nil {
		s.onVoxtypeFilter = func(uintptr, uintptr, uintptr) {
			if !s.settings.voice.filling {
				s.fillVoxtypeModels()
			}
		}
		s.onVoxtypeRow = func(b gtk.Button) { s.voxtypeModelAction(b.GetName()) }
		s.onVoxtypeMore = func(gtk.Button) { s.upgradeVoxtype() }
	}
	row, hint := displayRow(i18n.T("Speech models"), " ")
	m.head, m.hint = *row, *hint
	m.filter = *gtk.NewDropDownFromStrings(nil)
	m.filter.SetValign(gtk.AlignCenterValue)
	gtkx.ConnectNotify(&m.filter, "selected", &s.onVoxtypeFilter)
	gtkx.Append(row, &m.filter.Widget)
	gtkx.Append(body, &row.Widget)
	m.list = *gtk.NewBox(gtk.OrientationVerticalValue, 0)
	m.list.AddCssClass("rows")
	m.list.AddCssClass("voxtype-models")
	gtkx.Append(body, &m.list.Widget)
	row, _ = displayRow(i18n.T("More engines"), i18n.T("SenseVoice, Paraformer, Parakeet, Moonshine and others come with voxtype's fuller build (46 MB); your models stay"))
	m.moreRow = *row
	m.more = voiceButton(row, i18n.T("Get them"), &s.onVoxtypeMore)
	gtkx.Append(body, &row.Widget)
}

// showVoxtypeModels shows the list while voxtype is the engine and up,
// asking voxtype for its catalog the first time.
func (s *shellUI) showVoxtypeModels(on bool) {
	v := &s.settings.voice
	m := &v.models
	m.head.SetVisible(on && m.loaded)
	m.list.SetVisible(on && m.loaded)
	m.moreRow.SetVisible(on && m.loaded && m.catalog.More)
	m.more.SetSensitive(m.busy == "" && !m.upgrading)
	if m.upgrading {
		m.more.SetLabel(m.progress)
	} else {
		m.more.SetLabel(i18n.T("Get them"))
	}
	if !on || m.loaded || m.loading {
		return
	}
	m.loading = true
	go func() {
		c, err := system.VoxtypeModels(context.Background())
		gtkx.Post(func() {
			m.loading = false
			if err != nil {
				v.setStatus(err.Error())
				return
			}
			m.catalog, m.loaded = c, true
			m.engines = c.Engines
			names := make([]string, len(m.engines))
			for i, e := range m.engines {
				names[i] = cmp.Or(voxtypeEngineNames[e], e)
			}
			v.filling = true
			list := gtk.NewStringList(names)
			m.filter.SetModel(list)
			list.Unref()
			m.filter.SetSelected(uint32(max(0, slices.Index(m.engines, c.Engine))))
			v.filling = false
			s.fillVoxtypeModels()
			s.showVoice()
		})
	}()
}

// fillVoxtypeModels lists the chosen engine's models, a row each.
func (s *shellUI) fillVoxtypeModels() {
	m := &s.settings.voice.models
	i := int(m.filter.GetSelected())
	if i < 0 || i >= len(m.engines) {
		return
	}
	engine := m.engines[i]
	m.hint.SetText(i18n.T(voxtypeEngineHints[engine]))
	for c := m.list.GetFirstChild(); c != nil; c = m.list.GetFirstChild() {
		m.list.Remove(c)
	}
	for _, model := range m.catalog.Models[engine] {
		inUse := engine == m.catalog.Engine && model.Arg == m.catalog.Model
		facts := []string{}
		if model.MB > 0 {
			facts = append(facts, sizeText(model.MB))
		}
		if l := langsText(model.Langs); l != "" {
			facts = append(facts, l)
		}
		switch {
		case m.busy == model.Arg:
			facts = append(facts, m.progress)
		case inUse:
			facts = append(facts, i18n.T("In use"))
		case model.Installed:
			facts = append(facts, i18n.T("Downloaded"))
		}
		row, _ := displayRow(model.Name, strings.Join(facts, " · "))
		if inUse {
			row.AddCssClass("in-use")
		}
		if !inUse && model.Installed {
			del := gtk.NewButtonFromIconName("user-trash-symbolic")
			del.AddCssClass("plain")
			del.SetValign(gtk.AlignCenterValue)
			del.SetTooltipText(i18n.T("Delete its files; it can be downloaded again"))
			del.SetName("delete:" + model.Arg)
			del.SetSensitive(m.busy == "" && !m.upgrading)
			del.ConnectClicked(&s.onVoxtypeRow)
			gtkx.Append(row, &del.Widget)
		}
		if !inUse {
			label := i18n.T("Use")
			if !model.Installed {
				label = i18n.T("Download and use")
			}
			b := voiceButton(row, label, &s.onVoxtypeRow)
			b.SetName("use:" + model.Arg)
			b.SetSensitive(m.busy == "" && !m.upgrading)
		}
		gtkx.Append(&m.list, &row.Widget)
	}
}

// sizeText is a download's size: "239 MB", "2.6 GB".
func sizeText(mb int) string {
	if mb >= 1000 {
		return fmt.Sprintf("%.1f GB", float64(mb)/1000)
	}
	return fmt.Sprintf("%d MB", mb)
}

// langsText says a model's languages: their codes, a count when there are
// many, "many languages" for Whisper's.
func langsText(langs string) string {
	switch codes := strings.Split(langs, "/"); {
	case langs == "":
		return ""
	case langs == "*":
		return i18n.T("many languages")
	case strings.HasSuffix(langs, "+ langs"):
		return i18n.Tf("%s languages", strings.TrimSuffix(langs, " langs"))
	case len(codes) > 5:
		return i18n.Nf("%d language", "%d languages", len(codes), len(codes))
	default:
		return strings.ToUpper(strings.Join(codes, " "))
	}
}

// voxtypeModelAction does what a model's button says ("use:ARG",
// "delete:ARG").
func (s *shellUI) voxtypeModelAction(name string) {
	v := &s.settings.voice
	m := &v.models
	what, arg, _ := strings.Cut(name, ":")
	i := int(m.filter.GetSelected())
	if m.busy != "" || i < 0 || i >= len(m.engines) {
		return
	}
	j := slices.IndexFunc(m.catalog.Models[m.engines[i]], func(x system.VoxtypeModel) bool { return x.Arg == arg })
	if j < 0 {
		return
	}
	model := m.catalog.Models[m.engines[i]][j]
	v.setStatus("")
	if what == "delete" {
		if err := system.DeleteVoxtypeModel(model); err != nil {
			v.setStatus(err.Error())
		}
		m.loaded = false
		s.showVoice()
		return
	}
	m.busy, m.progress = model.Arg, i18n.T("Starting…")
	s.svc.voxtypeQuiet = time.Now().Add(20 * time.Second) // it restarts voxtype
	s.fillVoxtypeModels()
	go func() {
		err := system.UseVoxtypeModel(context.Background(), model, func(file string, pct float64) {
			text := i18n.T("Downloading…")
			if pct >= 0 {
				text = i18n.Tf("Downloading… %s", shell.Percent(pct/100))
			}
			gtkx.Post(func() {
				if m.progress != text {
					m.progress = text
					s.fillVoxtypeModels()
				}
			})
		})
		gtkx.Post(func() {
			m.busy, m.loaded = "", false
			if err != nil {
				v.setStatus(i18n.Tf("%s is not in use: %s", model.Name, err.Error()))
			}
			s.showVoice()
		})
	}()
}

// upgradeVoxtype puts voxtype's build with every engine in place of the
// Whisper-only one, saying how far on its button.
func (s *shellUI) upgradeVoxtype() {
	v := &s.settings.voice
	m := &v.models
	if m.upgrading || m.busy != "" {
		return
	}
	m.upgrading, m.progress = true, i18n.T("Downloading…")
	v.setStatus("")
	s.svc.voxtypeQuiet = time.Now().Add(20 * time.Second)
	s.showVoice()
	go func() {
		err := system.UpgradeVoxtype(context.Background(), func(done float64) {
			gtkx.Post(func() {
				m.progress = shell.Percent(done)
				s.showVoice()
			})
		})
		gtkx.Post(func() {
			m.upgrading, m.loaded = false, false
			if err != nil {
				v.setStatus(i18n.Tf("voxtype's other engines did not come: %s", err.Error()))
			}
			s.showVoice()
		})
	}()
}
