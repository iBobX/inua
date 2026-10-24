package appui

import (
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"

	"github.com/lyricat/inua/internal/gtkx"
)

// A library (design/ui.md) shows what the user runs or connects to as tiles,
// the chosen one's page in a panel beside it. Its layout is the app's own
// Blueprint, in the "library" classes; the tiles are made here.

// LibraryTile is one tile: what a repaint changes.
type LibraryTile struct {
	Child gtk.FlowBoxChild
	Mark  gtk.Box // filled ("on") while the thing runs or is connected
	Title gtk.Label
	Sub   gtk.Label
}

// NewLibraryTile makes a tile named name (what a selection reads back): the
// icon at the top with the mark at the other end, then the title and a line
// under it. Without marked, the tile has no mark.
func NewLibraryTile(name, icon, title, line string, marked bool) LibraryTile {
	box := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	box.AddCssClass("library-tile")
	top := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	top.AddCssClass("library-tile-top")
	img := gtk.NewImageFromIconName(icon)
	img.AddCssClass("library-tile-icon")
	img.SetPixelSize(32)
	gtkx.Append(top, &img.Widget)
	mark := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	mark.AddCssClass("mark")
	mark.SetHalign(gtk.AlignEndValue)
	mark.SetValign(gtk.AlignStartValue)
	mark.SetHexpand(true)
	mark.SetVisible(marked)
	gtkx.Append(top, &mark.Widget)
	gtkx.Append(box, &top.Widget)

	t := gtk.NewLabel(title)
	t.AddCssClass("library-tile-name")
	t.SetXalign(0)
	t.SetEllipsize(pango.EllipsizeEndValue)
	s := gtk.NewLabel(line)
	s.AddCssClass("library-tile-sub")
	s.SetXalign(0)
	s.SetEllipsize(pango.EllipsizeEndValue)
	gtkx.Append(box, &t.Widget)
	gtkx.Append(box, &s.Widget)

	child := gtk.NewFlowBoxChild()
	child.SetName(name)
	child.SetChild(&box.Widget)
	return LibraryTile{Child: *child, Mark: *mark, Title: *t, Sub: *s}
}

// SetOn fills the tile's mark, or empties it.
func (t *LibraryTile) SetOn(on bool) {
	if on {
		t.Mark.AddCssClass("on")
	} else {
		t.Mark.RemoveCssClass("on")
	}
}

// SelectedTile is the name of the tile selected in box, "" for none.
func SelectedTile(box *gtk.FlowBox) string {
	for i := int32(0); ; i++ {
		tile := box.GetChildAtIndex(i)
		if tile == nil {
			return ""
		}
		if tile.IsSelected() {
			return tile.GetName()
		}
	}
}
