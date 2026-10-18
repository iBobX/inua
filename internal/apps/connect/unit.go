package connect

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Unit is the systemd user unit the service runs in.
const Unit = "inua-connect.service"

// onFile says the service is on: it starts at login.
func onFile() string { return filepath.Join(Dir(), "on") }

// On says whether the service is on.
func On() bool {
	data, err := os.ReadFile(onFile())
	return err == nil && strings.TrimSpace(string(data)) == "1"
}

// Running says whether the service is on the bus.
func Running(ctx context.Context) bool {
	return Call(ctx, "Info").Err == nil
}

// SetOn turns the service on or off, now and at the next login.
func SetOn(ctx context.Context, on bool) error {
	if err := os.MkdirAll(Dir(), 0700); err != nil {
		return err
	}
	flag := "0"
	if on {
		flag = "1"
	}
	if err := os.WriteFile(onFile(), []byte(flag+"\n"), 0600); err != nil {
		return err
	}
	if !on {
		exec.CommandContext(ctx, "systemctl", "--user", "stop", Unit).Run()
		return nil
	}
	return Start(ctx)
}

// AtLogin starts the service when it is on: the shell calls it once the
// session's display is there.
func AtLogin(ctx context.Context) error {
	if !On() || Running(ctx) {
		return nil
	}
	return Start(ctx)
}

// Start runs the service in its unit, with the session's display and
// bus, and waits for it to be on the bus.
func Start(ctx context.Context) error {
	self, err := program()
	if err != nil {
		return err
	}
	exec.CommandContext(ctx, "systemctl", "--user", "reset-failed", Unit).Run()
	args := []string{"--user", "--unit=" + Unit, "--description=inua Mobile Link", "--collect", "--quiet",
		"--property=Restart=on-failure", "--property=RestartSec=3"}
	// The unit starts in the user manager's environment; the service needs
	// the session's display (the pointer, the clipboard) and its bus.
	for _, k := range []string{"WAYLAND_DISPLAY", "XDG_RUNTIME_DIR", "XDG_SESSION_TYPE", "XDG_CURRENT_DESKTOP",
		"HYPRLAND_INSTANCE_SIGNATURE", "DBUS_SESSION_BUS_ADDRESS", "PATH", "LANG", "LANGUAGE", "LC_ALL", "LC_MESSAGES"} {
		if v := os.Getenv(k); v != "" {
			args = append(args, "--setenv="+k+"="+v)
		}
	}
	args = append(args, "--", self, "--service")
	if out, err := exec.CommandContext(ctx, "systemd-run", args...).CombinedOutput(); err != nil {
		return &startError{strings.TrimSpace(string(out)), err}
	}
	for !Running(ctx) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return nil
}

type startError struct {
	out string
	err error
}

func (e *startError) Error() string {
	if e.out != "" {
		return "connect: " + e.out
	}
	return "connect: " + e.err.Error()
}

// program is inua-connect: this program, the one beside it (the shell's),
// or the one on PATH.
func program() (string, error) {
	self, err := os.Executable()
	if err == nil {
		if filepath.Base(self) == "inua-connect" {
			return self, nil
		}
		p := filepath.Join(filepath.Dir(self), "inua-connect")
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p, nil
		}
	}
	return exec.LookPath("inua-connect")
}
