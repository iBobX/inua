package remote

import (
	"context"
	"errors"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/keyring"
)

// Schema names the remote disks' items in the Secret Service.
const Schema = "io.github.lyricat.Inua.Files.Remote"

// Secrets keeps the disks' passwords and secret keys in the Secret
// Service, by address.
type Secrets struct {
	// Dial connects to the session bus.
	Dial func() (*dbus.Conn, error)
}

// NewSecrets is the session's Secret Service.
func NewSecrets() Secrets {
	return Secrets{Dial: func() (*dbus.Conn, error) { return dbus.ConnectSessionBus() }}
}

// ErrNoKeyring is a session with no Secret Service to keep a secret in.
var ErrNoKeyring = errors.New("no keyring (Secret Service) to keep it in")

func attrs(address string) map[string]string {
	return map[string]string{"xdg:schema": Schema, "address": address}
}

func (s Secrets) client() (*keyring.Client, func(), error) {
	conn, err := s.Dial()
	if err != nil {
		return nil, nil, ErrNoKeyring
	}
	c, err := keyring.Connect(conn, keyring.AlgDH)
	if err != nil {
		conn.Close()
		if errors.Is(err, keyring.ErrNoService) {
			return nil, nil, ErrNoKeyring
		}
		return nil, nil, err
	}
	return c, func() { c.Close(); conn.Close() }, nil
}

// Available says whether there is a keyring to keep secrets in.
func (s Secrets) Available() bool {
	_, done, err := s.client()
	if err == nil {
		done()
	}
	return err == nil
}

// Lookup is the secret kept for address; false when there is none.
func (s Secrets) Lookup(ctx context.Context, address string) (string, bool, error) {
	c, done, err := s.client()
	if err != nil {
		return "", false, err
	}
	defer done()
	v, ok, err := c.Lookup(ctx, attrs(address))
	return string(v), ok, err
}

// Store keeps the secret for d's address.
func (s Secrets) Store(ctx context.Context, d Disk, secret string) error {
	c, done, err := s.client()
	if err != nil {
		return err
	}
	defer done()
	return c.Store(ctx, "inua remote disk: "+d.Name, attrs(d.Address), []byte(secret))
}

// Delete forgets the secret for address.
func (s Secrets) Delete(address string) error {
	c, done, err := s.client()
	if err != nil {
		return err
	}
	defer done()
	return c.Delete(attrs(address))
}
