package mail

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"time"
)

// FirstSync is how many of a mailbox's newest messages a first sync
// fetches; Load older fetches as many again.
const FirstSync = 2000

// Session is an account at work: its sign-in, its IMAP connection and
// its mailboxes' indexes, in memory and in the cache. Its methods may be
// called from any goroutine; commands on the connection take turns.
type Session struct {
	Acc     Account
	ep      Endpoint // an OAuth account's provider
	store   *Store
	secrets *Secrets

	mu    sync.Mutex // the connection, one command at a time
	conn  *Conn
	auth  *Auth
	boxes []Mailbox

	imu     sync.Mutex // the indexes
	indexes map[string]*Index
}

// NewSession makes an account's session; nothing connects yet.
func NewSession(acc Account, settings Settings, store *Store, secrets *Secrets) *Session {
	s := &Session{Acc: acc, store: store, secrets: secrets, indexes: map[string]*Index{}}
	if acc.OAuth() {
		s.ep, _ = settings.Endpoint(acc.Provider)
	}
	return s
}

// UseAuth signs in with auth instead of the keyring's, for tests and for
// checking a new account before it is saved.
func (s *Session) UseAuth(a Auth) { s.mu.Lock(); s.auth = &a; s.mu.Unlock() }

// signIn is how the account signs in: the password or the refresh token
// from the keyring.
func (s *Session) signIn() (Auth, error) {
	if s.auth != nil {
		return *s.auth, nil
	}
	if s.Acc.OAuth() {
		if s.ep.Client.ClientID == "" {
			_, err := Settings{}.Endpoint(s.Acc.Provider)
			return Auth{}, err
		}
		refresh, err := s.secrets.Get(s.Acc.ID, SecretRefresh)
		if err != nil {
			return Auth{}, err
		}
		if refresh == "" {
			return Auth{}, ErrSignInAgain
		}
		id := s.Acc.ID
		a := Auth{User: s.Acc.User(), Tokens: NewTokens(s.ep, refresh, func(r string) error { return s.secrets.Set(id, SecretRefresh, r) })}
		s.auth = &a
		return a, nil
	}
	pw, err := s.secrets.Get(s.Acc.ID, SecretPassword)
	if err != nil {
		return Auth{}, err
	}
	if pw == "" {
		return Auth{}, ErrSignInAgain
	}
	a := Auth{User: s.Acc.User(), Password: pw}
	s.auth = &a
	return a, nil
}

// Do runs f on the connection, connecting first when there is none, and
// once more on a new connection when the old one had dropped.
func (s *Session) Do(ctx context.Context, f func(*Conn) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for try := 0; ; try++ {
		if s.conn == nil {
			a, err := s.signIn()
			if err != nil {
				return err
			}
			addr, _ := s.Acc.Servers()
			c, err := Dial(ctx, addr, a, nil)
			if err != nil {
				if errors.Is(err, ErrAuth) || errors.Is(err, ErrSignInAgain) {
					s.auth = nil // the keyring may hold a new one next time
				}
				return err
			}
			s.conn = c
		}
		err := f(s.conn)
		if err == nil || try > 0 || !dropped(s.conn, err) {
			return err
		}
		s.conn.c.Close()
		s.conn = nil
	}
}

// dropped says whether an error came from a connection that ended.
func dropped(c *Conn, err error) bool {
	select {
	case <-c.Closed():
		return true
	default:
	}
	var ne net.Error
	return errors.As(err, &ne) || errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "use of closed network connection") ||
		strings.Contains(err.Error(), "EOF")
}

// Close ends the connection.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
}

// Cached reads the mailboxes and the inbox from the cache, for the first
// frame.
func (s *Session) Cached() []Mailbox {
	boxes, _ := s.store.Mailboxes(s.Acc.ID)
	s.mu.Lock()
	if s.boxes == nil {
		s.boxes = boxes
	}
	s.mu.Unlock()
	return boxes
}

// Mailboxes lists the mailboxes on the server, and keeps them.
func (s *Session) Mailboxes(ctx context.Context) ([]Mailbox, error) {
	var boxes []Mailbox
	err := s.Do(ctx, func(c *Conn) error {
		var err error
		boxes, err = c.Mailboxes()
		return err
	})
	if err != nil {
		return nil, err
	}
	s.store.SaveMailboxes(s.Acc.ID, boxes)
	s.mu.Lock()
	s.boxes = boxes
	s.mu.Unlock()
	return boxes, nil
}

// Role is the name of the mailbox with a role; "" when there is none.
func (s *Session) Role(r Role) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range s.boxes {
		if b.Role == r {
			return b.Name
		}
	}
	if r == RoleInbox {
		return "INBOX"
	}
	return ""
}

