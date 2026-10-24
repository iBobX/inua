package host

import (
	"bytes"
	"encoding/binary"
	"net"
	"strings"
)

// The parts of reading other processes that are the same everywhere;
// proc_linux.go and proc_darwin.go read them.

// lookupEnv is key's value among env's KEY=value entries.
func lookupEnv(env []string, key string) string {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, key+"="); ok {
			return v
		}
	}
	return ""
}

// control runs f with a Unix socket's descriptor; not at all for another
// kind of connection.
func control(conn net.Conn, f func(fd int)) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return
	}
	raw.Control(func(fd uintptr) { f(int(fd)) })
}

// parseProcArgs reads macOS's KERN_PROCARGS2: the argument count (a
// 32-bit integer in the machine's order, little-endian on every Mac),
// the executable's path, NULs to align, then the arguments and the
// environment, each ending in a NUL.
func parseProcArgs(b []byte) (args, env []string) {
	if len(b) < 4 {
		return nil, nil
	}
	argc := int(binary.LittleEndian.Uint32(b))
	rest := b[4:]
	i := bytes.IndexByte(rest, 0) // the executable's path
	if i < 0 {
		return nil, nil
	}
	rest = bytes.TrimLeft(rest[i:], "\x00")
	for len(rest) > 0 {
		i := bytes.IndexByte(rest, 0)
		if i < 0 {
			i = len(rest)
		}
		s := string(rest[:i])
		rest = rest[min(i+1, len(rest)):]
		if len(args) < argc {
			args = append(args, s)
		} else if s == "" {
			break // the end of the environment
		} else {
			env = append(env, s)
		}
	}
	return args, env
}
