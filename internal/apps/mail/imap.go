package mail

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message"
	gomail "github.com/emersion/go-message/mail"
	"github.com/emersion/go-sasl"
)

// DialIMAP connects to an IMAP server: TLS on port 993, else STARTTLS. A
// test replaces it to reach a server without TLS.
var DialIMAP = func(addr string, opts *imapclient.Options) (*imapclient.Client, error) {
	_, port, _ := net.SplitHostPort(addr)
	if port == "993" {
		return imapclient.DialTLS(addr, opts)
	}
	return imapclient.DialStartTLS(addr, opts)
}

// Auth signs a connection in: a password, or a bearer token.
type Auth struct {
	User     string
	Password string  // a password account's
	Tokens   *Tokens // an OAuth account's
}

// sasl makes the SASL client for SMTP, or for IMAP's AUTHENTICATE.
func (a Auth) sasl(ctx context.Context) (sasl.Client, error) {
	if a.Tokens == nil {
		return sasl.NewPlainClient("", a.User, a.Password), nil
	}
	tok, err := a.Tokens.Access(ctx)
	if err != nil {
		return nil, err
	}
	return xoauth2{user: a.User, token: tok}, nil
}

// Conn is a signed-in IMAP connection. Its methods are not safe to call
// at once from several goroutines; Session takes turns.
type Conn struct {
	c        *imapclient.Client
	selected string
	validity uint32
	exists   uint32
}

// ErrAuth is a server refusing the user's sign-in.
var ErrAuth = errors.New("the server refused the sign-in")

// Dial connects and signs in. changed, when not nil, is called (on the
// connection's goroutine) when the selected mailbox gets or loses
// messages, for IDLE.
func Dial(ctx context.Context, addr string, auth Auth, changed func()) (*Conn, error) {
	opts := &imapclient.Options{}
	if changed != nil {
		opts.UnilateralDataHandler = &imapclient.UnilateralDataHandler{
			Expunge: func(uint32) { changed() },
			Mailbox: func(d *imapclient.UnilateralDataMailbox) {
				if d.NumMessages != nil {
					changed()
				}
			},
		}
	}
	type dialed struct {
		c   *imapclient.Client
		err error
	}
	ch := make(chan dialed, 1)
	go func() {
		c, err := DialIMAP(addr, opts)
		ch <- dialed{c, err}
	}()
	var d dialed
	select {
	case <-ctx.Done():
		go func() {
			if d := <-ch; d.c != nil {
				d.c.Close()
			}
		}()
		return nil, ctx.Err()
	case d = <-ch:
	}
	if d.err != nil {
		return nil, d.err
	}
	c := d.c
	if err := signIn(ctx, c, auth); err != nil {
		c.Close()
		return nil, err
	}
	return &Conn{c: c}, nil
}

func signIn(ctx context.Context, c *imapclient.Client, auth Auth) error {
	if auth.Tokens == nil {
		if err := c.Login(auth.User, auth.Password).Wait(); err != nil {
			return fmt.Errorf("%w: %v", ErrAuth, err)
		}
		return nil
	}
	for try := 0; ; try++ {
		cl, err := auth.sasl(ctx)
		if err != nil {
			return err
		}
		err = c.Authenticate(cl)
		if err == nil {
			return nil
		}
		if try > 0 {
			return fmt.Errorf("%w: %v", ErrAuth, err)
		}
		auth.Tokens.Expire() // an access token the server no longer takes: once more with a new one
	}
}

// Close logs out.
func (c *Conn) Close() error {
	c.c.Logout().Wait()
	return c.c.Close()
}

// Closed is closed when the connection ends.
func (c *Conn) Closed() <-chan struct{} { return c.c.Closed() }

