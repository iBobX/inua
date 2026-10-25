package gameweb

import "testing"

// Visitors are given this computer's addresses on the network, not its
// containers' bridges.
func TestBridges(t *testing.T) {
	for name, want := range map[string]bool{"docker0": true, "br-3f2a": true, "veth12": true, "virbr0": true, "wlan0": false, "enp3s0": false, "tailscale0": false} {
		if bridge(name) != want {
			t.Errorf("%s: bridge %v", name, !want)
		}
	}
}
