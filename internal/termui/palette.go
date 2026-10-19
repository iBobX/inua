package termui

import (
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/settings"
)

// The command palette (Ctrl+Shift+P) is every action by name with its
// keys, found by typing; the keys sheet (Ctrl+Shift+/) shows them all.

// fixedKeys are the keys the settings do not change.
var fixedKeys = []struct{ keys, label string }{
	{"Ctrl Shift ←↑→↓", i18n.M("Focus the pane in that direction")},
	{"Ctrl Shift Alt ←↑→↓", i18n.M("Resize the focused pane")},
	{"Ctrl Shift 1…9", i18n.M("Workspace 1…9")},
	{"Shift Insert", i18n.M("Paste the primary selection")},
	{"Shift PgUp / PgDn", i18n.M("Scroll a page")},
	{"Shift Home / End", i18n.M("Scroll to the top / the bottom")},
	{"Ctrl Shift PgUp / PgDn", i18n.M("Scroll to the previous / next prompt")},
}

// keycaps writes a chord as the keys show it: "Ctrl Shift T".
func keycaps(chord string) string {
	if chord == "" {
		return ""
	}
	names := map[string]string{
		"ctrl": "Ctrl", "shift": "Shift", "alt": "Alt", "super": "Super",
		"equal": "=", "minus": "-", "bracketleft": "[", "bracketright": "]", "slash": "/",
		"comma": ",", "space": "Space", "backslash": "\\", "period": ".",
	}
	parts := strings.Split(chord, "+")
	for i, p := range parts {
		if n, ok := names[p]; ok {
			parts[i] = n
		} else {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

// sheetFrame is an empty sheet over column 3, Escape closing it; close
// takes it away and gives the focused pane the keys again.
func (a *App) sheetFrame(title string, width int32) (sheet *gtk.Box, close func()) {
	sheet = gtk.NewBox(gtk.OrientationVerticalValue, 10)
	sheet.AddCssClass("term-sheet")
	sheet.AddCssClass("term-overview")
	sheet.SetHalign(gtk.AlignCenterValue)
	sheet.SetValign(gtk.AlignCenterValue)
	sheet.SetSizeRequest(width, -1)
	t := gtk.NewLabel(title)
	t.AddCssClass("term-sheet-title")
	t.SetXalign(0)
	sheet.Append(&t.Widget)
	close = func() {
		if a.sheet != sheet {
			return
		}
		a.stage.RemoveOverlay(&sheet.Widget)
		a.sheet = nil
		if s := a.shown(); s != nil {
			if t := a.tiles[s.Focus]; t != nil {
				t.pane.widget.GrabFocus()
			}
		}
	}
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue)
	a.onSheetEsc = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if int32(keyval) == gdk.KEY_Escape {
			close()
			return true
		}
		return false
	}
	keys.ConnectKeyPressed(&a.onSheetEsc)
	sheet.AddController(&keys.EventController)
	a.sheet = sheet
	a.stage.AddOverlay(&sheet.Widget)
	return sheet, close
}

// palette finds an action by name and does it.
func (a *App) palette(p *termPane) {
	if a.sheet != nil {
		return
	}
	sheet, close := a.sheetFrame(i18n.T("Actions"), 520)
	entry := gtk.NewSearchEntry()
	entry.SetPlaceholderText(i18n.T("Find an action"))
	sheet.Append(&entry.Widget)
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	scroll.SetMaxContentHeight(420)
	scroll.SetPropagateNaturalHeight(true)
	list := gtk.NewListBox()
	scroll.SetChild(&list.Widget)
	sheet.Append(&scroll.Widget)
	var shown []string
	fill := func() {
		q := strings.ToLower(strings.TrimSpace(entry.GetText()))
		list.RemoveAll()
		shown = shown[:0]
		for _, act := range settings.Actions {
			if act.Name == "palette" {
				continue
			}
			label := i18n.T(act.Label)
			if q != "" && !strings.Contains(strings.ToLower(label), q) && !strings.Contains(act.Name, q) {
				continue
			}
			list.Append(&actionRow(label, keycaps(a.set.Keys[act.Name])).Widget)
			shown = append(shown, act.Name)
		}
		if r := list.GetRowAtIndex(0); r != nil {
			list.SelectRow(r)
		}
	}
	run := func(i int) {
		if i < 0 || i >= len(shown) {
			return
		}
		action := shown[i]
		close()
		a.do(action, p)
	}
	a.onPaletteFind = func(uintptr, uintptr) { fill() }
	gtkx.ConnectChanged(entry, &a.onPaletteFind)
	a.onPaletteRun = func(gtk.SearchEntry) {
		if r := list.GetSelectedRow(); r != nil {
			run(int(r.GetIndex()))
		}
	}
	entry.ConnectActivate(&a.onPaletteRun)
	a.onPaletteRow = func(_ gtk.ListBox, row uintptr) {
		run(int(gtk.ListBoxRowNewFromInternalPtr(row).GetIndex()))
	}
	list.ConnectRowActivated(&a.onPaletteRow)
	// Up and Down walk the list while the entry has the keys.
	keys := gtk.NewEventControllerKey()
	a.onPaletteKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		r := list.GetSelectedRow()
		i := int32(-1)
		if r != nil {
			i = r.GetIndex()
		}
		switch int32(keyval) {
		case gdk.KEY_Down:
			i++
		case gdk.KEY_Up:
			i--
		default:
			return false
		}
		if n := list.GetRowAtIndex(i); n != nil {
			list.SelectRow(n)
		}
		return true
	}
	keys.ConnectKeyPressed(&a.onPaletteKey)
	entry.AddController(&keys.EventController)
	fill()
	entry.GrabFocus()
}

// actionRow is an action's label and its keys.
func actionRow(label, keys string) *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	row.AddCssClass("term-overview-row")
	l := gtk.NewLabel(label)
	l.SetXalign(0)
	l.SetHexpand(true)
	l.SetEllipsize(3)
	row.Append(&l.Widget)
	if keys != "" {
		k := gtk.NewLabel(keys)
		k.AddCssClass("keycap")
		row.Append(&k.Widget)
	}
	return row
}

// showKeys lists every key of inua-term's.
func (a *App) showKeys() {
	if a.sheet != nil {
		return
	}
	sheet, _ := a.sheetFrame(i18n.T("Keys"), 560)
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	scroll.SetMaxContentHeight(520)
	scroll.SetPropagateNaturalHeight(true)
	box := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	for _, act := range settings.Actions {
		if k := a.set.Keys[act.Name]; k != "" {
			box.Append(&actionRow(i18n.T(act.Label), keycaps(k)).Widget)
		}
	}
	for _, f := range fixedKeys {
		box.Append(&actionRow(i18n.T(f.label), f.keys).Widget)
	}
	note := gtk.NewLabel(i18n.T("Change them in the settings (Ctrl+Shift+,)."))
	note.AddCssClass("term-sheet-why")
	note.SetXalign(0)
	box.Append(&note.Widget)
	scroll.SetChild(&box.Widget)
	sheet.Append(&scroll.Widget)
	sheet.SetFocusable(true)
	sheet.GrabFocus()
}
