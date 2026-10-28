package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The window builds for macOS too (docs/terminal.md, "On macOS"): it may
// not bring in the shell, the light apps or GStreamer, and on macOS not
// the keyring (the web page's token is in the login keychain there).
func TestDependencies(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go command to list the packages")
	}
	forbidden := []string{
		"github.com/lyricat/inua/internal/shell",
		"github.com/lyricat/inua/internal/ui",
		"github.com/lyricat/inua/internal/lightui",
		"github.com/lyricat/inua/internal/layershell",
		"github.com/lyricat/inua/internal/gst",
		"github.com/lyricat/inua/internal/assistant",
	}
	for _, goos := range []string{"linux", "darwin"} {
		cmd := exec.Command("go", "list", "-deps", ".")
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH=arm64", "CGO_ENABLED=0")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("%s: go list: %v", goos, err)
		}
		not := forbidden
		if goos == "darwin" {
			not = append(not, "github.com/lyricat/inua/internal/keyring")
		}
		for _, dep := range strings.Fields(string(out)) {
			for _, f := range not {
				if dep == f || strings.HasPrefix(dep, f+"/") {
					t.Errorf("%s: inua-term depends on %s", goos, dep)
				}
			}
		}
	}
	cmd := exec.Command("go", "build", "-o", os.DevNull, ".")
	cmd.Env = append(os.Environ(), "GOOS=darwin", "GOARCH=arm64", "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("darwin build: %v\n%s", err, out)
	}
}
