package keyring

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newStore(t *testing.T) (*Store, *memory, string) {
	t.Helper()
	back := newMemory()
	path := filepath.Join(t.TempDir(), "inua-keyring", ".index.json")
	s, err := Open(path, back)
	if err != nil {
		t.Fatal(err)
	}
	return s, back, path
}

func TestStore(t *testing.T) {
	s, back, path := newStore(t)
	if s.Alias("default") != Login {
		t.Fatalf("default alias: %q", s.Alias("default"))
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("opening wrote the index")
	}
	chrome := map[string]string{"application": "chrome"}
	it, created, err := s.CreateItem(Login, "Chrome Safe Storage", chrome, []byte("s3cret"), "text/plain", true)
	if err != nil || !created {
		t.Fatal(err)
	}
	// The secret is the entry, as it is: `pass show` shows it.
	if got := back.m["login/chrome-safe-storage"]; string(got) != "s3cret" {
		t.Errorf("entry %q", got)
	}
	// Replacing: the same attributes, a new secret.
	it2, created, _ := s.CreateItem(Login, "Chrome Safe Storage", chrome, []byte("new"), "", true)
	if created || it2.ID != it.ID {
		t.Errorf("replace made another item: %+v", it2)
	}
	// Not replacing: another item, its entry named apart.
	it3, _, _ := s.CreateItem(Login, "Chrome Safe Storage", chrome, []byte("third"), "", false)
	if _, ok := back.m["login/chrome-safe-storage-2"]; !ok || it3.ID == it.ID {
		t.Errorf("second item: %+v %v", it3, back.m)
	}
	if found := s.Search("", map[string]string{"application": "chrome"}); len(found) != 2 {
		t.Errorf("search: %+v", found)
	}
	if found := s.Search(Login, map[string]string{"application": "other"}); len(found) != 0 {
		t.Errorf("search: %+v", found)
	}
	secret, ct, err := s.Secret(Login, it.ID, "")
	if err != nil || string(secret) != "new" || ct != "text/plain" {
		t.Errorf("secret %q %q %v", secret, ct, err)
	}

	// Not text: base64 in the entry, the bytes back.
	bin := []byte{0, 1, 2, 0xff}
	bi, _, _ := s.CreateItem(Login, "", nil, bin, "application/octet-stream", false)
	if got := back.m["login/item"]; string(got) != base64.StdEncoding.EncodeToString(bin) {
		t.Errorf("binary entry %q", got)
	}
	if got, _, _ := s.Secret(Login, bi.ID, ""); !bytes.Equal(got, bin) {
		t.Errorf("binary %v", got)
	}

	// Another process (inua keyring import) sees and changes the same index.
	other, err := Open(path, back)
	if err != nil {
		t.Fatal(err)
	}
	if items, _ := other.Items(Login); len(items) != 3 {
		t.Errorf("the other sees %d items", len(items))
	}
	if err := other.SetLabel(Login, it.ID, "Renamed"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Item(Login, it.ID); got.Label != "Renamed" {
		t.Errorf("the change was not seen: %+v", got)
	}

	if err := s.DeleteItem(Login, it3.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := back.m["login/chrome-safe-storage-2"]; ok {
		t.Error("the entry stayed")
	}
	if _, err := s.Item(Login, it3.ID); !errors.Is(err, ErrNoItem) {
		t.Error("the item stayed")
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("index mode %v", info.Mode().Perm())
	}
}

func TestEntryNames(t *testing.T) {
	s, back, _ := newStore(t)
	back.m["login/wi-fi"] = []byte("the user's own") // never clobbered
	it, _, _ := s.CreateItem(Login, "Wi-Fi", nil, []byte("x"), "", false)
	if string(back.m["login/wi-fi"]) != "the user's own" || string(back.m["login/wi-fi-2"]) != "x" {
		t.Errorf("%+v %v", it, back.m)
	}
}

func TestCollections(t *testing.T) {
	s, back, _ := newStore(t)
	work, err := s.CreateCollection("Work Stuff", "")
	if err != nil || work.ID != "work_stuff" {
		t.Fatalf("%+v %v", work, err)
	}
	again, _ := s.CreateCollection("Work Stuff", "")
	if again.ID != "work_stuff_2" {
		t.Errorf("a second one: %+v", again)
	}
	// An alias already naming one returns it.
	s.SetAlias("mine", work.ID)
	if c, _ := s.CreateCollection("Other", "mine"); c.ID != work.ID {
		t.Errorf("alias: %+v", c)
	}
	s.CreateItem(work.ID, "a", nil, []byte("1"), "", false)
	if err := s.DeleteCollection(work.ID); err != nil {
		t.Fatal(err)
	}
	if len(back.m) != 0 || s.Alias("mine") != "" {
		t.Errorf("left: %v %q", back.m, s.Alias("mine"))
	}

	// The session collection is in memory only.
	it, _, _ := s.CreateItem(Session, "temp", map[string]string{"k": "v"}, []byte("t"), "", false)
	if len(back.m) != 0 {
		t.Error("a session item reached the store")
	}
	if got, _, _ := s.Secret(Session, it.ID, ""); string(got) != "t" {
		t.Errorf("session secret %q", got)
	}
	if len(s.Collections()) != 3 { // login, work_stuff_2, session
		t.Errorf("collections %+v", s.Collections())
	}
}