// Mailboxes lists the mailboxes, by role first and then by name, with
// their counts.
func (c *Conn) Mailboxes() ([]Mailbox, error) {
	opts := &imap.ListOptions{}
	special := c.c.Caps().Has(imap.CapSpecialUse)
	if special {
		opts.ReturnSpecialUse = true
	}
	list, err := c.c.List("", "*", opts).Collect()
	if err != nil {
		return nil, err
	}
	var out []Mailbox
	gmail := false
	for _, l := range list {
		if slices.Contains(l.Attrs, imap.MailboxAttrNoSelect) || slices.Contains(l.Attrs, imap.MailboxAttrNonExistent) {
			continue
		}
		role := roleOf(l)
		if role == "" && (slices.Contains(l.Attrs, imap.MailboxAttrFlagged) || slices.Contains(l.Attrs, imap.MailboxAttrImportant)) {
			continue // Gmail's Starred and Important: inua's own views do that
		}
		if strings.HasPrefix(l.Mailbox, "[Gmail]/") {
			gmail = true
		}
		out = append(out, Mailbox{Name: l.Mailbox, Role: role})
	}
	// Gmail's All Mail is where archived mail goes, and there is no Archive.
	if gmail {
		for i := range out {
			if out[i].Role == "all" {
				out[i].Role = RoleArchive
			}
		}
	}
	// A role taken twice (a server's own and a name): the first keeps it.
	seen := map[Role]bool{}
	for i := range out {
		if out[i].Role == "all" {
			out[i].Role = RoleOther
		}
		if out[i].Role != RoleOther && seen[out[i].Role] {
			out[i].Role = RoleOther
		}
		seen[out[i].Role] = true
	}
	// Counts, pipelined.
	cmds := make([]*imapclient.StatusCommand, len(out))
	for i, m := range out {
		cmds[i] = c.c.Status(m.Name, &imap.StatusOptions{NumMessages: true, NumUnseen: true})
	}
	for i, cmd := range cmds {
		if d, err := cmd.Wait(); err == nil {
			if d.NumMessages != nil {
				out[i].Total = int(*d.NumMessages)
			}
			if d.NumUnseen != nil {
				out[i].Unread = int(*d.NumUnseen)
			}
		}
	}
	SortMailboxes(out)
	return out, nil
}

