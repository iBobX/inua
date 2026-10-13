package keyring

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lyricat/inua/internal/pinentry"
)

// showerFor is a Backend that can say whom a read is for: Pass, so its
// pinentry names the app.
type showerFor interface {
	ShowFor(entry, who string) ([]byte, error)
}

// Backend keeps the secrets themselves, by entry ("login/chrome-safe-
// storage", under the store's folder). Pass is the real one; tests use a
// map.
type Backend interface {
	Show(entry string) ([]byte, error)
	Insert(entry string, secret []byte) error
	Remove(entry string) error
	// Exists reports whether an entry is there, whoever made it, so a name
	// never clobbers the user's own.
	Exists(entry string) bool
}

// Pass keeps secrets with the pass command: gpg-encrypted, for the
// store's .gpg-id, committed when the store is a git repository.
type Pass struct {
	Dir    string // the password store: PASSWORD_STORE_DIR, else ~/.password-store
	Folder string // inua-keyring
}

// StoreDir is pass's password store.
func StoreDir() string {
	if d := os.Getenv("PASSWORD_STORE_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".password-store")
}

// Ready says why the store cannot be used, or nil: pass installed, and
// the store set up with `pass init`.
func (p Pass) Ready() error {
	if _, err := exec.LookPath("pass"); err != nil {
		return errors.New("pass is not installed")
	}
	if _, err := os.Stat(filepath.Join(p.Dir, ".gpg-id")); err != nil {
		return fmt.Errorf("no password store at %s (pass init GPG-ID sets one up)", p.Dir)
	}
	return nil
}

func (p Pass) name(entry string) string { return p.Folder + "/" + entry }

func (p Pass) run(stdin []byte, args ...string) ([]byte, error) {
	return p.runFor("", stdin, args...)
}

// runFor runs pass; who, when set, is handed to the pinentry gpg-agent may
// start (PINENTRY_USER_DATA), for it to say who the key is wanted for.
func (p Pass) runFor(who string, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.Command("pass", args...)
	cmd.Env = append(os.Environ(), "PASSWORD_STORE_DIR="+p.Dir)
	if who != "" {
		cmd.Env = append(cmd.Env, "PINENTRY_USER_DATA="+pinentry.UserData+who)
	}
	var out, errb bytes.Buffer
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("pass %s: %v: %s", args[0], err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

func (p Pass) Show(entry string) ([]byte, error) { return p.run(nil, "show", p.name(entry)) }

// ShowFor is Show for an app: who is "app\tsecret", for the passphrase
// dialog.
func (p Pass) ShowFor(entry, who string) ([]byte, error) {
	return p.runFor(who, nil, "show", p.name(entry))
}

// Insert writes the secret exactly as given (-m: to the end of input).
func (p Pass) Insert(entry string, secret []byte) error {
	_, err := p.run(secret, "insert", "--multiline", "--force", p.name(entry))
	return err
}

func (p Pass) Remove(entry string) error {
	_, err := p.run(nil, "rm", "--force", p.name(entry))
	return err
}

func (p Pass) Exists(entry string) bool {
	_, err := os.Stat(filepath.Join(p.Dir, p.Folder, entry+".gpg"))
	return err == nil
}

// memory keeps secrets in memory: the session collection, and tests. It
// has a lock of its own: secrets are read outside the store's.
type memory struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newMemory() *memory { return &memory{m: map[string][]byte{}} }

func (b *memory) Show(entry string) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	v, ok := b.m[entry]
	if !ok {
		return nil, fmt.Errorf("%s: no such entry", entry)
	}
	return bytes.Clone(v), nil
}

func (b *memory) Insert(entry string, secret []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.m[entry] = bytes.Clone(secret)
	return nil
}

func (b *memory) Remove(entry string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.m, entry)
	return nil
}

func (b *memory) Exists(entry string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.m[entry]
	return ok
}

// entries copies what is kept, for tests.
func (b *memory) entries() map[string]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := map[string]string{}
	for k, v := range b.m {
		out[k] = string(v)
	}
	return out
}

// NewMemoryBackend keeps secrets in memory, for tests of packages using
// the service.
func NewMemoryBackend() Backend { return newMemory() }
