package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The system's copy of inua-pinentry is reported only when it is there and
// is not the program just installed.
func TestStaleSystemCopy(t *testing.T) {
	dir := t.TempDir()
	program := filepath.Join(dir, "inua-pinentry")
	copy := filepath.Join(dir, "system-inua-pinentry")
	if err := os.WriteFile(program, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if stale(program, copy) {
		t.Error("no copy: reported stale")
	}
	if err := os.WriteFile(copy, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !stale(program, copy) {
		t.Error("an older copy: not reported")
	}
	if err := os.WriteFile(copy, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if stale(program, copy) {
		t.Error("the same copy: reported stale")
	}
}