// SortMailboxes puts the roles first, in Roles' order, then the rest by
// name.
func SortMailboxes(boxes []Mailbox) {
	rank := func(r Role) int {
		if i := slices.Index(Roles, r); i >= 0 {
			return i
		}
		return len(Roles)
	}
	slices.SortStableFunc(boxes, func(a, b Mailbox) int {
		if d := rank(a.Role) - rank(b.Role); d != 0 {
			return d
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
}

func roleOf(l *imap.ListData) Role {
	if strings.EqualFold(l.Mailbox, "INBOX") {
		return RoleInbox
	}
	for _, a := range l.Attrs {
		switch a {
		case imap.MailboxAttrSent:
			return RoleSent
		case imap.MailboxAttrDrafts:
			return RoleDrafts
		case imap.MailboxAttrTrash:
			return RoleTrash
		case imap.MailboxAttrJunk:
			return RoleJunk
		case imap.MailboxAttrArchive:
			return RoleArchive
		case imap.MailboxAttrAll:
			return "all"
		}
	}
	name := l.Mailbox
	if l.Delim != 0 {
		if i := strings.LastIndexByte(name, byte(l.Delim)); i >= 0 {
			name = name[i+1:]
		}
	}
	switch strings.ToLower(name) {
	case "sent", "sent items", "sent messages", "sent mail", "已发送", "送信済み":
		return RoleSent
	case "drafts", "draft", "草稿", "下書き":
		return RoleDrafts
	case "trash", "deleted items", "deleted messages", "bin", "已删除", "ゴミ箱":
		return RoleTrash
	case "junk", "spam", "junk email", "junk e-mail", "垃圾邮件", "迷惑メール":
		return RoleJunk
	case "archive", "archives", "归档", "アーカイブ":
		return RoleArchive
	}
	return RoleOther
}

// selectBox selects a mailbox unless it is selected already.
func (c *Conn) selectBox(name string) error {
	if c.selected == name && c.c.Mailbox() != nil {
		return nil
	}
	return c.reselect(name)
}

// fresh selects a mailbox, or polls it when it is selected already, so
// what other clients changed (an expunge) is known before reading it.
func (c *Conn) fresh(name string) error {
	if c.selected == name && c.c.Mailbox() != nil {
		if err := c.c.Noop().Wait(); err != nil {
			return err
		}
		c.exists = c.c.Mailbox().NumMessages
		return nil
	}
	return c.reselect(name)
}

func (c *Conn) reselect(name string) error {
	d, err := c.c.Select(name, nil).Wait()
	if err != nil {
		c.selected = ""
		return err
	}
	c.selected, c.validity, c.exists = name, d.UIDValidity, d.NumMessages
	return nil
}

// headerFields are the header fields the index keeps, beyond the envelope.
var headerFields = []string{"References", "List-Unsubscribe", "List-Id", "Precedence", "Auto-Submitted"}

// SyncResult says what a sync changed.
type SyncResult struct {
	New     []uint32 // UIDs that came
	Gone    []uint32 // UIDs that left
	Changed bool     // anything changed, flags included
	Reset   bool     // UIDVALIDITY changed: the cache was started again
}

// Sync brings a mailbox's index in step with the server: the messages
// above the highest known (the newest first time, up to first), the flags
// of the known ones, and the ones the server no longer has.
func (c *Conn) Sync(mailbox string, x *Index, first int) (SyncResult, error) {
	var res SyncResult
	if err := c.fresh(mailbox); err != nil {
		return res, err
	}
	if x.Validity != c.validity {
		res.Reset = x.Validity != 0
		*x = Index{Validity: c.validity, Summary: x.Summary}
	}
	if c.exists == 0 {
		if len(x.Messages) > 0 {
			res.Gone = x.Drop(func(uint32) bool { return true })
			res.Changed = true
		}
		return res, nil
	}
	// The known ones: their flags, and which are gone.
	if len(x.Messages) > 0 {
		var set imap.UIDSet
		set.AddRange(imap.UID(x.Lowest()), imap.UID(x.Highest()))
		msgs, err := c.c.Fetch(set, &imap.FetchOptions{UID: true, Flags: true}).Collect()
		if err != nil {
			return res, err
		}
		have := make(map[uint32][]imap.Flag, len(msgs))
		for _, m := range msgs {
			have[uint32(m.UID)] = m.Flags
		}
		res.Gone = x.Drop(func(uid uint32) bool { _, ok := have[uid]; return !ok })
		for i := range x.Messages {
			e := &x.Messages[i]
			f := flagStrings(have[e.UID])
			if !sameFlags(e.Flags, f) {
				e.Flags, res.Changed = f, true
			}
		}
		if len(res.Gone) > 0 {
			res.Changed = true
		}
	}
	// The new ones.
	var set imap.NumSet
	if len(x.Messages) == 0 {
		lo := uint32(1)
		if first > 0 && c.exists > uint32(first) {
			lo = c.exists - uint32(first) + 1
			x.Older = true
		}
		var seq imap.SeqSet
		seq.AddRange(lo, 0) // lo:*
		set = seq
	} else {
		var u imap.UIDSet
		u.AddRange(imap.UID(x.Highest()+1), 0) // n:*
		set = u
	}
	es, err := c.envelopes(set, x.Highest())
	if err != nil {
		return res, err
	}
	for _, e := range es {
		res.New = append(res.New, e.UID)
	}
	if len(es) > 0 {
		x.Put(es...)
		res.Changed = true
	}
	return res, nil
}

// Older fetches up to n messages older than the lowest known.
func (c *Conn) Older(mailbox string, x *Index, n int) (int, error) {
	if err := c.selectBox(mailbox); err != nil {
		return 0, err
	}
	low := x.Lowest()
	if low <= 1 {
		x.Older = false
		return 0, nil
	}
	var u imap.UIDSet
	u.AddRange(1, imap.UID(low-1))
	d, err := c.c.UIDSearch(&imap.SearchCriteria{UID: []imap.UIDSet{u}}, nil).Wait()
	if err != nil {
		return 0, err
	}
	uids := d.AllUIDs()
	if len(uids) <= n {
		x.Older = false
	} else {
		uids = uids[len(uids)-n:]
	}
	if len(uids) == 0 {
		return 0, nil
	}
	es, err := c.envelopes(imap.UIDSetNum(uids...), 0)
	if err != nil {
		return 0, err
	}
	x.Put(es...)
	return len(es), nil
}

// envelopes fetches messages' envelopes, flags, sizes, structure and the
// header fields the index keeps, then their snippets; UIDs at or below
// above are skipped (n:* always answers the last message).
func (c *Conn) envelopes(set imap.NumSet, above uint32) ([]Envelope, error) {
	hdr := &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, HeaderFields: headerFields, Peek: true}
	msgs, err := c.c.Fetch(set, &imap.FetchOptions{
		UID: true, Flags: true, Envelope: true, RFC822Size: true, InternalDate: true,
		BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
		BodySection:   []*imap.FetchItemBodySection{hdr},
	}).Collect()
	if err != nil {
		return nil, err
	}
	var out []Envelope
	parts := map[string][]int{} // a text part's path → the messages' indexes
	paths := map[string][]int{} // its path as numbers
	textOf := map[int]*imap.BodyStructureSinglePart{}
	for _, m := range msgs {
		if uint32(m.UID) <= above || m.Envelope == nil {
			continue
		}
		e := envelopeOf(m)
		h := headerOf(m.FindBodySection(hdr))
		e.References, _ = h.MsgIDList("References")
		e.Bulk = h.Get("List-Unsubscribe") != "" || h.Get("List-Id") != "" ||
			strings.EqualFold(strings.TrimSpace(h.Get("Precedence")), "bulk") || strings.EqualFold(strings.TrimSpace(h.Get("Precedence")), "list")
		if a := strings.ToLower(strings.TrimSpace(h.Get("Auto-Submitted"))); a != "" && a != "no" {
			e.Auto = true
		}
		var text *imap.BodyStructureSinglePart
		var path []int
		if m.BodyStructure != nil {
			e.Attach = hasAttachment(m.BodyStructure)
			path, text = textPart(m.BodyStructure)
		}
		i := len(out)
		out = append(out, e)
		if text != nil {
			k := fmt.Sprint(path)
			parts[k] = append(parts[k], i)
			paths[k] = path
			textOf[i] = text
		}
	}
	// Snippets: the start of each text part, a fetch per part path.
	for k, idx := range parts {
		var uids []imap.UID
		byUID := map[uint32]int{}
		for _, i := range idx {
			uids = append(uids, imap.UID(out[i].UID))
			byUID[out[i].UID] = i
		}
		sec := &imap.FetchItemBodySection{Part: paths[k], Peek: true, Partial: &imap.SectionPartial{Offset: 0, Size: 2048}}
		msgs, err := c.c.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{sec}}).Collect()
		if err != nil {
			continue // snippets are not worth failing the sync for
		}
		for _, m := range msgs {
			i, ok := byUID[uint32(m.UID)]
			if !ok {
				continue
			}
			out[i].Snippet = snippetOf(m.FindBodySection(sec), textOf[i])
		}
	}
	return out, nil
}

