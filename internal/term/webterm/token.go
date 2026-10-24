package webterm

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// The page's token is kept in the system's keyring: the Secret Service on
// Linux (token_linux.go), the login keychain on macOS (token_darwin.go).
// Where the keyring cannot keep it (a computer reached over SSH, its
// keychain locked; one with no desktop, so no Secret Service), it is kept
// in a file only the user can read.

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

// tokenStore is a place the token is kept.
type tokenStore interface {
	load() (string, error)
	save(token string) error
}

// keptToken is the token the first store has, else a new one kept in the
// first store that takes it; renew makes a new one. A token kept in one
// store goes from the stores after it, so an old one there is not read
// when the first cannot be.
func keptToken(renew bool, stores ...tokenStore) (string, error) {
	if !renew {
		for _, s := range stores {
			if t, err := s.load(); err == nil && t != "" {
				return t, nil
			}
		}
	}
	token, err := newToken()
	if err != nil {
		return "", err
	}
	var errs []error
	for i, s := range stores {
		if err := s.save(token); err != nil {
			errs = append(errs, err)
			continue
		}
		for _, later := range stores[i+1:] {
			if f, ok := later.(fileStore); ok {
				os.Remove(string(f))
			}
		}
		return token, nil
	}
	return "", errors.Join(errs...)
}

// fileStore keeps the token in a file of the user's alone (0600, in a
// folder of theirs).
type fileStore string

// tokenFile is the file: term-web-token in inua's folder of the user's
// configuration (~/.config/inua, ~/Library/Application Support/inua).
func tokenFile() fileStore {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return fileStore(filepath.Join(dir, "inua", "term-web-token"))
}

func (f fileStore) load() (string, error) {
	b, err := os.ReadFile(string(f))
	return strings.TrimSpace(string(b)), err
}

func (f fileStore) save(token string) error {
	if err := os.MkdirAll(filepath.Dir(string(f)), 0o700); err != nil {
		return err
	}
	tmp := string(f) + ".new"
	if err := os.WriteFile(tmp, []byte(token+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, string(f))
}
