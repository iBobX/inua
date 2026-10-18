package kdeconnect

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// VerificationKey is what both devices show while they pair, for their
// users to see that each pairs with the other: the hash of both public
// keys (the larger first) and the pairing's time, its first eight hex
// digits.
func VerificationKey(a, b *x509.Certificate, timestamp int64) string {
	ka, kb := a.RawSubjectPublicKeyInfo, b.RawSubjectPublicKeyInfo
	if bytes.Compare(ka, kb) < 0 {
		ka, kb = kb, ka
	}
	h := sha256.New()
	h.Write(ka)
	h.Write(kb)
	h.Write([]byte(strconv.FormatInt(timestamp, 10)))
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil))[:8])
}

// Trusted is a paired device, as kept: its name, type, certificate, the
// address it was last at, and its settings.
type Trusted struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Cert     string            `json:"cert"`
	LastAddr string            `json:"lastAddr,omitempty"`
	Settings map[string]string `json:"settings,omitempty"`
}

// Store keeps the paired devices, in a file readable only by the user.
type Store struct {
	path string
	mu   sync.Mutex
	m    map[string]Trusted
}

// OpenStore reads the store at path; a missing file is an empty store.
func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, m: map[string]Trusted{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.m); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) save() error {
	data, _ := json.MarshalIndent(s.m, "", "  ")
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Get is the device id's record, if it is paired.
func (s *Store) Get(id string) (Trusted, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.m[id]
	return t, ok
}

// All are the paired devices, by ID.
func (s *Store) All() map[string]Trusted {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]Trusted, len(s.m))
	for k, v := range s.m {
		out[k] = v
	}
	return out
}

// Put keeps a device's record.
func (s *Store) Put(id string, t Trusted) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = t
	return s.save()
}

// Update changes a paired device's record, if it is paired.
func (s *Store) Update(id string, f func(*Trusted)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.m[id]
	if !ok {
		return nil
	}
	f(&t)
	s.m[id] = t
	return s.save()
}

// Delete forgets a device.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
	return s.save()
}

// Cert is the certificate pinned for id, or nil: the links' Trust.
func (s *Store) Cert(id string) *x509.Certificate {
	t, ok := s.Get(id)
	if !ok {
		return nil
	}
	c, err := ParseCertPEM(t.Cert)
	if err != nil {
		return nil
	}
	return c
}
