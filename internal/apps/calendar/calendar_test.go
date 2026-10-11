package calendar

import (
	"testing"
	"time"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.Local) }

func TestMonth(t *testing.T) {
	// October 2026 starts on a Thursday and ends on a Saturday.
	weeks := Month(2026, time.October, true, day(2026, 10, 10))
	if len(weeks) != 5 {
		t.Fatalf("weeks %d", len(weeks))
	}
	first := weeks[0]
	if !first.Days[0].Date.Equal(day(2026, 9, 28)) || first.Days[0].InMonth || !first.Days[3].InMonth {
		t.Errorf("first week %v", first.Days[0].Date)
	}
	if first.Number != 40 {
		t.Errorf("week number %d", first.Number)
	}
	if !weeks[1].Days[5].Today || weeks[1].Days[5].Date.Day() != 10 {
		t.Errorf("today should be Saturday the 10th of the second week")
	}
	sunday := Month(2026, time.October, false, day(2026, 10, 10))
	if sunday[0].Days[0].Date.Weekday() != time.Sunday || !sunday[0].Days[0].Date.Equal(day(2026, 9, 27)) {
		t.Errorf("Sunday first: %v", sunday[0].Days[0].Date)
	}
	// February 2026 starts on Sunday: with Sunday first, four full weeks.
	if n := len(Month(2026, time.February, false, time.Time{})); n != 4 {
		t.Errorf("February 2026, Sunday first: %d weeks", n)
	}
}

func TestDayInfo(t *testing.T) {
	today := time.Date(2026, 10, 10, 9, 0, 0, 0, time.Local)
	for _, c := range []struct {
		day        time.Time
		title, sub string
	}{
		{today, "SATURDAY, OCTOBER 10", "TODAY · WEEK 41 · DAY 283"},
		{today.AddDate(0, 0, 1), "SUNDAY, OCTOBER 11", "TOMORROW · WEEK 41 · DAY 284"},
		{today.AddDate(0, 0, -1), "FRIDAY, OCTOBER 9", "YESTERDAY · WEEK 41 · DAY 282"},
		{today.AddDate(0, 0, 4), "WEDNESDAY, OCTOBER 14", "IN 4 DAYS · WEEK 42 · DAY 287"},
		{today.AddDate(0, 0, -30), "THURSDAY, SEPTEMBER 10", "30 DAYS AGO · WEEK 37 · DAY 253"},
		{time.Date(2027, 1, 1, 0, 0, 0, 0, time.Local), "FRIDAY, JANUARY 1, 2027", "IN 83 DAYS · WEEK 53 · DAY 1"},
	} {
		title, sub := DayInfo(c.day, today)
		if title != c.title || sub != c.sub {
			t.Errorf("DayInfo(%v) = %q, %q; want %q, %q", c.day.Format("2006-01-02"), title, sub, c.title, c.sub)
		}
	}
}
