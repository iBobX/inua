package host

import (
	"net"

	"golang.org/x/sys/unix"
)

// What the host reads of other processes, on macOS from the kernel's
// sysctl: a pane's foreground program, and who reports on a socket.

// procArgs is a process's argument list.
func procArgs(pid int) []string {
	b, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return nil
	}
	args, _ := parseProcArgs(b)
	return args
}

// procEnv is one variable of a process's environment.
func procEnv(pid int, key string) string {
	b, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return ""
	}
	_, env := parseProcArgs(b)
	return lookupEnv(env, key)
}

// peerPid is the process at the other end of a Unix socket, 0 when it
// cannot be told.
func peerPid(conn net.Conn) int {
	pid := 0
	control(conn, func(fd int) {
		if p, err := unix.GetsockoptInt(fd, unix.SOL_LOCAL, unix.LOCAL_PEERPID); err == nil {
			pid = p
		}
	})
	return pid
}
