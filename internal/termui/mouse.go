package termui

import (
	"log"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/term/vt"
)

// Programs that track the mouse (htop, vim with mouse=a, agents' TUIs)
// get its buttons, motion and wheel, encoded in the mode and format they
// asked for. Shift held keeps the pointer inua-term's: select, paste.

// trackMouse adds the controllers: motion keeps where the pointer is, in
// the widget's pixels (a raw event's position is the window's), and the
// raw buttons go to the program before the selection gestures see them.
func (p *termPane) trackMouse() {
	motion := gtk.NewEventControllerMotion()
	p.onMotion = func(c gtk.EventControllerMotion, x, y float64) {
		p.pointerX, p.pointerY = x, y
		if !p.barDrag {
			p.barHovered(p.inBar(x))
		}
		p.hoverLink(c.GetCurrentEventState()&gdk.ControlMaskValue != 0)
		if p.reporting(0) && !p.barDrag {
			p.report(vt.MouseMotion, p.held(), 0)
		}
	}
	p.onLeave = func(gtk.EventControllerMotion) {
		if !p.barDrag {
			p.barHovered(false)
		}
	}
	motion.ConnectMotion(&p.onMotion)
	motion.ConnectLeave(&p.onLeave)
	p.widget.AddController(&motion.EventController)

	raw := gtk.NewEventControllerLegacy()
	raw.SetPropagationPhase(gtk.PhaseCaptureValue)
	p.onRaw = func(_ gtk.EventControllerLegacy, ev uintptr) bool {
		e := gdk.EventNewFromInternalPtr(ev)
		kind := e.GetEventType()
		if kind != gdk.ButtonPressValue && kind != gdk.ButtonReleaseValue {
			return false
		}
		state := e.GetModifierState()
		// Ctrl+click opens a link, before selecting or the program.
		if kind == gdk.ButtonPressValue && state&gdk.ControlMaskValue != 0 && gdk.ButtonEventNewFromInternalPtr(ev).GetButton() == 1 {
			p.hoverLink(true)
			if p.hover != nil {
				p.openLink(p.hover.URI)
				return true
			}
		}
		// The scrollbar takes its own presses, from a program tracking the
		// mouse too.
		if p.barDrag || (kind == gdk.ButtonPressValue && p.inBar(p.pointerX)) {
			return false
		}
		if !p.reporting(state) {
			return false
		}
		button := buttonOf(gdk.ButtonEventNewFromInternalPtr(ev).GetButton())
		if button == vt.ButtonNone {
			return false
		}
		bit := 1 << button
		if kind == gdk.ButtonPressValue {
			p.widget.GrabFocus()
			p.buttons |= bit
			p.report(vt.MousePress, button, state)
		} else {
			p.buttons &^= bit
			p.report(vt.MouseRelease, button, state)
		}
		return true
	}
	raw.ConnectEvent(&p.onRaw)
	p.widget.AddController(&raw.EventController)
}

// hoverLink underlines the link under the pointer while Ctrl is held,
// with the hand for a cursor.
func (p *termPane) hoverLink(ctrl bool) {
	var hover *link
	if ctrl && p.cw > 0 && p.ch > 0 {
		x, y := int(p.pointerX-pad)/p.cw, int(p.pointerY-pad)/p.ch
		if x >= 0 && y >= 0 && x < p.cols && y < p.rows {
			if l, ok := p.term.LinkAt(x, y); ok {
				hover = &link{Link: l, Y: y}
			}
		}
	}
	if (hover == nil) == (p.hover == nil) && (hover == nil || *hover == *p.hover) {
		return
	}
	p.hover = hover
	if hover != nil {
		p.widget.SetCursorFromName("pointer")
	} else {
		p.widget.SetCursorFromName("text")
	}
	p.widget.QueueDraw()
}

// link is a link under the pointer, on row Y.
type link struct {
	vt.Link
	Y int
}

// openLink opens a URI with the program the desktop has for it.
func (p *termPane) openLink(uri string) {
	l := gtk.NewUriLauncher(uri)
	var win *gtk.Window
	if root := p.widget.GetRoot(); root != nil {
		win = &gtk.Window{}
		win.Ptr = root.GoPointer()
	}
	l.Launch(win, nil, nil, 0)
}

// reporting says the pointer is the program's now: it tracks the mouse,
// and Shift is not held.
func (p *termPane) reporting(state gdk.ModifierType) bool {
	return p.mouse && !p.exited && state&gdk.ShiftMaskValue == 0
}

// held is a button held down, for motion while dragging; ButtonNone for
// none.
func (p *termPane) held() int {
	for _, b := range []int{vt.ButtonLeft, vt.ButtonMiddle, vt.ButtonRight} {
		if p.buttons&(1<<b) != 0 {
			return b
		}
	}
	return vt.ButtonNone
}

// buttonOf is GDK's button number as the encoder's.
func buttonOf(b uint32) int {
	switch b {
	case 1:
		return vt.ButtonLeft
	case 2:
		return vt.ButtonMiddle
	case 3:
		return vt.ButtonRight
	}
	return vt.ButtonNone
}

// report sends the program a mouse event at the pointer.
func (p *termPane) report(action vt.MouseAction, button int, state gdk.ModifierType) {
	if p.cw == 0 || p.ch == 0 {
		return
	}
	b, err := p.term.Mouse(vt.MouseEvent{
		Action: action, Button: button, Mods: mods(state),
		X: p.pointerX, Y: p.pointerY,
		Width: int(p.widget.GetWidth()), Height: int(p.widget.GetHeight()),
		CellW: p.cw, CellH: p.ch, Pad: pad,
		Pressed: p.buttons != 0,
	})
	if err != nil {
		log.Printf("term: mouse: %v", err)
		return
	}
	p.send(b)
}
