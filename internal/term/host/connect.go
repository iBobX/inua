package host

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// Connect reaches the host on its socket, starting one (inua-termd) when
// none runs: in a session of its own, so it outlives the client, and in a
// systemd scope of its own, so stopping the unit of whoever started it
// (inua-termd serve's) does not end it.
func Connect(onOutput func(pane int, b []byte), onEvent func(Message)) (*Client, error) {
	path := SocketPath()
	if c, err := Dial(path, onOutput, onEvent); err == nil {
		return c, nil
	}
	if err := startHostProcess(); err != nil {
		return nil, err
	}
	var err error
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		var c *Client
		if c, err = Dial(path, onOutput, onEvent); err == nil {
			return c, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("the terminal host did not start: %w", err)
		}
	}
}

func startHostProcess() error {
	exe, err := Program("inua-termd")
	if err != nil {
		return err
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	cmd := exec.Command(exe)
	if run, ok := scopeRunner(); ok {
		// systemd-run --scope moves itself into a new scope and then
		// becomes inua-termd, which keeps this environment.
		cmd = exec.Command(run, "--user", "--scope", "--collect", "--quiet",
			"--description=inua-term host", "--", exe)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = null, null, null
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // reaped if it ends while this one runs
	return nil
}

// scopeRunner is systemd-run, when it and a user manager to ask are there.
func scopeRunner() (string, bool) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return "", false
	}
	if _, err := os.Stat(filepath.Join(dir, "systemd", "private")); err != nil {
		return "", false
	}
	run, err := exec.LookPath("systemd-run")
	return run, err == nil
}

// Program finds one of inua-term's programs (inua-term, inua-termd): this
// one when it is name, the one beside it (an install, a build's bin/),
// else the one on PATH.
func Program(name string) (string, error) {
	self, err := os.Executable()
	if err == nil {
		if filepath.Base(self) == name {
			return self, nil
		}
		p := filepath.Join(filepath.Dir(self), name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p, nil
		}
	}
	return exec.LookPath(name)
}
