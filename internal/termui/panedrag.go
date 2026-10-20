package termui

import (
	"strconv"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gobject/types"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/term/host"
)

// A pane's head is its handle. Dropped on another pane of its session,
// the two swap; on a session's head in column 2, the pane moves to that
// session, beside its focused pane; on a pane's row there, beside that
// pane (or the two swap, in its own session). What it would land on is
// lit while it is over it.

type paneDrag struct {
	id int // the pane dragged, 0 for none
	// lit is the drop target lit, spots column 2's targets by widget.
	lit   *gtk.Widget
	spots map[uintptr]dropSpot

	onBegin      func(gtk.DragSource, uintptr)
	onEnd        func(gtk.DragSource, uintptr, bool)
	onTileMotion func(gtk.DropTarget, float64, float64) gdk.DragAction
	onTileDrop   func(gtk.DropTarget, uintptr, float64, float64) bool
	onSpotMotion func(gtk.DropTarget, float64, float64) gdk.DragAction
	onSpotDrop   func(gtk.DropTarget, uintptr, float64, float64) bool
	onLeave      func(gtk.DropTarget)
}

// dropSpot is what a target in column 2 stands for: a session's head
// (pane 0), or a pane's row.
type dropSpot struct{ session, pane int }

// paneValue is what a pane's drag carries.
func paneValue(id int) string { return "pane:" + strconv.Itoa(id) }

// startPaneDrag readies the drags: column 3 takes drops on its tiles,
// each head is a drag source (dragHead), column 2's heads and rows
// targets (dropOn).
func (a *App) startPaneDrag() {
	d := &a.paneDrag
	d.spots = map[uintptr]dropSpot{}
	d.onBegin = func(ds gtk.DragSource, _ uintptr) {
		w := ds.GetWidget()
		for id, t := range a.tiles {
			if w == nil || t.head.GoPointer() != w.GoPointer() {
				continue
			}
			d.id = id
			// The head follows the pointer, held where it was taken.
			live := gtk.NewWidgetPaintable(w)
			still := live.GetCurrentImage()
			live.Unref()
			ds.SetIcon(still, 24, int32(w.GetHeight()/2))
			gobject.ObjectNewFromInternalPtr(still.GoPointer()).Unref() // ours (transfer full)
			return
		}
	}
	d.onEnd = func(gtk.DragSource, uintptr, bool) {
		d.id = 0
		a.light(nil)
	}
	d.onTileMotion = func(_ gtk.DropTarget, x, y float64) gdk.DragAction {
		other := a.tileAt(x, y)
		if d.id == 0 || other == 0 {
			a.light(nil)
			return 0
		}
		a.light(&a.tiles[other].box.Widget)
		return gdk.ActionMoveValue
	}
	d.onTileDrop = func(dt gtk.DropTarget, _ uintptr, x, y float64) bool {
		id, other := a.dragged(dt), a.tileAt(x, y)
		if id == 0 || other == 0 {
			return false
		}
		a.layoutOp(host.OpSwap, host.Message{From: id, Pane: other})
		return true
	}
	d.onSpotMotion = func(dt gtk.DropTarget, _, _ float64) gdk.DragAction {
		w := dt.GetWidget()
		if d.id == 0 || w == nil || !a.canDrop(d.spots[w.GoPointer()]) {
			a.light(nil)
			return 0
		}
		a.light(w)
		return gdk.ActionMoveValue
	}
	d.onSpotDrop = func(dt gtk.DropTarget, _ uintptr, _, _ float64) bool {
		id, w := a.dragged(dt), dt.GetWidget()
		if id == 0 || w == nil {
			return false
		}
		spot := d.spots[w.GoPointer()]
		if !a.canDrop(spot) {
			return false
		}
		if s := a.shown(); s != nil && spot.session == s.ID {
			a.layoutOp(host.OpSwap, host.Message{From: id, Pane: spot.pane})
		} else {
			a.layoutOp(host.OpMove, host.Message{Pane: id, To: spot.session, From: spot.pane})
		}
		return true
	}
	d.onLeave = func(gtk.DropTarget) { a.light(nil) }
	drop := gtk.NewDropTarget(types.GType(gobject.TypeStringVal), gdk.ActionMoveValue)
	drop.ConnectMotion(&d.onTileMotion)
	drop.ConnectLeave(&d.onLeave)
	drop.ConnectDrop(&d.onTileDrop)
	a.tilesW.AddController(&drop.EventController)
}

// dragHead makes a tile's head the handle that drags its pane.
func (a *App) dragHead(head *gtk.Box, id int) {
	var v gobject.Value
	v.Init(gobject.TypeStringVal)
	v.SetString(paneValue(id))
	content := gdk.NewContentProviderForValue(&v)
	v.Unset()
	ds := gtk.NewDragSource()
	ds.SetActions(gdk.ActionMoveValue)
	ds.SetContent(content)
	content.Unref()
	ds.ConnectDragBegin(&a.paneDrag.onBegin)
	ds.ConnectDragEnd(&a.paneDrag.onEnd)
	head.AddController(&ds.EventController)
	head.SetCursorFromName("grab")
}

// dropOn makes a widget of column 2 a target for a pane: a session's
// head (pane 0) or a pane's row.
func (a *App) dropOn(w *gtk.Widget, session, pane int) {
	d := &a.paneDrag
	d.spots[w.GoPointer()] = dropSpot{session, pane}
	drop := gtk.NewDropTarget(types.GType(gobject.TypeStringVal), gdk.ActionMoveValue)
	drop.ConnectMotion(&d.onSpotMotion)
	drop.ConnectLeave(&d.onLeave)
	drop.ConnectDrop(&d.onSpotDrop)
	w.AddController(&drop.EventController)
}

// canDrop reports whether the pane dragged has somewhere to go at spot:
// another session, or another pane of its own.
func (a *App) canDrop(spot dropSpot) bool {
	s := a.shown()
	switch {
	case spot.session == 0:
		return false
	case s == nil || spot.session != s.ID:
		return true
	}
	return spot.pane != 0 && spot.pane != a.paneDrag.id
}

// dragged is the pane a drop carries, 0 for anything else.
func (a *App) dragged(dt gtk.DropTarget) int {
	v := dt.GetValue()
	if v == nil {
		return 0
	}
	id, err := strconv.Atoi(strings.TrimPrefix(v.GetString(), "pane:"))
	if err != nil || !strings.HasPrefix(v.GetString(), "pane:") || id != a.paneDrag.id {
		return 0
	}
	return id
}

// tileAt is the pane under x, y in column 3 other than the one dragged,
// 0 for none.
func (a *App) tileAt(x, y float64) int {
	for id, r := range a.tiling.rects {
		if id != a.paneDrag.id && a.tiles[id] != nil && x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			return id
		}
	}
	return 0
}

// light marks w as where the pane would land, and no other.
func (a *App) light(w *gtk.Widget) {
	d := &a.paneDrag
	if d.lit != nil && (w == nil || d.lit.GoPointer() != w.GoPointer()) {
		d.lit.RemoveCssClass("drop-on")
		d.lit = nil
	}
	if w != nil && d.lit == nil {
		w.AddCssClass("drop-on")
		d.lit = w
	}
}
