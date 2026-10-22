package termui

import (
	"log"
	"math"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/term/vt"
)

// wheelRows is how many rows a notch of the wheel scrolls.
const wheelRows = 3

// A touchpad's scroll goes on after the fingers leave it, slowing as
// GTK's scrolled views do: the speed it had falls by e^(-glideFriction·t)
// until it is under glideStop rows a second, or the scroll reaches an
// end; a new scroll, a key or a click stops it.
const (
	glideFriction = 4
	glideStop     = 1.5
)

// touchpadFactor is how far a touchpad's pixel scrolls, as GTK's
// scrolled views take it (Files, the settings): further than the
// fingers went.
const touchpadFactor = 2.5

// rowsOf is a scroll's dy (notches, or pixels from a touchpad) in rows.
func (p *termPane) rowsOf(c *gtk.EventControllerScroll, dy float64) float64 {
	if c.GetUnit() == gdk.ScrollUnitWheelValue {
		return dy * wheelRows
	}
	return dy * touchpadFactor / float64(p.ch)
}

// wheel scrolls by dy, the scroll controller's: a program taking the
// wheel gets a wheel event a notch, as other terminals send them, and one
// a row of a touchpad's scroll (a notch's three rows were a third of the
// scroll, Claude Code's and Codex's).
func (p *termPane) wheel(c *gtk.EventControllerScroll, dy float64) bool {
	if p.ch == 0 {
		return false
	}
	p.stopGlide()
	perEvent := 1.0
	if c.GetUnit() == gdk.ScrollUnitWheelValue {
		perEvent = wheelRows
	}
	p.scrollRows(p.rowsOf(c, dy), perEvent, c.GetCurrentEventState())
	return true
}

// glide goes on scrolling at vy (the controller's, a second) once the
// fingers left the touchpad.
func (p *termPane) glide(c *gtk.EventControllerScroll, vy float64) {
	if p.ch == 0 || c.GetUnit() == gdk.ScrollUnitWheelValue {
		return
	}
	p.glideVel = p.rowsOf(c, vy)
	if math.Abs(p.glideVel) < glideStop {
		return
	}
	p.glideLast = time.Now()
	if p.glideTick == 0 {
		p.onGlide = func(uintptr, uintptr, uintptr) bool {
			now := time.Now()
			dt := now.Sub(p.glideLast).Seconds()
			p.glideLast = now
			before := p.scroll.Offset
			p.scrollRows(p.glideVel*dt, 1, 0)
			p.glideVel *= math.Exp(-glideFriction * dt)
			// At an end of the scrollback (the alternate screen and a
			// program taking the wheel have none to stop at).
			stuck := !p.alt && !p.reporting(0) && p.scroll.Offset == before && p.wheelRows == 0
			if math.Abs(p.glideVel) < glideStop || stuck {
				p.glideTick = 0
				return false
			}
			return true
		}
		p.glideTick = p.widget.AddTickCallback(&p.onGlide, 0, &noDestroy)
	}
}

// stopGlide ends a scroll going on by itself.
func (p *termPane) stopGlide() {
	if p.glideTick != 0 {
		p.widget.RemoveTickCallback(p.glideTick)
		p.glideTick = 0
	}
	p.glideVel = 0
}

// scrollRows scrolls the scrollback by rows, a fraction kept for the
// next. On the alternate screen there is none: the program gets the
// arrow keys instead, as xterm's alternate scroll does, so less, man and
// vim scroll; a program tracking the mouse gets a wheel event every
// perEvent rows.
func (p *termPane) scrollRows(rows, perEvent float64, state gdk.ModifierType) {
	p.wheelRows += rows
	n := int(p.wheelRows)
	p.wheelRows -= float64(n)
	if n == 0 {
		return
	}
	if p.reporting(state) {
		p.wheelNotches += float64(n) / perEvent
		k := int(p.wheelNotches)
		p.wheelNotches -= float64(k)
		button := vt.WheelDown
		if k < 0 {
			button, k = vt.WheelUp, -k
		}
		for range k {
			p.report(vt.MousePress, button, state)
		}
		return
	}
	if p.alt {
		if p.exited {
			return
		}
		key := uint32(gdk.KEY_Down)
		if n < 0 {
			key, n = uint32(gdk.KEY_Up), -n
		}
		for range n {
			b, err := p.term.Key(vt.KeyEvent{Keyval: key, Action: vt.Press})
			if err != nil {
				log.Printf("term: wheel: %v", err)
				return
			}
			p.send(b)
		}
		return
	}
	p.scrollBy(vt.ScrollRows, n)
	return
}

