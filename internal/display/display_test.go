package display

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/lyricat/inua/internal/config"
)

// fake is a computer with a lid in state ("open", "closed", or "" for
// no lid) and connectors by status.
func fake(t *testing.T, lid string, connectors map[string]string) System {
	t.Helper()
	root := t.TempDir()
	write := func(path, data string) {
		path = filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if lid != "" {
		write("proc/acpi/button/lid/LID0/state", "state:      "+lid+"\n")
	}
	for name, status := range connectors {
		write("sys/class/drm/card1-"+name+"/status", status+"\n")
	}
	return System{Root: root, Logind: func() (bool, error) { return false, errors.New("no logind") }}
}

func TestPanelsOff(t *testing.T) {
	both := map[string]string{"eDP-1": "connected", "DP-2": "connected", "DP-1": "disconnected", "Writeback-1": "unknown"}
	alone := map[string]string{"eDP-1": "connected", "DP-2": "disconnected", "Writeback-1": "connected"}
	for _, tc := range []struct {
		name       string
		lid        string
		connectors map[string]string
		want       []string
	}{
		{"lid closed, monitor plugged in", "closed", both, []string{"eDP-1"}},
		{"lid open, monitor plugged in", "open", both, nil},
		{"lid closed, nothing else", "closed", alone, nil},
		{"no lid", "", both, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := fake(t, tc.lid, tc.connectors).PanelsOff(); !slices.Equal(got, tc.want) {
				t.Errorf("PanelsOff() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConnected(t *testing.T) {
	s := fake(t, "open", map[string]string{"eDP-1": "connected", "DP-2": "connected", "HDMI-A-1": "disconnected", "Writeback-1": "connected"})
	if got, want := s.Connected(), []string{"DP-2", "eDP-1"}; !slices.Equal(got, want) {
		t.Errorf("Connected() = %v, want %v", got, want)
	}
	if !s.HasLid() || fake(t, "", nil).HasLid() {
		t.Error("HasLid wrong")
	}
}

// Without ACPI's lid, logind says.
func TestLidFromLogind(t *testing.T) {
	s := fake(t, "", map[string]string{"eDP-1": "connected", "DP-2": "connected"})
	s.Logind = func() (bool, error) { return true, nil }
	if got := s.PanelsOff(); !slices.Equal(got, []string{"eDP-1"}) {
		t.Errorf("PanelsOff() = %v, want [eDP-1]", got)
	}
}

func TestInternal(t *testing.T) {
	for name, want := range map[string]bool{"eDP-1": true, "LVDS-1": true, "DSI-1": true, "DP-2": false, "HDMI-A-1": false, "INUA-VIRTUAL": false} {
		if Internal(name) != want {
			t.Errorf("Internal(%q) = %v", name, !want)
		}
	}
}

const monitorsReply = `[{"name":"eDP-1","description":"BOE 0x0BCA","make":"BOE","model":"0x0BCA","width":2256,"height":1504,"refreshRate":59.999,"x":0,"y":0,"scale":1.0,"disabled":false,"mirrorOf":"none","availableModes":["2256x1504@60.00Hz","1920x1200@60.00Hz"]},
{"name":"DP-2","description":"Dell Inc. DELL U2720QM GZF09H3","make":"Dell Inc.","model":"DELL U2720QM","width":3840,"height":2160,"refreshRate":60.0,"x":2256,"y":0,"scale":1.5,"disabled":false,"mirrorOf":"none","availableModes":["3840x2160@60.00Hz","3840x2160@29.98Hz"]},
{"name":"INUA-VIRTUAL","description":"","width":1920,"height":1080,"refreshRate":60,"availableModes":[]}]`

func TestOutputs(t *testing.T) {
	outs, err := Outputs([]byte(monitorsReply))
	if err != nil || len(outs) != 2 {
		t.Fatalf("Outputs = %v, %v", outs, err)
	}
	panel, dell := outs[0], outs[1]
	if panel.Rule() != "eDP-1" || dell.Rule() != "desc:Dell Inc. DELL U2720QM GZF09H3" {
		t.Errorf("rules: %q %q", panel.Rule(), dell.Rule())
	}
	// 59.999 Hz is the listed 60.00 Hz.
	if got := panel.Mode(); got != "2256x1504@60.00" {
		t.Errorf("Mode() = %q", got)
	}
	want := config.Monitor{Output: dell.Rule(), Mode: "3840x2160@60.00", Position: "2256x0", Scale: 1.5}
	if got := dell.Seed(false); got != want {
		t.Errorf("Seed = %+v, want %+v", got, want)
	}
	// Off for the lid: the rule does not keep it off.
	panel.Disabled = true
	if panel.Seed(true).Disabled || !panel.Seed(false).Disabled {
		t.Error("Seed keeps the lid's off")
	}
	// A rule naming the connector is found for a display ruled by description.
	d := config.Displays{Monitors: []config.Monitor{{Output: "DP-2", Scale: 2}}}
	if m, ok := dell.RuleIn(d); !ok || m.Scale != 2 {
		t.Errorf("RuleIn = %+v, %v", m, ok)
	}
}

// A headless output's rate is no rate.
func TestModeHeadless(t *testing.T) {
	o := Output{Name: "TEST-1", Width: 1920, Height: 1080, Refresh: 0.06}
	if got := o.Mode(); got != "1920x1080" {
		t.Errorf("Mode() = %q", got)
	}
}

// Scales as Hyprland takes them: whole logical pixels, in 1/120 steps.
func TestValidScale(t *testing.T) {
	for _, tc := range []struct {
		w, h      int
		want, got float64
	}{
		{3840, 2160, 1.5, 1.5},
		{3840, 2160, 1.25, 1.25},
		{1280, 800, 1.5, 1.6}, // Hyprland's own suggestion
		{1112, 1394, 1.25, 1}, // likewise
		{2256, 1504, 2, 2},
		{1920, 1080, 0, 0}, // automatic
	} {
		if got := ValidScale(tc.w, tc.h, tc.want); got != tc.got {
			t.Errorf("ValidScale(%d, %d, %v) = %v, want %v", tc.w, tc.h, tc.want, got, tc.got)
		}
	}
	for _, f := range ScalesFor(2256, 1504) {
		if f != 0 && ValidScale(2256, 1504, f) != f {
			t.Errorf("ScalesFor offers %v, which Hyprland would move", f)
		}
	}
}
