package mail

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	gomail "github.com/emersion/go-message/mail"

	"github.com/lyricat/inua/internal/i18n"
)

// Draft is a message being written; kept on disk as it is typed.
type Draft struct {
	ID          string    `json:"id"`
	Account     string    `json:"account"`
	To          string    `json:"to,omitempty"` // as typed: addresses split by commas
	Cc          string    `json:"cc,omitempty"`
	Bcc         string    `json:"bcc,omitempty"`
	Subject     string    `json:"subject,omitempty"`
	Body        string    `json:"body,omitempty"`
	Attachments []string  `json:"attachments,omitempty"` // files' paths
	InReplyTo   string    `json:"in_reply_to,omitempty"`
	References  []string  `json:"references,omitempty"`
	Saved       time.Time `json:"saved"`
	// Server is the UID of its copy in the account's Drafts folder; 0
	// when none is kept there.
	Server uint32 `json:"server,omitempty"`
}

// NewDraft is an empty draft for an account, with its signature.
func NewDraft(acc Account) Draft {
	d := Draft{ID: newDraftID(), Account: acc.ID}
	if acc.Signature != "" {
		d.Body = "\n\n-- \n" + acc.Signature
	}
	return d
}

func newDraftID() string { return strings.ToLower(rand.Text()[:16]) }

// Empty says whether nothing was written.
func (d Draft) Empty() bool {
	return strings.TrimSpace(d.To+d.Cc+d.Bcc+d.Subject) == "" && len(d.Attachments) == 0 &&
		(strings.TrimSpace(d.Body) == "" || strings.HasPrefix(strings.TrimSpace(d.Body), "--"))
}

// ParseAddresses reads addresses as typed, split by commas or
// semicolons; a bare name without @ is refused.
func ParseAddresses(s string) ([]Address, error) {
	s = strings.ReplaceAll(s, ";", ",")
	var out []Address
	for _, part := range splitAddresses(s) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		a, err := mail.ParseAddress(part)
		if err != nil {
			return nil, errors.New(i18n.Tf("%q is not an address", part))
		}
		out = append(out, Address{Name: a.Name, Addr: a.Address})
	}
	return out, nil
}

