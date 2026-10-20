package vmui

import (
	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// dialog is a question asked over the window before the guest is removed or
// its disk deleted: a title, the thing it is about in mono, a line on what
// follows, and Cancel beside the action.
type dialog struct {
	win      *gtk.Window
	yes      func()
	onButton func(gtk.Button)
	onClose  func(gtk.Window) bool
	onKey    func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
}

// confirm asks; yes runs when the user takes the action. danger fills the
// action in the danger colour, for what cannot be undone.
func (v *vmWindow) confirm(title, item, text, action string, danger bool, yes func()) {
	if v.question != nil {
		return // one question at a time
	}
	d := &dialog{win: gtk.NewWindow(), yes: yes}
	v.question = d
	done := func(ok bool) {
		if v.question != d {
			return
		}
		v.question = nil
		d.win.Destroy()
		if ok && !v.closed {
			d.yes()
		}
	}
	d.onButton = func(b gtk.Button) { done(b.GetName() == "ok") }
	d.onClose = func(gtk.Window) bool { done(false); return false }
	d.onKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if keyval == uint32(gdk.KEY_Escape) {
			done(false)
			return true
		}
		return false
	}
	d.win.SetTransientFor(&v.win.Window)
	d.win.SetModal(true)
	d.win.SetDestroyWithParent(true)
	d.win.SetResizable(false)
	d.win.SetTitle(title)
	for _, c := range []string{"app", "settings", "vm-dialog"} {
		d.win.AddCssClass(c)
	}
	d.win.ConnectCloseRequest(&d.onClose)
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(&d.onKey)
	d.win.AddController(&keys.EventController)

	box := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	box.AddCssClass("page")
	box.SetSizeRequest(420, -1)
	t := gtk.NewLabel(title)
	t.AddCssClass("vm-dialog-title")
	t.SetXalign(0)
	t.SetWrap(true)
	t.SetMaxWidthChars(40)
	gtkx.Append(box, &t.Widget)
	if item != "" {
		l := gtk.NewLabel(item)
		l.AddCssClass("vm-command")
		l.SetXalign(0)
		l.SetWrap(true)
		l.SetWrapMode(2) // word_char: a path has few spaces
		l.SetMaxWidthChars(48)
		gtkx.Append(box, &l.Widget)
	}
	if text != "" {
		l := gtk.NewLabel(text)
		l.AddCssClass("sub")
		l.SetXalign(0)
		l.SetWrap(true)
		l.SetMaxWidthChars(52)
		gtkx.Append(box, &l.Widget)
	}
	buttons := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	buttons.SetHalign(gtk.AlignEndValue)
	buttons.SetMarginTop(8)
	cancel := gtk.NewButtonWithLabel(i18n.T("Cancel"))
	cancel.AddCssClass("outlined")
	cancel.SetName("cancel")
	cancel.ConnectClicked(&d.onButton)
	ok := gtk.NewButtonWithLabel(action)
	ok.SetName("ok")
	if danger {
		ok.AddCssClass("destructive-action")
	} else {
		ok.AddCssClass("suggested-action")
	}
	ok.ConnectClicked(&d.onButton)
	gtkx.Append(buttons, &cancel.Widget)
	gtkx.Append(buttons, &ok.Widget)
	gtkx.Append(box, &buttons.Widget)
	d.win.SetChild(&box.Widget)
	d.win.Present()
	cancel.GrabFocus() // Enter does not delete
}
