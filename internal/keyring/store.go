// Package keyring is inua's Secret Service (org.freedesktop.secrets),
// keeping every item in pass: the secret in a gpg-encrypted entry under
// the store's folder, the labels and attributes, which the specification
// does not treat as secret, in an index beside them. cmd/inua-keyring
// serves it; docs/feat/keyring.md has the design.
package keyring

import (
	"bytes"
	"cmp"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	// Login is the collection the default alias names, as gnome-keyring's.
	Login = "login"
	// Session is kept in memory and forgotten when the service stops.
	Session = "session"
)

var (
	ErrNoCollection = errors.New("no such collection")
	ErrNoItem       = errors.New("no such item")
)

// Collection is a group of items.
type Collection struct {
	ID, Label         string
	Created, Modified uint64 // Unix seconds
}

// Item is a secret's description; the secret is read with Store.Secret.
type Item struct {
	Collection, ID    string
	Label             string
	Attributes        map[string]string
	ContentType       string
	Created, Modified uint64
}

// index is what the index file holds.
type index struct {
	Collections []*collRec        `json:"collections"`
	Aliases     map[string]string `json:"aliases"`
}

type collRec struct {
	ID       string     `json:"id"`
	Label    string     `json:"label"`
	Created  uint64     `json:"created"`
	Modified uint64     `json:"modified"`
	NextID   int        `json:"next_id"`
	Items    []*itemRec `json:"items"`
}

type itemRec struct {
	ID          string            `json:"id"`
	Label       string            `json:"label"`
	Attributes  map[string]string `json:"attributes"`
	ContentType string            `json:"content_type,omitempty"`
	Entry       string            `json:"entry"`              // the backend's, "login/chrome-safe-storage"
	Encoding    string            `json:"encoding,omitempty"` // "base64" for a secret that is not text
	Created     uint64            `json:"created"`
	Modified    uint64            `json:"modified"`
}

// Store is the items, kept in a Backend with an index file. It can be
// shared with another process (inua keyring import): the index is read
// again when it changed on disk, and writes hold a lock on it.
type Store struct {
	path string // the index file
	back Backend
	// commit records the index in the store's git repository; nil when it
	// is not one.
	commit func(msg string)

	mu      sync.Mutex
	idx     index
	stamp   string  // the index file's time and size when read
	session collRec // in memory
	mem     *memory // the session collection's secrets
}

// OpenPass opens the store in pass's folder.
func OpenPass(p Pass) (*Store, error) {
	if err := p.Ready(); err != nil {
		return nil, err
	}
	s, err := Open(filepath.Join(p.Dir, p.Folder, ".index.json"), p)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(p.Dir, ".git")); err == nil {
		rel := filepath.Join(p.Folder, ".index.json")
		s.commit = func(msg string) {
			git := func(args ...string) error {
				return exec.Command("git", append([]string{"-C", p.Dir}, args...)...).Run()
			}
			if git("add", "--", rel) == nil {
				git("commit", "--quiet", "-m", msg, "--", rel)
			}
		}
	}
	return s, nil
}

