package hyprconf

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/config"
)

func ptr[T any](v T) *T { return &v }

func TestRenderNothing(t *testing.T) {
	got := Render(config.Config{Input: config.Input{}}, []string{"a-touchpad"})
	if strings.Contains(got, "input {") || strings.Contains(got, "device {") {
		t.Errorf("unset settings were written:\n%s", got)
	}
}

func TestRender(t *testing.T) {
	in := config.Input{KbOptions: ptr("caps:escape,ctrl:swap_lalt_lctl"), TouchpadSpeed: ptr(0.3), TouchpadAccel: "adaptive"}
	got := Render(config.Config{Input: in}, []string{"acme-inc.-trackpad-2", "elan0000:00-04f3:0000-touchpad"})
	for _, want := range []string{
		"input {\n    kb_options = caps:escape,ctrl:swap_lalt_lctl\n}\n",
		"device {\n    name = acme-inc.-trackpad-2\n    sensitivity = 0.30\n    accel_profile = adaptive\n}\n",
		"device {\n    name = elan0000:00-04f3:0000-touchpad\n    sensitivity = 0.30\n    accel_profile = adaptive\n}\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing\n%s\nin\n%s", want, got)
		}
	}
	// Only what is set: speed without an acceleration profile.
	got = Render(config.Config{Input: config.Input{TouchpadSpeed: ptr(-0.5)}}, []string{"x-touchpad"})
	if strings.Contains(got, "accel_profile") || strings.Contains(got, "kb_options") || !strings.Contains(got, "sensitivity = -0.50") {
		t.Errorf("got\n%s", got)
	}
	// An empty kb_options is a setting: it clears hyprland.conf's.
	if got := Render(config.Config{Input: config.Input{KbOptions: ptr("")}}, nil); !strings.Contains(got, "kb_options = \n") {
		t.Errorf("got\n%s", got)
	}
}

// Hyprland accepts what Render writes.
func TestRenderVerifies(t *testing.T) {
	if _, err := exec.LookPath("Hyprland"); err != nil {
		t.Skip("Hyprland not installed")
	}
	path := filepath.Join(t.TempDir(), "hyprland.conf")
	in := config.Input{KbOptions: ptr("caps:escape"), TouchpadSpeed: ptr(0.3), TouchpadAccel: "flat"}
	if err := os.WriteFile(path, []byte(Render(config.Config{Input: in}, []string{"x-touchpad"})), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command("Hyprland", "--verify-config", "-c", path).CombinedOutput()
	if !strings.Contains(string(out), "config ok") {
		t.Errorf("Hyprland rejects the file:\n%s", out)
	}
}

func TestTouchpads(t *testing.T) {
	reply := `{"mice": [
		{"name": "laptop-consumer-control-1"},
		{"name": "elan0000:00-04f3:0000-mouse"},
		{"name": "elan0000:00-04f3:0000-touchpad"},
		{"name": "acme-inc.-trackpad-2"},
		{"name": "evil-touchpad\nexec = x"}
	], "keyboards": []}`
	got, err := Touchpads([]byte(reply))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "elan0000:00-04f3:0000-touchpad,acme-inc.-trackpad-2" {
		t.Errorf("got %q", got)
	}
}

func TestApplyWithoutHyprland(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hypr", Name)
	ctx := context.Background()
	in := config.Input{KbOptions: ptr("ctrl:swap_lalt_lctl")}
	if changed, err := Apply(ctx, config.Config{Input: in}, path, nil); err != nil || !changed {
		t.Fatalf("first apply: changed=%v err=%v", changed, err)
	}
	if changed, err := Apply(ctx, config.Config{Input: in}, path, nil); err != nil || changed {
		t.Errorf("same settings rewrote the file: changed=%v err=%v", changed, err)
	}
	// A touchpad the file already has keeps its block while unplugged.
	old := Render(config.Config{Input: config.Input{TouchpadSpeed: ptr(0.1)}}, []string{"bt-trackpad"})
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, config.Config{Input: config.Input{TouchpadSpeed: ptr(0.4)}}, path, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "name = bt-trackpad\n    sensitivity = 0.40") {
		t.Errorf("got\n%s", got)
	}
}

func TestPath(t *testing.T) {
	if got := Path("/home/u/.config/inua/config.toml"); got != "/home/u/.config/hypr/inua.conf" {
		t.Errorf("got %s", got)
	}
}

