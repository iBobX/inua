package gamesui

import (
	"math"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// The board's camera: the whole board at zoom 1, closer up to zoomMax. The
// wheel or a pinch zooms about the pointer, a drag pans, and while zoomed
// in the camera follows the pawn on the move. view is the board's point,
// in canvas units, at the middle of the widget.
const (
	zoomMax    = 2.4
	zoomSettle = 300 * time.Millisecond // the zoom still, the board is drawn sharp again
)

type camera struct {
	zoom         float64
	view         games.Vec
	pointer      games.Vec // the last canvas point under the pointer
	panFrom      games.Vec
	pinchFrom    float64
	onScroll     func(gtk.EventControllerScroll, float64, float64) bool
	onDragBegin  func(gtk.GestureDrag, float64, float64)
	onDragUpdate func(gtk.GestureDrag, float64, float64)
	onPinchBegin func(gtk.Gesture, uintptr)
	onPinch      func(gtk.GestureZoom, float64)
	onZoomButton func(gtk.Button)
}

var boardMiddle = games.Vec{X: boardW / 2, Y: boardH / 2}

// look turns pn from canvas units to the camera's view.
func (c *camera) look(pn pen) {
	pn.Translate(boardMiddle.X, boardMiddle.Y)
	pn.Scale(c.zoom, c.zoom)
	pn.Translate(-c.view.X, -c.view.Y)
}

// seen is the canvas point (in the board's own projection) under the
// widget's canvas point s.
func (c *camera) seen(s games.Vec) games.Vec {
	return games.Vec{X: (s.X-boardMiddle.X)/c.zoom + c.view.X, Y: (s.Y-boardMiddle.Y)/c.zoom + c.view.Y}
}

// keep holds the view where the board fills it: no further out than the
// board's edges.
func (c *camera) keep() {
	hw, hh := boardMiddle.X/c.zoom, boardMiddle.Y/c.zoom
	c.view.X = max(hw, min(boardW-hw, c.view.X))
	c.view.Y = max(hh, min(boardH-hh, c.view.Y))
}

// zoomAt sets the zoom, keeping the board point under the widget's canvas
// point s where it is.
func (c *camera) zoomAt(z float64, s games.Vec) {
	z = max(1, min(zoomMax, z))
	at := c.seen(s)
	c.zoom = z
	c.view = games.Vec{X: at.X - (s.X-boardMiddle.X)/z, Y: at.Y - (s.Y-boardMiddle.Y)/z}
	c.keep()
}

// follow moves the view a step toward the point p, when zoomed in.
func (c *camera) follow(p games.Vec, dt float64) {
	if c.zoom < 1.05 {
		return
	}
	k := min(1, dt*5)
	c.view = games.Vec{X: c.view.X + (p.X-c.view.X)*k, Y: c.view.Y + (p.Y-c.view.Y)*k}
	c.keep()
}

// setupCamera wires the board's zoom and pan, and puts the zoom buttons
// over its corner; it returns the widget holding both.
func (p *fortune) setupCamera() *gtk.Widget {
	c := &p.cam
	c.zoom, c.view = 1, boardMiddle
	scroll := gtk.NewEventControllerScroll(gtk.EventControllerScrollVerticalValue)
	c.onScroll = func(_ gtk.EventControllerScroll, _, dy float64) bool {
		c.zoomAt(c.zoom*math.Pow(1.15, -dy), c.pointer)
		p.zoomed()
		return true
	}
	scroll.ConnectScroll(&c.onScroll)
	p.cv.area.AddController(&scroll.EventController)

	drag := gtk.NewGestureDrag()
	c.onDragBegin = func(gtk.GestureDrag, float64, float64) { c.panFrom = c.view }
	c.onDragUpdate = func(_ gtk.GestureDrag, dx, dy float64) {
		if c.zoom <= 1 || math.Hypot(dx, dy) < 4 {
			return
		}
		k := p.cv.scale * c.zoom
		c.view = games.Vec{X: c.panFrom.X - dx/k, Y: c.panFrom.Y - dy/k}
		c.keep()
		p.cv.redraw()
	}
	drag.ConnectDragBegin(&c.onDragBegin)
	drag.ConnectDragUpdate(&c.onDragUpdate)
	p.cv.area.AddController(&drag.EventController)

	pinch := gtk.NewGestureZoom()
	c.onPinchBegin = func(gtk.Gesture, uintptr) { c.pinchFrom = c.zoom }
	c.onPinch = func(_ gtk.GestureZoom, scale float64) {
		c.zoomAt(c.pinchFrom*scale, c.pointer)
		p.zoomed()
	}
	pinch.ConnectBegin(&c.onPinchBegin)
	pinch.ConnectScaleChanged(&c.onPinch)
	p.cv.area.AddController(&pinch.EventController)

	// The buttons: closer, further, the whole board.
	c.onZoomButton = func(b gtk.Button) {
		switch b.GetName() {
		case "in":
			c.zoomAt(c.zoom*1.35, boardMiddle)
		case "out":
			c.zoomAt(c.zoom/1.35, boardMiddle)
		default:
			c.zoomAt(1, boardMiddle)
		}
		p.zoomed()
	}
	box := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	box.AddCssClass("linked")
	box.AddCssClass("fortune-zoom")
	box.SetHalign(gtk.AlignEndValue)
	box.SetValign(gtk.AlignEndValue)
	for _, b := range [][3]string{{"in", "zoom-in-symbolic", i18n.T("Closer (+)")}, {"out", "zoom-out-symbolic", i18n.T("Further (−)")}, {"fit", "view-fullscreen-symbolic", i18n.T("The whole board (0)")}} {
		btn := gtk.NewButtonFromIconName(b[1])
		btn.SetName(b[0])
		btn.SetTooltipText(b[2])
		btn.SetFocusable(false)
		btn.ConnectClicked(&c.onZoomButton)
		gtkx.Append(box, &btn.Widget)
	}
	ov := gtk.NewOverlay()
	ov.SetChild(&p.cv.area.Widget)
	ov.AddOverlay(&box.Widget)
	box.Unref()
	return &ov.Widget
}

// zoomKey handles the camera's keys: + closer, − further, 0 the whole
// board.
func (p *fortune) zoomKey(k uint32) bool {
	c := &p.cam
	switch k {
	case '+', '=':
		c.zoomAt(c.zoom*1.35, boardMiddle)
	case '-', '_':
		c.zoomAt(c.zoom/1.35, boardMiddle)
	case '0':
		c.zoomAt(1, boardMiddle)
	default:
		return false
	}
	p.zoomed()
	return true
}

// zoomed draws the board again for a new zoom: stretched at once, sharp
// when the zoom has settled.
func (p *fortune) zoomed() {
	p.zoomedAt = time.Now()
	p.cv.redraw()
	time.AfterFunc(zoomSettle+20*time.Millisecond, func() { gtkx.Post(p.cv.redraw) })
}
