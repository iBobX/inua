package clipboard

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/hypr"
)

// Selection is where the selection being copied now comes from: the
// program that copied it, and the window focused, with sensitive (the
// programs listed as sensitive) and the types a password manager marks
// its secrets with.
func Selection(sensitive []string) Source {
	from := FindSource(focusedWindow(), sensitive)
	if MarkedSensitive(offeredTypes()) {
		from.Sensitive = true // a password manager's secret
	}
	return from
}

// offeredTypes are the types the clipboard's owner offers now; none when
// wl-paste cannot say in time.
func offeredTypes() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "wl-paste", "--list-types").Output()
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

// focusedWindow is the window focused now, as Hyprland names it; none
// without Hyprland.
func focusedWindow() Window {
	var w Window
	paths, err := hypr.PathsFromEnv()
	if err != nil {
		return w
	}
	c := hypr.NewClient(paths)
	c.Timeout = 300 * time.Millisecond // a copy is not held up for long
	data, err := c.Request(context.Background(), "j/activewindow")
	if err == nil {
		json.Unmarshal(data, &w)
	}
	return w
}
