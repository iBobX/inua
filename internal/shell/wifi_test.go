package shell

import (
	"errors"
	"testing"

	"github.com/lyricat/inua/internal/system"
)

func TestWifiLists(t *testing.T) {
	l := system.WifiList{Networks: []system.WifiNetwork{
		{SSID: "Home", InRange: true, Saved: true, Connected: true},
		{SSID: "Cafe", InRange: true},
		{SSID: "Work", InRange: true, Saved: true},
		{SSID: "Attic", Saved: true},
	}}
	current, known, nearby := WifiLists(l)
	if current == nil || current.SSID != "Home" {
		t.Fatalf("current %+v", current)
	}
	if len(known) != 2 || known[0].SSID != "Work" || known[1].SSID != "Attic" {
		t.Errorf("known %+v", known)
	}
	if len(nearby) != 1 || nearby[0].SSID != "Cafe" {
		t.Errorf("nearby %+v", nearby)
	}
	if current, _, _ := WifiLists(system.WifiList{}); current != nil {
		t.Errorf("current without networks %+v", current)
	}
}

func TestWifiStatus(t *testing.T) {
	for _, c := range []struct {
		n    system.WifiNetwork
		busy string
		want string
	}{
		{system.WifiNetwork{Connected: true, InRange: true, Frequency: 5180, Security: "wpa"}, "", "Connected · 5 GHz · WPA2"},
		{system.WifiNetwork{Connected: true, InRange: true, Frequency: 2412}, WifiDisconnect, "Disconnecting…"},
		{system.WifiNetwork{Saved: true}, "", "Not in range"},
		{system.WifiNetwork{Saved: true, InRange: true, Security: "sae"}, "", "Saved · WPA3"},
		{system.WifiNetwork{InRange: true}, "", "Open"},
		{system.WifiNetwork{InRange: true, Frequency: 6115, Security: "enterprise"}, "", "Enterprise"},
	} {
		if got := WifiStatus(c.n, c.busy); got != c.want {
			t.Errorf("WifiStatus(%+v, %q) = %q, want %q", c.n, c.busy, got, c.want)
		}
	}
	if WifiBand(6115) != "6 GHz" || WifiBand(0) != "—" {
		t.Error("WifiBand")
	}
}

func TestWifiError(t *testing.T) {
	if got := WifiError(&system.WifiError{Reason: 7}); got != "The password was not accepted." {
		t.Errorf("wrong password: %q", got)
	}
	if got := WifiError(errors.New("boom")); got != "Not connected: boom" {
		t.Errorf("other: %q", got)
	}
}
