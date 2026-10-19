package termui

import (
	"log"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/term/vt"
)

// wheelRows is how many rows a notch of the wheel scrolls.
const wheelRows = 3

// wheel scrolls the scrollback by dy (notches, or pixels from a
// touchpad). On the alternate screen there is none: the program gets
// the arrow keys instead, as xterm's alternate scroll does, so less,
// man and vim scroll.
func (p *termPane) wheel(c *gtk.EventControllerScroll, dy float64) bool {
	if p.ch == 0 {
		return false
	}
	rows := dy * wheelRows
	if c.GetUnit() != gdk.ScrollUnitWheelValue {
		rows = dy / float64(p.ch)
	}
	p.wheelRows += rows
	n := int(p.wheelRows)
	p.wheelRows -= float64(n)
	if n == 0 {
		return true
	}
	if p.reporting(c.GetCurrentEventState()) {
		// A notch is one wheel button for the program, as other
		// terminals send it.
		p.wheelNotches += float64(n) / wheelRows
		k := int(p.wheelNotches)
		p.wheelNotches -= float64(k)
		button := vt.WheelDown
		if k < 0 {
			button, k = vt.WheelUp, -k
		}
		for range k {
			p.report(vt.MousePress, button, c.GetCurrentEventState())
		}
		return true
	}
	if p.alt {
		if p.exited {
			return true
		}
		key := uint32(gdk.KEY_Down)
		if n < 0 {
			key, n = uint32(gdk.KEY_Up), -n
		}
		for range n {
			b, err := p.term.Key(vt.KeyEvent{Keyval: key, Action: vt.Press})
			if err != nil {
				log.Printf("term: wheel: %v", err)
				return true
			}
			p.send(b)
		}
		return true
	}
	p.scrollBy(vt.ScrollRows, n)
	return true
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

// drawScrollbar marks where the viewport is while it is away from the
// bottom: a thin thumb at the right edge.
func (p *termPane) drawScrollbar(w *gtk.Widget, s *gtk.Snapshot) {
	sb := p.scroll
	if sb.AtBottom() || sb.Total <= sb.Len || sb.Total == 0 {
		return
	}
	h := float32(w.GetHeight() - 2*pad)
	top := h * float32(sb.Offset) / float32(sb.Total)
	length := max(h*float32(sb.Len)/float32(sb.Total), 24)
	top = min(top, h-length)
	c := p.fg
	c.Alpha = 0.35
	s.Save()
	s.Translate(&graphene.Point{X: float32(w.GetWidth() - 5), Y: pad})
	s.AppendColor(&c, rect(0, top, 3, length))
	s.Restore()
}