func envelopeOf(m *imapclient.FetchMessageBuffer) Envelope {
	env := m.Envelope
	e := Envelope{
		UID: uint32(m.UID), Flags: flagStrings(m.Flags), Date: env.Date, Subject: decodeWords(env.Subject),
		Size: m.RFC822Size, MessageID: strings.Trim(env.MessageID, "<> "),
		To: imapAddrs(env.To), Cc: imapAddrs(env.Cc), ReplyTo: imapAddrs(env.ReplyTo),
	}
	if e.Date.IsZero() {
		e.Date = m.InternalDate
	}
	if len(env.InReplyTo) > 0 {
		e.InReplyTo = strings.Trim(env.InReplyTo[0], "<> ")
	}
	if f := imapAddrs(env.From); len(f) > 0 {
		e.From = f[0]
	}
	return e
}

var wordDecoder = &mime.WordDecoder{CharsetReader: message.CharsetReader}

func decodeWords(s string) string {
	if !strings.Contains(s, "=?") {
		return s
	}
	if d, err := wordDecoder.DecodeHeader(s); err == nil {
		return d
	}
	return s
}

func imapAddrs(l []imap.Address) []Address {
	out := make([]Address, 0, len(l))
	for _, a := range l {
		if a.IsGroupStart() || a.IsGroupEnd() {
			continue
		}
		out = append(out, Address{Name: decodeWords(a.Name), Addr: a.Addr()})
	}
	return out
}

func headerOf(b []byte) gomail.Header {
	if len(b) == 0 {
		return gomail.Header{}
	}
	if !bytes.HasSuffix(b, []byte("\r\n\r\n")) && !bytes.HasSuffix(b, []byte("\n\n")) {
		b = append(b, "\r\n\r\n"...)
	}
	e, err := message.Read(bytes.NewReader(b))
	if err != nil && e == nil {
		return gomail.Header{}
	}
	return gomail.Header{Header: e.Header}
}

