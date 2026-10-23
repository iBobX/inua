package host

import (
	"net"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// What the host reads of other processes, on Linux from /proc: a pane's
// foreground program, and who reports on a socket.

// procArgs is a process's argument list.
func procArgs(pid int) []string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil || len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
}

// procEnv is one variable of a process's environment.
func procEnv(pid int, key string) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	if err != nil {
		return ""
	}
	return lookupEnv(strings.Split(string(b), "\x00"), key)
}

// peerPid is the process at the other end of a Unix socket, 0 when it
// cannot be told.
func peerPid(conn net.Conn) int {
	pid := 0
	control(conn, func(fd int) {
		if cred, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED); err == nil {
			pid = int(cred.Pid)
		}
	})
	return pid
}
