// Package kdeconnect speaks KDE Connect's protocol (version 8), so phones
// and computers running KDE Connect, GSConnect or Valent pair with this
// one: devices found by UDP broadcast on the local network (and on the
// tailnet by unicast), links over TLS with each side's certificate pinned
// once paired, packets of JSON a line, and payloads (files) on connections
// of their own. It is written from the protocol's behaviour, not KDE
// Connect's code, and imports no GTK.
package kdeconnect

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ProtocolVersion is the protocol spoken: 8, the only one KDE Connect
// still accepts.
const ProtocolVersion = 8

// validID is a device ID as KDE Connect takes it.
var validID = regexp.MustCompile(`^[a-zA-Z0-9_-]{32,38}$`)

// ValidID says whether id can be a device's ID.
func ValidID(id string) bool { return validID.MatchString(id) }

// Identity is this computer to the devices it links with: its ID, the
// certificate named by it, and its key.
type Identity struct {
	ID   string
	Cert tls.Certificate
}

// The identity's files in its folder.
const (
	certFile = "certificate.pem"
	keyFile  = "private.pem"
)

// LoadIdentity reads the identity kept in dir, making and keeping one when
// there is none: an ID, and a certificate as KDE Connect makes its own
// (P-256, the ID its common name, ten years). The key is the user's alone.
func LoadIdentity(dir string) (*Identity, error) {
	certPEM, err1 := os.ReadFile(filepath.Join(dir, certFile))
	keyPEM, err2 := os.ReadFile(filepath.Join(dir, keyFile))
	if err1 == nil && err2 == nil {
		return parseIdentity(certPEM, keyPEM)
	}
	if !errors.Is(err1, os.ErrNotExist) && err1 != nil {
		return nil, err1
	}
	certPEM, keyPEM, err := newIdentity()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, keyFile), keyPEM, 0600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, certFile), certPEM, 0600); err != nil {
		return nil, err
	}
	return parseIdentity(certPEM, keyPEM)
}

func newIdentity() (certPEM, keyPEM []byte, err error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, nil, err
	}
	id := hex.EncodeToString(raw[:]) // 32 characters
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 159))
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: id, Organization: []string{"KDE"}, OrganizationalUnit: []string{"KDE Connect"}},
		NotBefore:    now.AddDate(-1, 0, 0),
		NotAfter:     now.AddDate(10, 0, 0),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), nil
}

func parseIdentity(certPEM, keyPEM []byte) (*Identity, error) {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("kdeconnect: the identity's files: %w", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, err
	}
	cert.Leaf = leaf
	id := strings.TrimSpace(leaf.Subject.CommonName)
	if !ValidID(id) {
		return nil, fmt.Errorf("kdeconnect: the certificate names %q, not a device", id)
	}
	return &Identity{ID: id, Cert: cert}, nil
}

// CertPEM is a certificate, written as PEM.
func CertPEM(c *x509.Certificate) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw}))
}

// ParseCertPEM reads a certificate written as PEM.
func ParseCertPEM(s string) (*x509.Certificate, error) {
	b, _ := pem.Decode([]byte(s))
	if b == nil {
		return nil, errors.New("kdeconnect: not a PEM certificate")
	}
	return x509.ParseCertificate(b.Bytes)
}
