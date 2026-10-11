// Package rundir is inua's runtime directory, for its control sockets, icon
// themes, captures and markers: $XDG_RUNTIME_DIR/inua, which the login
// session keeps private. Without XDG_RUNTIME_DIR it is a directory of the
// user's own in the system's temporary directory, /tmp/inua-UID; another
// account could have made that first, or put a link there, so it is checked
// before use: a directory, not a link, the user's, open to no one else.
package rundir

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// Path is the runtime directory, made or not.
func Path() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "inua")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("inua-%d", os.Getuid()))
}

// Dir makes the runtime directory if it is missing, checks it, and returns
// it. Anything made in it is the user's alone.
func Dir() (string, error) {
	dir := Path()
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
	}
	return dir, Check(dir)
}

// Sub is a directory in the runtime directory, made if missing.
func Sub(name string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	sub := filepath.Join(dir, name)
	if err := os.MkdirAll(sub, 0o700); err != nil {
		return "", err
	}
	return sub, Check(sub)
}

// Check makes sure dir is safe to write in and to trust: a directory, not
// a link to one, owned by the user and closed to everyone else. A directory
// of the user's that others may read is closed; any other is an error.
func Check(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%s is a link; inua will not use it", dir)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%s: cannot tell its owner", dir)
	}
	if int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s belongs to another user; inua will not use it", dir)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("%s is open to others: %w", dir, err)
		}
	}
	return nil
}
