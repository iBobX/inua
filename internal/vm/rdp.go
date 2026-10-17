package vm

import (
	"context"
	"io"
	"net"
	"time"
)

// rdpRequest opens an RDP connection: a TPKT header, an X.224 Connection
// Request, and an RDP Negotiation Request for TLS and CredSSP, which is what
// a client sends first.
var rdpRequest = []byte{
	0x03, 0x00, 0x00, 0x13, // TPKT: version 3, 19 bytes
	0x0e, 0xe0, 0x00, 0x00, 0x00, 0x00, 0x00, // X.224 Connection Request
	0x01, 0x00, 0x08, 0x00, 0x03, 0x00, 0x00, 0x00, // RDP_NEG_REQ: TLS | CredSSP
}

// rdpAnswers reports whether a Remote Desktop server answers at addr: it
// sends a connection request and waits for the X.224 Connection Confirm,
// then hangs up before anything is authenticated. A TCP connection alone
// says nothing about the guest: Docker's port forward takes it as soon as
// the container runs, long before Windows listens behind it.
func rdpAnswers(ctx context.Context, addr string) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false
	}
	defer c.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(deadline)
	}
	if _, err := c.Write(rdpRequest); err != nil {
		return false
	}
	// The TPKT header and the fixed part of the Connection Confirm.
	var reply [11]byte
	if _, err := io.ReadFull(c, reply[:]); err != nil {
		return false
	}
	return reply[0] == 0x03 && reply[5]&0xf0 == 0xd0
}
