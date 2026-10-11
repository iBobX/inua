package rundir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestXDG(t *testing.T) {
	run := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", run)
	dir, err := Dir()
	if err != nil || dir != filepath.Join(run, "inua") {
		t.Fatalf("%q %v", dir, err)
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("mode %v", fi.Mode())
	}
	if sub, err := Sub("icons"); err != nil || sub != filepath.Join(dir, "icons") {
		t.Errorf("sub %q %v", sub, err)
	}
}

func TestTemporary(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	path := Path()
	if filepath.Dir(path) != tmp || !strings.HasPrefix(filepath.Base(path), "inua-") {
		t.Fatalf("path %q", path)
	}

	// Someone's link in its place: refused, and nothing written through it.
	target := t.TempDir()
	os.Symlink(target, path)
	if _, err := Dir(); err == nil || !strings.Contains(err.Error(), "link") {
		t.Errorf("a link: %v", err)
	}
	if _, err := Sub("icons"); err == nil {
		t.Error("made a subdirectory through a link")
	}
	if entries, _ := os.ReadDir(target); len(entries) != 0 {
		t.Errorf("wrote through the link: %v", entries)
	}
	os.Remove(path)

	// Not a directory: refused.
	os.WriteFile(path, nil, 0o600)
	if _, err := Dir(); err == nil {
		t.Error("a file in its place")
	}
	os.Remove(path)

	// The user's own, but open to others: closed.
	os.Mkdir(path, 0o777)
	os.Chmod(path, 0o777)
	if _, err := Dir(); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o700 {
		t.Errorf("left open: %v", fi.Mode())
	}
}

func TestOthers(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root owns everything")
	}
	// A directory of another user's (root's): refused.
	if err := Check("/"); err == nil || !strings.Contains(err.Error(), "another user") {
		t.Errorf("/: %v", err)
	}
}
