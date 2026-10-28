package gtkx

import (
	"os"
	"path/filepath"
	"strings"
)

// libPath finds a library on macOS: libX.so.N is libX.N.dylib there, in
// the app's Frameworks (scripts/macos-app.sh) or Homebrew's lib.
func libPath(name string) string {
	leaf := libDarwin(name)
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "..", "Frameworks"))
	}
	for _, dir := range append(dirs, "/opt/homebrew/lib", "/usr/local/lib") {
		if p := filepath.Join(dir, leaf); fileExists(p) {
			return p
		}
	}
	return leaf
}

// libDarwin is a Linux library's name as macOS writes it.
func libDarwin(name string) string {
	base, n, ok := strings.Cut(name, ".so.")
	if !ok {
		return strings.TrimSuffix(name, ".so") + ".dylib"
	}
	return base + "." + n + ".dylib"
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
