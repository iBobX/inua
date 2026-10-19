package host

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Connect reaches the host on its socket, starting one (`inua-term
// --host`, this program) when none runs: in a session of its own, so it
// outlives the client.
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
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	cmd := exec.Command(exe, "--host")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = null, null, null
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // reaped if it ends while this one runs
	return nil
}
