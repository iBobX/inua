package shell

import (
	"testing"

	"github.com/lyricat/inua/internal/system"
)

func TestBluetoothLists(t *testing.T) {
	st := system.BluetoothState{Devices: []system.BluetoothDevice{
		{Path: "a", Name: "Keyboard", Paired: true},
		{Path: "b", Name: ""},
		{Path: "c", Name: "Phone"},
	}}
	mine, nearby := BluetoothLists(st)
	if len(mine) != 1 || mine[0].Path != "a" || len(nearby) != 1 || nearby[0].Path != "c" {
		t.Errorf("mine %v, nearby %v", mine, nearby)
	}
}

func TestBluetoothStatus(t *testing.T) {
	for _, tc := range []struct {
		d    system.BluetoothDevice
		busy string
		want string
	}{
		{system.BluetoothDevice{Paired: true, Connected: true, Battery: 70}, "", "Connected, battery 70%"},
		{system.BluetoothDevice{Paired: true, Connected: true, Battery: -1}, "", "Connected"},
		{system.BluetoothDevice{Paired: true, Battery: -1}, "", "Not connected"},
		{system.BluetoothDevice{Paired: true, Battery: -1}, BTConnect, "Connecting…"},
		{system.BluetoothDevice{Battery: -1}, "", ""},
		{system.BluetoothDevice{Battery: -1}, BTPair, "Pairing…"},
	} {
		if got := BluetoothStatus(tc.d, tc.busy); got != tc.want {
			t.Errorf("BluetoothStatus(%+v, %q) = %q, want %q", tc.d, tc.busy, got, tc.want)
		}
	}
}

func TestBluetoothIcon(t *testing.T) {
	if got := BluetoothIcon("input-keyboard"); got != "input-keyboard-symbolic" {
		t.Error(got)
	}
	if got := BluetoothIcon("modem"); got != "bluetooth-active-symbolic" {
		t.Error(got)
	}
}

func TestBluetoothPromptText(t *testing.T) {
	title, _ := BluetoothPromptText(system.BluetoothPrompt{Kind: system.PromptShow, Code: "012345", Device: system.BluetoothDevice{Name: "K380"}})
	if title != "Type 012345 on K380" {
		t.Error(title)
	}
	title, _ = BluetoothPromptText(system.BluetoothPrompt{Kind: system.PromptConfirm, Code: "004321", Device: system.BluetoothDevice{Address: "AA:BB"}})
	if title != "Does AA:BB show 004321?" {
		t.Error(title)
	}
}
