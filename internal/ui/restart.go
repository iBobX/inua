package ui

import (
	"fmt"
	"log"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/lyricat/inua/internal/gtkx"
)

// restartDelay lets the control socket answer before the process goes.
const restartDelay = 150 * time.Millisecond

// executable is the path of the shell binary. After a rebuild the running
// binary's file is gone and /proc/self/exe reads "<path> (deleted)"; the new
// binary is at <path>, which is the one to run.
func executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe = strings.TrimSuffix(exe, " (deleted)")
	if st, err := os.Stat(exe); err != nil {
		return "", err
	} else if st.Mode()&0o111 == 0 {
		return "", fmt.Errorf("%s is not executable", exe)
	}
	return exe, nil
}

// scheduleRestart replaces the process with a fresh run of the shell binary,
// keeping its PID, arguments, environment and log output. Every descriptor
// Go opened is close-on-exec, so the Wayland and D-Bus connections and the
// control socket close: the compositor drops the old surfaces and the new
// process takes the socket over. Errors finding the binary are returned to
// the caller; a failed exec leaves the shell running and is logged.
func (s *shellUI) scheduleRestart() error {
	exe, err := executable()
	if err != nil {
		return err
	}
	go func() {
		time.Sleep(restartDelay)
		gtkx.Post(func() {
			s.closeOverview(false)
			s.guardOverviewMouse(false)
			s.pauseOverviewKeys()
			log.Printf("restarting %s", exe)
			err := syscall.Exec(exe, os.Args, os.Environ())
			log.Printf("restart: %v", err) // Exec only returns on failure
		})
	}()
	return nil
}
