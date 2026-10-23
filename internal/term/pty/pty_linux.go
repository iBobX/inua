package pty

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// getTermios is the ioctl reading a terminal's settings.
const getTermios = unix.TCGETS

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
	// Non-blocking, the master goes through Go's poller: Close then ends
	// a Read waiting on it, and the program gets its SIGHUP at once.
	if err := unix.SetNonblock(fd, true); err != nil {
		unix.Close(fd)
		unix.Close(sfd)
		return nil, nil, fmt.Errorf("pty: %w", err)
	}
	return os.NewFile(uintptr(fd), "/dev/ptmx"), os.NewFile(uintptr(sfd), name), nil
}

// Controls says the terminal is process pid's controlling terminal: pid
// runs in the terminal, not merely started from it (a daemon that left
// it has none).
func (p *PTY) Controls(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	// pid (comm) state ppid pgrp session tty_nr …; comm may hold spaces.
	i := bytes.LastIndexByte(b, ')')
	if i < 0 {
		return false
	}
	f := strings.Fields(string(b[i+1:]))
	if len(f) < 5 {
		return false
	}
	tty, err := strconv.ParseUint(f[4], 10, 32)
	if err != nil || tty == 0 {
		return false
	}
	// tty_nr is the kernel's encoding: minor's low byte, the major, then
	// the rest of the minor.
	major, minor := uint32(tty>>8&0xfff), uint32(tty&0xff|tty>>12&0xfff00)
	return major == unix.Major(p.dev) && minor == unix.Minor(p.dev)
}
