package desktop

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

// LaunchWith starts the entry detached from this process: own session, no
// stdio. Terminal=true entries run through terminal, followed by "-e"; nil
// means $TERMINAL, then xdg-terminal-exec. Window placement and focus are
// left to Hyprland.
func LaunchWith(e *Entry, terminal []string) error {
	if len(e.Command) == 0 {
		return errors.New(e.ID + ": no command")
	}
	argv := e.Command
	if e.RunInTerminal {
		term := slices.Clone(terminal)
		if len(term) == 0 {
			term = strings.Fields(os.Getenv("TERMINAL"))
		}
		if len(term) == 0 {
			term = []string{"xdg-terminal-exec"}
		} else {
			term = append(term, "-e")
		}
		argv = append(term, argv...)
	}
	return Spawn(argv, e.WorkingDir)
}

// Spawn starts argv detached and reaps it in the background.
func Spawn(argv []string, dir string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// ReapInherited waits for the children this process already has when it
// starts: apps launched before `shell restart` re-executed it in place,
// whose waits ended with the old image. Without it they stay zombies.
func ReapInherited() {
	self := os.Getpid()
	dirs, _ := os.ReadDir("/proc")
	for _, d := range dirs {
		pid, err := strconv.Atoi(d.Name())
		if err != nil || pid == self || parentOf(pid) != self {
			continue
		}
		go func() {
			var ws syscall.WaitStatus
			for {
				if _, err := syscall.Wait4(pid, &ws, 0, nil); err != syscall.EINTR {
					return
				}
			}
		}()
	}
}

// parentOf reads a process's parent from /proc/PID/stat, or -1.
func parentOf(pid int) int {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return -1
	}
	// The name in parentheses may hold spaces; the fields after it are
	// state, then the parent.
	i := bytes.LastIndexByte(b, ')')
	if i < 0 {
		return -1
	}
	f := strings.Fields(string(b[i+1:]))
	if len(f) < 2 {
		return -1
	}
	ppid, err := strconv.Atoi(f[1])
	if err != nil {
		return -1
	}
	return ppid
}
