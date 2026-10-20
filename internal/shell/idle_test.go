package shell

import (
	"strings"
	"testing"
)

// A hypridle config as people write it: the four timers by what they run,
// a general section, and a listener of the user's own.
const idleSample = `general {
    lock_cmd = inua call lock now
    before_sleep_cmd = loginctl lock-session      # lock before suspend
    after_sleep_cmd = hyprctl dispatch dpms on
}

# 2.5 min: dim the panel
listener {
    timeout = 150
    on-timeout = brightnessctl -s set 10%
    on-resume = brightnessctl -r
}

# 5 min: lock
listener {
    timeout = 300
    on-timeout = loginctl lock-session
}

listener {
    timeout = 330
    on-timeout = hyprctl dispatch dpms off
    on-resume = hyprctl dispatch dpms on
}

listener {
    timeout = 600
    on-timeout = notify-send "still there?"
}

listener {
    timeout = 1800
    on-timeout = systemctl suspend
}
`

func TestParseIdle(t *testing.T) {
	i := ParseIdle(idleSample)
	if i.Dim != 150 || i.Lock != 300 || i.Off != 330 || i.Suspend != 1800 {
		t.Errorf("timers %+v", i)
	}
	if !strings.Contains(i.General, "lock_cmd = inua call lock now") {
		t.Errorf("general %q", i.General)
	}
	if len(i.Others) != 1 || !strings.Contains(i.Others[0], "still there?") {
		t.Errorf("others %q", i.Others)
	}
	if got := ParseIdle(""); got.Dim != 0 || got.Lock != 0 || got.Off != 0 || got.Suspend != 0 {
		t.Errorf("no file: %+v", got)
	}
}

// What the page writes reads back as the same timers, with the user's own
// listener and general section kept; a timer turned off has no listener.
func TestIdleConfRoundTrip(t *testing.T) {
	i := ParseIdle(idleSample)
	i.Lock, i.Suspend = 600, 0
	back := ParseIdle(i.Conf())
	if back.Dim != 150 || back.Lock != 600 || back.Off != 330 || back.Suspend != 0 {
		t.Errorf("timers %+v from\n%s", back, i.Conf())
	}
	if back.General != i.General || len(back.Others) != 1 || back.Others[0] != i.Others[0] {
		t.Errorf("kept %q %q", back.General, back.Others)
	}
	if strings.Contains(i.Conf(), "systemctl suspend") {
		t.Error("a timer turned off still has its listener")
	}
	// No general section of its own: the page's, which locks with inua's.
	if !strings.Contains((Idle{Lock: 300}).Conf(), "lock_cmd = inua call lock now") {
		t.Error("no general section written")
	}
}

func TestIdleLabel(t *testing.T) {
	for s, want := range map[int]string{0: "Never", 60: "1 minute", 300: "5 minutes", 150: "2.5 minutes", 3600: "1 hour"} {
		if got := IdleLabel(s); got != want {
			t.Errorf("IdleLabel(%d) = %q, want %q", s, got, want)
		}
	}
}
