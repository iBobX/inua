package termui

import (
	"math"
	"strconv"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gobject/types"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/term/host"
)

// Column 1's workspaces are put in order by dragging, as the dock's apps
// are (design/ui.md, Reordering): the row lifts, a still of it under the
// pointer and its place left open; the others slide aside to open a gap
// where it would land; the drop puts it there, in the window at once and
// in the host after. While the search narrows the list, rows stay put.

type wsDrag struct {
	id      int     // the workspace dragged, 0 for none
	from    int     // its row
	slot    float64 // its height with the space under it: the gap's
	gaps    []wsGap // each row's gap, above it and below it
	sources []*gtk.DragSource
	ticking bool

	onBegin  func(gtk.DragSource, uintptr)
	onEnd    func(gtk.DragSource, uintptr, bool)
	onMotion func(gtk.DropTarget, float64, float64) gdk.DragAction
	onLeave  func(gtk.DropTarget)
	onDrop   func(gtk.DropTarget, uintptr, float64, float64) bool
	onTick   glib.SourceFunc
}

// wsGap is a row's margins now and where they slide to, in pixels.
type wsGap struct{ top, bottom, toTop, toBottom float64 }

// startDrag makes the list a drop target; the rows are drag sources as
// they are made (dragRows).
func (c *columns) startDrag(a *App) {
	d := &c.drag
	d.onBegin = func(ds gtk.DragSource, _ uintptr) {
		w := ds.GetWidget()
		i := c.rowOf(w)
		if i < 0 {
			return
		}
		d.id, d.from = c.rowIDs[i], i
		d.slot = float64(w.GetHeight()) + 2 // the index rows' margin under
		// A still of the row follows the pointer, held where it was.
		live := gtk.NewWidgetPaintable(w)
		still := live.GetCurrentImage()
		live.Unref()
		ds.SetIcon(still, 24, int32(w.GetHeight()/2))
		gobject.ObjectNewFromInternalPtr(still.GoPointer()).Unref() // ours (transfer full)
		gtkx.Post(func() {
			if d.id == 0 {
				return
			}
			// Its place stays open as the gap, so nothing moves.
			c.setGap(d.from+1, true)
			if row := c.list.GetRowAtIndex(int32(d.from)); row != nil {
				row.SetVisible(false)
			}
		})
	}
	d.onEnd = func(gtk.DragSource, uintptr, bool) {
		// Dropped or not: the row is back in its place, or in its new
		// one (the list is made in that order already).
		d.id = 0
		for i := range c.rowIDs {
			if row := c.list.GetRowAtIndex(int32(i)); row != nil {
				row.SetVisible(true)
			}
		}
		c.setGap(-1, true)
	}
	d.onMotion = func(_ gtk.DropTarget, _, y float64) gdk.DragAction {
		if d.id == 0 {
			return 0
		}
		c.setGap(c.dropBefore(y), false)
		return gdk.ActionMoveValue
	}
	d.onLeave = func(gtk.DropTarget) {
		if d.id != 0 {
			c.setGap(-1, false)
		}
	}
	d.onDrop = func(dt gtk.DropTarget, _ uintptr, _, y float64) bool {
		v := dt.GetValue()
		if d.id == 0 || v == nil || v.GetString() != strconv.Itoa(d.id) || a.state == nil {
			return false
		}
		// Its place among the others, the list being every workspace.
		to := 0
		for i := 0; i < c.dropBefore(y); i++ {
			if i != d.from {
				to++
			}
		}
		id := d.id
		a.state.Model.Move(id, to)
		a.ask("move", func(cl *host.Client) error { return cl.MoveWorkspace(id, to) })
		gtkx.Post(func() { c.update(a) }) // after the drag ends
		return true
	}
	d.onTick = func(uintptr) bool {
		d.ticking = c.stepGaps()
		return d.ticking
	}
	drop := gtk.NewDropTarget(types.GType(gobject.TypeStringVal), gdk.ActionMoveValue)
	drop.ConnectMotion(&d.onMotion)
	drop.ConnectLeave(&d.onLeave)
	drop.ConnectDrop(&d.onDrop)
	c.list.AddController(&drop.EventController)
}

