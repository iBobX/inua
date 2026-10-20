package termui

import (
	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/agent"
	"github.com/lyricat/inua/internal/term/host"
)

// A workspace's row has a menu: a right click on it, or the Menu key
// (Shift+F10) on the row selected. Rename asks for the name; Close asks
// first, as every program in the workspace quits.

type wsMenu struct {
	id      int // the workspace the menu is for
	onPress func(gtk.GestureClick, int32, float64, float64)
	onKey   func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onItem  func(gio.SimpleAction, uintptr)
	onShut  func(gtk.Popover)
}

// startMenu readies the rows' menu; each row takes a right click as it is
// made (menuRow), the list the keys.
func (c *columns) startMenu(a *App) {
	m := &c.rowMenu
	m.onPress = func(g gtk.GestureClick, _ int32, x, y float64) {
		w := g.GetWidget()
		if i := c.rowOf(w); i >= 0 {
			c.showMenu(a, w, i, &gdk.Rectangle{X: int32(x), Y: int32(y), Width: 1, Height: 1})
		}
	}
	m.onKey = func(_ gtk.EventControllerKey, keyval, _ uint32, state gdk.ModifierType) bool {
		menuKey := int32(keyval) == gdk.KEY_Menu ||
			int32(keyval) == gdk.KEY_F10 && state&gdk.ShiftMaskValue != 0
		row := c.list.GetSelectedRow()
		if !menuKey || row == nil {
			return false
		}
		if i := int(row.GetIndex()); i >= 0 && i < len(c.rowIDs) {
			c.showMenu(a, &row.Widget, i, nil)
		}
		return true
	}
	m.onItem = func(act gio.SimpleAction, _ uintptr) {
		id := m.id
		switch act.GetName() {
		case "rename":
			a.renameWorkspace(id)
		case "close":
			a.closeWorkspace(id)
		}
	}
	m.onShut = func(p gtk.Popover) {
		gtkx.Post(func() { p.Unparent() })
	}
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(&m.onKey)
	c.list.AddController(&keys.EventController)
}

// menuRow gives a row its right click.
func (c *columns) menuRow(row *gtk.ListBoxRow) {
	click := gtk.NewGestureClick()
	click.SetButton(3)
	click.ConnectPressed(&c.rowMenu.onPress)
	row.AddController(&click.EventController)
}

// showMenu opens row i's menu on w, at the pointer when at is set.
func (c *columns) showMenu(a *App, w *gtk.Widget, i int, at *gdk.Rectangle) {
	m := &c.rowMenu
	m.id = c.rowIDs[i]
	group := gio.NewSimpleActionGroup()
	menu := gio.NewMenu()
	for _, it := range []struct{ label, action string }{
		{i18n.T("Rename…"), "rename"},
		{i18n.T("Close…"), "close"},
	} {
		act := gio.NewSimpleAction(it.action, nil)
		act.ConnectActivate(&m.onItem)
		group.AddAction(act)
		act.Unref()
		menu.Append(it.label, "ws."+it.action)
	}
	w.InsertActionGroup("ws", group)
	group.Unref()
	pop := gtk.NewPopoverMenuFromModel(&menu.MenuModel)
	menu.Unref()
	pop.SetHasArrow(false)
	pop.SetPosition(gtk.PosBottomValue)
	if at != nil {
		pop.SetPointingTo(at)
	}
	pop.SetParent(w)
	pop.ConnectClosed(&m.onShut)
	pop.Unref() // the row holds it
	pop.Popup()
}

// renameWorkspace asks for a workspace's new name.
func (a *App) renameWorkspace(id int) {
	if a.state == nil {
		return
	}
	w := a.state.Model.Workspace(id)
	if w == nil {
		return
	}
	a.renameSheet(i18n.T("Rename the workspace"), w.Name, func(c *host.Client, name string) error {
		return c.Rename(id, 0, name)
	})
}

// closeWorkspace asks before closing a workspace: its sessions and panes
// end, and every program running in them quits.
func (a *App) closeWorkspace(id int) {
	if a.state == nil || a.sheet != nil {
		return
	}
	w := a.state.Model.Workspace(id)
	if w == nil {
		return
	}
	panes := workspacePanes(w)
	busy := 0
	for _, p := range panes {
		if st := agent.State(a.info[p].State); a.info[p].Agent != "" && (st == agent.Working || st == agent.Blocked) {
			busy++
		}
	}
	sheet, done := a.sheetFrame(i18n.Tf("Close %s?", w.Name), 420)
	why := i18n.Tf("Its %s and %s end, and every program running in them quits.",
		i18n.Nf("%d session", "%d sessions", len(w.Sessions), len(w.Sessions)),
		i18n.Nf("%d pane", "%d panes", len(panes), len(panes)))
	if busy > 0 {
		why += " " + i18n.Nf("%d agent is working or waiting for you.", "%d agents are working or waiting for you.", busy, busy)
	}
	l := gtk.NewLabel(why)
	l.AddCssClass("term-sheet-why")
	l.SetXalign(0)
	l.SetWrap(true)
	sheet.Append(&l.Widget)
	buttons := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	buttons.SetHalign(gtk.AlignEndValue)
	cancel := gtk.NewButtonWithLabel(i18n.T("Cancel"))
	cancel.AddCssClass("outlined")
	ok := gtk.NewButtonWithLabel(i18n.T("Close"))
	ok.AddCssClass("destructive-action")
	buttons.Append(&cancel.Widget)
	buttons.Append(&ok.Widget)
	sheet.Append(&buttons.Widget)
	a.onCloseCancel = func(gtk.Button) { done() }
	a.onCloseOK = func(gtk.Button) {
		done()
		a.ask("close workspace", func(c *host.Client) error { return c.RemoveWorkspace(id) })
	}
	cancel.ConnectClicked(&a.onCloseCancel)
	ok.ConnectClicked(&a.onCloseOK)
	cancel.GrabFocus()
}