// index is a mailbox's index in memory, read from the cache the first
// time; s.imu is held.
func (s *Session) index(mailbox string) *Index {
	x := s.indexes[mailbox]
	if x == nil {
		x, _ = s.store.Index(s.Acc.ID, mailbox)
		s.indexes[mailbox] = x
	}
	return x
}

// Items are a mailbox's messages as known now (from the cache before the
// first sync), newest first. They are copies.
func (s *Session) Items(mailbox string) []Item {
	s.imu.Lock()
	defer s.imu.Unlock()
	x := s.index(mailbox)
	out := make([]Item, len(x.Messages))
	for i := range x.Messages {
		e := x.Messages[i]
		e.Flags = slices.Clone(e.Flags)
		out[i] = Item{Account: s.Acc.ID, Mailbox: mailbox, Envelope: &e}
	}
	return out
}

// Older says whether the server has older messages than those known.
func (s *Session) Older(mailbox string) bool {
	s.imu.Lock()
	defer s.imu.Unlock()
	return s.index(mailbox).Older
}

// Sync brings a mailbox in step with the server and keeps it.
func (s *Session) Sync(ctx context.Context, mailbox string) (SyncResult, error) {
	s.imu.Lock()
	work := s.index(mailbox).copy()
	s.imu.Unlock()
	var res SyncResult
	err := s.Do(ctx, func(c *Conn) error {
		var err error
		res, err = c.Sync(mailbox, work, FirstSync)
		return err
	})
	if err != nil {
		return res, err
	}
	s.imu.Lock()
	// A flag set while it synced went to the server; the next sync reads
	// it back.
	s.indexes[mailbox] = work
	s.imu.Unlock()
	if res.Reset {
		s.store.Reset(s.Acc.ID, mailbox)
	}
	s.store.DropRaw(s.Acc.ID, mailbox, res.Gone...)
	if res.Changed || res.Reset {
		return res, s.store.SaveIndex(s.Acc.ID, mailbox, work)
	}
	return res, nil
}

// LoadOlder fetches older messages of a mailbox.
func (s *Session) LoadOlder(ctx context.Context, mailbox string) (int, error) {
	s.imu.Lock()
	work := s.index(mailbox).copy()
	s.imu.Unlock()
	var n int
	err := s.Do(ctx, func(c *Conn) error {
		var err error
		n, err = c.Older(mailbox, work, FirstSync)
		return err
	})
	if err != nil {
		return 0, err
	}
	s.imu.Lock()
	s.indexes[mailbox] = work
	s.imu.Unlock()
	return n, s.store.SaveIndex(s.Acc.ID, mailbox, work)
}

func (x *Index) copy() *Index {
	c := *x
	c.Messages = slices.Clone(x.Messages)
	for i := range c.Messages {
		c.Messages[i].Flags = slices.Clone(c.Messages[i].Flags)
	}
	return &c
}

// Message reads a message: from the cache, else from the server, kept.
func (s *Session) Message(ctx context.Context, mailbox string, uid uint32) (*Message, error) {
	raw, ok := s.store.Raw(s.Acc.ID, mailbox, uid)
	if !ok {
		err := s.Do(ctx, func(c *Conn) error {
			var err error
			raw, err = c.Fetch(mailbox, uid)
			return err
		})
		if err != nil {
			return nil, err
		}
		s.store.SaveRaw(s.Acc.ID, mailbox, uid, raw)
	}
	return Parse(raw)
}

// Raw is a message as it came, for Open in browser and saving.
func (s *Session) Raw(mailbox string, uid uint32) ([]byte, bool) {
	return s.store.Raw(s.Acc.ID, mailbox, uid)
}

// SetFlag sets or clears a flag: in the index at once, then on the
// server; the index goes back if the server refuses.
func (s *Session) SetFlag(ctx context.Context, mailbox string, uids []uint32, flag string, on bool) error {
	before := s.edit(mailbox, uids, func(e *Envelope) { e.Set(flag, on) })
	err := s.Do(ctx, func(c *Conn) error { return c.SetFlag(mailbox, uids, flag, on) })
	if err != nil {
		s.restore(mailbox, before)
		return err
	}
	s.save(mailbox)
	return nil
}

// SetTag keeps a message's sorting tag, set by the user or the assistant.
func (s *Session) SetTag(mailbox string, uid uint32, t Tag, byUser bool) {
	s.edit(mailbox, []uint32{uid}, func(e *Envelope) { e.Tag, e.TagByUser = t, byUser })
	s.save(mailbox)
}

