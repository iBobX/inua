package hyprconf

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/lyricat/inua/internal/config"
)

// Action is something a shortcut can run (config.ShortcutActions).
type Action struct {
	ID, Label string
	Shot      bool // a screenshot; the settings window lists them apart
}

// Actions are the shell's actions, in the order the settings window shows
// them.
var Actions = []Action{
	{ID: "launcher", Label: "Launcher"},
	{ID: "windows", Label: "Switch windows"},
	{ID: "overview", Label: "Workspace overview"},
	{ID: "control", Label: "Control center"},
	{ID: "notifications", Label: "Notification center"},
	{ID: "focus", Label: "Focus on or off"},
	{ID: "settings", Label: "Settings"},
	{ID: "restart", Label: "Restart the shell"},
	{ID: "lock", Label: "Lock the screen"},
	{ID: "shot-area", Label: "Take a screenshot", Shot: true},
	{ID: "shot-screen", Label: "The whole screen, at once", Shot: true},
	{ID: "shot-window", Label: "The focused window, at once", Shot: true},
	{ID: "shot-record", Label: "Record the screen (again: stop)", Shot: true},
	{ID: "color", Label: "Pick a colour", Shot: true},
}

// Command is the shell command behind an action.
func Command(c config.Config, action string) string {
	switch action {
	case "launcher":
		return "inua call launcher toggle"
	case "windows":
		return "inua call launcher windows"
	case "overview":
		return "inua call overview toggle"
	case "control":
		return "inua call control toggle"
	case "notifications":
		return "inua call notifications toggle"
	case "focus":
		return "inua call notifications focus"
	case "settings":
		return "inua call settings toggle"
	case "restart":
		return "inua call shell restart"
	case "lock":
		return c.Control.Lock
	case "shot-area":
		return "inua call screenshot area"
	case "shot-screen":
		return "inua call screenshot screen"
	case "shot-window":
		return "inua call screenshot window"
	case "shot-record":
		return "inua call screenshot record"
	case "color":
		return "inua call screenshot color"
	}
	return ""
}

// ShortcutCommand is what a shortcut runs.
func ShortcutCommand(c config.Config, sc config.Shortcut) string {
	if sc.Action == "exec" {
		return sc.Command
	}
	return Command(c, sc.Action)
}

// RecordSubmap is a Hyprland submap with no shortcuts: while the settings
// window records keys, the compositor passes every key to it. SUPER+CTRL+
// ALT+Escape leaves it should inua fail to.
const RecordSubmap = "inua-record"

// Modifier order in key strings ("SUPER+CTRL+ALT+SHIFT+key").
var modOrder = []string{"SUPER", "CTRL", "ALT", "SHIFT"}

// Keys builds a key string from modifiers and an XKB key name. A single
// letter is upper case, as Hyprland configs write them.
func Keys(super, ctrl, alt, shift bool, key string) string {
	var parts []string
	for i, on := range []bool{super, ctrl, alt, shift} {
		if on {
			parts = append(parts, modOrder[i])
		}
	}
	if len(key) == 1 {
		key = strings.ToUpper(key)
	}
	return strings.Join(append(parts, key), "+")
}

// splitKeys parses "SUPER+SHIFT+S" into Hyprland's "SUPER SHIFT" and "S".
func splitKeys(keys string) (mods, key string) {
	parts := strings.Split(keys, "+")
	return strings.Join(parts[:len(parts)-1], " "), parts[len(parts)-1]
}

// Label is a key string to show: "Super Shift S".
func Label(keys string) string {
	if keys == "" {
		return ""
	}
	parts := strings.Split(keys, "+")
	for i, p := range parts[:len(parts)-1] {
		parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}
	k := parts[len(parts)-1]
	if len(k) > 1 && strings.ToLower(k) == k {
		k = strings.ToUpper(k[:1]) + k[1:] // "space" → "Space"
	}
	parts[len(parts)-1] = k
	return strings.Join(parts, " ")
}

// SameKeys compares key strings, ignoring the case of the key name.
func SameKeys(a, b string) bool { return strings.EqualFold(a, b) }

// Bind is a shortcut Hyprland has (hyprctl binds).
type Bind struct {
	Keys       string // as Keys builds them
	Dispatcher string
	Arg        string
	Submap     string
}

// ParseBinds reads a j/binds reply. Mouse binds are skipped.
func ParseBinds(reply []byte) ([]Bind, error) {
	var raw []struct {
		Modmask    int    `json:"modmask"`
		Key        string `json:"key"`
		Mouse      bool   `json:"mouse"`
		Dispatcher string `json:"dispatcher"`
		Arg        string `json:"arg"`
		Submap     string `json:"submap"`
	}
	if err := json.Unmarshal(reply, &raw); err != nil {
		return nil, err
	}
	var binds []Bind
	for _, r := range raw {
		if r.Mouse || r.Key == "" {
			continue
		}
		m := r.Modmask
		binds = append(binds, Bind{
			Keys:       Keys(m&64 != 0, m&4 != 0, m&8 != 0, m&1 != 0, r.Key),
			Dispatcher: r.Dispatcher,
			Arg:        r.Arg,
			Submap:     r.Submap,
		})
	}
	return binds, nil
}

// KeysFor lists the keys of the binds that run command, outside submaps.
func KeysFor(binds []Bind, command string) []string {
	var keys []string
	norm := strings.Join(strings.Fields(command), " ")
	for _, b := range binds {
		if b.Submap == "" && b.Dispatcher == "exec" && strings.Join(strings.Fields(b.Arg), " ") == norm &&
			!slices.ContainsFunc(keys, func(k string) bool { return SameKeys(k, b.Keys) }) {
			keys = append(keys, b.Keys)
		}
	}
	return keys
}

// renderShortcuts writes the shortcuts: unbinds first (the keys an action
// had in hyprland.conf, and every key inua binds, whatever else it did),
// then the binds, then the recording submap.
func renderShortcuts(b *strings.Builder, c config.Config) {
	var unbind []string
	for _, sc := range c.Shortcuts {
		for _, k := range append(slices.Clone(sc.Replaces), sc.Keys) {
			if k != "" && !slices.ContainsFunc(unbind, func(u string) bool { return SameKeys(u, k) }) {
				unbind = append(unbind, k)
			}
		}
	}
	if len(c.Shortcuts) > 0 {
		b.WriteString("\n# Shortcuts from inua's settings window.\n")
	}
	for _, k := range unbind {
		mods, key := splitKeys(k)
		fmt.Fprintf(b, "unbind = %s, %s\n", mods, key)
	}
	for _, sc := range c.Shortcuts {
		cmd := ShortcutCommand(c, sc)
		if sc.Keys == "" || cmd == "" {
			continue
		}
		mods, key := splitKeys(sc.Keys)
		// Hyprland reads # as a comment; ## is a literal #.
		fmt.Fprintf(b, "bind = %s, %s, exec, %s\n", mods, key, strings.ReplaceAll(cmd, "#", "##"))
	}
	fmt.Fprintf(b, "\n# While inua's settings window records a shortcut, no other is active.\nsubmap = %s\nbind = SUPER CTRL ALT, Escape, submap, reset\nsubmap = reset\n", RecordSubmap)
}