func flagStrings(fs []imap.Flag) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		if f == `\Recent` {
			continue
		}
		out = append(out, string(f))
	}
	return out
}

func sameFlags(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, f := range a {
		if !slices.ContainsFunc(b, func(g string) bool { return strings.EqualFold(f, g) }) {
			return false
		}
	}
	return true
}

// hasAttachment says whether a structure has a part that is a file.
func hasAttachment(bs imap.BodyStructure) bool {
	found := false
	bs.Walk(func(path []int, part imap.BodyStructure) bool {
		sp, ok := part.(*imap.BodyStructureSinglePart)
		if !ok {
			return true
		}
		d := sp.Disposition()
		switch {
		case d != nil && strings.EqualFold(d.Value, "attachment"):
			found = true
		case sp.Filename() != "" && !strings.EqualFold(sp.Type, "image"):
			found = true // a named part that is not a picture in the text
		case strings.EqualFold(sp.Type, "application"):
			found = true
		}
		return !found
	})
	return found
}

// textPart finds the part the snippet comes from: the first text/plain,
// else the first text/html, not attached.
func textPart(bs imap.BodyStructure) ([]int, *imap.BodyStructureSinglePart) {
	var plainPath, htmlPath []int
	var plain, html *imap.BodyStructureSinglePart
	bs.Walk(func(path []int, part imap.BodyStructure) bool {
		sp, ok := part.(*imap.BodyStructureSinglePart)
		if !ok || !strings.EqualFold(sp.Type, "text") {
			return true
		}
		if d := sp.Disposition(); d != nil && strings.EqualFold(d.Value, "attachment") {
			return true
		}
		switch strings.ToLower(sp.Subtype) {
		case "plain":
			if plain == nil {
				plain, plainPath = sp, slices.Clone(path)
			}
		case "html":
			if html == nil {
				html, htmlPath = sp, slices.Clone(path)
			}
		}
		return true
	})
	if plain != nil {
		return plainPath, plain
	}
	return htmlPath, html
}

// snippetOf decodes the start of a text part into a list's line.
func snippetOf(b []byte, part *imap.BodyStructureSinglePart) string {
	if len(b) == 0 || part == nil {
		return ""
	}
	switch strings.ToLower(part.Encoding) {
	case "base64":
		clean := bytes.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' {
				return -1
			}
			return r
		}, b)
		clean = clean[:len(clean)/4*4]
		d, err := base64.StdEncoding.DecodeString(string(clean))
		if err != nil {
			return ""
		}
		b = d
	case "quoted-printable":
		if i := bytes.LastIndexByte(b, '='); i >= len(b)-2 && i >= 0 {
			b = b[:i] // a cut escape
		}
		d, _ := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(b)))
		b = d
	}
	if cs := part.Params["charset"]; cs != "" && !strings.EqualFold(cs, "utf-8") && !strings.EqualFold(cs, "us-ascii") {
		if r, err := message.CharsetReader(cs, bytes.NewReader(b)); err == nil {
			if d, err := io.ReadAll(r); err == nil {
				b = d
			}
		}
	}
	text := string(b)
	if strings.EqualFold(part.Subtype, "html") {
		text, _ = HTMLToText(text)
	}
	return Snippet(cleanText(text), 200)
}

// Fetch reads a whole message without marking it seen.
func (c *Conn) Fetch(mailbox string, uid uint32) ([]byte, error) {
	if err := c.selectBox(mailbox); err != nil {
		return nil, err
	}
	sec := &imap.FetchItemBodySection{Peek: true}
	msgs, err := c.c.Fetch(imap.UIDSetNum(imap.UID(uid)), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{sec}}).Collect()
	if err != nil {
		return nil, err
	}
	for _, m := range msgs {
		if uint32(m.UID) == uid {
			return m.FindBodySection(sec), nil
		}
	}
	return nil, ErrNotFound
}

