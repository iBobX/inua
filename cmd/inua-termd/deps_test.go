package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// inua-termd runs where there is no desktop, and on macOS: neither it
// nor the host may bring in GTK or the shell's packages, and on macOS
// nothing of the Linux desktop (Hyprland, D-Bus, the keyring).
func TestDependencies(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go command to list the packages")
	}
	gui := []string{
		"codeberg.org/puregotk/puregotk",
		"github.com/lyricat/inua/internal/gtkx",
		"github.com/lyricat/inua/internal/appui",
		"github.com/lyricat/inua/internal/termui",
		"github.com/lyricat/inua/internal/ui",
		"github.com/lyricat/inua/internal/layershell",
		"github.com/lyricat/inua/internal/shell",
	}
	linuxDesktop := []string{
		"github.com/lyricat/inua/internal/hypr",
		"github.com/godbus/dbus",
		"github.com/lyricat/inua/internal/keyring",
	}
	for _, c := range []struct {
		goos, pkg string
		forbidden []string
	}{
		{"linux", ".", gui},
		{"linux", "github.com/lyricat/inua/internal/term/host", append(gui, linuxDesktop...)},
		{"darwin", ".", append(gui, linuxDesktop...)},
	} {
		cmd := exec.Command("go", "list", "-deps", c.pkg)
		cmd.Env = append(os.Environ(), "GOOS="+c.goos, "GOARCH=arm64", "CGO_ENABLED=0")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("%s %s: go list: %v", c.goos, c.pkg, err)
		}
		for _, dep := range strings.Fields(string(out)) {
			for _, f := range c.forbidden {
				if dep == f || strings.HasPrefix(dep, f+"/") {
					t.Errorf("%s %s depends on %s", c.goos, c.pkg, dep)
				}
			}
		}
	}
}
