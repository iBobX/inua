package shell

import (
	"slices"
	"strings"
)

// The keyboard choices the settings window offers, as XKB options in
// Hyprland's input:kb_options. Other options in the list are kept.

// Swap Ctrl and Alt: "" (no), "left" or "both".
var swapOptions = map[string][]string{
	"left": {"ctrl:swap_lalt_lctl"},
	"both": {"ctrl:swap_lalt_lctl", "ctrl:swap_ralt_rctl"},
}

// Caps Lock: "" (Caps Lock), "escape" or "ctrl".
var capsOptions = map[string][]string{
	"escape": {"caps:escape"},
	"ctrl":   {"ctrl:nocaps"},
}

// SwapCtrlAlt reads the Ctrl/Alt swap from kb_options.
func SwapCtrlAlt(opts string) string { return choice(opts, swapOptions, isSwap) }

// SetSwapCtrlAlt returns kb_options with the Ctrl/Alt swap changed.
func SetSwapCtrlAlt(opts, v string) string { return setChoice(opts, v, swapOptions, isSwap) }

// CapsLock reads what Caps Lock does from kb_options.
func CapsLock(opts string) string { return choice(opts, capsOptions, isCaps) }

// SetCapsLock returns kb_options with what Caps Lock does changed.
func SetCapsLock(opts, v string) string { return setChoice(opts, v, capsOptions, isCaps) }

func isSwap(o string) bool { return o == "ctrl:swap_lalt_lctl" || o == "ctrl:swap_ralt_rctl" }

// isCaps matches every option that changes Caps Lock, so picking one drops
// the others.
func isCaps(o string) bool {
	return strings.HasPrefix(o, "caps:") || o == "ctrl:nocaps" || o == "ctrl:swapcaps"
}

func splitOptions(opts string) []string {
	var out []string
	for _, o := range strings.Split(opts, ",") {
		if o = strings.TrimSpace(o); o != "" {
			out = append(out, o)
		}
	}
	return out
}

// choice is the value whose options are exactly the group's options in
// opts, "" when there are none; an unknown combination reads as "".
func choice(opts string, values map[string][]string, inGroup func(string) bool) string {
	var have []string
	for _, o := range splitOptions(opts) {
		if inGroup(o) {
			have = append(have, o)
		}
	}
	slices.Sort(have)
	for v, want := range values {
		if slices.Equal(have, slices.Sorted(slices.Values(want))) {
			return v
		}
	}
	return ""
}

// setChoice replaces the group's options with v's; a choice that does not
// change keeps the list's order.
func setChoice(opts, v string, values map[string][]string, inGroup func(string) bool) string {
	out := splitOptions(opts)
	if choice(opts, values, inGroup) == v {
		return strings.Join(out, ",")
	}
	out = slices.DeleteFunc(out, inGroup)
	out = append(out, values[v]...)
	return strings.Join(out, ",")
}
