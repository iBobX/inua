package i18n

import (
	"strings"
	"time"
)

var months = []string{M("January"), M("February"), M("March"), M("April"), M("May"), M("June"), M("July"), M("August"), M("September"), M("October"), M("November"), M("December")}
var shortMonths = []string{M("Jan"), M("Feb"), M("Mar"), M("Apr"), M("May"), M("Jun"), M("Jul"), M("Aug"), M("Sep"), M("Oct"), M("Nov"), M("Dec")}
var weekdays = []string{M("Sunday"), M("Monday"), M("Tuesday"), M("Wednesday"), M("Thursday"), M("Friday"), M("Saturday")}
var shortWeekdays = []string{M("Sun"), M("Mon"), M("Tue"), M("Wed"), M("Thu"), M("Fri"), M("Sat")}

// Time formats a translated Go time layout, including localized month and
// weekday names. Numeric layouts used in filenames and protocols use time.Format.
func Time(t time.Time, layout string) string {
	switch layout {
	case "January", "Jan", "Monday", "Mon", "PM", "pm":
	default:
		layout = T(layout)
	}
	layout = strings.NewReplacer("January", "\x01", "Jan", "\x02", "Monday", "\x03", "Mon", "\x04", "PM", "\x05", "pm", "\x06").Replace(layout)
	period := T("AM")
	if t.Hour() >= 12 {
		period = T("PM")
	}
	return strings.NewReplacer("\x01", T(months[t.Month()-1]), "\x02", T(shortMonths[t.Month()-1]), "\x03", T(weekdays[t.Weekday()]), "\x04", T(shortWeekdays[t.Weekday()]), "\x05", period, "\x06", strings.ToLower(period)).Replace(t.Format(layout))
}
