package shell

import (
	"fmt"
	"strings"
	"time"
)

// Ago is how long ago a notification arrived, short like iOS's: "now",
// "5m", "3h", "Yesterday", the weekday within a week, then the date.
func Ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour && t.Day() == now.Day():
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	y, m, day := now.Date()
	midnight := time.Date(y, m, day, 0, 0, 0, 0, now.Location())
	switch {
	case !t.Before(midnight.AddDate(0, 0, -1)):
		return "Yesterday"
	case !t.Before(midnight.AddDate(0, 0, -6)):
		return t.Format("Mon")
	case t.Year() == now.Year():
		return t.Format("Jan 2")
	}
	return t.Format("Jan 2, 2006")
}

// FocusTile is the subtitle of the control center's Focus tile. native
// means inua shows notifications itself; otherwise Focus needs mako.
func FocusTile(native, mako, on bool) string {
	switch {
	case !native && !mako:
		return "Needs notifications"
	case on:
		return "Banners quiet"
	}
	return "Off"
}

// OneParagraph joins a notification's lines into one paragraph, for a
// label with a line limit: the limit holds per paragraph, so text of many
// short lines would make the notification as tall as the text.
func OneParagraph(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r' || r == '\u2028' || r == '\u2029'
	}), " ")
}
