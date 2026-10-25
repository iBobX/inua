package main

import "testing"

func TestSelection(t *testing.T) {
	list := "94558[HC>]firefox[HT>]Call — Mozilla Firefox[HE>]55d1c0a0[HA>]" +
		"94560[HC>]kitty[HT>]~/x[HE>]0x55d1c2b0[HA>]"
	cases := []struct {
		choice string
		token  bool
		want   string
	}{
		{"screen:DP-1", false, "[SELECTION]/screen:DP-1"},
		{"screen:eDP-1", true, "[SELECTION]r/screen:eDP-1"},
		{"window:0x55D1C0A0", false, "[SELECTION]/window:94558"},
		{"window:0x55d1c2b0", false, "[SELECTION]/window:94560"},
		{"region:DP-1@10,20,640,480", false, "[SELECTION]/region:DP-1@10,20,640,480"},
	}
	for _, c := range cases {
		got, err := Selection(c.choice, list, c.token)
		if err != nil || got != c.want {
			t.Errorf("Selection(%q) = %q, %v; want %q", c.choice, got, err, c.want)
		}
	}
	if _, err := Selection("window:0xdead", list, false); err == nil {
		t.Error("a window xdph did not list was answered")
	}
	if _, err := Selection("", list, false); err == nil {
		t.Error("no choice answered")
	}
}
