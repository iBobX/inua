package sysmon

import (
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"syscall"
)

// Signals are the ways the System Monitor ends or holds a process, by the
// name the kill command knows them by.
var Signals = map[syscall.Signal]string{
	syscall.SIGTERM: "TERM", // asks it to quit; it may save its work first
	syscall.SIGKILL: "KILL", // ends it at once; it cannot refuse
	syscall.SIGINT:  "INT",  // as Ctrl+C in a terminal
	syscall.SIGHUP:  "HUP",  // as closing its terminal; daemons reload
	syscall.SIGSTOP: "STOP", // pauses it
	syscall.SIGCONT: "CONT", // lets a paused process go on
}

// Descendants are the processes started by pid, and those they started,
// deepest first, so a parent is not left to restart a child.
func Descendants(ps []Process, pid int) []int {
	children := map[int][]int{}
	for _, p := range ps {
		if p.PID != p.PPID {
			children[p.PPID] = append(children[p.PPID], p.PID)
		}
	}
	var out []int
	seen := map[int]bool{pid: true}
	var walk func(int)
	walk = func(parent int) {
		for _, c := range children[parent] {
			if seen[c] {
				continue
			}
			seen[c] = true
			walk(c)
			out = append(out, c)
		}
	}
	walk(pid)
	return out
}

// Named are the processes of a user with a name, by PID.
func Named(ps []Process, name string, uid int) []int {
	var out []int
	for _, p := range ps {
		if p.Name == name && p.UID == uid {
			out = append(out, p.PID)
		}
	}
	slices.Sort(out)
	return out
}

// ErrCanceled is a signal not sent because no administrator's password
// was given.
var ErrCanceled = errors.New("not authorized")

// Signal sends sig to processes. The user's own get it directly; others
// get it through pkexec, which asks for an administrator's password and
// blocks until it is given. A process already gone is no error.
func Signal(sig syscall.Signal, pids []int, own func(pid int) bool) error {
	name, ok := Signals[sig]
	if !ok {
		return fmt.Errorf("signal %d is not one the System Monitor sends", sig)
	}
	var errs []error
	var others []string
	for _, pid := range pids {
		if pid <= 1 { // 0 and -1 reach many processes; 1 is init
			continue
		}
		if !own(pid) {
			others = append(others, strconv.Itoa(pid))
			continue
		}
		if err := syscall.Kill(pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
			errs = append(errs, fmt.Errorf("process %d: %w", pid, err))
		}
	}
	if len(others) > 0 {
		kill, err := exec.LookPath("kill")
		if err == nil {
			err = exec.Command("pkexec", append([]string{kill, "-s", name, "--"}, others...)...).Run()
		}
		var exit *exec.ExitError
		switch {
		// pkexec exits 126 when the password dialog is dismissed and 127
		// when it is refused or there is no agent to ask.
		case errors.As(err, &exit) && (exit.ExitCode() == 126 || exit.ExitCode() == 127):
			errs = append(errs, ErrCanceled)
		case err != nil:
			errs = append(errs, fmt.Errorf("as administrator: %w", err))
		}
	}
	return errors.Join(errs...)
}
