package mail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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

	amu  sync.Mutex // the sign-in, which every connection reads
	auth *Auth

	mu   sync.Mutex // the connection that syncs and acts, one command at a time
	conn *Conn

	rmu   sync.Mutex // the connection that reads messages: opening one never waits for a sync
	rconn *Conn

	bmu   sync.Mutex // the mailboxes: never held across a command, so the window never waits for the network
	boxes []Mailbox

	imu     sync.Mutex // the indexes
	indexes map[string]*Index
	held    map[string]map[uint32]bool // out of the list for an action that can still be undone
}

// NewSession makes an account's session; nothing connects yet.
func NewSession(acc Account, settings Settings, store *Store, secrets *Secrets) *Session {
	s := &Session{Acc: acc, store: store, secrets: secrets, indexes: map[string]*Index{}, held: map[string]map[uint32]bool{}}
	if acc.OAuth() {
		s.ep, _ = settings.Endpoint(acc.Provider)
	}
	return s
}

// UseAuth signs in with auth instead of the keyring's, for tests and for
// checking a new account before it is saved.
func (s *Session) UseAuth(a Auth) { s.amu.Lock(); s.auth = &a; s.amu.Unlock() }

// signIn is how the account signs in: the password or the refresh token
// from the keyring, read once.
func (s *Session) signIn() (Auth, error) {
	s.amu.Lock()
	defer s.amu.Unlock()
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

// Picture asks the provider for the address of the account's photo, for
// one whose sign-in did not give it: Google leaves it out of the ID token
// of some Workspace accounts, and says it in the profile the token reads.
// "" when there is no photo, or no way to ask.
func (s *Session) Picture(ctx context.Context) (string, error) {
	if s.ep.UserInfo == "" {
		return "", nil
	}
	a, err := s.signIn()
	if err != nil || a.Tokens == nil {
		return "", err
	}
	access, err := a.Tokens.Access(ctx)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.ep.UserInfo, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the profile: %s", resp.Status)
	}
	var profile struct {
		Picture string `json:"picture"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&profile); err != nil {
		return "", fmt.Errorf("the profile: %w", err)
	}
	if !strings.HasPrefix(profile.Picture, "https://") {
		return "", nil
	}
	return profile.Picture, nil
}

// Do runs f on the connection that syncs and acts, connecting first when
// there is none, and once more on a new connection when the old one had
// dropped.
func (s *Session) Do(ctx context.Context, f func(*Conn) error) error {
	return s.on(ctx, &s.mu, &s.conn, f)
}

// on runs f on a connection, under its lock.
func (s *Session) on(ctx context.Context, mu *sync.Mutex, conn **Conn, f func(*Conn) error) error {
	mu.Lock()
	defer mu.Unlock()
	for try := 0; ; try++ {
		if *conn == nil {
			a, err := s.signIn()
			if err != nil {
				return err
			}
			addr, _ := s.Acc.Servers()
			c, err := Dial(ctx, addr, a, nil)
			if err != nil {
				if errors.Is(err, ErrAuth) || errors.Is(err, ErrSignInAgain) {
					s.amu.Lock()
					s.auth = nil // the keyring may hold a new one next time
					s.amu.Unlock()
				}
				return err
			}
			*conn = c
		}
		err := f(*conn)
		if err == nil || try > 0 || !dropped(*conn, err) {
			return err
		}
		(*conn).c.Close()
		*conn = nil
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
	for _, l := range []struct {
		mu   *sync.Mutex
		conn **Conn
	}{{&s.rmu, &s.rconn}, {&s.mu, &s.conn}} {
		l.mu.Lock()
		if *l.conn != nil {
			(*l.conn).Close()
			*l.conn = nil
		}
		l.mu.Unlock()
	}
}

// Cached reads the mailboxes and the inbox from the cache, for the first
// frame.
func (s *Session) Cached() []Mailbox {
	boxes, _ := s.store.Mailboxes(s.Acc.ID)
	s.bmu.Lock()
	if s.boxes == nil {
		s.boxes = boxes
	}
	s.bmu.Unlock()
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
	s.bmu.Lock()
	s.boxes = boxes
	s.bmu.Unlock()
	return boxes, nil
}

// Role is the name of the mailbox with a role; "" when there is none.
func (s *Session) Role(r Role) string {
	s.bmu.Lock()
	defer s.bmu.Unlock()
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
	// it back. What only this computer keeps (tags, summaries, answers)
	// is taken from the index as it is now.
	keepLocal(work, s.index(mailbox))
	work.Drop(func(u uint32) bool { return s.held[mailbox][u] }) // still on the server until the action is done
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
	keepLocal(work, s.index(mailbox))
	s.indexes[mailbox] = work
	s.imu.Unlock()
	return n, s.store.SaveIndex(s.Acc.ID, mailbox, work)
}

// keepLocal carries into a synced index what the server does not keep
// and may have changed while it synced: the tags and the summaries.
func keepLocal(work, now *Index) {
	if work.Validity != now.Validity {
		return // started over: the old UIDs mean other messages
	}
	work.Summary = now.Summary
	for i := range work.Messages {
		if e := now.Find(work.Messages[i].UID); e != nil && e.Tag != "" {
			work.Messages[i].Tag, work.Messages[i].TagByUser = e.Tag, e.TagByUser
		}
	}
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
	r := s.Messages(ctx, mailbox, []uint32{uid})[0]
	return r.Message, r.Err
}

// Read is a message read, or why it could not be.
type Read struct {
	UID     uint32
	Message *Message
	Err     error
}

// Messages reads messages of a mailbox, one Read for each UID: those kept
// at once, the rest in one command on the reading connection, so opening
// a conversation never waits for a sync, and they are kept.
func (s *Session) Messages(ctx context.Context, mailbox string, uids []uint32) []Read {
	raws := map[uint32][]byte{}
	var missing []uint32
	for _, u := range uids {
		if raw, ok := s.store.Raw(s.Acc.ID, mailbox, u); ok {
			raws[u] = raw
		} else {
			missing = append(missing, u)
		}
	}
	var err error
	if len(missing) > 0 {
		err = s.on(ctx, &s.rmu, &s.rconn, func(c *Conn) error {
			got, err := c.FetchMany(mailbox, missing)
			for u, raw := range got {
				raws[u] = raw
				s.store.SaveRaw(s.Acc.ID, mailbox, u, raw)
			}
			return err
		})
	}
	out := make([]Read, len(uids))
	for i, u := range uids {
		out[i].UID = u
		raw, ok := raws[u]
		switch {
		case ok:
			out[i].Message, out[i].Err = Parse(raw)
		case err != nil:
			out[i].Err = err
		default:
			out[i].Err = ErrNotFound
		}
	}
	return out
}

// PrefetchCount and PrefetchMax bound what Prefetch keeps ahead: the
// newest messages, and none larger (a big attachment waits until it is
// opened).
const (
	PrefetchCount = 20
	PrefetchMax   = 2 << 20
)

// Prefetch keeps a mailbox's newest messages before they are opened:
// those of the newest PrefetchCount not kept yet and not larger than
// PrefetchMax, in one command on the connection that syncs, after it.
// Peeked, so none is marked read. It says how many it fetched.
func (s *Session) Prefetch(ctx context.Context, mailbox string) (int, error) {
	s.imu.Lock()
	x := s.index(mailbox)
	all := make([]Envelope, len(x.Messages))
	copy(all, x.Messages)
	s.imu.Unlock()
	slices.SortFunc(all, func(a, b Envelope) int { return int(int64(b.UID) - int64(a.UID)) })
	var missing []uint32
	for i := 0; i < len(all) && i < PrefetchCount; i++ {
		if all[i].Size <= PrefetchMax && !s.store.HasRaw(s.Acc.ID, mailbox, all[i].UID) {
			missing = append(missing, all[i].UID)
		}
	}
	if len(missing) == 0 {
		return 0, nil
	}
	n := 0
	err := s.Do(ctx, func(c *Conn) error {
		got, err := c.FetchMany(mailbox, missing)
		for u, raw := range got {
			s.store.SaveRaw(s.Acc.ID, mailbox, u, raw)
			n++
		}
		return err
	})
	return n, err
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

// Hold takes messages out of the list at once, for an action the user
// can still undo; the server is told later (MoveHeld), or never (Release).
// A sync meanwhile leaves them out.
func (s *Session) Hold(mailbox string, uids []uint32) []Envelope {
	s.imu.Lock()
	if s.held[mailbox] == nil {
		s.held[mailbox] = map[uint32]bool{}
	}
	for _, u := range uids {
		s.held[mailbox][u] = true
	}
	s.imu.Unlock()
	return s.take(mailbox, uids)
}

// Release puts held messages back, the action undone.
func (s *Session) Release(mailbox string, held []Envelope) {
	s.unhold(mailbox, held)
	s.restore(mailbox, held)
}

func (s *Session) unhold(mailbox string, held []Envelope) {
	s.imu.Lock()
	for _, e := range held {
		delete(s.held[mailbox], e.UID)
	}
	s.imu.Unlock()
}

// MoveHeld moves held messages on the server; when the server refuses,
// they come back to the list.
func (s *Session) MoveHeld(ctx context.Context, mailbox string, held []Envelope, to string) error {
	uids := make([]uint32, len(held))
	for i, e := range held {
		uids[i] = e.UID
	}
	err := s.Do(ctx, func(c *Conn) error { return c.Move(mailbox, uids, to) })
	s.unhold(mailbox, held)
	if err != nil {
		s.restore(mailbox, held)
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

// SaveAnswer keeps the answer the user gave an invitation (a message of
// mailbox), which the invitation itself does not show.
func (s *Session) SaveAnswer(mailbox string, uid uint32, answer string) {
	s.SaveSummary(mailbox, fmt.Sprintf("answer/%d", uid), answer)
}

// AnswerOf is the answer kept for an invitation, or "".
func (s *Session) AnswerOf(mailbox string, uid uint32) string {
	return s.Summary(mailbox, fmt.Sprintf("answer/%d", uid))
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
	return b, s.SendBuilt(ctx, b)
}

// Answer answers an invitation for the user, to its organizer.
func (s *Session) Answer(ctx context.Context, inv *Invite, answer string) error {
	b, err := InviteReply(inv, s.Acc, answer, time.Now())
	if err != nil {
		return err
	}
	return s.SendBuilt(ctx, b)
}

// SendBuilt sends a built message: SMTP, then a copy in Sent unless the
// server keeps one itself.
func (s *Session) SendBuilt(ctx context.Context, b Built) error {
	a, err := s.signIn()
	if err != nil {
		return err
	}
	_, smtpAddr := s.Acc.Servers()
	if err := Send(ctx, smtpAddr, a, b); err != nil {
		return err
	}
	if !s.Acc.SavesSent() {
		if sent := s.Role(RoleSent); sent != "" {
			if err := s.Do(ctx, func(c *Conn) error { return c.Append(sent, b.Raw, Seen) }); err != nil {
				return fmt.Errorf("%w: %v", ErrNotSaved, err)
			}
		}
	}
	return nil
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
		a, err := s.signIn()
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