// Move moves messages to another mailbox: out of the index at once, then
// on the server; back in if the server refuses.
func (s *Session) Move(ctx context.Context, mailbox string, uids []uint32, to string) error {
	if to == "" {
		return errors.New("no such mailbox")
	}
	before := s.take(mailbox, uids)
	err := s.Do(ctx, func(c *Conn) error { return c.Move(mailbox, uids, to) })
	if err != nil {
		s.restore(mailbox, before)
		return err
	}
	s.store.DropRaw(s.Acc.ID, mailbox, uids...)
	s.save(mailbox)
	return nil
}

// Delete removes messages for good.
func (s *Session) Delete(ctx context.Context, mailbox string, uids []uint32) error {
	before := s.take(mailbox, uids)
	err := s.Do(ctx, func(c *Conn) error { return c.Delete(mailbox, uids) })
	if err != nil {
		s.restore(mailbox, before)
		return err
	}
	s.store.DropRaw(s.Acc.ID, mailbox, uids...)
	s.save(mailbox)
	return nil
}

// edit changes messages in the index and returns them as they were.
func (s *Session) edit(mailbox string, uids []uint32, f func(*Envelope)) []Envelope {
	s.imu.Lock()
	defer s.imu.Unlock()
	x := s.index(mailbox)
	var before []Envelope
	for _, u := range uids {
		if e := x.Find(u); e != nil {
			b := *e
			b.Flags = slices.Clone(e.Flags)
			before = append(before, b)
			f(e)
		}
	}
	return before
}

// take takes messages out of the index and returns them.
func (s *Session) take(mailbox string, uids []uint32) []Envelope {
	s.imu.Lock()
	defer s.imu.Unlock()
	x := s.index(mailbox)
	var taken []Envelope
	for _, u := range uids {
		if e := x.Find(u); e != nil {
			taken = append(taken, *e)
		}
	}
	x.Drop(func(u uint32) bool { return slices.Contains(uids, u) })
	return taken
}

// restore puts messages back as they were.
func (s *Session) restore(mailbox string, es []Envelope) {
	s.imu.Lock()
	defer s.imu.Unlock()
	s.index(mailbox).Put(es...)
}

func (s *Session) save(mailbox string) {
	s.imu.Lock()
	x := s.index(mailbox).copy()
	s.imu.Unlock()
	s.store.SaveIndex(s.Acc.ID, mailbox, x)
}

// SaveSummary keeps the assistant's summary of a conversation.
func (s *Session) SaveSummary(mailbox, conv, text string) {
	s.imu.Lock()
	x := s.index(mailbox)
	if x.Summary == nil {
		x.Summary = map[string]string{}
	}
	x.Summary[conv] = text
	s.imu.Unlock()
	s.save(mailbox)
}

// Summary is a conversation's summary kept, or "".
func (s *Session) Summary(mailbox, conv string) string {
	s.imu.Lock()
	defer s.imu.Unlock()
	return s.index(mailbox).Summary[conv]
}

// Search asks the server for a mailbox's messages matching q, and adds
// those the index did not have; it returns the UIDs.
func (s *Session) Search(ctx context.Context, mailbox string, q Query) ([]uint32, error) {
	var uids []uint32
	err := s.Do(ctx, func(c *Conn) error {
		var err error
		uids, err = c.Search(mailbox, q)
		return err
	})
	if err != nil || len(uids) == 0 {
		return uids, err
	}
	s.imu.Lock()
	x := s.index(mailbox)
	var missing []uint32
	for _, u := range uids {
		if x.Find(u) == nil {
			missing = append(missing, u)
		}
	}
	s.imu.Unlock()
	if len(missing) > 200 {
		missing = missing[len(missing)-200:] // the newest
	}
	if len(missing) > 0 {
		var es []Envelope
		err = s.Do(ctx, func(c *Conn) error {
			if err := c.selectBox(mailbox); err != nil {
				return err
			}
			var err error
			es, err = c.envelopes(uidSet(missing), 0)
			return err
		})
		if err == nil {
			s.imu.Lock()
			s.index(mailbox).Put(es...)
			s.imu.Unlock()
			s.save(mailbox)
		}
	}
	return uids, nil
}

// Send sends a draft: SMTP, then a copy in Sent unless the server keeps
// one itself, then the original's \Answered.
func (s *Session) Send(ctx context.Context, d Draft) (Built, error) {
	b, err := Build(d, s.Acc, time.Now())
	if err != nil {
		return b, err
	}
	s.mu.Lock()
	a, err := s.signIn()
	s.mu.Unlock()
	if err != nil {
		return b, err
	}
	_, smtpAddr := s.Acc.Servers()
	if err := Send(ctx, smtpAddr, a, b); err != nil {
		return b, err
	}
	if !s.Acc.SavesSent() {
		if sent := s.Role(RoleSent); sent != "" {
			if err := s.Do(ctx, func(c *Conn) error { return c.Append(sent, b.Raw, Seen) }); err != nil {
				return b, fmt.Errorf("%w: %v", ErrNotSaved, err)
			}
		}
	}
	return b, nil
}

