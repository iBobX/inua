package gamesui

import (
	"math"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/games"
)

// The board's camera: the board is larger than the window, which looks at
// part of it, one canvas unit to a unit of the widget. A drag or the wheel
// moves the view about, the arrow keys too, and Home brings it back to
// the player's pawn; a pawn on the move near the view's edge is followed.
// view is the board's point, in canvas units, at the middle of the widget.
type camera struct {
	view         games.Vec
	panFrom      games.Vec
	dragging     bool
	onScroll     func(gtk.EventControllerScroll, float64, float64) bool
	onDragBegin  func(gtk.GestureDrag, float64, float64)
	onDragUpdate func(gtk.GestureDrag, float64, float64)
	onDragEnd    func(gtk.GestureDrag, float64, float64)
	size         func() games.Vec // the widget's size
}

const (
	followMargin = 170.0 // how near the view's edge a moving pawn may come
	panStep      = 90.0  // an arrow key's move
)

var boardMiddle = games.Vec{X: boardW / 2, Y: boardH / 2}

// half is half the widget's size.
func (c *camera) half() games.Vec {
	s := c.size()
	return games.Vec{X: s.X / 2, Y: s.Y / 2}
}

// look turns pn from canvas units to the camera's view, whole pixels apart
// so the still layer is laid pixel for pixel.
func (c *camera) look(pn pen) {
	h, k := c.half(), pn.DeviceScale()
	pn.Translate(math.Round((h.X-c.view.X)*k)/k, math.Round((h.Y-c.view.Y)*k)/k)
}

// seen is the canvas point under the widget's point s.
func (c *camera) seen(s games.Vec) games.Vec {
	h := c.half()
	return games.Vec{X: s.X - h.X + c.view.X, Y: s.Y - h.Y + c.view.Y}
}

// keep holds the view on the board: no further out than its edges, or in
// its middle when the widget is larger than it; below its foot as far as
// the hand of cards over it stands, so nothing need hide behind them.
func (c *camera) keep() {
	h := c.half()
	clamp := func(v, half, size float64) float64 {
		if 2*half >= size {
			return size / 2
		}
		return max(half, min(size-half, v))
	}
	c.view = games.Vec{X: clamp(c.view.X, h.X, boardW), Y: clamp(c.view.Y, h.Y, boardH+handRoom)}
}

// handRoom is the room under the board for the hand of cards.
const handRoom = cardH + 30

// pan moves the view by d canvas units.
func (c *camera) pan(d games.Vec) {
	c.view = games.Vec{X: c.view.X + d.X, Y: c.view.Y + d.Y}
	c.keep()
}

// follow moves the view a step toward the point p when p is near its edge,
// unless the player is dragging it.
func (c *camera) follow(p games.Vec, dt float64) {
	if c.dragging {
		return
	}
	h, k := c.half(), min(1, dt*5)
	over := func(d, half float64) float64 {
		lim := max(0, half-followMargin)
		switch {
		case d > lim:
			return (d - lim) * k
		case d < -lim:
			return (d + lim) * k
		}
		return 0
	}
	c.pan(games.Vec{X: over(p.X-c.view.X, h.X), Y: over(p.Y-c.view.Y, h.Y)})
}

// setupCamera wires the board's drag and wheel; it returns the widget to
// lay out.
func (p *fortune) setupCamera() *gtk.Widget {
	c := &p.cam
	c.view = boardMiddle
	c.size = func() games.Vec { return games.Vec{X: p.cv.w, Y: p.cv.h} }
	scroll := gtk.NewEventControllerScroll(gtk.EventControllerScrollBothAxesValue | gtk.EventControllerScrollKineticValue)
	c.onScroll = func(s gtk.EventControllerScroll, dx, dy float64) bool {
		k := 1.0 // a touchpad's scroll is in pixels; a wheel's in notches
		if s.GetUnit() == gdk.ScrollUnitWheelValue {
			k = 60
		}
		c.pan(games.Vec{X: dx * k, Y: dy * k})
		p.cv.redraw()
		return true
	}
	scroll.ConnectScroll(&c.onScroll)
	p.cv.area.AddController(&scroll.EventController)

	drag := gtk.NewGestureDrag()
	c.onDragBegin = func(gtk.GestureDrag, float64, float64) { c.panFrom = c.view }
	c.onDragUpdate = func(_ gtk.GestureDrag, dx, dy float64) {
		if !c.dragging && math.Hypot(dx, dy) < 4 {
			return
		}
		c.dragging = true
		c.view = games.Vec{X: c.panFrom.X - dx, Y: c.panFrom.Y - dy}
		c.keep()
		p.cv.redraw()
	}
	c.onDragEnd = func(gtk.GestureDrag, float64, float64) { c.dragging = false }
	drag.ConnectDragBegin(&c.onDragBegin)
	drag.ConnectDragUpdate(&c.onDragUpdate)
	drag.ConnectDragEnd(&c.onDragEnd)
	p.cv.area.AddController(&drag.EventController)
	return &p.cv.area.Widget
}

// panKey handles the camera's keys: the arrows move the view, Home brings
// it back to the player's pawn.
func (p *fortune) panKey(k uint32) bool {
	c := &p.cam
	switch k {
	case keyLeft:
		c.pan(games.Vec{X: -panStep})
	case keyRight:
		c.pan(games.Vec{X: panStep})
	case keyUp:
		c.pan(games.Vec{Y: -panStep})
	case keyDown:
		c.pan(games.Vec{Y: panStep})
	case keyHome:
		p.lookAt(0)
	default:
		return false
	}
	p.cv.redraw()
	return true
}

// lookAt brings the view to player pi's pawn.
func (p *fortune) lookAt(pi int) {
	at, _ := p.token(pi)
	p.cam.view = project(at.X, at.Y, 0)
	p.cam.keep()
}
