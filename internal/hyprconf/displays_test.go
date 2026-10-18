package hyprconf

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/config"
)

var dell = "desc:Dell Inc. DELL U2720QM GZF09H3"

func displays(lidOff bool) config.Config {
	return config.Config{Displays: config.Displays{LidOff: lidOff, Monitors: []config.Monitor{
		{Output: "eDP-1", Mode: "2256x1504@60", Scale: 1},
		{Output: dell, Mode: "3840x2160@60", Position: "auto-right", Scale: 1.5},
		{Output: "HDMI-A-1", Mirror: "eDP-1"},
		{Output: "DP-3", Disabled: true},
	}}}
}

func TestRenderDisplays(t *testing.T) {
	got := Render(displays(true), Devices{Lids: []string{"Lid Switch"}})
	for _, want := range []string{
		"monitor = eDP-1, 2256x1504@60, auto, 1\n",
		"monitor = " + dell + ", 3840x2160@60, auto-right, 1.5\n",
		"monitor = HDMI-A-1, preferred, auto, auto, mirror, eDP-1\n",
		"monitor = DP-3, disable\n",
		"bindl = , switch:Lid Switch, exec, inua config apply\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "Nothing set in inua") {
		t.Errorf("monitors set, but the file says nothing is:\n%s", got)
	}

	// The lid closed with another display: the panel's own rule gives way
	// to turning it off, and is not written twice.
	got = Render(displays(true), Devices{Lids: []string{"Lid Switch"}, PanelsOff: []string{"eDP-1"}})
	if strings.Contains(got, "monitor = eDP-1, 2256x1504") || !strings.Contains(got, "monitor = eDP-1, disable\n") {
		t.Errorf("panel not turned off:\n%s", got)
	}
	// With lid_off off, the lid neither turns it off nor is bound.
	got = Render(displays(false), Devices{Lids: []string{"Lid Switch"}, PanelsOff: []string{"eDP-1"}})
	if strings.Contains(got, "eDP-1, disable") || strings.Contains(got, "switch:") {
		t.Errorf("lid_off off, but:\n%s", got)
	}
}

func TestRenderDisplaysLua(t *testing.T) {
	got := RenderLua(displays(true), Devices{Lids: []string{"Lid Switch"}, PanelsOff: []string{"eDP-1"}})
	for _, want := range []string{
		`hl.monitor({ output = "eDP-1", disabled = true })`,
		`hl.monitor({ output = "` + dell + `", mode = "3840x2160@60", position = "auto-right", scale = 1.5 })`,
		`hl.monitor({ output = "HDMI-A-1", mode = "preferred", position = "auto", scale = "auto", mirror = "eDP-1" })`,
		`hl.monitor({ output = "DP-3", disabled = true })`,
		`hl.bind("switch:Lid Switch", hl.dsp.exec_cmd("inua config apply"), { locked = true })`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
}

func TestLids(t *testing.T) {
	reply := `{"mice":[],"keyboards":[],"switches":[{"address":"0x1","name":"Lid Switch"},{"address":"0x2","name":"Tablet Mode Switch"}]}`
	got, err := Lids([]byte(reply))
	if err != nil || !slices.Equal(got, []string{"Lid Switch"}) {
		t.Errorf("Lids = %v, %v", got, err)
	}
	if got := knownLids(Render(displays(true), Devices{Lids: got})); !slices.Equal(got, []string{"Lid Switch"}) {
		t.Errorf("knownLids = %v", got)
	}
}

// Apply turns the panel off while the lid says so, keeps the lid's bind
// from the file without Hyprland, and turns the panel on again after.
func TestApplyLid(t *testing.T) {
	path := filepath.Join(t.TempDir(), Name)
	ctx := context.Background()
	c := config.Config{Displays: config.Displays{LidOff: true}}
	if err := os.WriteFile(path, []byte(Render(c, Devices{Lids: []string{"Lid Switch"}})), 0o644); err != nil {
		t.Fatal(err)
	}
	defer func(old func() []string) { panelsOff = old }(panelsOff)
	panelsOff = func() []string { return []string{"eDP-1"} }
	if changed, err := Apply(ctx, c, path, nil); err != nil || !changed {
		t.Fatalf("lid closed: changed=%v err=%v", changed, err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "monitor = eDP-1, disable\n") || !strings.Contains(string(got), "switch:Lid Switch") {
		t.Errorf("lid closed:\n%s", got)
	}
	panelsOff = func() []string { return nil }
	if changed, err := Apply(ctx, c, path, nil); err != nil || !changed {
		t.Fatalf("lid open: changed=%v err=%v", changed, err)
	}
	got, _ = os.ReadFile(path)
	if strings.Contains(string(got), "disable") || !strings.Contains(string(got), "switch:Lid Switch") {
		t.Errorf("lid open:\n%s", got)
	}
}

// Hyprland accepts the monitor rules and the lid's bind.
func TestRenderDisplaysVerifies(t *testing.T) {
	if _, err := exec.LookPath("Hyprland"); err != nil {
		t.Skip("Hyprland not installed")
	}
	path := filepath.Join(t.TempDir(), "hyprland.conf")
	file := Render(displays(true), Devices{Lids: []string{"Lid Switch"}, PanelsOff: []string{"eDP-1"}})
	if err := os.WriteFile(path, []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command("Hyprland", "--verify-config", "-c", path).CombinedOutput()
	if !strings.Contains(string(out), "config ok") {
		t.Errorf("Hyprland rejects the file:\n%s\n%s", out, file)
	}
}