// splitAddresses splits on commas outside quotes and angle brackets.
func splitAddresses(s string) []string {
	var out []string
	depth, quoted, start := 0, false, 0
	for i, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
		case r == '<' && !quoted:
			depth++
		case r == '>' && !quoted && depth > 0:
			depth--
		case r == ',' && !quoted && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// ErrNoRecipient is a message with no one to send it to.
var ErrNoRecipient = errors.New("no recipient")

// Built is a message ready to send.
type Built struct {
	Raw        []byte
	MessageID  string
	From       string
	Recipients []string // To, Cc and Bcc
}

// Build makes the message from a draft: text/plain in UTF-8, the
// attachments as base64, Message-ID and Date, and a reply's In-Reply-To
// and References. Bcc goes in the recipients only.
func Build(d Draft, acc Account, now time.Time) (Built, error) { return build(d, acc, now, false) }

// BuildDraft makes the copy of a draft kept in the Drafts folder: no
// recipient is needed, Bcc stays in the header, and addresses still being
// typed are kept as typed.
func BuildDraft(d Draft, acc Account, now time.Time) (Built, error) { return build(d, acc, now, true) }

func build(d Draft, acc Account, now time.Time, draft bool) (Built, error) {
	var h gomail.Header
	var all []Address
	for _, f := range []struct{ key, typed string }{{"To", d.To}, {"Cc", d.Cc}, {"Bcc", d.Bcc}} {
		l, err := ParseAddresses(f.typed)
		switch {
		case err != nil && draft:
			h.SetText(f.key, strings.TrimSpace(f.typed))
			continue
		case err != nil:
			return Built{}, err
		}
		all = append(all, l...)
		if len(l) > 0 && (f.key != "Bcc" || draft) {
			h.SetAddressList(f.key, gaddrs(l))
		}
	}
	if len(all) == 0 && !draft {
		return Built{}, ErrNoRecipient
	}
	from := &gomail.Address{Name: acc.Name, Address: acc.Address}
	h.SetAddressList("From", []*gomail.Address{from})
	h.SetSubject(d.Subject)
	h.SetDate(now)
	_, domain, _ := strings.Cut(acc.Address, "@")
	if err := h.GenerateMessageIDWithHostname(cmpOr(domain, "inua.local")); err != nil {
		return Built{}, err
	}
	id, _ := h.MessageID()
	if d.InReplyTo != "" {
		h.SetMsgIDList("In-Reply-To", []string{d.InReplyTo})
	}
	if len(d.References) > 0 {
		h.SetMsgIDList("References", d.References)
	}
	h.Set("User-Agent", "inua-mail")
	var buf bytes.Buffer
	text := strings.ReplaceAll(d.Body, "\r\n", "\n")
	if len(d.Attachments) == 0 {
		h.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
		w, err := gomail.CreateSingleInlineWriter(&buf, h)
		if err != nil {
			return Built{}, err
		}
		io.WriteString(w, text)
		if err := w.Close(); err != nil {
			return Built{}, err
		}
	} else {
		mw, err := gomail.CreateWriter(&buf, h)
		if err != nil {
			return Built{}, err
		}
		var th gomail.InlineHeader
		th.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
		tw, err := mw.CreateSingleInline(th)
		if err != nil {
			return Built{}, err
		}
		io.WriteString(tw, text)
		tw.Close()
		for _, path := range d.Attachments {
			data, err := os.ReadFile(path)
			if err != nil {
				return Built{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
			}
			var ah gomail.AttachmentHeader
			ah.SetContentType(TypeOf(path, data), nil)
			ah.SetFilename(filepath.Base(path))
			aw, err := mw.CreateAttachment(ah)
			if err != nil {
				return Built{}, err
			}
			aw.Write(data)
			if err := aw.Close(); err != nil {
				return Built{}, err
			}
		}
		if err := mw.Close(); err != nil {
			return Built{}, err
		}
	}
	b := Built{Raw: buf.Bytes(), MessageID: id, From: acc.Address}
	for _, a := range all {
		if !slices.Contains(b.Recipients, a.Addr) {
			b.Recipients = append(b.Recipients, a.Addr)
		}
	}
	return b, nil
}

// TypeOf is a file's media type, by its name, else by its first bytes.
func TypeOf(name string, data []byte) string {
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); t != "" {
		t, _, _ = strings.Cut(t, ";")
		return t
	}
	if len(data) > 0 {
		t := httpDetect(data)
		t, _, _ = strings.Cut(t, ";")
		return t
	}
	return "application/octet-stream"
}

func gaddrs(l []Address) []*gomail.Address {
	out := make([]*gomail.Address, len(l))
	for i, a := range l {
		out[i] = &gomail.Address{Name: a.Name, Address: a.Addr}
	}
	return out
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Reply is a draft answering m: to whom it asks replies to go (and with
// all, everyone else in it but the user), Re: on the subject, the quote
// under the cursor, and the headers that keep the conversation together.
// mine says whether an address is one of the user's.
func Reply(m *Message, acc Account, all bool, mine func(string) bool) Draft {
	d := NewDraft(acc)
	to := m.ReplyTo
	if len(to) == 0 {
		to = []Address{m.From}
	}
	if mine(m.From.Addr) && len(m.ReplyTo) == 0 {
		to = m.To // answering one's own message: to the same people
	}
	var cc []Address
	if all {
		cc = append(slices.Clone(m.To), m.Cc...)
	}
	// The user's own addresses are dropped, unless nothing else is left
	// (a note to oneself).
	seen := map[string]bool{}
	keep := func(l []Address) []Address {
		var out []Address
		for _, a := range l {
			k := strings.ToLower(a.Addr)
			if k == "" || seen[k] || mine(a.Addr) {
				continue
			}
			seen[k] = true
			out = append(out, a)
		}
		return out
	}
	toKept := keep(to)
	if len(toKept) == 0 {
		toKept = to
	}
	d.To = joinAddrs(toKept)
	d.Cc = joinAddrs(keep(cc))
	d.Subject = m.Subject
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(m.Subject)), "re:") {
		d.Subject = "Re: " + BaseSubject(m.Subject)
	}
	d.InReplyTo = m.MessageID
	d.References = slices.Clone(m.References)
	if m.MessageID != "" && !slices.Contains(d.References, m.MessageID) {
		d.References = append(d.References, m.MessageID)
	}
	sig := strings.TrimLeft(d.Body, "\n")
	d.Body = "\n\n"
	if sig != "" {
		d.Body += sig + "\n\n"
	}
	d.Body += Quote(m)
	return d
}

// Quote is a message's text quoted, under the line naming who wrote it
// and when.
func Quote(m *Message) string {
	var b strings.Builder
	who := m.From.Short()
	b.WriteString(i18n.Tf("On %s, %s wrote:", m.Date.Local().Format("Mon, 2 Jan 2006 15:04"), who))
	b.WriteByte('\n')
	for _, l := range strings.Split(m.Text, "\n") {
		if strings.HasPrefix(l, ">") {
			b.WriteString(">" + l + "\n")
		} else {
			b.WriteString("> " + l + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// Forward is a draft passing m on: Fwd: on the subject, its header and
// text under a line, and its attachments, written into dir.
func Forward(m *Message, acc Account, dir string) (Draft, error) {
	d := NewDraft(acc)
	d.Subject = "Fwd: " + BaseSubject(m.Subject)
	var b strings.Builder
	b.WriteString("\n\n---------- " + i18n.T("Forwarded message") + " ----------\n")
	fmt.Fprintf(&b, "%s %s\n", i18n.T("From:"), m.From.String())
	fmt.Fprintf(&b, "%s %s\n", i18n.T("Date:"), m.Date.Local().Format("Mon, 2 Jan 2006 15:04"))
	fmt.Fprintf(&b, "%s %s\n", i18n.T("Subject:"), m.Subject)
	if len(m.To) > 0 {
		fmt.Fprintf(&b, "%s %s\n", i18n.T("To:"), joinAddrs(m.To))
	}
	b.WriteString("\n" + m.Text)
	sig := strings.TrimPrefix(d.Body, "\n\n")
	d.Body = b.String()
	if sig != "" {
		d.Body = "\n\n" + sig + d.Body
	}
	if len(m.Shown()) > 0 {
		folder := filepath.Join(dir, d.ID)
		if err := os.MkdirAll(folder, 0o700); err != nil {
			return d, err
		}
		for _, a := range m.Shown() {
			p := filepath.Join(folder, safeName(a.Name))
			if err := os.WriteFile(p, a.Data, 0o600); err != nil {
				return d, err
			}
			d.Attachments = append(d.Attachments, p)
		}
	}
	return d, nil
}

// DraftOf makes a draft from a message kept in the Drafts folder (uid
// there), to go on writing it: its people, subject, text and reply
// headers; its files written into dir.
func DraftOf(m *Message, acc Account, uid uint32, dir string) (Draft, error) {
	d := Draft{ID: newDraftID(), Account: acc.ID, Server: uid}
	d.To, d.Cc, d.Bcc = joinAddrs(m.To), joinAddrs(m.Cc), joinAddrs(m.Bcc)
	if d.To == "" {
		d.To = strings.TrimSpace(m.Header.Get("To")) // still being typed when it was kept
	}
	d.Subject = m.Subject
	d.Body = strings.Replace(m.Text, "\n--\n", "\n-- \n", 1) // the signature's line, its space trimmed in reading
	d.InReplyTo, d.References = m.InReplyTo, m.References
	if len(m.Shown()) > 0 {
		folder := filepath.Join(dir, d.ID)
		if err := os.MkdirAll(folder, 0o700); err != nil {
			return d, err
		}
		for _, a := range m.Shown() {
			p := filepath.Join(folder, safeName(a.Name))
			if err := os.WriteFile(p, a.Data, 0o600); err != nil {
				return d, err
			}
			d.Attachments = append(d.Attachments, p)
		}
	}
	return d, nil
}

// safeName is a file name from a message, without folders in it.
func safeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" || name == ".." || name == "" {
		return "attachment"
	}
	return name
}

// SafeName is safeName for the window's saving.
func SafeName(name string) string { return safeName(name) }

// Drafts keeps drafts on disk: $XDG_DATA_HOME/inua/mail/drafts/ID.json,
// with what a forward attached in ID/.
type Drafts struct{ Dir string }

// NewDrafts is the user's drafts folder.
func NewDrafts() *Drafts {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, _ := os.UserHomeDir()
		data = filepath.Join(home, ".local", "share")
	}
	return &Drafts{Dir: filepath.Join(data, "inua", "mail", "drafts")}
}

// Save keeps a draft.
func (s *Drafts) Save(d Draft) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(s.Dir, d.ID+".json"), b)
}

// Delete removes a draft and what was attached to it from a forward.
func (s *Drafts) Delete(id string) error {
	os.RemoveAll(filepath.Join(s.Dir, id))
	err := os.Remove(filepath.Join(s.Dir, id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// List reads the drafts, the newest first.
func (s *Drafts) List() ([]Draft, error) {
	ents, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Draft
	for _, e := range ents {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.Dir, e.Name()))
		if err != nil {
			continue
		}
		var d Draft
		if json.Unmarshal(b, &d) == nil && d.ID != "" {
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b Draft) int { return b.Saved.Compare(a.Saved) })
	return out, nil
}