// dragRows makes the list's rows drag sources, just made; filtered, the
// list takes no drag.
func (c *columns) dragRows(filtered bool) {
	d := &c.drag
	d.sources, d.gaps = d.sources[:0], make([]wsGap, len(c.rowIDs))
	for i, id := range c.rowIDs {
		row := c.list.GetRowAtIndex(int32(i))
		if row == nil {
			continue
		}
		var v gobject.Value
		v.Init(gobject.TypeStringVal)
		v.SetString(strconv.Itoa(id))
		content := gdk.NewContentProviderForValue(&v)
		v.Unset()
		ds := gtk.NewDragSource()
		ds.SetActions(gdk.ActionMoveValue)
		ds.SetContent(content)
		content.Unref()
		ds.ConnectDragBegin(&d.onBegin)
		ds.ConnectDragEnd(&d.onEnd)
		if filtered {
			ds.SetPropagationPhase(gtk.PhaseNoneValue)
		}
		row.AddController(&ds.EventController)
		d.sources = append(d.sources, ds)
	}
}

// rowOf is the place of the row w is, -1 for none.
func (c *columns) rowOf(w *gtk.Widget) int {
	for i := range c.rowIDs {
		if row := c.list.GetRowAtIndex(int32(i)); row != nil && w != nil && row.GoPointer() == w.GoPointer() {
			return i
		}
	}
	return -1
}

// bounds is a row's top and height in the list, ok false for none.
func (c *columns) bounds(i int) (top, height float64, ok bool) {
	row := c.list.GetRowAtIndex(int32(i))
	if row == nil || !row.GetVisible() {
		return 0, 0, false
	}
	var r graphene.Rect
	if !row.ComputeBounds(&c.list.Widget, &r) {
		return 0, 0, false
	}
	f := *(*[4]float32)(unsafe.Pointer(&r)) // x, y, width, height
	return float64(f[1]), float64(f[3]), true
}

// dropBefore is the row a drop at y goes before, the dragged one left
// out; len(rows) for after the last.
func (c *columns) dropBefore(y float64) int {
	for i := range c.rowIDs {
		if i == c.drag.from {
			continue
		}
		if top, h, ok := c.bounds(i); ok && y < top+h/2 {
			return i
		}
	}
	return len(c.rowIDs)
}

// setGap opens the gap above row before (below the last row for
// len(rows)), or closes it (-1); the rows slide aside, unless now.
func (c *columns) setGap(before int, now bool) {
	d := &c.drag
	last := -1
	for i := range c.rowIDs {
		if i != d.from || d.id == 0 {
			last = i
		}
	}
	for i := range d.gaps {
		g := &d.gaps[i]
		g.toTop, g.toBottom = 0, 0
		if before >= 0 && i == before && i != d.from {
			g.toTop = d.slot
		}
		if before == len(c.rowIDs) && i == last {
			g.toBottom = d.slot
		}
		if now {
			g.top, g.bottom = g.toTop, g.toBottom
			c.applyGap(i)
		}
	}
	if !now && !d.ticking {
		d.ticking = true
		glibTimeout(16, &d.onTick)
	}
}

// stepGaps slides each gap a step toward where it goes; true while any
// still moves.
func (c *columns) stepGaps() bool {
	moving := false
	step := func(cur, to float64) float64 {
		if math.Abs(to-cur) < 0.6 {
			return to
		}
		moving = true
		return cur + (to-cur)*0.3
	}
	for i := range c.drag.gaps {
		g := &c.drag.gaps[i]
		top, bottom := step(g.top, g.toTop), step(g.bottom, g.toBottom)
		if top != g.top || bottom != g.bottom {
			g.top, g.bottom = top, bottom
			c.applyGap(i)
		}
	}
	return moving
}

func (c *columns) applyGap(i int) {
	row := c.list.GetRowAtIndex(int32(i))
	if row == nil {
		return
	}
	g := c.drag.gaps[i]
	row.SetMarginTop(int32(math.Round(g.top)))
	row.SetMarginBottom(int32(math.Round(g.bottom)))
}
