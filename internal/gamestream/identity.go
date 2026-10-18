// Package gamestream is a client for GameStream, the protocol Moonlight
// speaks to Sunshine: finding what a host offers, pairing with it by a PIN,
// launching a session, and the session's streams (RTSP to set it up, the
// control stream on ENet, video and audio over RTP, input on the control
// stream). It is written from the protocol's behaviour; it does not import
// GTK, and decoding is the caller's.
package gamestream

import (
	"crypto/rand"
	"crypto/rsa"
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
	"time"
)

// Identity is this client to the hosts it pairs with: a self-signed
// certificate, its key, and a unique ID. A host trusts the certificate once
// paired; the same identity must be kept for every later connection.
type Identity struct {
	UniqueID string
	CertPEM  []byte
	KeyPEM   []byte

	cert *x509.Certificate
	key  *rsa.PrivateKey
}

// NewIdentity makes a new identity: an RSA 2048 key and a certificate valid
// for twenty years, named as Moonlight's are.
func NewIdentity() (*Identity, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:       serial,
		Subject:            pkix.Name{CommonName: "NVIDIA GameStream Client"},
		NotBefore:          now.Add(-time.Hour),
		NotAfter:           now.AddDate(20, 0, 0),
		SignatureAlgorithm: x509.SHA256WithRSA,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	return parseIdentity(
		hex.EncodeToString(id[:]),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}),
	)
}

func parseIdentity(uniqueID string, certPEM, keyPEM []byte) (*Identity, error) {
	cb, _ := pem.Decode(certPEM)
	kb, _ := pem.Decode(keyPEM)
	if cb == nil || kb == nil {
		return nil, errors.New("gamestream: the identity's files are not PEM")
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, err
	}
	key, err := x509.ParsePKCS1PrivateKey(kb.Bytes)
	if err != nil {
		return nil, err
	}
	return &Identity{UniqueID: uniqueID, CertPEM: certPEM, KeyPEM: keyPEM, cert: cert, key: key}, nil
}

// The identity's files in its folder.
const (
	certFile = "client.pem"
	keyFile  = "client.key"
	idFile   = "uniqueid"
)

// LoadIdentity reads the identity kept in dir, making and keeping a new
// one when there is none. The key is readable only by the user.
func LoadIdentity(dir string) (*Identity, error) {
	certPEM, err1 := os.ReadFile(filepath.Join(dir, certFile))
	keyPEM, err2 := os.ReadFile(filepath.Join(dir, keyFile))
	uid, err3 := os.ReadFile(filepath.Join(dir, idFile))
	if err := errors.Join(err1, err2, err3); err == nil {
		return parseIdentity(string(uid), certPEM, keyPEM)
	} else if !errors.Is(err1, os.ErrNotExist) && !errors.Is(err2, os.ErrNotExist) && !errors.Is(err3, os.ErrNotExist) {
		return nil, err
	}
	id, err := NewIdentity()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	for name, data := range map[string][]byte{certFile: id.CertPEM, keyFile: id.KeyPEM, idFile: []byte(id.UniqueID)} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			return nil, fmt.Errorf("keeping the identity: %w", err)
		}
	}
	return id, nil
}

// tlsCertificate is the identity as TLS presents it to a host.
func (id *Identity) tlsCertificate() tls.Certificate {
	return tls.Certificate{Certificate: [][]byte{id.cert.Raw}, PrivateKey: id.key, Leaf: id.cert}
}
