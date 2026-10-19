package termui

import (
	"log"
	"math"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/agent"
	"github.com/lyricat/inua/internal/term/host"
	"github.com/lyricat/inua/internal/term/layout"
)

// Column 3 is the shown session's panes and nothing else: tiles laid out
// by the session's split tree, 8 px apart and from the edges, each a
// 32 px head (state, kind, title, folder) over its terminal. The focused
// tile's border is the accent; the gaps drag to resize.

// gap is the space between tiles and around them.
const gap = 8

// tile is one pane of the shown session.
type tile struct {
	id    int
	box   *gtk.Box
	mark  *gtk.Box
	icon  *gtk.Image
	title *gtk.Label
	dir   *gtk.Label
	state *gtk.Label
	pane  *termPane
}

// tiling is column 3's layout: the tree laid out (a copy while a gap is
// dragged), and the drag.
type tiling struct {
	layout   *gtk.CustomLayout
	tree     *layout.Node
	zoom     int
	dragging *layout.Divider
	dragFrom float64

	onMeasure                            gtk.CustomMeasureFunc
	onAllocate                           gtk.CustomAllocateFunc
	onMode                               gtk.CustomRequestModeFunc
	onDragBegin, onDragUpdate, onDragEnd func(gtk.GestureDrag, float64, float64)
	onMotion                             func(gtk.EventControllerMotion, float64, float64)
}

func (a *App) buildTiling() {
	t := &a.tiling
	t.onMode = func(uintptr) gtk.SizeRequestMode { return gtk.SizeRequestConstantSizeValue }
	t.onMeasure = func(_ uintptr, _ gtk.Orientation, _ int32, min, nat, minBase, natBase *int32) {
		*min, *nat, *minBase, *natBase = 1, 1, -1, -1
	}
	t.onAllocate = func(_ uintptr, w, h, _ int32) { a.allocateTiles(int(w), int(h)) }
	t.layout = gtk.NewCustomLayout(&t.onMode, &t.onMeasure, &t.onAllocate)
	a.tilesW.SetLayoutManager(&t.layout.LayoutManager)

	// Dragging a gap moves its split; the tiles' own gestures take the
	// drags that start on them.
	drag := gtk.NewGestureDrag()
	t.onDragBegin = func(g gtk.GestureDrag, x, y float64) {
		d := a.dividerAt(x, y)
		if d == nil {
			g.SetState(gtk.EventSequenceDeniedValue)
			return
		}
		t.dragging = d
		t.dragFrom = x
		if d.Node.Split == layout.Down {
			t.dragFrom = y
		}
	}
	t.onDragUpdate = func(_ gtk.GestureDrag, dx, dy float64) {
		d := t.dragging
		if d == nil {
			return
		}
		pos := t.dragFrom + dx
		if d.Node.Split == layout.Down {
			pos = t.dragFrom + dy
		}
		d.SetRatio(pos, gap)
		a.tilesW.QueueAllocate()
	}
	t.onDragEnd = func(gtk.GestureDrag, float64, float64) {
		d := t.dragging
		t.dragging = nil
		s := a.shown()
		if d == nil || s == nil {
			return
		}
		session, path, ratio := s.ID, d.Path, d.Node.Ratio
		a.ask("resize", func(c *host.Client) error {
			return c.Layout(session, host.OpRatio, host.Message{Path: path, Ratio: ratio})
		})
	}
	drag.ConnectDragBegin(&t.onDragBegin)
	drag.ConnectDragUpdate(&t.onDragUpdate)
	drag.ConnectDragEnd(&t.onDragEnd)
	a.tilesW.AddController(&drag.EventController)

	motion := gtk.NewEventControllerMotion()
	t.onMotion = func(_ gtk.EventControllerMotion, x, y float64) {
		cursor := ""
		if d := a.dividerAt(x, y); d != nil {
			cursor = "col-resize"
			if d.Node.Split == layout.Down {
				cursor = "row-resize"
			}
		}
		a.tilesW.SetCursorFromName(cursor)
	}
	motion.ConnectMotion(&t.onMotion)
	a.tilesW.AddController(&motion.EventController)
}

// area is where the tiles go in column 3.
func (a *App) area() layout.Rect {
	w, h := float64(a.tilesW.GetWidth()), float64(a.tilesW.GetHeight())
	return layout.Rect{X: gap, Y: gap, W: max(w-2*gap, 1), H: max(h-2*gap, 1)}
}

// dividerAt is the gap under x, y, a few pixels either side; nil for
// none.
func (a *App) dividerAt(x, y float64) *layout.Divider {
	t := &a.tiling
	if t.tree == nil || t.zoom != 0 {
		return nil
	}
	for _, d := range layout.Dividers(t.tree, a.area(), gap) {
		l := d.Line
		if x >= l.X-2 && x <= l.X+l.W+2 && y >= l.Y-2 && y <= l.Y+l.H+2 {
			return &d
		}
	}
	return nil
}

// allocateTiles places each tile where the tree puts it: the zoomed one
// over all of them.
func (a *App) allocateTiles(w, h int) {
	if a.wantFolds() != a.folds() {
		gtkx.Post(a.applyFolds) // not during the layout
	}
	t := &a.tiling
	r := layout.Rect{X: gap, Y: gap, W: max(float64(w)-2*gap, 1), H: max(float64(h)-2*gap, 1)}
	rects := layout.Rects(t.tree, r, gap)
	for id, tl := range a.tiles {
		rr, ok := rects[id]
		if t.zoom != 0 {
			ok = id == t.zoom
			rr = r
		}
		tl.box.SetChildVisible(ok)
		if !ok {
			continue
		}
		rect := gdk.Rectangle{X: int32(rr.X), Y: int32(rr.Y), Width: int32(math.Max(rr.W, 1)), Height: int32(math.Max(rr.H, 1))}
		tl.box.SizeAllocate((*gtk.Allocation)(unsafe.Pointer(&rect)), -1)
	}
}

