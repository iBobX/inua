package kdeconnect

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sync"
	"time"
)

// Link is a TLS connection to a device, the handshake done: its identity
// and certificate, and its packets.
type Link struct {
	Peer DeviceInfo
	Cert *x509.Certificate
	Addr net.IP

	conn *tls.Conn
	r    *reader
	wmu  sync.Mutex
}

// Send sends p.
func (l *Link) Send(p Packet) error {
	l.wmu.Lock()
	defer l.wmu.Unlock()
	l.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	_, err := l.conn.Write(p.Encode())
	return err
}

// Read reads the device's next packet.
func (l *Link) Read() (Packet, error) {
	l.conn.SetReadDeadline(time.Time{})
	return l.r.read()
}

// Close ends the link.
func (l *Link) Close() error { return l.conn.Close() }

// Trust is the certificate pinned for a device when it is paired, else nil.
type Trust func(deviceID string) *x509.Certificate

// handshakeTimeout bounds each step of the handshake.
const handshakeTimeout = 10 * time.Second

// tlsConfig is the TLS of a link to peerID ("" when it is not known yet):
// self-signed certificates on both sides, a paired device's checked
// against the one pinned, any other's common name against its ID.
func tlsConfig(me *Identity, peerID func() string, trust Trust) *tls.Config {
	return &tls.Config{
		Certificates:       []tls.Certificate{me.Cert},
		ClientAuth:         tls.RequireAnyClientCert,
		InsecureSkipVerify: true, // self-signed: checked below
		MinVersion:         tls.VersionTLS12,
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("kdeconnect: the device gave no certificate")
			}
			cert, err := x509.ParseCertificate(raw[0])
			if err != nil {
				return err
			}
			id := peerID()
			if id != "" && cert.Subject.CommonName != id {
				return fmt.Errorf("kdeconnect: the certificate names %q, not %q", cert.Subject.CommonName, id)
			}
			if pinned := trust(cert.Subject.CommonName); pinned != nil && !bytes.Equal(pinned.Raw, cert.Raw) {
				return fmt.Errorf("kdeconnect: %s's certificate is not the one paired with", cert.Subject.CommonName)
			}
			return nil
		},
	}
}

// Dial connects to a device that announced itself at addr: this side
// writes its identity on the TCP connection, then is TLS's server, as the
// protocol has the connecting side.
func Dial(ctx context.Context, me *Identity, info DeviceInfo, addr string, peerID string, trust Trust) (*Link, error) {
	d := net.Dialer{Timeout: handshakeTimeout, KeepAlive: 30 * time.Second}
	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	conn := info
	conn.TargetDeviceID, conn.TargetProtocolVersion = peerID, ProtocolVersion
	conn.DeviceType, conn.IncomingCapabilities, conn.OutgoingCapabilities = "", nil, nil
	raw.SetDeadline(time.Now().Add(handshakeTimeout))
	if _, err := raw.Write(NewPacket(TypeIdentity, conn).Encode()); err != nil {
		raw.Close()
		return nil, err
	}
	tc := tls.Server(raw, tlsConfig(me, func() string { return peerID }, trust))
	return finish(ctx, tc, info, peerID)
}

// Accept takes a device's connection: it reads the device's identity from
// the TCP connection, then is TLS's client.
func Accept(ctx context.Context, raw net.Conn, me *Identity, info DeviceInfo, trust Trust) (*Link, error) {
	raw.SetDeadline(time.Now().Add(handshakeTimeout))
	line, err := readLine(raw, 8192)
	if err != nil {
		raw.Close()
		return nil, err
	}
	var p Packet
	var peer DeviceInfo
	if err := jsonPacket(line, &p, &peer); err != nil || p.Type != TypeIdentity || !ValidID(peer.DeviceID) {
		raw.Close()
		return nil, fmt.Errorf("kdeconnect: not a device's identity (%v): %.400q", err, line)
	}
	if peer.TargetDeviceID != "" && peer.TargetDeviceID != me.ID {
		raw.Close()
		return nil, fmt.Errorf("kdeconnect: %s was looking for another device", peer.DeviceID)
	}
	if peer.ProtocolVersion < ProtocolVersion {
		raw.Close()
		return nil, fmt.Errorf("kdeconnect: %s speaks protocol %d, older than %d", peer.DeviceID, peer.ProtocolVersion, ProtocolVersion)
	}
	tc := tls.Client(raw, tlsConfig(me, func() string { return peer.DeviceID }, trust))
	return finish(ctx, tc, info, peer.DeviceID)
}

// readLine reads up to a newline, a byte at a time: what follows it is
// TLS's, and must stay unread.
func readLine(c net.Conn, max int) ([]byte, error) {
	var line []byte
	b := make([]byte, 1)
	for len(line) < max {
		if _, err := c.Read(b); err != nil {
			return nil, err
		}
		if b[0] == '\n' {
			return line, nil
		}
		line = append(line, b[0])
	}
	return nil, ErrTooLong
}

// finish completes TLS, then each side gives its identity inside it.
func finish(ctx context.Context, tc *tls.Conn, info DeviceInfo, peerID string) (*Link, error) {
	hctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()
	if err := tc.HandshakeContext(hctx); err != nil {
		tc.Close()
		return nil, err
	}
	tc.SetDeadline(time.Now().Add(handshakeTimeout))
	if _, err := tc.Write(NewPacket(TypeIdentity, info).Encode()); err != nil {
		tc.Close()
		return nil, err
	}
	r := newReader(tc)
	p, err := r.read()
	if err != nil {
		tc.Close()
		return nil, err
	}
	var peer DeviceInfo
	if p.Type != TypeIdentity || p.Decode(&peer) != nil {
		tc.Close()
		return nil, errors.New("kdeconnect: the device's identity, inside TLS, is missing")
	}
	switch {
	case peer.DeviceID != peerID:
		tc.Close()
		return nil, fmt.Errorf("kdeconnect: the device changed its ID during the handshake (%s, then %s)", peerID, peer.DeviceID)
	case peer.ProtocolVersion < ProtocolVersion:
		tc.Close()
		return nil, fmt.Errorf("kdeconnect: %s speaks protocol %d", peerID, peer.ProtocolVersion)
	}
	peer.DeviceName = FilterName(peer.DeviceName)
	if peer.DeviceName == "" {
		tc.Close()
		return nil, errors.New("kdeconnect: the device has no name")
	}
	tc.SetDeadline(time.Time{})
	l := &Link{Peer: peer, Cert: tc.ConnectionState().PeerCertificates[0], conn: tc, r: r}
	if a, ok := tc.RemoteAddr().(*net.TCPAddr); ok {
		l.Addr = a.IP
	}
	return l, nil
}

var badNameChars = regexp.MustCompile(`["',;:.!?()\[\]<>]`)

// FilterName is a device's name as KDE Connect keeps it: without the
// characters it refuses, at most 32 of them.
func FilterName(s string) string {
	s = badNameChars.ReplaceAllString(s, "")
	r := []rune(s)
	if len(r) > 32 {
		r = r[:32]
	}
	return string(r)
}
