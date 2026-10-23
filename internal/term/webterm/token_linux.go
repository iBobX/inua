package webterm

import (
	"context"
	"errors"
	"fmt"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/keyring"
)

// ErrNoKeyring is a session with no Secret Service to keep the token in.
var ErrNoKeyring = errors.New("no keyring (Secret Service) to keep the token in: start inua-keyring")

// Token is the page's token from the keyring (the Secret Service,
// inua-keyring's), made and kept there the first time; renew makes a new
// one, and the links with the old stop working.
func Token(ctx context.Context, renew bool) (string, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return "", ErrNoKeyring
	}
	defer conn.Close()
	c, err := keyring.Connect(conn, keyring.AlgDH)
	if err != nil {
		if errors.Is(err, keyring.ErrNoService) {
			return "", ErrNoKeyring
		}
		return "", err
	}
	defer c.Close()
	attrs := map[string]string{"xdg:schema": Schema}
	if !renew {
		v, ok, err := c.Lookup(ctx, attrs)
		if err != nil {
			return "", err
		}
		if ok && len(v) > 0 {
			return string(v), nil
		}
	}
	token, err := newToken()
	if err != nil {
		return "", err
	}
	if err := c.Store(ctx, "inua-term serve", attrs, []byte(token)); err != nil {
		return "", fmt.Errorf("keeping the token: %w", err)
	}
	return token, nil
}
