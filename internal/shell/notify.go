package shell

import (
	"strings"
	"time"

	"github.com/lyricat/inua/internal/i18n"
)

// Ago is how long ago a notification arrived, short like iOS's: "now",
// "5m", "3h", "Yesterday", the weekday within a week, then the date.
func Ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return i18n.T("now")
	case d < time.Hour:
		return i18n.Tf("%dm", int(d.Minutes()))
	case d < 24*time.Hour && t.Day() == now.Day():
		return i18n.Tf("%dh", int(d.Hours()))
	}
	y, m, day := now.Date()
	midnight := time.Date(y, m, day, 0, 0, 0, 0, now.Location())
	switch {
	case !t.Before(midnight.AddDate(0, 0, -1)):
		return i18n.T("Yesterday")
	case !t.Before(midnight.AddDate(0, 0, -6)):
		return i18n.Time(t, "Mon")
	case t.Year() == now.Year():
		return i18n.Time(t, "Jan 2")
	}
	return i18n.Time(t, "Jan 2, 2006")
}

// FocusTile is the subtitle of the control center's Focus tile. native
// means inua shows notifications itself; otherwise Focus needs mako.
func FocusTile(native, mako, on bool) string {
	switch {
	case !native && !mako:
		return i18n.T("Needs notifications")
	case on:
		return i18n.T("Banners quiet")
	}
	return i18n.T("Off")
}

// OneParagraph joins a notification's lines into one paragraph, for a
// label with a line limit: the limit holds per paragraph, so text of many
// short lines would make the notification as tall as the text.
func OneParagraph(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r' || r == '\u2028' || r == '\u2029'
	}), " ")
}
