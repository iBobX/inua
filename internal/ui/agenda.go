package ui

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/calendar"
	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/layershell"
)

// agenda is the calendar panel under the bar's clock. Like the control
// center, its window (agenda.ui) is built on every open and destroyed on
// close, and it covers the screen, the bar too, so a click anywhere else
// (the clock too) closes it.
type agenda struct {
	open    bool
	closing bool

	win      gtk.ApplicationWindow
	backdrop gtk.Box
	reveal   gtk.Revealer
	refs     gtkx.Scope

	today, month gtk.Label
	grid         gtk.Grid

	shown       time.Time // the first of the month shown
	mondayFirst bool      // as set in the Calendar app
}

func (s *shellUI) bindAgendaHandlers() {
	s.onClockButton = func(gtk.Button) { s.toggleAgenda() }
	s.onAgendaButton = func(b gtk.Button) {
		a := &s.agenda
		switch b.GetName() {
		case "prev":
			a.shown = a.shown.AddDate(0, -1, 0)
		case "next":
			a.shown = a.shown.AddDate(0, 1, 0)
		case "today":
			a.shown = firstOfMonth(time.Now())
		}
		s.showAgenda()
	}
	s.onAgendaPressed = func(g gtk.GestureClick, _ int32, x, y float64) {
		a := &s.agenda
		hit := a.backdrop.Pick(x, y, gtk.PickDefaultValue)
		if hit == nil {
			return
		}
		outside := hit.GoPointer() == a.backdrop.GoPointer()
		hit.Unref()
		if outside {
			s.closeAgenda()
		}
	}
	s.onAgendaKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if keyval == keyEscape {
			s.closeAgenda()
			return true
		}
		return false
	}
	s.onAgendaGone = func(uintptr) bool {
		a := &s.agenda
		a.win.Destroy()
		a.refs.Release()
		a.open, a.closing = false, false
		return false
	}
}

func (s *shellUI) toggleAgenda() {
	if s.agenda.open {
		s.closeAgenda()
	} else {
		s.openAgenda()
	}
}

func (s *shellUI) openAgenda() {
	a := &s.agenda
	if a.open {
		return
	}
	s.closeControl()
	s.closeCenter()
	var data calendar.Data
	if err := store.Load(calendar.StoreName, &data); err != nil {
		log.Printf("calendar: %v", err)
	}
	a.mondayFirst = data.MondayFirst
	a.shown = firstOfMonth(time.Now())
	s.buildAgenda()
	a.open = true
	s.showAgenda()
	a.win.Present()
	a.reveal.SetRevealChild(true) // after Present, so it slides in
	s.refocusPointer()
	for _, b := range s.bars {
		b.clockButton.AddCssClass("open")
	}
}

func (s *shellUI) closeAgenda() {
	a := &s.agenda
	if !a.open || a.closing {
		return
	}
	a.closing = true
	a.reveal.SetRevealChild(false)
	for _, b := range s.bars {
		b.clockButton.RemoveCssClass("open")
	}
	glib.TimeoutAdd(revealMS, &s.onAgendaGone, 0)
}

func (s *shellUI) buildAgenda() {
	a := &s.agenda
	ui := s.load("agenda.ui", &a.refs)
	ui.Get("agenda", &a.win)
	ui.Get("backdrop", &a.backdrop)
	ui.Get("reveal", &a.reveal)
	ui.Get("today", &a.today)
	ui.Get("month", &a.month)
	ui.Get("grid", &a.grid)
	var prev, next, today gtk.Button
	ui.Get("prev", &prev)
	ui.Get("next", &next)
	ui.Get("back_today", &today)
	ui.Close()

	prev.SetName("prev")
	next.SetName("next")
	today.SetName("today")
	for _, b := range []*gtk.Button{&prev, &next, &today} {
		b.ConnectClicked(&s.onAgendaButton)
	}

	click := gtk.NewGestureClick()
	click.ConnectPressed(&s.onAgendaPressed)
	a.backdrop.AddController(&click.EventController)
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue)
	keys.ConnectKeyPressed(&s.onAgendaKey)
	a.win.AddController(&keys.EventController)

	a.win.SetApplication(s.app)
	w := a.win.GoPointer()
	if s.layer {
		layershell.InitForWindow(w)
		layershell.SetNamespace(w, "inua-calendar")
		layershell.SetLayer(w, layershell.LayerOverlay)
		layershell.SetKeyboardMode(w, layershell.KeyboardExclusive)
		for _, e := range []layershell.Edge{layershell.EdgeTop, layershell.EdgeBottom, layershell.EdgeLeft, layershell.EdgeRight} {
			layershell.SetAnchor(w, e, true)
		}
		layershell.SetExclusiveZone(w, -1) // cover the bar too
		if mon := s.focusedMonitor(); mon != nil {
			layershell.SetMonitor(w, mon.GoPointer())
		}
	} else {
		a.win.SetTitle("inua calendar")
		a.win.SetDefaultSize(360, 420)
	}
}

// showAgenda draws today and the month shown, with week numbers.
func (s *shellUI) showAgenda() {
	a := &s.agenda
	now := time.Now()
	_, week := now.ISOWeek()
	a.today.SetText(fmt.Sprintf("%s · WK %d", strings.ToUpper(now.Format("Mon Jan 2")), week))
	a.month.SetText(strings.ToUpper(a.shown.Format("January 2006")))

	for ch := a.grid.GetFirstChild(); ch != nil; ch = a.grid.GetFirstChild() {
		a.grid.Remove(ch)
	}
	cell := func(text string, col, row int, classes ...string) {
		l := gtk.NewLabel(text)
		for _, c := range classes {
			l.AddCssClass(c)
		}
		a.grid.Attach(&l.Widget, int32(col), int32(row), 1, 1)
		l.Unref()
	}
	cell("WK", 0, 0, "grid-head", "week")
	names := []string{"S", "M", "T", "W", "T", "F", "S"}
	if a.mondayFirst {
		names = append(names[1:], names[0])
	}
	for i, n := range names {
		cell(n, i+1, 0, "grid-head")
	}
	for r, w := range calendar.Month(a.shown.Year(), a.shown.Month(), a.mondayFirst, now) {
		cell(strconv.Itoa(w.Number), 0, r+1, "week")
		for i, d := range w.Days {
			classes := []string{"agenda-day"}
			if !d.InMonth {
				classes = append(classes, "outside")
			}
			if d.Today {
				classes = append(classes, "today")
			}
			cell(strconv.Itoa(d.Date.Day()), i+1, r+1, classes...)
		}
	}
}

func firstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.Local)
}