// SetFlag adds or removes a flag on messages.
func (c *Conn) SetFlag(mailbox string, uids []uint32, flag string, on bool) error {
	if err := c.selectBox(mailbox); err != nil {
		return err
	}
	op := imap.StoreFlagsDel
	if on {
		op = imap.StoreFlagsAdd
	}
	return c.c.Store(uidSet(uids), &imap.StoreFlags{Op: op, Silent: true, Flags: []imap.Flag{imap.Flag(flag)}}, nil).Close()
}

// Move moves messages to another mailbox: MOVE when the server has it,
// else COPY, \Deleted and EXPUNGE.
func (c *Conn) Move(mailbox string, uids []uint32, to string) error {
	if err := c.selectBox(mailbox); err != nil {
		return err
	}
	if c.c.Caps().Has(imap.CapMove) {
		_, err := c.c.Move(uidSet(uids), to).Wait()
		return err
	}
	if _, err := c.c.Copy(uidSet(uids), to).Wait(); err != nil {
		return err
	}
	return c.expunge(uids)
}

// Delete removes messages for good: \Deleted and EXPUNGE.
func (c *Conn) Delete(mailbox string, uids []uint32) error {
	if err := c.selectBox(mailbox); err != nil {
		return err
	}
	return c.expunge(uids)
}

func (c *Conn) expunge(uids []uint32) error {
	set := uidSet(uids)
	if err := c.c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
		return err
	}
	if c.c.Caps().Has(imap.CapUIDPlus) {
		return c.c.UIDExpunge(set).Close()
	}
	return c.c.Expunge().Close()
}

// Append puts a message in a mailbox, as after sending it.
func (c *Conn) Append(mailbox string, raw []byte, flags ...string) error {
	_, err := c.appendUID(mailbox, raw, flags...)
	return err
}

// appendUID is Append, with the UID the server gave the message (0 when
// it does not say: no UIDPLUS).
func (c *Conn) appendUID(mailbox string, raw []byte, flags ...string) (uint32, error) {
	opts := &imap.AppendOptions{Time: time.Now()}
	for _, f := range flags {
		opts.Flags = append(opts.Flags, imap.Flag(f))
	}
	cmd := c.c.Append(mailbox, int64(len(raw)), opts)
	if _, err := cmd.Write(raw); err != nil {
		cmd.Close()
		return 0, err
	}
	if err := cmd.Close(); err != nil {
		return 0, err
	}
	d, err := cmd.Wait()
	if err != nil || d == nil {
		return 0, err
	}
	return uint32(d.UID), nil
}

// uidOf finds a message in a mailbox by its Message-ID; 0 when it is not
// there.
func (c *Conn) uidOf(mailbox, messageID string) (uint32, error) {
	if err := c.fresh(mailbox); err != nil {
		return 0, err
	}
	d, err := c.c.UIDSearch(&imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "Message-Id", Value: messageID}}}, nil).Wait()
	if err != nil {
		return 0, err
	}
	uids := d.AllUIDs()
	if len(uids) == 0 {
		return 0, nil
	}
	return uint32(uids[len(uids)-1]), nil
}

// Search asks the server which messages of a mailbox match.
func (c *Conn) Search(mailbox string, q Query) ([]uint32, error) {
	if err := c.selectBox(mailbox); err != nil {
		return nil, err
	}
	d, err := c.c.UIDSearch(q.Criteria(), nil).Wait()
	if err != nil {
		return nil, err
	}
	var out []uint32
	for _, u := range d.AllUIDs() {
		out = append(out, uint32(u))
	}
	return out, nil
}

// Idle waits in IDLE on a mailbox until ctx ends, the connection drops,
// or 25 minutes pass (servers end an IDLE after 30). The handler given to
// Dial says what changed.
func (c *Conn) Idle(ctx context.Context, mailbox string) error {
	if err := c.selectBox(mailbox); err != nil {
		return err
	}
	cmd, err := c.c.Idle()
	if err != nil {
		return err
	}
	t := time.NewTimer(25 * time.Minute)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	case <-c.c.Closed():
		return errors.New("the connection ended")
	}
	if err := cmd.Close(); err != nil {
		return err
	}
	return cmd.Wait()
}

// Noop asks the server for news, as a check that the connection is up.
func (c *Conn) Noop() error { return c.c.Noop().Wait() }

func uidSet(uids []uint32) imap.UIDSet {
	var s imap.UIDSet
	for _, u := range uids {
		s.AddNum(imap.UID(u))
	}
	return s
}
