// Package pty starts programs on pseudo-terminals, in pure Go: /dev/ptmx
// and the terminal ioctls through golang.org/x/sys/unix, no cgo.
package pty

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

// PTY is a program on a pseudo-terminal: read its output from the PTY,
// write its input to it.
type PTY struct {
	master *os.File
	cmd    *exec.Cmd
}

// Start runs argv in dir on a new terminal of cols × rows, as the leader
// of a new session with the terminal as its controlling one. env is the
// whole environment (nil: this process's).
func Start(argv []string, dir string, env []string, cols, rows int) (*PTY, error) {
	if len(argv) == 0 {
		return nil, errors.New("pty: no command")
	}
	master, slave, err := open()
	if err != nil {
		return nil, err
	}
	defer slave.Close() // the child has its own copies
	if err := setSize(master, cols, rows); err != nil {
		master.Close()
		return nil, err
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir, cmd.Env = dir, env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	// The child's fd 0 (the slave) becomes its controlling terminal.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		master.Close()
		return nil, fmt.Errorf("pty: %s: %w", argv[0], err)
	}
	return &PTY{master: master, cmd: cmd}, nil
}

// open makes a master and its slave.
func open() (master, slave *os.File, err error) {
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("pty: /dev/ptmx: %w", err)
	}
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil { // unlockpt
		unix.Close(fd)
		return nil, nil, fmt.Errorf("pty: unlock: %w", err)
	}
	n, err := unix.IoctlGetUint32(fd, unix.TIOCGPTN) // ptsname
	if err != nil {
		unix.Close(fd)
		return nil, nil, fmt.Errorf("pty: number: %w", err)
	}
	name := "/dev/pts/" + strconv.Itoa(int(n))
	sfd, err := unix.Open(name, unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		unix.Close(fd)
		return nil, nil, fmt.Errorf("pty: %s: %w", name, err)
	}
	return os.NewFile(uintptr(fd), "/dev/ptmx"), os.NewFile(uintptr(sfd), name), nil
}

func setSize(f *os.File, cols, rows int) error {
	ws := &unix.Winsize{Col: uint16(cols), Row: uint16(rows)}
	if err := unix.IoctlSetWinsize(int(f.Fd()), unix.TIOCSWINSZ, ws); err != nil {
		return fmt.Errorf("pty: size: %w", err)
	}
	return nil
}

// Read reads the program's output. When the program and everything it
// started have closed the terminal, it returns io.EOF (Linux reports EIO).
func (p *PTY) Read(b []byte) (int, error) {
	n, err := p.master.Read(b)
	if errors.Is(err, syscall.EIO) {
		err = io.EOF
	}
	return n, err
}

// Write sends input to the program.
func (p *PTY) Write(b []byte) (int, error) { return p.master.Write(b) }

// Resize changes the terminal's size; the program gets SIGWINCH.
func (p *PTY) Resize(cols, rows int) error { return setSize(p.master, cols, rows) }

// Pid is the program's process.
func (p *PTY) Pid() int { return p.cmd.Process.Pid }

// Foreground is the terminal's foreground process group: the program
// running at the shell's prompt, or the shell itself.
func (p *PTY) Foreground() (int, error) {
	return unix.IoctlGetInt(int(p.master.Fd()), unix.TIOCGPGRP)
}

// Echoing reports whether the terminal echoes what is typed; off while a
// program reads a password.
func (p *PTY) Echoing() (bool, error) {
	t, err := unix.IoctlGetTermios(int(p.master.Fd()), unix.TCGETS)
	if err != nil {
		return false, err
	}
	return t.Lflag&unix.ECHO != 0, nil
}

// Wait waits for the program to end and returns its exit code; -1 when a
// signal ended it.
func (p *PTY) Wait() (int, error) {
	err := p.cmd.Wait()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exit):
		return exit.ExitCode(), nil
	}
	return -1, err
}

// Close closes the terminal; the program gets SIGHUP.
func (p *PTY) Close() error { return p.master.Close() }
