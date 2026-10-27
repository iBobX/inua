package ui

import (
	"math"
	"time"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gsk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
)

// bubble is the dock's drawer as a Dock folder opens on macOS: the card on
// glass of its own, a little way from the dock, with an arrow at the
// button it opened from. It grows out of the arrow's tip as it fades in,
// and shrinks back into it. The canvas fills the drawer's window and
// draws the glass, the arrow and the shadow round the card.
type bubble struct {
	canvas *gtk.Widget
	card   *gtk.Widget

	side       string  // the card's side the arrow is on: bottom, top, left or right
	tipX, tipY float64 // where the arrow points, in the window
	least      float64 // the least y of the card's top, under the bar
	radius     float64
	x, y, w, h float64 // the card's box
	glass      gdk.RGBA
	line       gdk.RGBA

	u       float64 // 0 hidden to 1 shown
	opening bool
	moving  bool
	last    time.Time
	tick    uint32
	gone    func()

	onTick     gtk.TickCallback
	onTickDone glib.DestroyNotify
}

const (
	bubbleArrow   = 10   // how far the arrow stands out from the card
	bubbleGap     = 5    // between the arrow's tip and the dock
	bubbleOpenMS  = 200  // growing out of the tip
	bubbleCloseMS = 140  // and back into it
	bubbleFrom    = 0.55 // the size it grows from
)

// newBubble puts card in a canvas, the drawer window's whole content.
func newBubble(card *gtk.Widget, side string, tipX, tipY, least, radius float64) *bubble {
	b := &bubble{card: card, side: side, tipX: tipX, tipY: tipY, least: least, radius: radius}
	b.canvas = gtkx.NewCanvas(&gtkx.Canvas{Snapshot: b.snapshot, Allocate: b.allocate})
	b.canvas.SetHexpand(true)
	b.canvas.SetVexpand(true)
	card.SetParent(b.canvas)
	b.onTick = func(uintptr, uintptr, uintptr) bool { return b.step() }
	b.onTickDone = func(uintptr) { b.tick = 0 }
	return b
}

// colors reads the dock's glass and border from the theme, once the
// canvas is in a window.
func (b *bubble) colors() {
	sc := b.canvas.GetStyleContext()
	if !sc.LookupColor("glass", &b.glass) {
		b.glass = gdk.RGBA{Red: 0.95, Green: 0.95, Blue: 0.96, Alpha: 0.92}
	}
	if !sc.LookupColor("line", &b.line) {
		b.line = gdk.RGBA{Red: 0.5, Green: 0.5, Blue: 0.55, Alpha: 0.5}
	}
	b.line.Alpha *= 0.9 // as the dock's: alpha(@line, 0.9)
}

// forget lets the canvas go with its window.
func (b *bubble) forget() { gtkx.ForgetCanvas(b.canvas) }

// open grows the bubble out of its tip; with animations off it is there
// at once.
func (b *bubble) open() {
	b.colors()
	b.opening = true
	if !gtk.SettingsGetDefault().GetPropertyGtkEnableAnimations() {
		b.u = 1
		b.canvas.QueueDraw()
		return
	}
	b.u = 0
	b.start()
}

// close shrinks it back into its tip and calls gone once it is.
func (b *bubble) close(gone func()) {
	b.opening = false
	b.gone = gone
	if !gtk.SettingsGetDefault().GetPropertyGtkEnableAnimations() || b.u <= 0 {
		b.gone = nil
		gone()
		return
	}
	b.start()
}

func (b *bubble) start() {
	b.moving = true
	b.last = time.Now()
	if b.tick == 0 {
		b.tick = b.canvas.AddTickCallback(&b.onTick, 0, &b.onTickDone)
	}
}

// step moves it on a frame; a slow frame moves it on one frame's time at
// most, so it never jumps.
func (b *bubble) step() bool {
	now := time.Now()
	dt := min(float64(now.Sub(b.last).Milliseconds()), 34)
	b.last = now
	if b.opening {
		b.u = min(1, b.u+dt/bubbleOpenMS)
	} else {
		b.u = max(0, b.u-dt/bubbleCloseMS)
	}
	b.canvas.QueueDraw()
	if b.opening && b.u >= 1 {
		b.moving = false
		return false
	}
	if !b.opening && b.u <= 0 {
		b.moving = false
		if gone := b.gone; gone != nil {
			b.gone = nil
			gone()
		}
		return false
	}
	return true
}

// allocate places the card at its natural size beside the arrow, inside
// the window.
func (b *bubble) allocate(_ *gtk.Widget, width, height int) {
	var w, h int32
	b.card.Measure(gtk.OrientationHorizontalValue, -1, nil, &w, nil, nil)
	b.card.Measure(gtk.OrientationVerticalValue, w, nil, &h, nil, nil)
	b.w, b.h = float64(w), float64(h)
	clamp := func(v, lo, hi float64) float64 { return math.Round(max(lo, min(v, hi))) }
	W, H := float64(width), float64(height)
	switch b.side {
	case "top":
		b.x = clamp(b.tipX-b.w/2, drawerGap, W-drawerGap-b.w)
		b.y = math.Round(b.tipY + bubbleArrow)
	case "left":
		b.x = math.Round(b.tipX + bubbleArrow)
		b.y = clamp(b.tipY-b.h/2, b.least, H-drawerGap-b.h)
	case "right":
		b.x = math.Round(b.tipX - bubbleArrow - b.w)
		b.y = clamp(b.tipY-b.h/2, b.least, H-drawerGap-b.h)
	default: // bottom
		b.x = clamp(b.tipX-b.w/2, drawerGap, W-drawerGap-b.w)
		b.y = math.Round(b.tipY - bubbleArrow - b.h)
	}
	t := gsk.NewTransform().Translate(&graphene.Point{X: float32(b.x), Y: float32(b.y)})
	b.card.Allocate(w, h, -1, t)
}

