package keys

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbustest"
	"github.com/lyricat/inua/internal/keyring"
)

func TestFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inua", "keys.json")
	k := NewAt(path, nil)
	if err := k.Set("openai", " sk-1 "); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", info.Mode().Perm())
	}
	if err := k.Rename("openai", "work"); err != nil {
		t.Fatal(err)
	}
	if v, _ := k.Get("work"); v != "sk-1" {
		t.Errorf("renamed: %q", v)
	}
	if saved, _ := k.Saved(); saved["work"] == 0 || saved["x"] != 0 {
		t.Errorf("saved: %v", saved)
	}
	k.Set("work", "")
	if saved, _ := k.Saved(); len(saved) != 0 {
		t.Errorf("removed: %v", saved)
	}
	os.Chmod(path, 0o644)
	if _, err := k.Saved(); err == nil {
		t.Error("a file others can read is refused")
	}
}

// TestKeyring keeps the keys in a Secret Service (inua-keyring's, on a
// private bus): the file's keys move there.
func TestKeyring(t *testing.T) {
	addr := dbustest.Start(t)
	store, err := keyring.Open(filepath.Join(t.TempDir(), "index.json"), keyring.NewMemoryBackend())
	if err != nil {
		t.Fatal(err)
	}
	go keyring.NewService(dbustest.Conn(t, addr), store).Serve(t.Context())
	dial := func() (*dbus.Conn, error) { return dbus.Connect(addr) }
	for range 100 {
		if c, err := dial(); err == nil {
			var has bool
			c.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, keyring.Name).Store(&has)
			c.Close()
			if has {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	path := filepath.Join(t.TempDir(), "keys.json")
	NewAt(path, nil).Set("old", "sk-old") // from before the keyring
	k := NewAt(path, dial)
	if err := k.Set("work", "sk-work"); err != nil {
		t.Fatal(err)
	}
	if left, _ := load(path); len(left) != 0 {
		t.Errorf("left in the file: %v", left)
	}
	saved, err := k.Saved()
	if err != nil || saved["old"] == 0 || saved["work"] == 0 {
		t.Errorf("saved %v %v", saved, err)
	}
	if items := store.Search("", map[string]string{"xdg:schema": Schema, "profile": "work"}); len(items) != 1 || items[0].Label != "inua assistant: work" {
		t.Errorf("items %+v", items)
	}
	if err := k.Rename("work", "job"); err != nil {
		t.Fatal(err)
	}
	if v, err := k.Get("job"); err != nil || v != "sk-work" {
		t.Errorf("renamed: %q %v", v, err)
	}
	if v, _ := k.Get("work"); v != "" {
		t.Errorf("the old name: %q", v)
	}
	if err := k.Set("job", ""); err != nil {
		t.Fatal(err)
	}
	if v, _ := k.Get("job"); v != "" {
		t.Errorf("removed: %q", v)
	}
	if w := k.Where(); w != "the keyring, in pass (inua-keyring)" {
		t.Errorf("where: %q", w)
	}
	if token, err := k.ServeToken(false); err != nil || token != "" {
		t.Fatalf("missing endpoint token: %v", err)
	}
	token, err := k.ServeToken(true)
	if err != nil || len(token) < 26 {
		t.Fatalf("creating endpoint token: %v", err)
	}
	for _, create := range []bool{false, true} {
		got, err := k.ServeToken(create)
		if err != nil || got != token {
			t.Fatalf("endpoint token was not retained: %v", err)
		}
	}
	if saved, err := k.Saved(); err != nil || saved[ServeTokenKey] == 0 {
		t.Fatalf("endpoint token missing from key metadata: %v", err)
	}
	if left, _ := load(path); len(left) != 0 {
		t.Error("endpoint token reached the plaintext fallback")
	}
}

func TestServeTokenRequiresKeyring(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	if _, err := NewAt(path, nil).ServeToken(true); err == nil {
		t.Fatal("created an endpoint token without a Secret Service")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("created a plaintext fallback")
	}
}
