package webterm

import (
	"crypto/rand"
	"encoding/hex"
)

// The page's token is kept in the system's keyring: the Secret Service on
// Linux (token_linux.go), the login keychain on macOS (token_darwin.go).

// Schema names the token's item: its schema in the Secret Service, its
// service in the keychain.
const Schema = "io.github.lyricat.Inua.Term.Serve"

// newToken is a new token: 128 random bits, in hex.
func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
