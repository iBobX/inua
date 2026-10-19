package termui

import (
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/host"
	"github.com/lyricat/inua/internal/term/layout"
)

// inua-term's own keys are Ctrl+Shift chords (section 7); everything else
// goes to the program. A chord is matched by its key without Shift (base),
// so Ctrl+Shift+1 is "1" whatever Shift makes of it on the layout.

// chord does the window's action for a chord of the settings' keys
// (term.toml [keys]), or the fixed ones: Ctrl+Shift with an arrow moves
// the focus, with Alt too resizes, and an arrow after the swap key swaps.
// false leaves the key to the pane.
func (a *App) chord(p *termPane, base, keyval uint32, state gdk.ModifierType) bool {
	held := state & (gdk.ControlMaskValue | gdk.ShiftMaskValue | gdk.AltMaskValue | gdk.SuperMaskValue)
	if a.swapping {
		a.swapping = false
		if d, ok := arrow(keyval); ok {
			a.layoutOp(host.OpSwap, host.Message{Direction: int(d)})
			return true
		}
	}
	cs := gdk.ControlMaskValue | gdk.ShiftMaskValue
	if d, ok := arrow(keyval); ok {
		switch held {
		case cs:
			a.layoutOp(host.OpFocus, host.Message{Direction: int(d)})
			return true
		case cs | gdk.AltMaskValue:
			a.layoutOp(host.OpResize, host.Message{Direction: int(d), Step: 0.05})
			return true
		}
		return false
	}
	if a.nthChord(base, held) {
		return true
	}
	action, ok := a.keymap[chordName(base, held)]
	if !ok {
		return false
	}
	return a.do(action, p)
}

// chordName is a key as term.toml writes it: "ctrl+shift+t".
func chordName(base uint32, held gdk.ModifierType) string {
	var b strings.Builder
	for _, m := range []struct {
		mask gdk.ModifierType
		name string
	}{{gdk.ControlMaskValue, "ctrl"}, {gdk.ShiftMaskValue, "shift"}, {gdk.AltMaskValue, "alt"}, {gdk.SuperMaskValue, "super"}} {
		if held&m.mask != 0 {
			b.WriteString(m.name + "+")
		}
	}
	b.WriteString(strings.ToLower(gdk.KeyvalName(gdk.KeyvalToLower(base))))
	return b.String()
}

// keymapOf is the settings' keys by chord.
func keymapOf(keys map[string]string) map[string]string {
	m := map[string]string{}
	for action, k := range keys {
		if k != "" {
			m[k] = action
		}
	}
	return m
}

// do is an action of the window's, by its name in term.toml; p is the
// pane the key came from, for the pane's own.
func (a *App) do(action string, p *termPane) bool {
	switch action {
	case "new_session":
		a.newSession()
	case "new_workspace":
		a.newWorkspace()
	case "split_right":
		a.split(layout.Right)
	case "split_down":
		a.split(layout.Down)
	case "close_pane":
		a.closeFocused()
	case "swap":
		a.swapping = true
	case "zoom":
		a.layoutOp(host.OpZoom, host.Message{})
	case "equalize":
		a.layoutOp(host.OpEqualize, host.Message{})
	case "next_layout":
		a.layoutOp(host.OpPreset, host.Message{})
	case "previous_session":
		a.stepSession(-1)
	case "next_session":
		a.stepSession(1)
	case "next_waiting":
		a.nextWaiting()
	case "agents":
		a.overview()
	case "fold_workspaces":
		a.toggleColumn(0)
	case "fold_sessions":
		a.toggleColumn(1)
	case "rename":
		a.renameSession()
	case "font_larger":
		a.zoomFont(1)
	case "font_smaller":
		a.zoomFont(-1)
	case "font_reset":
		a.zoomFont(0)
	case "palette":
		a.palette(p)
	case "keys":
		a.showKeys()
	case "settings":
		a.openSettings()
	case "copy", "paste", "find":
		if p == nil {
			return false
		}
		switch action {
		case "copy":
			p.copySelection(true)
		case "paste":
			p.pasteFrom(false)
		case "find":
			p.openFind()
		}
	default:
		if a.extra != nil && a.extra(action, p) {
			return true
		}
		return false
	}
	return true
}

// nth workspaces: Ctrl+Shift+1…9, fixed.
func (a *App) nthChord(base uint32, held gdk.ModifierType) bool {
	k := rune(gdk.KeyvalToUnicode(base))
	if held != gdk.ControlMaskValue|gdk.ShiftMaskValue || k < '1' || k > '9' {
		return false
	}
	a.nthWorkspace(int(k - '1'))
	return true
}

// arrow is an arrow key's direction.
func arrow(keyval uint32) (layout.Dir, bool) {
	switch int32(keyval) {
	case gdk.KEY_Left, gdk.KEY_KP_Left:
		return layout.Left, true
	case gdk.KEY_Right, gdk.KEY_KP_Right:
		return layout.RightOf, true
	case gdk.KEY_Up, gdk.KEY_KP_Up:
		return layout.Up, true
	case gdk.KEY_Down, gdk.KEY_KP_Down:
		return layout.Below, true
	}
	return 0, false
}

