package keyring

import (
	"bytes"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestVault(t *testing.T) {
	for _, plain := range []bool{false, true} {
		v := NewVault()
		v.plain = plain // true: the fallback, mmap and mlock
		if err := v.Put("a", []byte("first")); err != nil {
			t.Fatal(err)
		}
		big := bytes.Repeat([]byte("x"), 10000) // more than a page
		if err := v.Put("b", big); err != nil {
			t.Fatal(err)
		}
		if got, ok := v.Get("a"); !ok || string(got) != "first" {
			t.Errorf("plain %v: a = %q %v", plain, got, ok)
		}
		if got, _ := v.Get("b"); !bytes.Equal(got, big) {
			t.Errorf("plain %v: b is %d bytes", plain, len(got))
		}
		v.Put("a", []byte("second"))
		if got, _ := v.Get("a"); string(got) != "second" {
			t.Errorf("plain %v: replaced a = %q", plain, got)
		}
		got, _ := v.Get("a")
		clear(got) // the copy, not what is kept
		if again, _ := v.Get("a"); string(again) != "second" {
			t.Errorf("plain %v: clearing a copy changed it: %q", plain, again)
		}
		v.Remove("a")
		if v.Has("a") || v.Len() != 1 {
			t.Errorf("plain %v: after Remove: has a %v, %d kept", plain, v.Has("a"), v.Len())
		}
		v.Wipe()
		if _, ok := v.Get("b"); ok || v.Len() != 0 {
			t.Errorf("plain %v: Wipe kept %d", plain, v.Len())
		}
	}
}

// counting is a backend that counts reads, whose entries have stamps,
// and that can refuse to read: gpg-agent without the passphrase.
type counting struct {
	*memory
	mu     sync.Mutex
	shows  map[string]int
	stamps map[string]string
	locked bool // ShowQuiet fails, as gpg with --pinentry-mode=error
}

func newCounting() *counting {
	return &counting{memory: newMemory(), shows: map[string]int{}, stamps: map[string]string{}}
}

func (c *counting) Show(entry string) ([]byte, error) {
	c.mu.Lock()
	c.shows[entry]++
	c.mu.Unlock()
	return c.memory.Show(entry)
}

func (c *counting) ShowQuiet(entry string) ([]byte, error) {
	c.mu.Lock()
	locked := c.locked
	c.mu.Unlock()
	if locked {
		return nil, errLocked
	}
	return c.Show(entry)
}

var errLocked = errors.New("gpg: no passphrase, and no pinentry to ask")

func (c *counting) Stamp(entry string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stamps[entry]
}

func (c *counting) count(entry string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.shows[entry]
}

func (c *counting) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, v := range c.shows {
		n += v
	}
	return n
}

// waitKept waits for the store to keep n secrets.
func waitKept(t *testing.T, v *Vault, n int) {
	t.Helper()
	for range 200 {
		if v.Len() == n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%d secrets kept, want %d", v.Len(), n)
}

func TestUnlock(t *testing.T) {
	back := newCounting()
	s, err := Open(filepath.Join(t.TempDir(), "index.json"), back)
	if err != nil {
		t.Fatal(err)
	}
	v := NewVault()
	s.KeepUnlocked(v)
	var events []bool
	s.OnLock = func(on bool) { events = append(events, on) }
	a, _, _ := s.CreateItem(Login, "a", map[string]string{"n": "a"}, []byte("alpha"), "", false)
	b, _, _ := s.CreateItem(Login, "b", map[string]string{"n": "b"}, []byte("beta"), "", false)
	c, _, _ := s.CreateItem(Login, "c", map[string]string{"n": "c"}, []byte{0xff, 0}, "", false)
	if v.Len() != 0 {
		t.Fatalf("written while locked, %d kept", v.Len())
	}
	if on, _ := s.Unlocked(); on {
		t.Fatal("unlocked before a read")
	}

	// The first read unlocks; the others are read meanwhile.
	if got, _, err := s.Secret(Login, a.ID, "app\ta"); err != nil || string(got) != "alpha" {
		t.Fatalf("a = %q %v", got, err)
	}
	waitKept(t, v, 3)
	if on, _ := s.Unlocked(); !on {
		t.Error("not unlocked after a read")
	}
	before := back.total()
	for range 3 {
		got, _, _ := s.Secret(Login, b.ID, "")
		bin, _, _ := s.Secret(Login, c.ID, "")
		if string(got) != "beta" || !bytes.Equal(bin, []byte{0xff, 0}) {
			t.Fatalf("from the vault: %q %v", got, bin)
		}
	}
	if back.total() != before {
		t.Errorf("unlocked reads went to the backend %d times", back.total()-before)
	}

	// A write while unlocked is kept; an entry changed outside is read
	// again.
	s.SetSecret(Login, b.ID, []byte("beta 2"), "")
	if got, _, _ := s.Secret(Login, b.ID, ""); string(got) != "beta 2" || back.total() != before {
		t.Errorf("after SetSecret: %q, %d reads", got, back.total()-before)
	}
	back.memory.Insert("login/a", []byte("edited"))
	back.mu.Lock()
	back.stamps["login/a"] = "edited with pass"
	back.mu.Unlock()
	if got, _, _ := s.Secret(Login, a.ID, ""); string(got) != "edited" {
		t.Errorf("an entry edited outside: %q", got)
	}
	s.DeleteItem(Login, c.ID)
	if v.Has("login/c") {
		t.Error("a deleted item stayed in the vault")
	}

	// Lock wipes; the next read goes to the backend.
	s.Lock()
	if v.Len() != 0 {
		t.Errorf("%d kept after Lock", v.Len())
	}
	n := back.count("login/b")
	if got, _, _ := s.Secret(Login, b.ID, ""); string(got) != "beta 2" || back.count("login/b") != n+1 {
		t.Errorf("after Lock: %q, read %d times", got, back.count("login/b")-n)
	}
	waitKept(t, v, 2)
	if len(events) != 3 || !events[0] || events[1] || !events[2] {
		t.Errorf("lock events %v, want unlock, lock, unlock", events)
	}

	// Without the passphrase in gpg-agent, the rest wait for their reads.
	s.Lock()
	back.mu.Lock()
	back.locked = true
	back.mu.Unlock()
	s.Secret(Login, a.ID, "")
	waitKept(t, v, 1)
	time.Sleep(20 * time.Millisecond)
	if v.Has("login/b") {
		t.Error("filled without the passphrase")
	}
}