// scrollKey scrolls for Shift+PgUp, PgDn, Home and End, and
// Ctrl+Shift+PgUp and PgDn (by prompts); the alternate screen has no
// scrollback, so the program gets those keys.
func (p *termPane) scrollKey(keyval uint32, chord gdk.ModifierType) bool {
	if p.alt {
		return false
	}
	page := max(p.rows-1, 1)
	switch {
	case chord == gdk.ShiftMaskValue && int32(keyval) == gdk.KEY_Page_Up:
		p.scrollBy(vt.ScrollRows, -page)
	case chord == gdk.ShiftMaskValue && int32(keyval) == gdk.KEY_Page_Down:
		p.scrollBy(vt.ScrollRows, page)
	case chord == gdk.ShiftMaskValue && int32(keyval) == gdk.KEY_Home:
		p.scrollBy(vt.ScrollTop, 0)
	case chord == gdk.ShiftMaskValue && int32(keyval) == gdk.KEY_End:
		p.scrollBy(vt.ScrollBottom, 0)
	case chord == gdk.ControlMaskValue|gdk.ShiftMaskValue && int32(keyval) == gdk.KEY_Page_Up:
		p.scrollBy(vt.ScrollPrompts, -1)
	case chord == gdk.ControlMaskValue|gdk.ShiftMaskValue && int32(keyval) == gdk.KEY_Page_Down:
		p.scrollBy(vt.ScrollPrompts, 1)
	default:
		return false
	}
	return true
}

func (p *termPane) scrollBy(kind vt.ScrollKind, n int) {
	p.term.Scroll(kind, n)
	p.update(false)
}

// The scrollbar, as GTK's overlay scrollbars: a thin thumb at the right
// edge while the viewport is away from the bottom, wider over a faint
// track while the pointer is on it (and shown then at the bottom too),
// taken by a drag; a press on the track jumps there and drags on. It
// takes the pointer barZone pixels from the edge, before a selection or a
// program tracking the mouse.
const (
	barZone      = 12
	barThin      = 3
	barWide      = 8
	barMinLength = 24
)

// hasBar says there is scrollback to scroll: not on the alternate screen.
func (p *termPane) hasBar() bool {
	return !p.alt && p.scroll.Total > p.scroll.Len
}

// inBar says x (the widget's) is on the scrollbar.
func (p *termPane) inBar(x float64) bool {
	return p.hasBar() && x >= float64(p.widget.GetWidth()-barZone)
}

// barHovered follows the pointer on and off the scrollbar.
func (p *termPane) barHovered(on bool) {
	if on == p.barHover {
		return
	}
	p.barHover = on
	switch {
	case on:
		p.widget.SetCursorFromName("default")
	case p.hover != nil:
		p.widget.SetCursorFromName("pointer")
	default:
		p.widget.SetCursorFromName("text")
	}
	p.widget.QueueDraw()
}

// barThumb is the track's height, and the thumb's top and length in it.
func (p *termPane) barThumb() (track, top, length float32) {
	sb := p.scroll
	track = float32(p.widget.GetHeight() - 2*pad)
	if sb.Total == 0 || track <= 0 {
		return track, 0, 0
	}
	length = min(max(track*float32(sb.Len)/float32(sb.Total), barMinLength), track)
	if room := sb.Total - sb.Len; room > 0 {
		top = (track - length) * float32(sb.Offset) / float32(room)
	}
	return track, top, length
}

// barPress takes the scrollbar at y: by the thumb where it was pressed,
// or by its middle after a jump there.
func (p *termPane) barPress(y float64) {
	p.stopGlide()
	_, top, length := p.barThumb()
	at := y - pad
	if at >= float64(top) && at <= float64(top+length) {
		p.barGrab = at - float64(top)
	} else {
		p.barGrab = float64(length) / 2
	}
	p.barDrag = true
	p.barTo(y)
	p.widget.QueueDraw()
}

// barTo scrolls so the thumb, as taken, is at y.
func (p *termPane) barTo(y float64) {
	track, _, length := p.barThumb()
	room := float64(track - length)
	sb := p.scroll
	if room <= 0 || sb.Total <= sb.Len {
		return
	}
	f := min(max((y-pad-p.barGrab)/room, 0), 1)
	if d := int(math.Round(f*float64(sb.Total-sb.Len))) - sb.Offset; d != 0 {
		p.scrollBy(vt.ScrollRows, d)
	}
}

func (p *termPane) drawScrollbar(w *gtk.Widget, s *gtk.Snapshot) {
	wide := p.barHover || p.barDrag
	if !p.hasBar() || (p.scroll.AtBottom() && !wide) {
		return
	}
	track, top, length := p.barThumb()
	width := float32(barThin)
	if wide {
		width = barWide
	}
	s.Save()
	s.Translate(&graphene.Point{X: float32(w.GetWidth()) - 2 - width, Y: pad})
	if wide {
		t := p.fg
		t.Alpha = 0.08
		s.AppendColor(&t, rect(0, 0, width, track))
	}
	c := p.fg
	c.Alpha = 0.35
	if p.barDrag {
		c.Alpha = 0.55
	}
	s.AppendColor(&c, rect(0, top, width, length))
	s.Restore()
}

// noDestroy is a destroy notify with nothing to do, for GTK's callbacks.
var noDestroy glib.DestroyNotify = func(uintptr) {}
