package ui

import (
	"cmp"
	"context"
	"slices"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

// voxtypePicker is the Dictation page's choice of voxtype's speech model:
// one of its engines, one of that engine's models (those downloaded
// marked), and Use, which downloads it if need be; and, on a build with
// Whisper alone, a row to get the one with every engine.
type voxtypePicker struct {
	row, moreRow    gtk.Box
	hint            gtk.Label
	engine, model   gtk.DropDown
	use, more       gtk.Button
	catalog         system.VoxtypeCatalog
	loaded, loading bool
	engines         []string // the engine drop-down's, as voxtype names them
	models          []system.VoxtypeModel
	filling         bool // the drop-downs being set, not chosen
}

// voxtypeEngineNames are voxtype's engines as their makers write them.
var voxtypeEngineNames = map[string]string{
	"whisper": "Whisper", "parakeet": "Parakeet", "moonshine": "Moonshine", "sensevoice": "SenseVoice",
	"paraformer": "Paraformer", "dolphin": "Dolphin", "omnilingual": "Omnilingual", "cohere": "Cohere", "openvino": "OpenVINO",
}

// voxtypeEngineHints say what each of voxtype's engines is for.
var voxtypeEngineHints = map[string]string{
	"whisper":     i18n.M("OpenAI's open Whisper, run here: many languages; the .en models English alone"),
	"parakeet":    i18n.M("NVIDIA's Parakeet: English (v2), or 25 European languages (v3)"),
	"moonshine":   i18n.M("Small and quick: English, and base-zh, base-ja, tiny-ko, tiny-ar for those languages"),
	"sensevoice":  i18n.M("Alibaba's SenseVoice: Chinese, Cantonese, English, Japanese and Korean"),
	"paraformer":  i18n.M("Alibaba's Paraformer: Chinese (zh) or English (en)"),
	"dolphin":     i18n.M("Dolphin: Eastern languages"),
	"omnilingual": i18n.M("Meta's Omnilingual: more than 1,600 languages"),
	"cohere":      i18n.M("Cohere's Transcribe"),
	"openvino":    i18n.M("Whisper on Intel's processors and NPUs (OpenVINO)"),
}

// buildVoxtypePicker adds the picker's rows to the engine section.
func (s *shellUI) buildVoxtypePicker(body *gtk.Box, button func(*gtk.Box, string, *func(gtk.Button)) gtk.Button) {
	p := &s.settings.voice.pick
	if s.onVoxtypeEngine == nil {
		s.onVoxtypeEngine = func(uintptr, uintptr, uintptr) {
			if !p.filling {
				s.fillVoxtypeModels("")
			}
		}
		s.onVoxtypeModel = func(uintptr, uintptr, uintptr) {
			if !p.filling {
				s.showVoxtypeUse()
			}
		}
		s.onVoxtypeUse = func(gtk.Button) { s.useVoxtypeModel() }
		s.onVoxtypeMore = func(gtk.Button) { s.upgradeVoxtype() }
	}
	row, hint := displayRow(i18n.T("Speech model"), " ")
	p.row, p.hint = *row, *hint
	p.engine = *gtk.NewDropDownFromStrings(nil)
	p.model = *gtk.NewDropDownFromStrings(nil)
	for _, d := range []*gtk.DropDown{&p.engine, &p.model} {
		d.SetValign(gtk.AlignCenterValue)
		gtkx.Append(row, &d.Widget)
	}
	gtkx.ConnectNotify(&p.engine, "selected", &s.onVoxtypeEngine)
	gtkx.ConnectNotify(&p.model, "selected", &s.onVoxtypeModel)
	p.use = button(row, i18n.T("Use"), &s.onVoxtypeUse)
	gtkx.Append(body, &row.Widget)
	row, _ = displayRow(i18n.T("More engines"), i18n.T("SenseVoice, Paraformer, Parakeet, Moonshine and others come with voxtype's build for them (about 46 MB); your models stay"))
	p.moreRow = *row
	p.more = button(row, i18n.T("Get them"), &s.onVoxtypeMore)
	gtkx.Append(body, &row.Widget)
}

// showVoxtypePicker shows the picker while voxtype is the engine and up,
// asking voxtype for its catalog the first time.
func (s *shellUI) showVoxtypePicker(on bool) {
	st := &s.settings.voice
	p := &st.pick
	p.row.SetVisible(on && p.loaded)
	p.moreRow.SetVisible(on && p.loaded && p.catalog.More && !st.busy)
	for _, w := range []*gtk.Widget{&p.engine.Widget, &p.model.Widget, &p.use.Widget, &p.more.Widget} {
		w.SetSensitive(!st.busy)
	}
	if !on || p.loaded || p.loading {
		return
	}
	p.loading = true
	go func() {
		c, err := system.VoxtypeModels(context.Background())
		gtkx.Post(func() {
			p.loading = false
			if err != nil {
				st.setStatus(err.Error())
				return
			}
			p.catalog, p.loaded = c, true
			p.engines = c.Engines
			names := make([]string, len(p.engines))
			for i, e := range p.engines {
				names[i] = cmp.Or(voxtypeEngineNames[e], e)
			}
			p.filling = true
			list := gtk.NewStringList(names)
			p.engine.SetModel(list)
			list.Unref()
			p.engine.SetSelected(uint32(max(0, slices.Index(p.engines, c.Engine))))
			p.filling = false
			s.fillVoxtypeModels(c.Model)
			s.showVoice()
		})
	}()
}

// fillVoxtypeModels lists the chosen engine's models, choosing want (an
// Arg), else the one in use, else the first downloaded, else the first.
func (s *shellUI) fillVoxtypeModels(want string) {
	p := &s.settings.voice.pick
	i := int(p.engine.GetSelected())
	if i < 0 || i >= len(p.engines) {
		return
	}
	engine := p.engines[i]
	p.models = p.catalog.Models[engine]
	labels := make([]string, len(p.models))
	for j, m := range p.models {
		switch {
		case engine == p.catalog.Engine && m.Arg == p.catalog.Model:
			labels[j] = i18n.Tf("%s (in use)", m.Name)
		case m.Installed:
			labels[j] = i18n.Tf("%s (downloaded)", m.Name)
		default:
			labels[j] = m.Name
		}
	}
	pick := slices.IndexFunc(p.models, func(m system.VoxtypeModel) bool { return m.Arg == want })
	if pick < 0 && engine == p.catalog.Engine {
		pick = slices.IndexFunc(p.models, func(m system.VoxtypeModel) bool { return m.Arg == p.catalog.Model })
	}
	if pick < 0 {
		pick = max(0, slices.IndexFunc(p.models, func(m system.VoxtypeModel) bool { return m.Installed }))
	}
	p.filling = true
	list := gtk.NewStringList(labels)
	p.model.SetModel(list)
	list.Unref()
	p.model.SetSelected(uint32(pick))
	p.filling = false
	p.hint.SetText(i18n.T(voxtypeEngineHints[engine]))
	s.showVoxtypeUse()
}

// chosen is the model the drop-downs show, if any.
func (p *voxtypePicker) chosen() (system.VoxtypeModel, bool) {
	i := int(p.model.GetSelected())
	if i < 0 || i >= len(p.models) {
		return system.VoxtypeModel{}, false
	}
	return p.models[i], true
}

// showVoxtypeUse labels Use for the model chosen: in use already,
// downloaded (Use), or to download first.
func (s *shellUI) showVoxtypeUse() {
	p := &s.settings.voice.pick
	m, ok := p.chosen()
	inUse := ok && m.Engine == p.catalog.Engine && m.Arg == p.catalog.Model
	switch {
	case inUse:
		p.use.SetLabel(i18n.T("In use"))
	case ok && m.Installed:
		p.use.SetLabel(i18n.T("Use"))
	default:
		p.use.SetLabel(i18n.T("Download and use"))
	}
	p.use.SetSensitive(ok && !inUse && !s.settings.voice.busy)
}

// useVoxtypeModel downloads the chosen model if need be and has voxtype
// use it, saying how far it is; then the catalog is asked for again.
func (s *shellUI) useVoxtypeModel() {
	st := &s.settings.voice
	m, ok := st.pick.chosen()
	if !ok || st.busy {
		return
	}
	st.busy, st.busyTitle, st.progress = true, i18n.Tf("Getting %s…", m.Name), " "
	st.setStatus("")
	s.showVoice()
	go func() {
		err := system.UseVoxtypeModel(context.Background(), m, func(file string, pct float64) {
			text := i18n.Tf("Downloading %s…", file)
			if pct >= 0 {
				text = i18n.Tf("Downloading %s… %s", file, shell.Percent(pct/100))
			}
			gtkx.Post(func() {
				st.progress = text
				s.showVoice()
			})
		})
		gtkx.Post(func() {
			st.busy, st.pick.loaded = false, false
			if err != nil {
				st.setStatus(i18n.Tf("%s is not in use: %s", m.Name, err.Error()))
			}
			s.showVoice()
		})
	}()
}

// upgradeVoxtype puts voxtype's build with every engine in place of the
// Whisper-only one.
func (s *shellUI) upgradeVoxtype() {
	st := &s.settings.voice
	if st.busy {
		return
	}
	st.busy, st.busyTitle, st.progress = true, i18n.T("Getting voxtype's other engines…"), " "
	st.setStatus("")
	s.showVoice()
	go func() {
		err := system.UpgradeVoxtype(context.Background(), func(done float64) {
			gtkx.Post(func() {
				st.progress = i18n.Tf("Downloading voxtype %s… %s", system.VoxtypeVersion, shell.Percent(done))
				s.showVoice()
			})
		})
		gtkx.Post(func() {
			st.busy, st.pick.loaded = false, false
			if err != nil {
				st.setStatus(i18n.Tf("voxtype's other engines did not come: %s", err.Error()))
			}
			s.showVoice()
		})
	}()
}
