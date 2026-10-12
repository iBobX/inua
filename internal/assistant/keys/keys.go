// Package keys keeps the API keys typed in the settings window, by
// profile: in the Secret Service (inua-keyring, gnome-keyring …) when one
// is on the session bus, else in a file only the user can read
// ($XDG_DATA_HOME/inua/keys.json); never in config.toml. Keys left in the
// file move to the Secret Service once one is there. It does not import
// Mister Morph, so the shell can use it.
package keys

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/keyring"
)

// Schema names the assistant's items in the Secret Service.
const Schema = "io.github.lyricat.Inua.Assistant"

// ServeTokenKey cannot be a model profile name.
const ServeTokenKey = "@runtime-api"

// ServeToken reads the endpoint's token, optionally creating it. Endpoint
// tokens require the Secret Service; they never fall back to keys.json.
func (k *Keys) ServeToken(create bool) (string, error) {
	c, done, err := k.service()
	if err != nil {
		return "", err
	}
	if c == nil {
		return "", errors.New("the Console token needs a Secret Service; enable inua-keyring first")
	}
	defer done()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	v, found, err := c.Lookup(ctx, attrs(ServeTokenKey))
	if err != nil {
		return "", err
	}
	if found || !create {
		return strings.TrimSpace(string(v)), nil
	}
	token := rand.Text()
	if err := c.Store(ctx, "inua assistant: Console token", attrs(ServeTokenKey), []byte(token)); err != nil {
		return "", err
	}
	return token, nil
}

// Keys is where the keys are kept.
type Keys struct {
	file string
	// dial connects to the session bus; nil means no Secret Service.
	dial func() (*dbus.Conn, error)
}

// New keeps keys in the session's Secret Service, else in the file.
func New() *Keys {
	return &Keys{file: Path(), dial: func() (*dbus.Conn, error) { return dbus.ConnectSessionBus() }}
}

// NewAt is New with another file and bus, for tests; dial nil uses the
// file only.
func NewAt(file string, dial func() (*dbus.Conn, error)) *Keys { return &Keys{file: file, dial: dial} }

// Path is the file keys are kept in without a Secret Service.
func Path() string {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, _ := os.UserHomeDir()
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "inua", "keys.json")
}

func attrs(profile string) map[string]string {
	return map[string]string{"xdg:schema": Schema, "profile": profile}
}

func label(profile string) string { return "inua assistant: " + profile }

// timeout bounds a call, which may wait for gpg's passphrase.
const timeout = 2 * time.Minute

// service connects to the Secret Service, moving the file's keys into it;
// nil (and no error) when there is none.
func (k *Keys) service() (*keyring.Client, func(), error) {
	if k.dial == nil {
		return nil, nil, nil
	}
	conn, err := k.dial()
	if err != nil {
		return nil, nil, nil // no session bus: the file
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
	done := func() { c.Close(); conn.Close() }
	if err := k.migrate(c); err != nil {
		done()
		return nil, nil, fmt.Errorf("moving the keys from %s: %w", k.file, err)
	}
	return c, done, nil
}

// migrate moves the file's keys into the Secret Service.
func (k *Keys) migrate(c *keyring.Client) error {
	saved, err := load(k.file)
	if err != nil || len(saved) == 0 {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for profile, key := range saved {
		if err := c.Store(ctx, label(profile), attrs(profile), []byte(key)); err != nil {
			return err
		}
		delete(saved, profile)
		if err := write(k.file, saved); err != nil {
			return err
		}
	}
	return nil
}

// Where says where keys go, for the settings page.
func (k *Keys) Where() string {
	c, done, err := k.service()
	switch {
	case err != nil:
		return "the keyring (" + err.Error() + ")"
	case c == nil:
		return strings.Replace(k.file, os.Getenv("HOME"), "~", 1) + ", readable only by you"
	}
	defer done()
	if c.Ours() {
		return "the keyring, in pass (inua-keyring)"
	}
	return "the keyring (Secret Service)"
}

// Saved lists the profiles with a saved key, with when it was saved, by
// profile. Nothing is decrypted: no passphrase is asked.
func (k *Keys) Saved() (map[string]uint64, error) {
	c, done, err := k.service()
	if err != nil {
		return nil, err
	}
	out := map[string]uint64{}
	if c == nil {
		saved, err := load(k.file)
		for p := range saved {
			out[p] = 1
		}
		return out, err
	}
	defer done()
	items, err := c.Search(map[string]string{"xdg:schema": Schema})
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if p := it.Attributes["profile"]; p != "" {
			out[p] = cmp.Or(it.Modified, 1)
		}
	}
	return out, nil
}

// Get returns a profile's key, "" when none is saved. The keyring may ask
// for its passphrase (pass: gpg's).
func (k *Keys) Get(profile string) (string, error) {
	c, done, err := k.service()
	if err != nil {
		return "", err
	}
	if c == nil {
		saved, err := load(k.file)
		return saved[profile], err
	}
	defer done()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	v, _, err := c.Lookup(ctx, attrs(profile))
	return strings.TrimSpace(string(v)), err
}

// Set saves a profile's key; "" removes it.
func (k *Keys) Set(profile, key string) error {
	key = strings.TrimSpace(key)
	c, done, err := k.service()
	if err != nil {
		return err
	}
	if c == nil {
		saved, err := load(k.file)
		if err != nil {
			return err
		}
		if key == "" {
			delete(saved, profile)
		} else {
			saved[profile] = key
		}
		return write(k.file, saved)
	}
	defer done()
	if key == "" {
		return c.Delete(attrs(profile))
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.Store(ctx, label(profile), attrs(profile), []byte(key))
}

// Rename moves a key to a profile's new name, without reading it.
func (k *Keys) Rename(from, to string) error {
	c, done, err := k.service()
	if err != nil {
		return err
	}
	if c == nil {
		saved, err := load(k.file)
		if err != nil || saved[from] == "" {
			return err
		}
		saved[to] = saved[from]
		delete(saved, from)
		return write(k.file, saved)
	}
	defer done()
	return c.Update(attrs(from), attrs(to), label(to))
}

// load reads the file's keys; none when there is no file. A file others
// can read is refused: someone may have seen the keys.
func load(path string) (map[string]string, error) {
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
		return nil, fmt.Errorf("%s: others can read it (chmod 600 it, or delete it and type the keys again)", path)
	}
	keys := map[string]string{}
	if err := json.NewDecoder(f).Decode(&keys); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return keys, nil
}

func write(path string, keys map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		return err
	}
	// Written beside it with 0600 from the start, then moved over it.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".keys-*.json")
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