// syncTiles makes the shown session's tiles: new panes get one, attached
// to the host; panes gone or no longer shown lose theirs.
func (a *App) syncTiles() {
	s := a.shown()
	want := map[int]bool{}
	if s != nil {
		for _, id := range s.Tree.Panes() {
			want[id] = true
		}
		a.tiling.tree, a.tiling.zoom = s.Tree.Clone(), s.Zoom
	} else {
		a.tiling.tree, a.tiling.zoom = nil, 0
	}
	for id := range a.tiles {
		if !want[id] {
			a.dropTile(id)
		}
	}
	for id := range want {
		if a.tiles[id] == nil {
			if err := a.addTile(id); err != nil {
				log.Printf("term: pane %d: %v", id, err)
			}
		}
	}
	for id, t := range a.tiles {
		t.update(a.info[id])
		if id == s.Focus {
			t.box.AddCssClass("focused")
		} else {
			t.box.RemoveCssClass("focused")
		}
	}
	a.tilesW.QueueAllocate()
	if s != nil {
		if t := a.tiles[s.Focus]; t != nil && a.sheet == nil && !t.pane.widget.HasFocus() && !t.pane.busy() {
			t.pane.widget.GrabFocus()
		}
	}
}

// addTile makes a pane's tile and attaches it: its replay comes first.
func (a *App) addTile(id int) error {
	info := a.info[id]
	p, err := newTermPane(paneBackend{a, id}, max(info.Cols, 2), max(info.Rows, 1), a.c.Colors, a.look())
	if err != nil {
		return err
	}
	p.id = id
	p.chord = a.chord
	p.focused = func() { a.focusPane(id) }
	t := &tile{id: id, pane: p}
	t.box = gtk.NewBox(gtk.OrientationVerticalValue, 0)
	t.box.AddCssClass("term-tile")
	t.box.SetCursorFromName("default")
	head := gtk.NewBox(gtk.OrientationHorizontalValue, 9)
	head.AddCssClass("term-head")
	t.mark = gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	t.mark.AddCssClass("mark")
	t.mark.SetValign(gtk.AlignCenterValue)
	head.Append(&t.mark.Widget)
	t.icon = gtk.NewImageFromIconName(iconOf(info))
	t.icon.SetPixelSize(14)
	t.icon.AddCssClass("term-kind")
	head.Append(&t.icon.Widget)
	t.title = gtk.NewLabel("")
	t.title.AddCssClass("term-head-title")
	t.title.SetEllipsize(3) // end
	t.title.SetXalign(0)
	t.title.SetHexpand(true)
	head.Append(&t.title.Widget)
	t.dir = gtk.NewLabel("")
	t.dir.AddCssClass("term-head-dir")
	t.dir.SetEllipsize(1) // start
	head.Append(&t.dir.Widget)
	t.state = gtk.NewLabel("")
	t.state.AddCssClass("term-state")
	head.Append(&t.state.Widget)
	t.box.Append(&head.Widget)
	t.box.Append(&p.root.Widget)
	t.box.SetParent(&a.tilesW.Widget)
	a.tiles[id] = t
	a.mu.Lock()
	a.live[id] = p
	a.mu.Unlock()
	t.update(info)
	a.ask("attach", func(c *host.Client) error { return c.Attach(id) })
	return nil
}

// dropTile detaches a pane and forgets its tile; the pane runs on.
func (a *App) dropTile(id int) {
	t := a.tiles[id]
	if t == nil {
		return
	}
	delete(a.tiles, id)
	a.mu.Lock()
	delete(a.live, id)
	a.mu.Unlock()
	if _, alive := a.info[id]; alive { // a closed pane is gone already
		a.ask("detach", func(c *host.Client) error { return c.Detach(id) })
	}
	t.pane.close()
	t.box.Unparent()
}

// update writes a pane's head.
func (t *tile) update(info host.PaneInfo) {
	t.title.SetText(paneTitle(info))
	t.dir.SetText(shortDir(info.Dir))
	t.icon.SetFromIconName(iconOf(info))
	paint(t.mark, t.state, lookOf(info))
}

// focusPane tells the host which pane has the focus, when the user moved
// it here.
func (a *App) focusPane(id int) {
	s := a.shown()
	if s == nil || s.Focus == id {
		return
	}
	for tid, t := range a.tiles {
		if tid == id {
			t.box.AddCssClass("focused")
		} else {
			t.box.RemoveCssClass("focused")
		}
	}
	s.Focus = id
	session := s.ID
	a.ask("focus", func(c *host.Client) error {
		return c.Layout(session, host.OpFocus, host.Message{Pane: id})
	})
}

// paneTitle is what a pane's head and row call it: an agent's title
// without its spinner, a shell's command running, else the title the
// program set, else the program.
func paneTitle(info host.PaneInfo) string {
	if info.Agent != "" {
		if t, _ := agent.Title(info.Title); t != "" {
			return t
		}
		return info.AgentName
	}
	if info.Command != "" {
		return info.Command
	}
	if info.Title != "" {
		return info.Title
	}
	if len(info.Argv) > 0 {
		return baseName(info.Argv[0])
	}
	return i18n.T("Terminal")
}
