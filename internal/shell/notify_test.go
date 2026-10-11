package shell

import (
	"testing"
	"time"
)

func TestAgo(t *testing.T) {
	now := time.Date(2026, 10, 9, 15, 30, 0, 0, time.Local) // a Friday
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{now.Add(-20 * time.Second), "now"},
		{now.Add(-5 * time.Minute), "5m"},
		{now.Add(-3 * time.Hour), "3h"},
		{time.Date(2026, 10, 8, 23, 0, 0, 0, time.Local), "Yesterday"},
		{time.Date(2026, 10, 6, 9, 0, 0, 0, time.Local), "Tue"},
		{time.Date(2026, 9, 1, 9, 0, 0, 0, time.Local), "Sep 1"},
		{time.Date(2025, 12, 31, 9, 0, 0, 0, time.Local), "Dec 31, 2025"},
	} {
		if got := Ago(c.t, now); got != c.want {
			t.Errorf("Ago(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestOneParagraph(t *testing.T) {
	for in, want := range map[string]string{
		"one line":               "one line",
		"a\nb\r\nc":              "a b c",
		"a\n\n\nb":               "a b",
		"\n<b>x</b>\n":           "<b>x</b>",
		"para\u2028graph\u2029s": "para graph s",
		"":                       "",
	} {
		if got := OneParagraph(in); got != want {
			t.Errorf("OneParagraph(%q) = %q, want %q", in, got, want)
		}
	}
}
