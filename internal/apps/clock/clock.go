// Package clock is the Clock app's logic: world clocks, alarms, the
// stopwatch and the timer. The shell rings the alarms, so they go off with
// the app closed; both read and write the same file (store "clock").
package clock

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"time"
)

// StoreName is the file the app and the shell share.
const StoreName = "clock"

// Data is what the Clock app keeps.
type Data struct {
	Zones  []string `json:"zones"` // world clocks, IANA names
	Alarms []Alarm  `json:"alarms"`
}

// Alarm goes off at Hour:Minute local time, on the days set (Sunday
// first), or once when none is.
type Alarm struct {
	ID     string  `json:"id"`
	Hour   int     `json:"hour"`
	Minute int     `json:"minute"`
	Label  string  `json:"label"`
	Days   [7]bool `json:"days"`
	On     bool    `json:"on"`
}

// Once reports whether the alarm has no days, so it turns off after it
// goes off.
func (a Alarm) Once() bool { return !slices.Contains(a.Days[:], true) }

// Next is the first time after t (exclusive) the alarm goes off, ignoring
// whether it is on.
func (a Alarm) Next(t time.Time) time.Time {
	day := time.Date(t.Year(), t.Month(), t.Day(), a.Hour, a.Minute, 0, 0, t.Location())
	for i := 0; i < 8; i++ {
		at := day.AddDate(0, 0, i)
		if at.After(t) && (a.Once() || a.Days[at.Weekday()]) {
			return at
		}
	}
	return time.Time{} // unreachable: some day in a week matches
}

// Due lists the alarms that are on and went off in (since, now].
func Due(alarms []Alarm, since, now time.Time) []Alarm {
	var due []Alarm
	for _, a := range alarms {
		if a.On && !a.Next(since).After(now) {
			due = append(due, a)
		}
	}
	return due
}

// DaysLabel describes an alarm's days: "Once", "Every day", "Weekdays",
// "Weekends", or the days' short names.
func DaysLabel(days [7]bool) string {
	names := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	var on []string
	for i, d := range days {
		if d {
			on = append(on, names[i])
		}
	}
	switch {
	case len(on) == 0:
		return "Once"
	case len(on) == 7:
		return "Every day"
	case len(on) == 5 && !days[0] && !days[6]:
		return "Weekdays"
	case len(on) == 2 && days[0] && days[6]:
		return "Weekends"
	}
	return strings.Join(on, " ")
}

// City is a time zone's city: "America/New_York" → "New York".
func City(zone string) string {
	i := strings.LastIndex(zone, "/")
	return strings.ReplaceAll(zone[i+1:], "_", " ")
}

// Region is the rest of a time zone's name: "America/Argentina/Salta" →
// "America · Argentina".
func Region(zone string) string {
	i := strings.LastIndex(zone, "/")
	if i < 0 {
		return ""
	}
	return strings.ReplaceAll(strings.ReplaceAll(zone[:i], "/", " · "), "_", " ")
}

// Difference describes a zone's time against local time at t: "Today,
// +7 h", "Tomorrow, +13 h 30 min", "Same time".
func Difference(loc *time.Location, t time.Time) string {
	there := t.In(loc)
	_, off := there.Zone()
	_, here := t.Zone()
	d := time.Duration(off-here) * time.Second
	day := "Today"
	ty, tm, td := t.Date()
	y, m, dd := there.Date()
	switch c := time.Date(y, m, dd, 0, 0, 0, 0, time.UTC).Compare(time.Date(ty, tm, td, 0, 0, 0, 0, time.UTC)); {
	case c > 0:
		day = "Tomorrow"
	case c < 0:
		day = "Yesterday"
	}
	if d == 0 {
		return day + ", same time"
	}
	sign := "+"
	if d < 0 {
		sign, d = "−", -d
	}
	h, min := int(d.Hours()), int(d.Minutes())%60
	if min == 0 {
		return fmt.Sprintf("%s, %s%d h", day, sign, h)
	}
	return fmt.Sprintf("%s, %s%d h %d min", day, sign, h, min)
}

// Zones lists the time zones to choose from, from the system's tzdata
// (zone1970.tab), sorted by city.
func Zones() []string {
	f, err := os.Open("/usr/share/zoneinfo/zone1970.tab")
	if err != nil {
		return []string{"UTC"}
	}
	defer f.Close()
	var zones []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		if fields := strings.Split(line, "\t"); len(fields) >= 3 {
			zones = append(zones, fields[2])
		}
	}
	slices.SortFunc(zones, func(a, b string) int { return strings.Compare(City(a), City(b)) })
	return zones
}

// Match reports whether a zone matches a search: every word of the query
// starts a word of the city or region.
func Match(zone, query string) bool {
	hay := strings.Fields(strings.ToLower(City(zone) + " " + strings.NewReplacer("/", " ", "_", " ").Replace(zone)))
	for _, q := range strings.Fields(strings.ToLower(query)) {
		if !slices.ContainsFunc(hay, func(w string) bool { return strings.HasPrefix(w, q) }) {
			return false
		}
	}
	return true
}

// Stopwatch shows elapsed time as "MM:SS.cc", with hours once there are.
func Stopwatch(d time.Duration) string {
	cs := int(d / (10 * time.Millisecond))
	h, m, s, c := cs/360000, cs/6000%60, cs/100%60, cs%100
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d.%02d", h, m, s, c)
	}
	return fmt.Sprintf("%02d:%02d.%02d", m, s, c)
}

// Countdown shows a timer's remaining time, rounded up to whole seconds:
// "4:59", "1:00:00".
func Countdown(d time.Duration) string {
	s := int(math.Ceil(max(0, d.Seconds())))
	if h := s / 3600; h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
