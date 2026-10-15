package filesui

import (
	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
)

// menuItem is an entry of a menu: a label with its key, or a separator
// when the label is empty. A checkable one shows a mark when on, and keeps
// the menu open, so several can be turned on and off in a row.
type menuItem struct {
	label, key string
	run        func()
	check, on  bool
}

// popMenu is a menu popping up over a widget, in the shell's look; its
// buttons go through the hub and are forgotten when it is filled again.
type popMenu struct {
	h      *hub
	parent *gtk.Widget
	pop    *gtk.Popover
	box    gtk.Box
	ptrs   []uintptr
	items  func() []menuItem // what it shows, asked again after a check
}

func newPopMenu(h *hub, parent *gtk.Widget) *popMenu {
	m := &popMenu{h: h, parent: parent}
	m.pop = gtk.NewPopover()
	m.pop.AddCssClass("files-menu")
	m.pop.SetHasArrow(false)
	m.box = *gtk.NewBox(gtk.OrientationVerticalValue, 0)
	m.pop.SetChild(&m.box.Widget)
	m.box.Unref()
	m.pop.SetParent(parent)
	return m
}

// show opens the menu at x, y in its parent.
func (m *popMenu) show(items func() []menuItem, x, y int32) {
	m.items = items
	m.fill()
	rect := gdk.Rectangle{X: x, Y: y, Width: 1, Height: 1}
	m.pop.SetPointingTo(&rect)
	m.pop.Popup()
}

func (m *popMenu) fill() {
	m.h.forget(m.ptrs)
	m.ptrs = m.ptrs[:0]
	gtkx.Clear(&m.box)
	for _, it := range m.items() {
		if it.label == "" {
			sep := gtk.NewSeparator(gtk.OrientationHorizontalValue)
			gtkx.Append(&m.box, &sep.Widget)
			continue
		}
		b := gtk.NewButton()
		b.AddCssClass("files-menu-item")
		box := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
		if it.check {
			mark := gtk.NewImageFromIconName("object-select-symbolic")
			mark.AddCssClass("files-menu-check")
			mark.SetOpacity(map[bool]float64{true: 1, false: 0}[it.on])
			gtkx.Append(box, &mark.Widget)
		}
		l := gtk.NewLabel(it.label)
		l.SetXalign(0)
		l.SetHexpand(true)
		gtkx.Append(box, &l.Widget)
		if it.key != "" {
			k := gtk.NewLabel(it.key)
			k.AddCssClass("readout")
			k.AddCssClass("files-menu-key")
			gtkx.Append(box, &k.Widget)
		}
		b.SetChild(&box.Widget)
		box.Unref()
		run, keep := it.run, it.check
		m.h.click(b, func() {
			if !keep {
				m.pop.Popdown()
			}
			if run != nil {
				run()
			}
			if keep {
				m.fill()
			}
		})
		m.ptrs = append(m.ptrs, b.GoPointer())
		gtkx.Append(&m.box, &b.Widget)
	}
}

func (m *popMenu) destroy() {
	m.h.forget(m.ptrs)
	m.pop.Unparent()
}
