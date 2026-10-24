package webterm

import (
	"context"
	"errors"
	"fmt"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/keyring"
)

// ErrNoKeyring is a computer where the token could be kept nowhere.
var ErrNoKeyring = errors.New("the token could be kept neither in a keyring (Secret Service) nor in a file")

// Token is the page's token from the keyring (the Secret Service,
// inua-keyring's), made and kept there the first time, or in a file when
// there is no keyring (no desktop session); renew makes a new one, and
// the links with the old stop working.
func Token(ctx context.Context, renew bool) (string, error) {
	t, err := keptToken(renew, secretService{ctx}, tokenFile())
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNoKeyring, err)
	}
	return t, nil
}

// secretService is the session's Secret Service.
type secretService struct{ ctx context.Context }

var attrs = map[string]string{"xdg:schema": Schema}

func (s secretService) open() (*keyring.Client, func(), error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, nil, fmt.Errorf("no session bus: %w", err)
	}
	c, err := keyring.Connect(conn, keyring.AlgDH)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	return c, func() { c.Close(); conn.Close() }, nil
}

func (s secretService) load() (string, error) {
	c, done, err := s.open()
	if err != nil {
		return "", err
	}
	defer done()
	v, ok, err := c.Lookup(s.ctx, attrs)
	if err != nil || !ok {
		return "", err
	}
	return string(v), nil
}

func (s secretService) save(token string) error {
	c, done, err := s.open()
	if err != nil {
		return err
	}
	defer done()
	if err := c.Store(s.ctx, "inua-term serve", attrs, []byte(token)); err != nil {
		return fmt.Errorf("keeping the token: %w", err)
	}
	return nil
}
