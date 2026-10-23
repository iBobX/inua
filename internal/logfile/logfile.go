// Package logfile keeps a program's log in $XDG_STATE_HOME/inua when it
// has nowhere else to go: started from the dock or by Hyprland, a program's
// standard error is no terminal, or /dev/null.
package logfile

import (
	"log"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// maxLog is how large a log grows before it is kept as NAME.1 and begun
// again.
const maxLog = 1 << 20

// To has the log, and everything written to the standard error (a Go
// panic, a crash's trace, GTK's warnings), go to $XDG_STATE_HOME/inua/NAME.
func To(name string) {
	state, err := os.UserHomeDir()
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		state = dir
	} else if err == nil {
		state = filepath.Join(state, ".local", "state")
	}
	dir := filepath.Join(state, "inua")
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	path := filepath.Join(dir, name)
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLog {
		os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	log.SetOutput(f)
	if err := unix.Dup2(int(f.Fd()), 2); err != nil {
		log.Printf("%s: the standard error stays: %v", filepath.Base(os.Args[0]), err)
	}
	log.Printf("%s %d: started", filepath.Base(os.Args[0]), os.Getpid())
}

// Discarded reports whether the standard error is closed or /dev/null,
// as for a program Hyprland starts: what it writes there is lost.
func Discarded() bool {
	var st, null syscall.Stat_t
	if syscall.Fstat(2, &st) != nil {
		return true
	}
	if syscall.Stat(os.DevNull, &null) != nil {
		return false
	}
	return st.Rdev == null.Rdev && st.Mode&syscall.S_IFMT == syscall.S_IFCHR
}