func (a *App) layoutOp(op string, m host.Message) {
	s := a.shown()
	if s == nil {
		return
	}
	session := s.ID
	a.ask(op, func(c *host.Client) error { return c.Layout(session, op, m) })
}

func (a *App) split(sp layout.Split) {
	s := a.shown()
	if s == nil {
		return
	}
	session := s.ID
	a.ask("split", func(c *host.Client) error {
		_, err := c.Split(session, string(sp))
		return err
	})
}

// closeFocused ends the focused pane's program and closes it.
func (a *App) closeFocused() {
	s := a.shown()
	if s == nil {
		return
	}
	pane := s.Focus
	a.ask("close", func(c *host.Client) error { return c.ClosePane(pane) })
}

// newSession adds a session to the shown workspace.
func (a *App) newSession() {
	w := a.shownWorkspace()
	if w == nil {
		return
	}
	id := w.ID
	a.ask("new session", func(c *host.Client) error {
		_, err := c.NewSession(id, "")
		return err
	})
}

// newWorkspace asks for a folder and opens its workspace.
func (a *App) newWorkspace() {
	d := gtk.NewFileDialog()
	d.SetTitle(i18n.T("Choose the workspace's folder"))
	d.SetModal(true)
	a.onFolder = func(_, result, _ uintptr) {
		f, err := d.SelectFolderFinish(&gio.AsyncResultBase{Ptr: result})
		if err != nil || f == nil {
			return
		}
		dir := f.GetPath()
		if dir == "" {
			return
		}
		a.ask("open", func(c *host.Client) error {
			_, err := c.Open(dir, "")
			return err
		})
	}
	d.SelectFolder(&a.win.Window, nil, &a.onFolder, 0)
}

// nthWorkspace shows the workspace at place i in column 1.
func (a *App) nthWorkspace(i int) {
	if a.state == nil || i >= len(a.state.Model.Workspaces) {
		return
	}
	a.showWorkspace(a.state.Model.Workspaces[i].ID)
}

// stepSession shows the previous (-1) or next session of the workspace.
func (a *App) stepSession(dir int) {
	w := a.shownWorkspace()
	if w == nil || len(w.Sessions) == 0 {
		return
	}
	i := 0
	for j, s := range w.Sessions {
		if s.ID == a.state.Model.Shown.Session {
			i = j
		}
	}
	i = (i + dir + len(w.Sessions)) % len(w.Sessions)
	a.showSession(w.ID, w.Sessions[i].ID)
}

// renameSession asks for the shown session's new name, in a sheet over
// column 3.
func (a *App) renameSession() {
	s := a.shown()
	if s == nil || a.sheet != nil {
		return
	}
	session := s.ID
	sheet := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	sheet.AddCssClass("term-sheet")
	sheet.SetHalign(gtk.AlignCenterValue)
	sheet.SetValign(gtk.AlignCenterValue)
	sheet.SetSizeRequest(360, -1)
	title := gtk.NewLabel(i18n.T("Rename the session"))
	title.AddCssClass("term-sheet-title")
	title.SetXalign(0)
	sheet.Append(&title.Widget)
	entry := gtk.NewEntry()
	entry.SetText(s.Name)
	sheet.Append(&entry.Widget)
	buttons := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	buttons.SetHalign(gtk.AlignEndValue)
	cancel := gtk.NewButtonWithLabel(i18n.T("Cancel"))
	cancel.AddCssClass("outlined")
	ok := gtk.NewButtonWithLabel(i18n.T("Rename"))
	ok.AddCssClass("suggested-action")
	buttons.Append(&cancel.Widget)
	buttons.Append(&ok.Widget)
	sheet.Append(&buttons.Widget)
	done := func(rename bool) {
		name := entry.GetText()
		a.stage.RemoveOverlay(&sheet.Widget)
		a.sheet = nil
		if t := a.tiles[s.Focus]; t != nil {
			t.pane.widget.GrabFocus()
		}
		if rename && name != "" {
			a.ask("rename", func(c *host.Client) error { return c.Rename(0, session, name) })
		}
	}
	a.onRenameOK = func(gtk.Button) { done(true) }
	a.onRenameCancel = func(gtk.Button) { done(false) }
	a.onRenameActivate = func(gtk.Entry) { done(true) }
	ok.ConnectClicked(&a.onRenameOK)
	cancel.ConnectClicked(&a.onRenameCancel)
	entry.ConnectActivate(&a.onRenameActivate)
	keys := gtk.NewEventControllerKey()
	a.onRenameKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if int32(keyval) == gdk.KEY_Escape {
			done(false)
			return true
		}
		return false
	}
	keys.ConnectKeyPressed(&a.onRenameKey)
	sheet.AddController(&keys.EventController)
	a.sheet = sheet
	a.stage.AddOverlay(&sheet.Widget)
	entry.GrabFocus()
	entry.SelectRegion(0, -1)
}
