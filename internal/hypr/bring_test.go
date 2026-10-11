package hypr

import (
	"slices"
	"testing"
)

func TestBringHere(t *testing.T) {
	active := []byte(`{"id": 3, "name": "3"}`)
	clients := []byte(`[
	  {"address": "0xAAA", "pid": 10, "mapped": true, "workspace": {"id": 1}},
	  {"address": "0xbbb", "pid": 20, "mapped": false, "workspace": {"id": 1}},
	  {"address": "0xccc", "pid": 20, "mapped": true, "workspace": {"id": -98}},
	  {"address": "0xddd", "pid": 30, "mapped": true, "workspace": {"id": 3}}
	]`)
	for pid, want := range map[int][]string{
		10: {"movetoworkspacesilent 3,address:0xaaa", "focuswindow address:0xaaa"},
		20: {"movetoworkspacesilent 3,address:0xccc", "focuswindow address:0xccc"}, // from a special workspace too; the unmapped one is skipped
		30: {"focuswindow address:0xddd"},                                          // already here
	} {
		got, err := bringHere(active, clients, pid)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("pid %d: %q %v, want %q", pid, got, err, want)
		}
	}
	if _, err := bringHere(active, clients, 99); err == nil {
		t.Error("no window, no error")
	}
}
