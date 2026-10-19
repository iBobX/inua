package termui

import (
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

// chord does the window's action for a Ctrl+Shift chord; false leaves the
// key to the pane.
func (a *App) chord(base, keyval uint32, state gdk.ModifierType) bool {
	held := state & (gdk.ControlMaskValue | gdk.ShiftMaskValue | gdk.AltMaskValue | gdk.SuperMaskValue)
	if a.swapping {
		a.swapping = false
		if d, ok := arrow(keyval); ok {
			a.layoutOp(host.OpSwap, host.Message{Direction: int(d)})
			return true
		}
	}
	cs := gdk.ControlMaskValue | gdk.ShiftMaskValue
	if held == cs|gdk.AltMaskValue {
		if d, ok := arrow(keyval); ok {
			a.layoutOp(host.OpResize, host.Message{Direction: int(d), Step: 0.05})
			return true
		}
		return false
	}
	if held != cs {
		return false
	}
	if d, ok := arrow(keyval); ok {
		a.layoutOp(host.OpFocus, host.Message{Direction: int(d)})
		return true
	}
	k := rune(gdk.KeyvalToUnicode(gdk.KeyvalToLower(base)))
	switch {
	case k == 't':
		a.newSession()
	case k == 'n':
		a.newWorkspace()
	case k == 'd':
		a.split(layout.Right)
	case k == 'e':
		a.split(layout.Down)
	case k == 'w':
		a.closeFocused()
	case k == 'x':
		a.swapping = true
	case k == 'z':
		a.layoutOp(host.OpZoom, host.Message{})
	case k == '=':
		a.layoutOp(host.OpEqualize, host.Message{})
	case k == 'l':
		a.layoutOp(host.OpPreset, host.Message{})
	case k >= '1' && k <= '9':
		a.nthWorkspace(int(k - '1'))
	case k == '[':
		a.stepSession(-1)
	case k == ']':
		a.stepSession(1)
	case k == 'b':
		a.toggleColumn(0)
	case k == 's':
		a.toggleColumn(1)
	case k == 'r':
		a.renameSession()
	case k == 'j':
		a.nextWaiting()
	case k == 'a':
		a.overview()
	default:
		return false
	}
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
