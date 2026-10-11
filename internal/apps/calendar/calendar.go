// Package calendar is the Calendar app's logic: a month's grid with week
// numbers, and what to say about a day.
package calendar

import (
	"fmt"
	"strings"
	"time"
)

// StoreName is the app's file in the store.
const StoreName = "calendar"

// Data is what the Calendar app keeps.
type Data struct {
	// MondayFirst starts weeks on Monday, else Sunday.
	MondayFirst bool `json:"monday_first"`
}

// Day is one cell of a month's grid.
type Day struct {
	Date    time.Time
	InMonth bool // in the month shown, not the days around it
	Today   bool
}

// Week is a row of the grid with its ISO week number.
type Week struct {
	Number int
	Days   [7]Day
}

// Month lays out a month in weeks, from the week holding the 1st to the
// week holding the last day; today marks today.
func Month(year int, month time.Month, mondayFirst bool, today time.Time) []Week {
	first := time.Date(year, month, 1, 0, 0, 0, 0, time.Local)
	offset := int(first.Weekday())
	if mondayFirst {
		offset = (offset + 6) % 7
	}
	start := first.AddDate(0, 0, -offset)
	ty, tm, td := today.Date()
	var weeks []Week
	for d := start; ; {
		var w Week
		for i := range 7 {
			y, m, dd := d.Date()
			w.Days[i] = Day{Date: d, InMonth: m == month, Today: y == ty && m == tm && dd == td}
			d = d.AddDate(0, 0, 1)
		}
		// The week's number is that of its Thursday (ISO 8601).
		thu := w.Days[0].Date
		for thu.Weekday() != time.Thursday {
			thu = thu.AddDate(0, 0, 1)
		}
		_, w.Number = thu.ISOWeek()
		weeks = append(weeks, w)
		if d.Month() != month {
			break
		}
	}
	return weeks
}

// DayInfo says where a day is from today, and its week and day of the
// year: "WEDNESDAY, OCTOBER 14", "IN 4 DAYS · WEEK 42 · DAY 287".
func DayInfo(day, today time.Time) (title, sub string) {
	title = strings.ToUpper(day.Format("Monday, January 2"))
	if day.Year() != today.Year() {
		title += day.Format(", 2006")
	}
	d0 := time.Date(today.Year(), today.Month(), today.Day(), 12, 0, 0, 0, time.UTC)
	d1 := time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, time.UTC)
	n := int(d1.Sub(d0).Hours() / 24)
	var when string
	switch {
	case n == 0:
		when = "TODAY"
	case n == 1:
		when = "TOMORROW"
	case n == -1:
		when = "YESTERDAY"
	case n > 1:
		when = fmt.Sprintf("IN %d DAYS", n)
	default:
		when = fmt.Sprintf("%d DAYS AGO", -n)
	}
	_, week := day.ISOWeek()
	return title, fmt.Sprintf("%s · WEEK %d · DAY %d", when, week, day.YearDay())
}
