package pty

import (
	"bytes"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// getTermios is the ioctl reading a terminal's settings.
const getTermios = unix.TIOCGETA

// open makes a master and its slave: posix_openpt, grantpt, unlockpt and
// ptsname as macOS's libc does them, with its ioctls.
func open() (master, slave *os.File, err error) {
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("pty: /dev/ptmx: %w", err)
	}
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err != nil {
		unix.Close(fd)
		return nil, nil, fmt.Errorf("pty: grant: %w", err)
	}
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0); err != nil {
		unix.Close(fd)
		return nil, nil, fmt.Errorf("pty: unlock: %w", err)
	}
	// x/sys has no ioctl taking a buffer: the call itself, as creack/pty
	// makes it. The buffer is the ioctl's size: TIOCPTYGNAME is _IOC(…, 128).
	var buf [128]byte
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&buf[0]))); errno != 0 {
		unix.Close(fd)
		return nil, nil, fmt.Errorf("pty: name: %w", errno)
	}
	name := string(buf[:bytes.IndexByte(buf[:], 0)])
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
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || kp.Proc.P_pid != int32(pid) {
		return false
	}
	return kp.Eproc.Tdev != -1 && uint64(kp.Eproc.Tdev) == p.dev // -1: NODEV, no terminal
}