func (b *bubble) snapshot(_ *gtk.Widget, s *gtk.Snapshot) {
	if b.u <= 0 {
		return
	}
	e := 1 - math.Pow(1-b.u, 3) // quick, then settling
	k := float32(bubbleFrom + (1-bubbleFrom)*e)
	tip := graphene.Point{X: float32(b.tipX), Y: float32(b.tipY)}
	s.Save()
	s.Translate(&tip)
	s.Scale(k, k)
	s.Translate(&graphene.Point{X: -tip.X, Y: -tip.Y})
	s.PushOpacity(min(1, e*1.3))

	shadow := gdk.RGBA{Alpha: 0.22}
	s.AppendOutsetShadow(roundedRect(b.x, b.y, b.w, b.h, b.radius), &shadow, 0, 6, 0, 24)
	fill := b.path(0)
	s.AppendFill(fill, gsk.FillRuleWindingValue, &b.glass)
	fill.Unref()
	stroke := gsk.NewStroke(1)
	edge := b.path(0.5) // half a pixel in, so its pixels are whole
	s.AppendStroke(edge, stroke, &b.line)
	edge.Unref()
	stroke.Free()
	b.canvas.SnapshotChild(b.card, s)

	s.Pop()
	s.Restore()
}

// path is the card's rounded box with the arrow on its side, inset by in.
// It goes round clockwise from the top left corner; the arrow, on the
// straight of its side, points at the tip as nearly as that allows.
func (b *bubble) path(in float64) *gsk.Path {
	x, y, w, h := b.x+in, b.y+in, b.w-2*in, b.h-2*in
	r := max(0, b.radius-in)
	reach := float64(bubbleArrow) + bubbleJoin*(math.Sqrt2-1) // the arrow's half width at its base
	along := func(v, lo, hi float64) float64 { return max(lo+r+reach, min(v, hi-r-reach)) }
	pb := gsk.NewPathBuilder()
	pb.MoveTo(f32(x), f32(y+r))
	arc(pb, x+r, y+r, r, 180, 270)
	if b.side == "top" {
		at := along(b.tipX, x, x+w)
		caret(pb, func(u, v float64) (float64, float64) { return at + u, y - v })
	}
	arc(pb, x+w-r, y+r, r, 270, 360)
	if b.side == "right" {
		at := along(b.tipY, y, y+h)
		caret(pb, func(u, v float64) (float64, float64) { return x + w + v, at + u })
	}
	arc(pb, x+w-r, y+h-r, r, 0, 90)
	if b.side == "bottom" {
		at := along(b.tipX, x, x+w)
		caret(pb, func(u, v float64) (float64, float64) { return at - u, y + h + v })
	}
	arc(pb, x+r, y+h-r, r, 90, 180)
	if b.side == "left" {
		at := along(b.tipY, y, y+h)
		caret(pb, func(u, v float64) (float64, float64) { return x - v, at - u })
	}
	pb.Close()
	return pb.FreeToPath()
}

// bubbleJoin is the radius of the arcs that join the arrow to the card's
// edge, as the arcs join a panel to the bar; bubbleTip rounds its tip.
const (
	bubbleJoin = 12.0
	bubbleTip  = 3.0
)

// caret draws the arrow, going on along the card's edge: at, through to,
// u along the edge the way the outline goes and v out from it, with the
// arrow's middle at u 0. Its sides are at 45°; an arc on each joins it to
// the edge, and its tip is round.
func caret(pb *gsk.PathBuilder, at func(u, v float64) (float64, float64)) {
	a, f, t := float64(bubbleArrow), bubbleJoin, bubbleTip
	q := 1 / math.Sqrt2
	// A curve from p to e, leaving p along dp and reaching e along de,
	// a circle's arc that turns by turn degrees.
	bend := func(pu, pv, du, dv, eu, ev, eu2, ev2, r, turn float64) {
		k := 4.0 / 3 * math.Tan(turn*math.Pi/180/4) * r
		x0, y0 := at(pu, pv)
		x1, y1 := at(pu+k*du, pv+k*dv)
		x2, y2 := at(eu-k*eu2, ev-k*ev2)
		x3, y3 := at(eu, ev)
		pb.LineTo(f32(x0), f32(y0))
		pb.CubicTo(f32(x1), f32(y1), f32(x2), f32(y2), f32(x3), f32(y3))
	}
	// Into the arrow: off the edge onto its first side, which goes out
	// along (q, q).
	c := -a - f*(math.Sqrt2-1)
	bend(c, 0, 1, 0, c+f*q, f-f*q, q, q, f, 45)
	// Round the tip, from the first side onto the second, along (q, -q).
	tv := a - t*math.Sqrt2
	bend(-t*q, tv+t*q, q, q, t*q, tv+t*q, q, -q, t, 90)
	// Off the second side back onto the edge.
	c = a + f*(math.Sqrt2-1)
	bend(c-f*q, f-f*q, q, -q, c, 0, 1, 0, f, 45)
}

// roundedRect is a box with every corner rounded by r, for a shadow. As
// with graphene_rect_t (rect), puregotk declares GskRoundedRect with
// pointer fields; the C struct is twelve floats: the bounds, then each
// corner's width and height from the top left round.
func roundedRect(x, y, w, h, r float64) *gsk.RoundedRect {
	q := float32(r)
	v := new([12]float32)
	*v = [12]float32{float32(x), float32(y), float32(w), float32(h), q, q, q, q, q, q, q, q}
	return (*gsk.RoundedRect)(unsafe.Pointer(v))
}