// Open opens the store whose index is at path, keeping secrets in back.
// A new store has the login collection, the default alias; nothing is
// written until something is stored.
func Open(path string, back Backend) (*Store, error) {
	s := &Store{path: path, back: back, mem: newMemory()}
	now := unix()
	s.session = collRec{ID: Session, Label: "Session", Created: now, Modified: now, NextID: 1}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// withLogin gives an index without collections the login collection, as
// the default alias.
func withLogin(idx *index) {
	if len(idx.Collections) == 0 {
		now := unix()
		idx.Collections = []*collRec{{ID: Login, Label: "Login", Created: now, Modified: now, NextID: 1}}
		if idx.Aliases["default"] == "" {
			idx.Aliases["default"] = Login
		}
	}
}

func unix() uint64 { return uint64(time.Now().Unix()) }

// load reads the index when it changed on disk since it was last read.
func (s *Store) load() error {
	info, err := os.Stat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		if s.stamp != "" || s.idx.Aliases == nil {
			s.idx = index{Aliases: map[string]string{}}
			withLogin(&s.idx)
			s.stamp = ""
		}
		return nil
	}
	if err != nil {
		return err
	}
	stamp := fmt.Sprint(info.ModTime().UnixNano(), info.Size())
	if stamp == s.stamp {
		return nil
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var idx index
	if err := json.Unmarshal(b, &idx); err != nil {
		return fmt.Errorf("%s: %w", s.path, err)
	}
	if idx.Aliases == nil {
		idx.Aliases = map[string]string{}
	}
	withLogin(&idx)
	s.idx, s.stamp = idx, stamp
	return nil
}

// write changes the index under its lock: read again, change, save.
func (s *Store) write(msg string, change func() error) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	s.stamp = "" // whatever another process wrote
	if err := s.load(); err != nil {
		return err
	}
	if err := change(); err != nil {
		s.stamp = "" // the change may be half made: read the file again
		return err
	}
	b, err := json.MarshalIndent(s.idx, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	s.stamp = ""
	if err := s.load(); err != nil {
		return err
	}
	if s.commit != nil {
		s.commit("inua-keyring: " + msg)
	}
	return nil
}

// fresh reads the index again if another process changed it. A failure
// keeps what was read before.
func (s *Store) fresh() {
	if err := s.load(); err != nil {
		fmt.Fprintln(os.Stderr, "keyring:", err)
	}
}

func (s *Store) coll(id string) *collRec {
	if id == Session {
		return &s.session
	}
	for _, c := range s.idx.Collections {
		if c.ID == id {
			return c
		}
	}
	return nil
}

func (s *Store) backend(coll string) Backend {
	if coll == Session {
		return s.mem
	}
	return s.back
}

// persisted runs change, saving the index unless the collection is the
// session's.
func (s *Store) persisted(coll, msg string, change func() error) error {
	if coll == Session {
		return change()
	}
	return s.write(msg, change)
}

func (c *collRec) public() Collection {
	return Collection{ID: c.ID, Label: c.Label, Created: c.Created, Modified: c.Modified}
}

func (r *itemRec) public(coll string) Item {
	return Item{Collection: coll, ID: r.ID, Label: r.Label, Attributes: maps.Clone(r.Attributes),
		ContentType: r.ContentType, Created: r.Created, Modified: r.Modified}
}

func (c *collRec) item(id string) *itemRec {
	for _, r := range c.Items {
		if r.ID == id {
			return r
		}
	}
	return nil
}

// Collections lists the collections, the session's last.
func (s *Store) Collections() []Collection {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fresh()
	var out []Collection
	for _, c := range s.idx.Collections {
		out = append(out, c.public())
	}
	return append(out, s.session.public())
}

// Collection returns a collection by ID.
func (s *Store) Collection(id string) (Collection, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fresh()
	if c := s.coll(id); c != nil {
		return c.public(), true
	}
	return Collection{}, false
}

// Alias is the collection an alias names, or "".
func (s *Store) Alias(name string) string {
	if name == Session {
		return Session
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fresh()
	if id := s.idx.Aliases[name]; s.coll(id) != nil {
		return id
	}
	return ""
}

// SetAlias points an alias at a collection; "" removes it.
func (s *Store) SetAlias(name, id string) error {
	if name == Session {
		return errors.New("the session alias cannot change")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write("alias "+name, func() error {
		if id == "" {
			delete(s.idx.Aliases, name)
			return nil
		}
		if c := s.coll(id); c == nil || id == Session {
			return ErrNoCollection
		}
		s.idx.Aliases[name] = id
		return nil
	})
}

var nonID = regexp.MustCompile(`[^a-z0-9_]+`)

// CreateCollection makes a collection; with an alias already naming one,
// that one is returned instead, as the specification says.
func (s *Store) CreateCollection(label, alias string) (Collection, error) {
	if alias == Session {
		return s.session.public(), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var made *collRec
	err := s.write("collection "+cmp.Or(label, alias), func() error {
		if c := s.coll(s.idx.Aliases[alias]); alias != "" && c != nil {
			made = c
			return nil
		}
		base := strings.Trim(nonID.ReplaceAllString(strings.ToLower(cmp.Or(label, alias, "collection")), "_"), "_")
		id := cmp.Or(base, "collection")
		for n := 2; s.coll(id) != nil || id == Session || id == "aliases"; n++ {
			id = fmt.Sprintf("%s_%d", cmp.Or(base, "collection"), n)
		}
		now := unix()
		made = &collRec{ID: id, Label: label, Created: now, Modified: now, NextID: 1}
		s.idx.Collections = append(s.idx.Collections, made)
		if alias != "" {
			s.idx.Aliases[alias] = id
		}
		return nil
	})
	if err != nil {
		return Collection{}, err
	}
	return made.public(), nil
}

// DeleteCollection deletes a collection and its items. The session's is
// emptied instead.
func (s *Store) DeleteCollection(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == Session {
		for _, r := range s.session.Items {
			s.mem.Remove(r.Entry)
		}
		s.session.Items = nil
		return nil
	}
	return s.write("delete collection "+id, func() error {
		c := s.coll(id)
		if c == nil {
			return ErrNoCollection
		}
		for _, r := range c.Items {
			if err := s.back.Remove(r.Entry); err != nil && s.back.Exists(r.Entry) {
				return err
			}
		}
		s.idx.Collections = slices.DeleteFunc(s.idx.Collections, func(x *collRec) bool { return x == c })
		for a, target := range s.idx.Aliases {
			if target == id {
				delete(s.idx.Aliases, a)
			}
		}
		return nil
	})
}

// SetCollectionLabel renames a collection; its ID stays.
func (s *Store) SetCollectionLabel(id, label string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persisted(id, "label collection "+id, func() error {
		c := s.coll(id)
		if c == nil {
			return ErrNoCollection
		}
		c.Label, c.Modified = label, unix()
		return nil
	})
}

// Items lists a collection's items.
func (s *Store) Items(coll string) ([]Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fresh()
	c := s.coll(coll)
	if c == nil {
		return nil, ErrNoCollection
	}
	var out []Item
	for _, r := range c.Items {
		out = append(out, r.public(coll))
	}
	return out, nil
}

// Item returns an item.
func (s *Store) Item(coll, id string) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fresh()
	c := s.coll(coll)
	if c == nil {
		return Item{}, ErrNoCollection
	}
	r := c.item(id)
	if r == nil {
		return Item{}, ErrNoItem
	}
	return r.public(coll), nil
}

// Search finds the items having all the attributes, in a collection, or
// in all with coll "".
func (s *Store) Search(coll string, attrs map[string]string) []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fresh()
	var out []Item
	for _, c := range append(slices.Clone(s.idx.Collections), &s.session) {
		if coll != "" && c.ID != coll {
			continue
		}
		for _, r := range c.Items {
			if matches(r.Attributes, attrs) {
				out = append(out, r.public(c.ID))
			}
		}
	}
	return out
}

func matches(have, want map[string]string) bool {
	for k, v := range want {
		if got, ok := have[k]; !ok || got != v {
			return false
		}
	}
	return true
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// entryName names a new item's entry from its label, unused by any item
// and by anything already in the store.
func (s *Store) entryName(c *collRec, label string) string {
	base := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(label), "-"), "-")
	if len(base) > 60 {
		base = strings.TrimRight(base[:60], "-")
	}
	base = c.ID + "/" + cmp.Or(base, "item")
	back := s.backend(c.ID)
	name := base
	for n := 2; ; n++ {
		taken := back.Exists(name)
		for _, cc := range append(slices.Clone(s.idx.Collections), &s.session) {
			for _, r := range cc.Items {
				taken = taken || r.Entry == name
			}
		}
		if !taken {
			return name
		}
		name = fmt.Sprintf("%s-%d", base, n)
	}
}

// encode keeps a secret that is text as it is, so `pass show` shows it,
// and anything else in base64.
func encode(secret []byte) ([]byte, string) {
	if utf8.Valid(secret) && !bytes.ContainsRune(secret, 0) {
		return secret, ""
	}
	return []byte(base64.StdEncoding.EncodeToString(secret)), "base64"
}

// CreateItem stores a secret. With replace, an item of the collection with
// the same attributes gets the new label and secret instead; created
// reports which happened.
func (s *Store) CreateItem(coll, label string, attrs map[string]string, secret []byte, contentType string, replace bool) (Item, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out Item
	created := false
	err := s.persisted(coll, "item "+label, func() error {
		c := s.coll(coll)
		if c == nil {
			return ErrNoCollection
		}
		now := unix()
		var r *itemRec
		if replace {
			for _, x := range c.Items {
				if maps.Equal(x.Attributes, attrs) || (len(x.Attributes) == 0 && len(attrs) == 0) {
					r = x
					break
				}
			}
		}
		if r == nil {
			r = &itemRec{ID: fmt.Sprint(c.NextID), Entry: s.entryName(c, label), Created: now}
			created = true
		}
		data, enc := encode(secret)
		if err := s.backend(coll).Insert(r.Entry, data); err != nil {
			return err
		}
		r.Label, r.Attributes, r.ContentType, r.Encoding, r.Modified = label, maps.Clone(attrs), contentType, enc, now
		if r.Attributes == nil {
			r.Attributes = map[string]string{}
		}
		if created {
			c.NextID++
			c.Items = append(c.Items, r)
		}
		c.Modified = now
		out = r.public(coll)
		return nil
	})
	return out, created, err
}

// Secret reads an item's secret and its content type; for pass, gpg-agent
// may ask for the passphrase, saying the read is for who ("app\tsecret";
// "" for no one in particular).
func (s *Store) Secret(coll, id, who string) ([]byte, string, error) {
	s.mu.Lock()
	c := s.coll(coll)
	var r itemRec
	if c != nil {
		if x := c.item(id); x != nil {
			r = *x
		}
	}
	s.mu.Unlock()
	if c == nil {
		return nil, "", ErrNoCollection
	}
	if r.ID == "" {
		return nil, "", ErrNoItem
	}
	// Not under the lock: pinentry may wait for the user.
	b := s.backend(coll)
	var data []byte
	var err error
	if f, ok := b.(showerFor); ok && who != "" {
		data, err = f.ShowFor(r.Entry, who)
	} else {
		data, err = b.Show(r.Entry)
	}
	if err != nil {
		return nil, "", err
	}
	if r.Encoding == "base64" {
		data, err = base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	}
	return data, cmp.Or(r.ContentType, "text/plain"), err
}

// change changes an item under the lock.
func (s *Store) change(coll, id, msg string, f func(c *collRec, r *itemRec) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persisted(coll, msg, func() error {
		c := s.coll(coll)
		if c == nil {
			return ErrNoCollection
		}
		r := c.item(id)
		if r == nil {
			return ErrNoItem
		}
		if err := f(c, r); err != nil {
			return err
		}
		r.Modified = unix()
		c.Modified = r.Modified
		return nil
	})
}

// SetSecret replaces an item's secret.
func (s *Store) SetSecret(coll, id string, secret []byte, contentType string) error {
	return s.change(coll, id, "secret "+coll+"/"+id, func(c *collRec, r *itemRec) error {
		data, enc := encode(secret)
		if err := s.backend(coll).Insert(r.Entry, data); err != nil {
			return err
		}
		r.Encoding, r.ContentType = enc, contentType
		return nil
	})
}

// SetLabel renames an item; its entry keeps its name.
func (s *Store) SetLabel(coll, id, label string) error {
	return s.change(coll, id, "label "+coll+"/"+id, func(_ *collRec, r *itemRec) error {
		r.Label = label
		return nil
	})
}

// SetAttributes replaces an item's attributes.
func (s *Store) SetAttributes(coll, id string, attrs map[string]string) error {
	return s.change(coll, id, "attributes "+coll+"/"+id, func(_ *collRec, r *itemRec) error {
		r.Attributes = maps.Clone(attrs)
		if r.Attributes == nil {
			r.Attributes = map[string]string{}
		}
		return nil
	})
}

// DeleteItem deletes an item and its entry.
func (s *Store) DeleteItem(coll, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persisted(coll, "delete "+coll+"/"+id, func() error {
		c := s.coll(coll)
		if c == nil {
			return ErrNoCollection
		}
		r := c.item(id)
		if r == nil {
			return ErrNoItem
		}
		back := s.backend(coll)
		if err := back.Remove(r.Entry); err != nil && back.Exists(r.Entry) {
			return err
		}
		c.Items = slices.DeleteFunc(c.Items, func(x *itemRec) bool { return x == r })
		c.Modified = unix()
		return nil
	})
}