// ErrNoDrafts is an account with no Drafts folder: drafts stay on this
// computer.
var ErrNoDrafts = errors.New("no Drafts folder")

// SaveDraft keeps a copy of a draft in the account's Drafts folder, in
// place of the copy kept before (d.Server), and returns the new copy's UID.
func (s *Session) SaveDraft(ctx context.Context, d Draft) (uint32, error) {
	box := s.Role(RoleDrafts)
	if box == "" {
		return 0, ErrNoDrafts
	}
	b, err := BuildDraft(d, s.Acc, time.Now())
	if err != nil {
		return 0, err
	}
	var uid uint32
	err = s.Do(ctx, func(c *Conn) error {
		var err error
		if uid, err = c.appendUID(box, b.Raw, Seen, DraftFlag); err != nil {
			return err
		}
		if uid == 0 {
			if uid, err = c.uidOf(box, b.MessageID); err != nil {
				return err
			}
		}
		if d.Server != 0 && d.Server != uid {
			return c.Delete(box, []uint32{d.Server})
		}
		return nil
	})
	if err != nil {
		return uid, err
	}
	s.imu.Lock()
	x := s.index(box)
	x.Drop(func(u uint32) bool { return u == d.Server })
	s.imu.Unlock()
	s.save(box)
	return uid, nil
}

// DeleteDraft removes a draft's copy from the Drafts folder, after it was
// sent or discarded.
func (s *Session) DeleteDraft(ctx context.Context, uid uint32) error {
	box := s.Role(RoleDrafts)
	if box == "" || uid == 0 {
		return nil
	}
	return s.Delete(ctx, box, []uint32{uid})
}

// ErrNotSaved is a message sent but not put in Sent.
var ErrNotSaved = errors.New("sent, but not saved in Sent")

// Answered marks the message a reply answered, wherever it is in the
// indexes.
func (s *Session) Answered(ctx context.Context, messageID string) {
	if messageID == "" {
		return
	}
	s.imu.Lock()
	var mailbox string
	var uid uint32
	for name, x := range s.indexes {
		for i := range x.Messages {
			if x.Messages[i].MessageID == messageID {
				mailbox, uid = name, x.Messages[i].UID
			}
		}
	}
	s.imu.Unlock()
	if mailbox != "" {
		s.SetFlag(ctx, mailbox, []uint32{uid}, Answered, true)
	}
}

// Watch keeps a second connection in IDLE on the inbox until ctx ends,
// calling changed when it gets or loses messages; a dropped connection
// is made again, waiting longer each time.
func (s *Session) Watch(ctx context.Context, changed func()) {
	wait := 5 * time.Second
	for ctx.Err() == nil {
		s.mu.Lock()
		a, err := s.signIn()
		s.mu.Unlock()
		if err != nil {
			return
		}
		addr, _ := s.Acc.Servers()
		news := make(chan struct{}, 1)
		c, err := Dial(ctx, addr, a, func() {
			select {
			case news <- struct{}{}:
			default:
			}
		})
		if err == nil {
			wait = 5 * time.Second
			go func() {
				for {
					select {
					case <-news:
						changed()
					case <-c.Closed():
						return
					case <-ctx.Done():
						return
					}
				}
			}()
			for ctx.Err() == nil && err == nil {
				err = c.Idle(ctx, s.Role(RoleInbox))
			}
			c.c.Close()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, 5*time.Minute)
	}
}

// Mine says whether an address is the account's.
func (s *Session) Mine(addr string) bool { return strings.EqualFold(addr, s.Acc.Address) }

// Check signs in to the servers of an account not saved yet: IMAP, then
// SMTP's greeting and sign-in.
func Check(ctx context.Context, acc Account, a Auth) error {
	imapAddr, smtpAddr := acc.Servers()
	c, err := Dial(ctx, imapAddr, a, nil)
	if err != nil {
		return fmt.Errorf("IMAP %s: %w", imapAddr, err)
	}
	c.Close()
	cl, err := DialSMTP(smtpAddr)
	if err != nil {
		return fmt.Errorf("SMTP %s: %w", smtpAddr, err)
	}
	defer cl.Close()
	sc, err := a.sasl(ctx)
	if err != nil {
		return err
	}
	if err := cl.Auth(sc); err != nil {
		return fmt.Errorf("SMTP %s: %w: %v", smtpAddr, ErrAuth, err)
	}
	return nil
}
