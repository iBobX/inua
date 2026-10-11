package shell

import "testing"

func TestKeyboardChoices(t *testing.T) {
	for _, c := range []struct{ opts, swap, caps string }{
		{"", "", ""},
		{"caps:escape", "", "escape"},
		{"ctrl:swap_lalt_lctl,caps:escape", "left", "escape"},
		{"ctrl:swap_ralt_rctl, ctrl:swap_lalt_lctl,ctrl:nocaps", "both", "ctrl"},
		{"ctrl:swap_ralt_rctl", "", ""}, // right only: not a choice offered
		{"caps:escape,ctrl:nocaps", "", ""},
	} {
		if got := SwapCtrlAlt(c.opts); got != c.swap {
			t.Errorf("SwapCtrlAlt(%q) = %q, want %q", c.opts, got, c.swap)
		}
		if got := CapsLock(c.opts); got != c.caps {
			t.Errorf("CapsLock(%q) = %q, want %q", c.opts, got, c.caps)
		}
	}
	for _, c := range []struct{ opts, swap, caps, want string }{
		// Other options are kept; the group's are replaced.
		{"grp:alt_shift_toggle,caps:escape", "left", "escape", "grp:alt_shift_toggle,caps:escape,ctrl:swap_lalt_lctl"},
		{"ctrl:swap_lalt_lctl,caps:escape", "both", "escape", "caps:escape,ctrl:swap_lalt_lctl,ctrl:swap_ralt_rctl"},
		{"ctrl:swap_lalt_lctl,ctrl:swap_ralt_rctl,caps:escape", "", "escape", "caps:escape"},
		{"caps:escape_shifted_capslock", "", "ctrl", "ctrl:nocaps"},
		{"caps:escape", "", "", ""},
	} {
		got := SetCapsLock(SetSwapCtrlAlt(c.opts, c.swap), c.caps)
		if got != c.want {
			t.Errorf("set %q swap=%q caps=%q: got %q, want %q", c.opts, c.swap, c.caps, got, c.want)
		}
	}
}
