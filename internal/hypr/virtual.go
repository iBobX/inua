package hypr

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// VirtualScreen is the name of the headless output inua makes when no
// monitor is connected, so a computer without one still has a screen to
// share.
const VirtualScreen = "INUA-VIRTUAL"

// fallbackOutput is the placeholder Hyprland keeps while it has no
// output: nothing is drawn there for a client to capture.
const fallbackOutput = "FALLBACK"

var modeRe = regexp.MustCompile(`^([1-9][0-9]{2,4})x([1-9][0-9]{2,4})@([1-9][0-9]{1,2})$`)

// ParseMode reads a mode such as "1920x1080@60".
func ParseMode(mode string) (width, height, refresh int, ok bool) {
	m := modeRe.FindStringSubmatch(mode)
	if m == nil {
		return 0, 0, 0, false
	}
	width, _ = strconv.Atoi(m[1])
	height, _ = strconv.Atoi(m[2])
	refresh, _ = strconv.Atoi(m[3])
	return width, height, refresh, true
}

// SyncVirtualScreen makes Hyprland's outputs match the setting: with mode
// set ("1920x1080@60") and no other monitor on, the virtual screen is
// there at that mode; otherwise it is not. Monitors turned off do not
// count: on a laptop with its lid closed they are still connected.
// It changes nothing when they already match, so it can follow every
// monitor event. changed reports whether it added, removed or resized
// the screen.
func (c *Client) SyncVirtualScreen(ctx context.Context, mode string) (changed bool, err error) {
	var want struct{ w, h, r int }
	if mode != "" {
		var ok bool
		if want.w, want.h, want.r, ok = ParseMode(mode); !ok {
			return false, fmt.Errorf("virtual screen: mode %q: want WIDTHxHEIGHT@HZ", mode)
		}
	}
	reply, err := c.Request(ctx, "j/monitors all")
	if err != nil {
		return false, err
	}
	var monitors []struct {
		Name        string  `json:"name"`
		Width       int     `json:"width"`
		Height      int     `json:"height"`
		RefreshRate float64 `json:"refreshRate"`
		Disabled    bool    `json:"disabled"`
	}
	if err := json.Unmarshal(reply, &monitors); err != nil {
		return false, fmt.Errorf("hyprland monitors: %w", err)
	}
	other, have := false, false
	sized := false
	for _, m := range monitors {
		switch m.Name {
		case VirtualScreen:
			have = true
			sized = m.Width == want.w && m.Height == want.h && math.Round(m.RefreshRate) == float64(want.r)
		case fallbackOutput:
		default:
			// Disabled monitors are listed with "all" so that the virtual
			// screen is not made only because a laptop's lid is closed.
			other = true
		}
	}
	if mode == "" || other {
		if !have {
			return false, nil
		}
		return true, c.command(ctx, "output remove "+VirtualScreen)
	}
	if !have {
		if err := c.command(ctx, "output create headless "+VirtualScreen); err != nil {
			return false, err
		}
	} else if sized {
		return false, nil
	}
	// A rule by name: it holds for this output only, and a reload of
	// hyprland.conf drops it (the shell sets it again on configreloaded).
	return true, c.command(ctx, fmt.Sprintf("keyword monitor %s,%dx%d@%d,auto,1", VirtualScreen, want.w, want.h, want.r))
}

// command runs a request Hyprland answers "ok" to.
func (c *Client) command(ctx context.Context, cmd string) error {
	out, err := c.Request(ctx, cmd)
	if err != nil {
		return err
	}
	if reply := strings.TrimSpace(string(out)); reply != "ok" {
		return fmt.Errorf("hyprland %q: %s", cmd, reply)
	}
	return nil
}
