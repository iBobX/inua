package hypr

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Instance is a running Hyprland, as its directory under
// $XDG_RUNTIME_DIR/hypr says: hyprland.lock holds its process ID and its
// Wayland socket's name.
type Instance struct {
	Signature string // HYPRLAND_INSTANCE_SIGNATURE
	Runtime   string // XDG_RUNTIME_DIR
	PID       int
	Wayland   string // WAYLAND_DISPLAY, "wayland-1"
}

// Paths are the instance's sockets.
func (i Instance) Paths() Paths {
	dir := filepath.Join(i.Runtime, "hypr", i.Signature)
	return Paths{Request: filepath.Join(dir, ".socket.sock"), Event: filepath.Join(dir, ".socket2.sock")}
}

// ErrNoInstance is no Hyprland running for this user.
var ErrNoInstance = errors.New("no Hyprland session is running")

// FindInstance is the Hyprland running under runtime ("" for this user's
// /run/user/UID), for a program outside it: one started over SSH. A
// directory whose process is gone is left over from an earlier session.
// With several running (a nested one in a session), the first started is
// the session's.
func FindInstance(runtime string) (Instance, error) {
	var found Instance
	first := int64(0)
	for _, i := range instances(runtime) {
		// The signature is commit_seconds_random: its second field is when
		// the instance started.
		started := int64(0)
		if f := strings.Split(i.Signature, "_"); len(f) >= 2 {
			started, _ = strconv.ParseInt(f[1], 10, 64)
		}
		if found.PID == 0 || started < first {
			found, first = i, started
		}
	}
	if found.PID == 0 {
		return Instance{}, ErrNoInstance
	}
	return found, nil
}

// InstanceOf is the Hyprland that the process pid is, or that it started:
// start-hyprland runs Hyprland as its child. It finds the session a
// program started with it belongs to, though another Hyprland runs.
func InstanceOf(runtime string, pid int) (Instance, error) {
	for _, i := range instances(runtime) {
		if i.PID == pid || parent(i.PID) == pid {
			return i, nil
		}
	}
	return Instance{}, ErrNoInstance
}

// instances are the Hyprlands running under runtime.
func instances(runtime string) []Instance {
	if runtime == "" {
		runtime = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	dirs, err := os.ReadDir(filepath.Join(runtime, "hypr"))
	if err != nil {
		return nil
	}
	var found []Instance
	for _, d := range dirs {
		lock, err := os.ReadFile(filepath.Join(runtime, "hypr", d.Name(), "hyprland.lock"))
		if err != nil {
			continue
		}
		lines := strings.Fields(string(lock))
		if len(lines) < 2 {
			continue
		}
		pid, err := strconv.Atoi(lines[0])
		if err != nil || pid <= 0 || syscall.Kill(pid, 0) == syscall.ESRCH {
			continue
		}
		found = append(found, Instance{Signature: d.Name(), Runtime: runtime, PID: pid, Wayland: lines[1]})
	}
	return found
}

// parent is the process ID of pid's parent, or 0.
func parent(pid int) int {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0
	}
	// pid (comm) state ppid ...: comm may hold spaces and parentheses.
	f := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
	if len(f) < 2 {
		return 0
	}
	ppid, _ := strconv.Atoi(f[1])
	return ppid
}

// SessionPaths are the sockets of the Hyprland this process runs under,
// or else of the one running for this user.
func SessionPaths() (Paths, error) {
	if p, err := PathsFromEnv(); err == nil {
		return p, nil
	}
	i, err := FindInstance(os.Getenv("XDG_RUNTIME_DIR"))
	if err != nil {
		return Paths{}, err
	}
	return i.Paths(), nil
}
