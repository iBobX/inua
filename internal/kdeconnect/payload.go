package kdeconnect

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// payloadWait is how long a payload offered waits for the device to fetch
// it.
const payloadWait = 60 * time.Second

// payloadTLS is TLS for a payload's connection: only the device's own
// certificate, the link's.
func (c *Core) payloadTLS(peer *x509.Certificate) *tls.Config {
	return &tls.Config{
		Certificates:       []tls.Certificate{c.me.Cert},
		ClientAuth:         tls.RequireAnyClientCert,
		InsecureSkipVerify: true, // checked below
		MinVersion:         tls.VersionTLS12,
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 || !bytes.Equal(raw[0], peer.Raw) {
				return errors.New("kdeconnect: a payload's connection from another device")
			}
			return nil
		},
	}
}

// SendPayload sends p with a payload of size bytes read from r: the
// device is told the port, connects, and reads it. It returns when the
// device has it all, or gives up.
func (c *Core) SendPayload(ctx context.Context, id string, p Packet, r io.Reader, size int64) error {
	l, err := c.pairedLink(id, p.Type)
	if err != nil {
		return err
	}
	ln, err := c.listenPayload()
	if err != nil {
		return err
	}
	defer ln.Close()
	p.PayloadSize = size
	p.PayloadTransferInfo = &TransferInfo{Port: ln.Addr().(*net.TCPAddr).Port}
	if err := l.Send(p); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, payloadWait)
	defer cancel()
	go func() { <-ctx.Done(); ln.Close() }()
	for {
		raw, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("kdeconnect: the device did not fetch the file: %w", ctx.Err())
			}
			return err
		}
		tc := tls.Server(raw, c.payloadTLS(l.Cert))
		if err := tc.HandshakeContext(ctx); err != nil {
			// Someone else, or a try that failed: the device may try again.
			raw.Close()
			continue
		}
		stop := context.AfterFunc(ctx, func() { tc.Close() })
		_, err = io.CopyN(tc, r, size)
		stop()
		tc.Close()
		return err
	}
}

// listenPayload listens on the first port free in the payloads' range.
func (c *Core) listenPayload() (net.Listener, error) {
	p := c.cfg.Ports
	if p.PayloadMin == 0 {
		return net.Listen("tcp", ":0")
	}
	for port := p.PayloadMin; port <= p.PayloadMax; port++ {
		if ln, err := net.Listen("tcp", ":"+strconv.Itoa(port)); err == nil {
			return ln, nil
		}
	}
	return nil, fmt.Errorf("kdeconnect: no port free for a file, from %d to %d", p.PayloadMin, p.PayloadMax)
}

// Payload fetches the payload of p, a packet from the device id: it is
// read from the result, then closed.
func (c *Core) Payload(ctx context.Context, id string, p Packet) (io.ReadCloser, error) {
	if p.PayloadTransferInfo == nil || p.PayloadSize < 0 {
		return nil, errors.New("kdeconnect: the packet has no payload")
	}
	c.mu.Lock()
	d := c.devices[id]
	var cert *x509.Certificate
	var ip net.IP
	if d != nil && d.state == Paired {
		cert, ip = d.cert, d.addr
	}
	c.mu.Unlock()
	if cert == nil || ip == nil {
		return nil, ErrNotPaired
	}
	addr := net.JoinHostPort(ip.String(), strconv.Itoa(p.PayloadTransferInfo.Port))
	dl := net.Dialer{Timeout: handshakeTimeout}
	raw, err := dl.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	tc := tls.Client(raw, c.payloadTLS(cert))
	hctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()
	if err := tc.HandshakeContext(hctx); err != nil {
		raw.Close()
		return nil, err
	}
	return payloadReader{io.LimitReader(tc, p.PayloadSize), tc}, nil
}

type payloadReader struct {
	io.Reader
	io.Closer
}
