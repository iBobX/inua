// Package socktest is the one place a test makes a directory for a Unix
// socket.
//
// A Unix socket's path must fit in sun_path - 108 bytes on Linux, 104 on
// macOS - and testing.T's temporary directory is under TMPDIR, which can be
// long enough to break that. A long TMPDIR then makes every bind fail with
// EINVAL, an error that says nothing about the code under test. This finds the
// usual short place, /tmp, and falls back to the test's own directory only
// when even that is not usable.
package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// Dir is a directory, removed when the test ends, whose paths are short enough
// for a Unix socket in it.
func Dir(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ivm")
	if err != nil {
		// A system without a usable /tmp: the test's own directory is the
		// best that is left, and the caller will see what it does.
		if dir, err = os.MkdirTemp("", "ivm"); err != nil {
			t.Fatalf("a directory for a Unix socket: %v", err)
		}
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// Socket is a path for one socket, short enough to bind, in a directory of its
// own. name is the file name to use.
func Socket(t testing.TB, name string) string {
	t.Helper()
	return filepath.Join(Dir(t), name)
}
