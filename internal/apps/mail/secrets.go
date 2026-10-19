package mail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/keyring"
)

// Schema names inua-mail's items in the Secret Service.
const Schema = "io.github.lyricat.Inua.Mail"

// What a secret is.
const (
	SecretRefresh  = "refresh"  // an OAuth refresh token
	SecretPassword = "password" // an IMAP and SMTP password
)

// Secrets keeps the accounts' refresh tokens and passwords: in the
// Secret Service (inua-keyring, gnome-keyring …) when one is on the
// session bus, else in a file only the user can read; never in mail.toml.
type Secrets struct {
	file string
	dial func() (*dbus.Conn, error) // nil: the file only
}

// NewSecrets keeps secrets in the session's Secret Service, else the file.
func NewSecrets() *Secrets {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, _ := os.UserHomeDir()
		data = filepath.Join(home, ".local", "share")
	}
	return &Secrets{file: filepath.Join(data, "inua", "mail-secrets.json"), dial: func() (*dbus.Conn, error) { return dbus.ConnectSessionBus() }}
}

// SecretsAt keeps secrets in file only, for tests.
func SecretsAt(file string) *Secrets { return &Secrets{file: file} }

func secretAttrs(account, kind string) map[string]string {
	return map[string]string{"xdg:schema": Schema, "account": account, "kind": kind}
}

const secretTimeout = 2 * time.Minute // the keyring may ask for gpg's passphrase

func (s *Secrets) service() (*keyring.Client, func(), error) {
	if s.dial == nil {
		return nil, nil, nil
	}
	conn, err := s.dial()
	if err != nil {
		return nil, nil, nil
	}
	c, err := keyring.Connect(conn, keyring.AlgDH)
	if errors.Is(err, keyring.ErrNoService) {
		conn.Close()
		return nil, nil, nil
	}
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	return c, func() { c.Close(); conn.Close() }, nil
}

// Get returns an account's secret of kind, "" when none is saved.
func (s *Secrets) Get(account, kind string) (string, error) {
	c, done, err := s.service()
	if err != nil {
		return "", err
	}
	if c == nil {
		saved, err := loadSecrets(s.file)
		return saved[account+"/"+kind], err
	}
	defer done()
	ctx, cancel := context.WithTimeout(context.Background(), secretTimeout)
	defer cancel()
	v, _, err := c.Lookup(ctx, secretAttrs(account, kind))
	return string(v), err
}

// Set saves an account's secret of kind; "" removes it.
func (s *Secrets) Set(account, kind, value string) error {
	c, done, err := s.service()
	if err != nil {
		return err
	}
	if c == nil {
		saved, err := loadSecrets(s.file)
		if err != nil {
			return err
		}
		if value == "" {
			delete(saved, account+"/"+kind)
		} else {
			saved[account+"/"+kind] = value
		}
		return writeSecrets(s.file, saved)
	}
	defer done()
	if value == "" {
		return c.Delete(secretAttrs(account, kind))
	}
	ctx, cancel := context.WithTimeout(context.Background(), secretTimeout)
	defer cancel()
	return c.Store(ctx, "inua mail: "+account+" ("+kind+")", secretAttrs(account, kind), []byte(value))
}

// Forget removes every secret of an account.
func (s *Secrets) Forget(account string) error {
	return errors.Join(s.Set(account, SecretRefresh, ""), s.Set(account, SecretPassword, ""))
}

func loadSecrets(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s: others can read it (chmod 600 it, or delete it and sign in again)", path)
	}
	m := map[string]string{}
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

func writeSecrets(path string, m map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mail-secrets-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
