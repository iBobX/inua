package mail

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
)

// Role is what a mailbox is for, from its special-use flag or its name.
type Role string

const (
	RoleInbox   Role = "inbox"
	RoleDrafts  Role = "drafts"
	RoleSent    Role = "sent"
	RoleArchive Role = "archive"
	RoleJunk    Role = "junk"
	RoleTrash   Role = "trash"
	RoleOther   Role = ""
)

// Roles are the roles in the order the index lists them.
var Roles = []Role{RoleInbox, RoleDrafts, RoleSent, RoleArchive, RoleJunk, RoleTrash}

// Mailbox is a mailbox on the server.
type Mailbox struct {
	Name   string `json:"name"`
	Role   Role   `json:"role,omitempty"`
	Unread int    `json:"unread"`
	Total  int    `json:"total"`
}

// Index is what the cache keeps of a mailbox.
type Index struct {
	Validity uint32            `json:"validity"`
	Older    bool              `json:"older,omitempty"`   // the server has messages older than these
	Messages []Envelope        `json:"messages"`          // newest UID first
	Summary  map[string]string `json:"summary,omitempty"` // conversation → the assistant's summary
}

// Find is the message with this UID, or nil.
func (x *Index) Find(uid uint32) *Envelope {
	i, ok := slices.BinarySearchFunc(x.Messages, uid, func(e Envelope, u uint32) int {
		switch {
		case e.UID > u:
			return -1
		case e.UID < u:
			return 1
		}
		return 0
	})
	if !ok {
		return nil
	}
	return &x.Messages[i]
}

// Highest is the highest UID known; 0 when none.
func (x *Index) Highest() uint32 {
	if len(x.Messages) == 0 {
		return 0
	}
	return x.Messages[0].UID
}

// Lowest is the lowest UID known; 0 when none.
func (x *Index) Lowest() uint32 {
	if len(x.Messages) == 0 {
		return 0
	}
	return x.Messages[len(x.Messages)-1].UID
}

// Put adds messages, keeping the newest UID first; a UID known already
// is replaced but keeps its tag.
func (x *Index) Put(es ...Envelope) {
	for _, e := range es {
		if old := x.Find(e.UID); old != nil {
			if e.Tag == "" {
				e.Tag, e.TagByUser = old.Tag, old.TagByUser
			}
			*old = e
			continue
		}
		x.Messages = append(x.Messages, e)
	}
	slices.SortFunc(x.Messages, func(a, b Envelope) int { return int(int64(b.UID) - int64(a.UID)) })
}

// Drop takes out the messages gone says are gone, and returns their UIDs.
func (x *Index) Drop(gone func(uid uint32) bool) (dropped []uint32) {
	x.Messages = slices.DeleteFunc(x.Messages, func(e Envelope) bool {
		if gone(e.UID) {
			dropped = append(dropped, e.UID)
			return true
		}
		return false
	})
	return dropped
}

// Unread counts the messages not seen.
func (x *Index) Unread() int {
	n := 0
	for i := range x.Messages {
		if !x.Messages[i].Has(Seen) {
			n++
		}
	}
	return n
}

// Store is the cache on disk: $XDG_CACHE_HOME/inua/mail/ACCOUNT/MAILBOX/.
// It is a copy of the servers' and can be deleted at any time.
type Store struct {
	dir string
	mu  sync.Mutex // one write of a file at a time
}

// NewStore is the cache in the user's cache folder.
func NewStore() *Store {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache")
	}
	return &Store{dir: filepath.Join(dir, "inua", "mail")}
}

// StoreAt is a cache in dir, for tests.
func StoreAt(dir string) *Store { return &Store{dir: dir} }

func (s *Store) box(account, mailbox string) string {
	return filepath.Join(s.dir, url.PathEscape(account), url.PathEscape(mailbox))
}

// Mailboxes reads an account's mailboxes as last listed.
func (s *Store) Mailboxes(account string) ([]Mailbox, error) {
	var out []Mailbox
	err := s.read(filepath.Join(s.dir, url.PathEscape(account), "mailboxes.json"), &out)
	return out, err
}

// SaveMailboxes keeps an account's mailboxes.
func (s *Store) SaveMailboxes(account string, boxes []Mailbox) error {
	return s.write(filepath.Join(s.dir, url.PathEscape(account), "mailboxes.json"), boxes)
}

// Index reads a mailbox's index; an empty one when there is none.
func (s *Store) Index(account, mailbox string) (*Index, error) {
	x := &Index{}
	err := s.read(filepath.Join(s.box(account, mailbox), "index.json"), x)
	return x, err
}

// SaveIndex keeps a mailbox's index.
func (s *Store) SaveIndex(account, mailbox string, x *Index) error {
	return s.write(filepath.Join(s.box(account, mailbox), "index.json"), x)
}

// Raw reads a message kept; ok false when it is not.
func (s *Store) Raw(account, mailbox string, uid uint32) ([]byte, bool) {
	b, err := os.ReadFile(filepath.Join(s.box(account, mailbox), strconv.FormatUint(uint64(uid), 10)+".eml"))
	return b, err == nil
}

// SaveRaw keeps a message.
func (s *Store) SaveRaw(account, mailbox string, uid uint32, raw []byte) error {
	dir := s.box(account, mailbox)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, strconv.FormatUint(uint64(uid), 10)+".eml"), raw)
}

// DropRaw removes kept messages, after they left the mailbox.
func (s *Store) DropRaw(account, mailbox string, uids ...uint32) {
	for _, u := range uids {
		os.Remove(filepath.Join(s.box(account, mailbox), strconv.FormatUint(uint64(u), 10)+".eml"))
	}
}

// Reset forgets a mailbox, after its UIDVALIDITY changed.
func (s *Store) Reset(account, mailbox string) error {
	return os.RemoveAll(s.box(account, mailbox))
}

// Forget removes an account's cache.
func (s *Store) Forget(account string) error {
	return os.RemoveAll(filepath.Join(s.dir, url.PathEscape(account)))
}

func (s *Store) read(path string, v any) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return nil // a broken cache is an empty one; the server fills it again
	}
	return nil
}

func (s *Store) write(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeFile(path, b)
}

// writeFile writes beside path and moves it over, so a reader never sees
// half a file.
func writeFile(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Dir is the folder the store keeps everything in, every account's.
func (s *Store) Dir() string { return s.dir }

// AvatarPath is where an account's photo is kept.
func (s *Store) AvatarPath(account string) string {
	return filepath.Join(s.dir, url.PathEscape(account), "avatar")
}
