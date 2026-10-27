//go:build !linux

package ipc

import "net"

// peerPid is the process at the other end of conn: not told here.
func peerPid(net.Conn) int { return 0 }