// The sample inua init installs is the file for no settings.
func TestSample(t *testing.T) {
	got, err := os.ReadFile("../../config/hypr/" + Name)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != Render(config.Config{}, nil) {
		t.Errorf("config/hypr/%s is not Render(config.Config{}, nil)", Name)
	}
}

func TestKeys(t *testing.T) {
	if got := Keys(true, false, false, true, "s"); got != "SUPER+SHIFT+S" {
		t.Errorf("Keys: %s", got)
	}
	if got := Keys(false, true, true, false, "Return"); got != "CTRL+ALT+Return" {
		t.Errorf("Keys: %s", got)
	}
	if got := Label("SUPER+space"); got != "Super Space" {
		t.Errorf("Label: %s", got)
	}
	if !SameKeys("SUPER+space", "SUPER+Space") || SameKeys("SUPER+S", "SUPER+SHIFT+S") {
		t.Error("SameKeys")
	}
	reply := `[
		{"modmask": 64, "key": "Space", "dispatcher": "exec", "arg": "inua call launcher toggle", "submap": ""},
		{"modmask": 65, "key": "S", "dispatcher": "exec", "arg": "inua  call screenshot   area", "submap": ""},
		{"modmask": 0, "key": "Print", "dispatcher": "exec", "arg": "inua call screenshot area", "submap": ""},
		{"modmask": 64, "key": "mouse:272", "mouse": true, "dispatcher": "movewindow", "arg": ""},
		{"modmask": 0, "key": "x", "dispatcher": "exec", "arg": "inua call launcher toggle", "submap": "resize"}
	]`
	binds, err := ParseBinds([]byte(reply))
	if err != nil || len(binds) != 4 {
		t.Fatalf("binds %+v %v", binds, err)
	}
	c := config.Default()
	if k := KeysFor(binds, Command(c, "shot-area")); len(k) != 2 || k[0] != "SUPER+SHIFT+S" || k[1] != "Print" {
		t.Errorf("KeysFor shot-area: %v", k)
	}
	if k := KeysFor(binds, Command(c, "launcher")); len(k) != 1 {
		t.Errorf("KeysFor launcher (submap binds do not count): %v", k)
	}
	for _, a := range Actions {
		if Command(c, a.ID) == "" {
			t.Errorf("no command for %s", a.ID)
		}
	}
	if len(Actions) != len(config.ShortcutActions) {
		t.Error("Actions and config.ShortcutActions differ")
	}
}

func TestRenderShortcuts(t *testing.T) {
	c := config.Default()
	c.Shortcuts = []config.Shortcut{
		{Action: "launcher", Keys: "SUPER+D", Replaces: []string{"SUPER+Space"}},
		{Action: "shot-area", Keys: "", Replaces: []string{"Print"}},
		{Action: "exec", Keys: "SUPER+SHIFT+Return", Command: "kitty # a comment-looking arg"},
	}
	got := Render(c, nil)
	for _, want := range []string{
		"unbind = SUPER, Space\n",
		"unbind = SUPER, D\n",
		"unbind = , Print\n",
		"unbind = SUPER SHIFT, Return\n",
		"bind = SUPER, D, exec, inua call launcher toggle\n",
		"bind = SUPER SHIFT, Return, exec, kitty ## a comment-looking arg\n",
		"submap = " + RecordSubmap + "\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "wl-copy") {
		t.Error("an action turned off was bound")
	}
	if strings.Index(got, "unbind = SUPER, D") > strings.Index(got, "bind = SUPER, D") {
		t.Error("unbinds must come before binds")
	}
	if _, err := exec.LookPath("Hyprland"); err == nil {
		path := filepath.Join(t.TempDir(), "hyprland.conf")
		os.WriteFile(path, []byte(got), 0o644)
		if out, _ := exec.Command("Hyprland", "--verify-config", "-c", path).CombinedOutput(); !strings.Contains(string(out), "config ok") {
			t.Errorf("Hyprland rejects:\n%s", out)
		}
	}
}

func TestRenderLockRestore(t *testing.T) {
	if got := Render(config.Default(), nil); !strings.Contains(got, "allow_session_lock_restore = true") {
		t.Errorf("inua.conf does not let the lock screen take the lock back:\n%s", got)
	}
}
