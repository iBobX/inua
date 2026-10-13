package ui

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/calendar"
	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/apps/weather"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/layershell"
)

// agenda is the calendar panel under the bar's clock. Like the control
// center, its window (agenda.ui) is built on every open and destroyed on
// close, and it covers the screen, the bar too, so a click anywhere else
// (the clock too) closes it. Under the month, the weather where the
// Weather app is set, from a forecast kept for a while between opens.
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

	weather                       gtk.Button
	wxIcon                        gtk.Image
	wxTemp, wxText, wxPlace, wxHL gtk.Label
	wxDays                        gtk.Box

	// The last forecast, for the place and unit it was read for.
	wx        weather.Forecast
	wxFor     weather.Place
	wxF       bool
	wxAt      time.Time
	wxLoading bool
	wxErr     error
}

// agendaWeatherAge is how long a forecast is shown without reading it
// again.
const agendaWeatherAge = 15 * time.Minute

// weatherApp is the Weather app's desktop ID.
const weatherApp = "io.github.lyricat.Inua.Weather"

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
	s.onAgendaWeather = func(gtk.Button) {
		s.closeAgenda()
		if e := s.dockState.apps.ByID(weatherApp); e != nil {
			s.launchApp(e)
		}
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
	s.agendaWeather()
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
	ui.Get("weather", &a.weather)
	ui.Get("weather_icon", &a.wxIcon)
	ui.Get("weather_temp", &a.wxTemp)
	ui.Get("weather_text", &a.wxText)
	ui.Get("weather_place", &a.wxPlace)
	ui.Get("weather_range", &a.wxHL)
	ui.Get("weather_days", &a.wxDays)
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
	a.weather.ConnectClicked(&s.onAgendaWeather)

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
		a.win.SetTitle(i18n.T("inua calendar"))
		a.win.SetDefaultSize(360, 420)
	}
}

// showAgenda draws today and the month shown, with week numbers.
func (s *shellUI) showAgenda() {
	a := &s.agenda
	now := time.Now()
	_, week := now.ISOWeek()
	a.today.SetText(i18n.Tf("%s · WK %d", strings.ToUpper(i18n.Time(now, "Mon Jan 2")), week))
	a.month.SetText(strings.ToUpper(i18n.Time(a.shown, "January 2006")))

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
	cell(i18n.T("WK"), 0, 0, "grid-head", "week")
	names := []string{i18n.T("Su"), i18n.T("Mo"), i18n.T("Tu"), i18n.T("We"), i18n.T("Th"), i18n.T("Fr"), i18n.T("Sa")}
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

// agendaWeather shows the weather where the Weather app is set: the last
// forecast at once, read again in the background when it is old.
func (s *shellUI) agendaWeather() {
	a := &s.agenda
	var data weather.Data
	if err := store.Load(weather.StoreName, &data); err != nil {
		log.Printf("calendar: weather: %v", err)
	}
	if len(data.Places) == 0 {
		a.wxFor = weather.Place{}
		s.showAgendaWeather(false)
		return
	}
	p := data.Places[max(0, min(data.Selected, len(data.Places)-1))]
	same := p == a.wxFor && data.Fahrenheit == a.wxF && !a.wxAt.IsZero()
	s.showAgendaWeather(true)
	if same && time.Since(a.wxAt) < agendaWeatherAge || a.wxLoading {
		return
	}
	if !same {
		a.wxAt = time.Time{} // another place: nothing to show meanwhile
		a.wxFor, a.wxF = p, data.Fahrenheit
		s.showAgendaWeather(true)
	}
	a.wxLoading = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		f, err := weather.Get(ctx, &http.Client{}, p, data.Fahrenheit)
		gtkx.Post(func() {
			a.wxLoading = false
			if p != a.wxFor {
				return // the place changed meanwhile
			}
			a.wxErr = err
			if err == nil {
				a.wx, a.wxAt = f, time.Now()
			} else {
				log.Printf("calendar: weather: %v", err)
			}
			if a.open && !a.closing {
				s.showAgendaWeather(true)
			}
		})
	}()
}

// showAgendaWeather fills the weather under the month: now and the next
// five days, a line while it is read, or how to add a place.
func (s *shellUI) showAgendaWeather(havePlace bool) {
	a := &s.agenda
	gtkx.Clear(&a.wxDays)
	a.wxDays.SetVisible(false)
	if !havePlace {
		a.wxIcon.SetFromIconName("weather-few-clouds-symbolic")
		a.wxTemp.SetText(i18n.T("Weather"))
		a.wxText.SetText(i18n.T("Add a place in Weather to see it here"))
		a.wxPlace.SetText("")
		a.wxHL.SetText("")
		return
	}
	a.wxPlace.SetText(strings.ToUpper(a.wxFor.Name))
	if a.wxAt.IsZero() {
		a.wxIcon.SetFromIconName("weather-few-clouds-symbolic")
		a.wxTemp.SetText("—")
		a.wxHL.SetText("")
		if a.wxErr != nil && !a.wxLoading {
			a.wxText.SetText(i18n.T("No forecast: offline?"))
		} else {
			a.wxText.SetText(i18n.T("Reading the forecast…"))
		}
		return
	}
	f := a.wx
	text, icon := weather.Describe(f.Now.Code, f.Now.Day)
	a.wxIcon.SetFromIconName(icon)
	a.wxTemp.SetText(weather.Temp(f.Now.Temp))
	a.wxText.SetText(i18n.Tf("%s · feels %s", text, weather.Temp(f.Now.Feels)))
	a.wxHL.SetText("")
	if len(f.Days) > 0 {
		a.wxHL.SetText(i18n.Tf("H %s  L %s", weather.Temp(f.Days[0].Max), weather.Temp(f.Days[0].Min)))
	}
	for _, d := range f.Days[min(1, len(f.Days)):min(6, len(f.Days))] {
		col := gtk.NewBox(gtk.OrientationVerticalValue, 3)
		col.AddCssClass("agenda-weather-day")
		name := gtk.NewLabel(strings.ToUpper(i18n.Time(d.Date, "Mon")))
		name.AddCssClass("grid-head")
		gtkx.Append(col, &name.Widget)
		_, icon := weather.Describe(d.Code, true)
		img := gtk.NewImageFromIconName(icon)
		img.SetPixelSize(16)
		gtkx.Append(col, &img.Widget)
		t := gtk.NewLabel(fmt.Sprintf("%s %s", weather.Temp(d.Max), weather.Temp(d.Min)))
		t.AddCssClass("agenda-weather-text")
		gtkx.Append(col, &t.Widget)
		gtkx.Append(&a.wxDays, &col.Widget)
		a.wxDays.SetVisible(true)
	}
}
