package mailui

import (
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
)

// loader is Mail's sign of work in progress: three square marks lit in
// turn (style.css animates them), with what is being done under them when
// it is said. GTK's spinner would draw the desktop theme's icon.
func loader(text string) *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	box.AddCssClass("mail-loader")
	marks := gtk.NewBox(gtk.OrientationHorizontalValue, 5)
	marks.AddCssClass("mail-loader-marks")
	marks.SetHalign(gtk.AlignCenterValue)
	for i := range 3 {
		m := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		m.AddCssClass("mark")
		m.AddCssClass([]string{"one", "two", "three"}[i])
		gtkx.Append(marks, &m.Widget)
	}
	gtkx.Append(box, &marks.Widget)
	if text != "" {
		l := gtk.NewLabel(text)
		l.AddCssClass("mail-loader-text")
		gtkx.Append(box, &l.Widget)
	}
	return box
}

// skeleton is a message being read: the loader over the shape of a few
// lines of text, pulsing.
func skeleton() *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	box.AddCssClass("mail-skeleton")
	l := loader("")
	l.SetHalign(gtk.AlignStartValue)
	gtkx.Append(box, &l.Widget)
	for _, w := range []int32{280, 240, 260, 150} {
		line := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		line.AddCssClass("mail-skeleton-line")
		line.SetSizeRequest(w, 10)
		line.SetHalign(gtk.AlignStartValue)
		gtkx.Append(box, &line.Widget)
	}
	return box
}
