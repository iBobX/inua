package shell

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lyricat/inua/internal/i18n"
)

// The idle timers of Settings → Power are hypridle's: its config,
// ~/.config/hypr/hypridle.conf, holds a listener for each, and the page
// reads them from it and writes it again when one changes. A listener the
// page does not know, and the general section, are kept as they were.

// Idle are the four timers, in seconds; 0 is never.
type Idle struct {
	Dim, Lock, Off, Suspend int
	// General is the file's general section as written, "" for none (the
	// page's own is written then); Others are the listeners it does not
	// know, as written.
	General string
	Others  []string
}

// IdleDefaults are the timers of a file with none: dim at 2 minutes, lock
// at 5, the screens off at 6 and suspend at 30.
var IdleDefaults = Idle{Dim: 120, Lock: 300, Off: 360, Suspend: 1800}

// idleGeneral is the general section the page writes when the file has
// none: lock with inua's lock screen, and before a suspend.
const idleGeneral = `general {
    lock_cmd = inua call lock now
    before_sleep_cmd = loginctl lock-session
    after_sleep_cmd = hyprctl dispatch dpms on
}`

// The commands the page's listeners run.
const (
	idleDimOn    = "brightnessctl -s set 10%"
	idleDimOff   = "brightnessctl -r"
	idleLockOn   = "loginctl lock-session"
	idleScreenOn = "hyprctl dispatch dpms off"
	idleScreenUp = "hyprctl dispatch dpms on"
	idleSuspend  = "systemctl suspend"
)

// ParseIdle reads a hypridle config: its timers, by what each listener's
// on-timeout does, its general section and the listeners it does not know.
// A timer with no listener is 0 (never).
func ParseIdle(text string) Idle {
	var idle Idle
	for _, block := range idleBlocks(text) {
		name, body := block[0], block[1]
		switch name {
		case "general":
			idle.General = body
			continue
		case "listener":
		default:
			idle.Others = append(idle.Others, body)
			continue
		}
		timeout, cmd := idleField(body, "timeout"), idleField(body, "on-timeout")
		seconds, err := strconv.Atoi(timeout)
		if err != nil || seconds <= 0 {
			idle.Others = append(idle.Others, body)
			continue
		}
		switch {
		case strings.Contains(cmd, "brightnessctl"):
			idle.Dim = seconds
		case strings.Contains(cmd, "lock-session"), strings.Contains(cmd, "lock now"), strings.Contains(cmd, "hyprlock"):
			idle.Lock = seconds
		case strings.Contains(cmd, "dpms off"):
			idle.Off = seconds
		case strings.Contains(cmd, "suspend"), strings.Contains(cmd, "hibernate"):
			idle.Suspend = seconds
		default:
			idle.Others = append(idle.Others, body)
		}
	}
	return idle
}

// Conf is the config file for these timers: the general section, a
// listener for each timer that is on, and the listeners kept.
func (i Idle) Conf() string {
	var b strings.Builder
	b.WriteString("# Written by inua: Settings → Power. The timers are its own; another\n")
	b.WriteString("# listener, and the general section, are kept as they are.\n\n")
	general := i.General
	if general == "" {
		general = idleGeneral
	}
	b.WriteString(general + "\n")
	listener := func(what string, seconds int, on, off string) {
		if seconds <= 0 {
			return
		}
		fmt.Fprintf(&b, "\n# %s\nlistener {\n    timeout = %d\n    on-timeout = %s\n", what, seconds, on)
		if off != "" {
			fmt.Fprintf(&b, "    on-resume = %s\n", off)
		}
		b.WriteString("}\n")
	}
	listener("Dim the screen", i.Dim, idleDimOn, idleDimOff)
	listener("Lock", i.Lock, idleLockOn, "")
	listener("Screens off", i.Off, idleScreenOn, idleScreenUp)
	listener("Suspend", i.Suspend, idleSuspend, "")
	for _, o := range i.Others {
		b.WriteString("\n" + o + "\n")
	}
	return b.String()
}

// idleBlocks are a hyprlang file's top-level blocks, as name and the whole
// block's text: `listener { … }`.
func idleBlocks(text string) [][2]string {
	var out [][2]string
	depth, start, name := 0, -1, ""
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if c := strings.Index(t, "#"); c >= 0 {
			t = strings.TrimSpace(t[:c])
		}
		if depth == 0 && strings.HasSuffix(t, "{") {
			start, name = i, strings.TrimSpace(strings.TrimSuffix(t, "{"))
		}
		depth += strings.Count(t, "{") - strings.Count(t, "}")
		if depth == 0 && start >= 0 {
			out = append(out, [2]string{name, strings.Join(lines[start:i+1], "\n")})
			start = -1
		}
	}
	return out
}

// idleField is a key's value in a block: `timeout = 300`.
func idleField(block, key string) string {
	for _, line := range strings.Split(block, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == key {
			if c := strings.Index(v, "#"); c >= 0 {
				v = v[:c]
			}
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// IdleChoices are the times a timer is offered, in seconds; 0 is never.
var IdleChoices = []int{0, 60, 120, 180, 300, 600, 900, 1200, 1800, 2700, 3600}

// IdleLabel is a timer as the page words it: "Never", "5 minutes",
// "2.5 minutes", "1 hour".
func IdleLabel(seconds int) string {
	switch {
	case seconds <= 0:
		return i18n.T("Never")
	case seconds%3600 == 0:
		return i18n.Nf("%d hour", "%d hours", seconds/3600, seconds/3600)
	case seconds%60 == 0:
		return i18n.Nf("%d minute", "%d minutes", seconds/60, seconds/60)
	}
	return i18n.Tf("%s minutes", strconv.FormatFloat(float64(seconds)/60, 'f', -1, 64))
}
