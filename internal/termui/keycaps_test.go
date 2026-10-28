package termui

import (
	"runtime"
	"testing"
)

// The keys sheet writes the fixed keys from the modifiers of the system:
// Ctrl and Alt on Linux, ⌘ and ⌥ on a Mac.
func TestKeycaps(t *testing.T) {
	want := map[string]string{
		"ctrl+shift+t":                "Ctrl Shift T",
		"ctrl+alt+←↑→↓":               "Ctrl Alt ←↑→↓",
		"super+equal":                 "Super =",
		"shift+page_up":               "Shift PgUp",
		modNames(sessionMods) + "1…8": "Alt 1…8",
	}
	if runtime.GOOS == "darwin" {
		want = map[string]string{
			"ctrl+shift+t":                "⌃⇧T",
			"ctrl+alt+←↑→↓":               "⌃⌥←↑→↓",
			"super+equal":                 "⌘=",
			"shift+page_up":               "⇧PgUp",
			modNames(sessionMods) + "1…8": "⌘1…8",
		}
	}
	for chord, w := range want {
		if got := keycaps(chord); got != w {
			t.Errorf("keycaps(%q) = %q, want %q", chord, got, w)
		}
	}
	if fixedKeys[0].keys != keycaps(modNames(focusMods)+"←↑→↓") || modNames(focusMods) == "" {
		t.Errorf("focus keys %q", fixedKeys[0].keys)
	}
}
