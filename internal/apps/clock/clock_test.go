package clock

import (
	"testing"
	"time"
)

func TestAlarmNext(t *testing.T) {
	loc := time.FixedZone("X", 9*3600)
	sat := time.Date(2026, 10, 10, 8, 0, 0, 0, loc) // a Saturday, 08:00
	once := Alarm{Hour: 7, Minute: 30}
	if got := once.Next(sat); !got.Equal(time.Date(2026, 10, 11, 7, 30, 0, 0, loc)) {
		t.Errorf("once, already past today: %v", got)
	}
	if got := (Alarm{Hour: 9}).Next(sat); !got.Equal(time.Date(2026, 10, 10, 9, 0, 0, 0, loc)) {
		t.Errorf("once, later today: %v", got)
	}
	weekdays := Alarm{Hour: 7, Days: [7]bool{false, true, true, true, true, true, false}}
	if got := weekdays.Next(sat); !got.Equal(time.Date(2026, 10, 12, 7, 0, 0, 0, loc)) {
		t.Errorf("weekdays from Saturday: %v, want Monday", got)
	}
	at := time.Date(2026, 10, 10, 9, 0, 0, 0, loc)
	if got := (Alarm{Hour: 9}).Next(at); !got.After(at) {
		t.Errorf("Next is exclusive: %v", got)
	}
}

func TestDue(t *testing.T) {
	loc := time.FixedZone("X", 0)
	alarms := []Alarm{
		{ID: "a", Hour: 7, On: true},
		{ID: "b", Hour: 7, On: false},
		{ID: "c", Hour: 8, On: true},
	}
	due := Due(alarms, time.Date(2026, 10, 10, 6, 59, 50, 0, loc), time.Date(2026, 10, 10, 7, 0, 10, 0, loc))
	if len(due) != 1 || due[0].ID != "a" {
		t.Errorf("due = %+v", due)
	}
}

func TestDaysLabel(t *testing.T) {
	for days, want := range map[[7]bool]string{
		{}: "Once",
		{true, true, true, true, true, true, true}:      "Every day",
		{false, true, true, true, true, true, false}:    "Weekdays",
		{true, false, false, false, false, false, true}: "Weekends",
		{false, true, false, true, false, false, false}: "Mon Wed",
	} {
		if got := DaysLabel(days); got != want {
			t.Errorf("DaysLabel(%v) = %q, want %q", days, got, want)
		}
	}
}

func TestZoneNames(t *testing.T) {
	if City("America/New_York") != "New York" || Region("America/Argentina/Salta") != "America · Argentina" {
		t.Error(City("America/New_York"), Region("America/Argentina/Salta"))
	}
	if !Match("America/New_York", "new y") || !Match("Asia/Tokyo", "asia") || Match("Asia/Tokyo", "york") {
		t.Error("Match")
	}
}

func TestDifference(t *testing.T) {
	here := time.FixedZone("H", 9*3600)
	now := time.Date(2026, 10, 10, 20, 0, 0, 0, here)
	for _, tc := range []struct {
		off  int
		want string
	}{
		{9 * 3600, "Today, same time"},
		{-4 * 3600, "Today, −13 h"},
		{(9*60 + 330) * 60, "Tomorrow, +5 h 30 min"},
		{-10 * 3600, "Today, −19 h"},
	} {
		if got := Difference(time.FixedZone("T", tc.off), now); got != tc.want {
			t.Errorf("offset %d: %q, want %q", tc.off, got, tc.want)
		}
	}
}

func TestDifferenceYesterday(t *testing.T) {
	now := time.Date(2026, 10, 10, 8, 0, 0, 0, time.FixedZone("H", 9*3600))
	if got := Difference(time.FixedZone("T", -10*3600), now); got != "Yesterday, −19 h" {
		t.Error(got)
	}
}

func TestFormats(t *testing.T) {
	if got := Stopwatch(83*time.Second + 456*time.Millisecond); got != "01:23.45" {
		t.Error(got)
	}
	if got := Stopwatch(time.Hour + 2*time.Second); got != "1:00:02.00" {
		t.Error(got)
	}
	if got := Countdown(299*time.Second + 100*time.Millisecond); got != "5:00" {
		t.Error(got)
	}
	if got := Countdown(3600 * time.Second); got != "1:00:00" {
		t.Error(got)
	}
	if got := Countdown(-time.Second); got != "0:00" {
		t.Error(got)
	}
}
