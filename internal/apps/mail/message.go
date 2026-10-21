package mail

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset" // charsets other than UTF-8
	gomail "github.com/emersion/go-message/mail"
)

// Address is a name and an address.
type Address struct {
	Name string `json:"n,omitempty"`
	Addr string `json:"a"`
}

// String is the address as the user types it: Name <addr>, the name in
// quotes only when it holds a character that would split it.
func (a Address) String() string {
	if a.Name == "" {
		return a.Addr
	}
	name := a.Name
	if strings.ContainsAny(name, `,;:<>@"()[]\`) {
		name = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name) + `"`
	}
	return name + " <" + a.Addr + ">"
}

// Short is how a list shows the address: the name, else the address.
func (a Address) Short() string {
	if a.Name != "" {
		return a.Name
	}
	return a.Addr
}

// Domain is the address's domain, lower case.
func (a Address) Domain() string {
	_, d, _ := strings.Cut(strings.ToLower(a.Addr), "@")
	return d
}

// Flags a message carries.
const (
	Seen      = `\Seen`
	Flagged   = `\Flagged`
	Answered  = `\Answered`
	Deleted   = `\Deleted`
	DraftFlag = `\Draft`
)

// Envelope is what a mailbox's index keeps of a message: enough to list
// it, put it in its conversation and search it without its text.
type Envelope struct {
	UID        uint32    `json:"u"`
	Flags      []string  `json:"f,omitempty"`
	Date       time.Time `json:"d"`
	From       Address   `json:"fr"`
	To         []Address `json:"to,omitempty"`
	Cc         []Address `json:"cc,omitempty"`
	ReplyTo    []Address `json:"rt,omitempty"`
	Subject    string    `json:"s,omitempty"`
	Snippet    string    `json:"sn,omitempty"`
	Size       int64     `json:"z,omitempty"`
	MessageID  string    `json:"id,omitempty"`
	InReplyTo  string    `json:"ir,omitempty"`
	References []string  `json:"rf,omitempty"`
	Attach     bool      `json:"at,omitempty"` // it has attachments
	Bulk       bool      `json:"b,omitempty"`  // List-Unsubscribe, List-Id or Precedence: bulk/list
	Auto       bool      `json:"au,omitempty"` // Auto-Submitted other than no
	Verified   bool      `json:"vf,omitempty"` // DMARC passed for its From's domain (BIMI's logo may show)
	Tag        Tag       `json:"t,omitempty"`  // the sorting's (7.2)
	TagByUser  bool      `json:"tu,omitempty"` // the user set it
}

// Has says whether the message carries flag.
func (e *Envelope) Has(flag string) bool {
	return slices.ContainsFunc(e.Flags, func(f string) bool { return strings.EqualFold(f, flag) })
}

// Set adds or removes a flag.
func (e *Envelope) Set(flag string, on bool) {
	e.Flags = slices.DeleteFunc(e.Flags, func(f string) bool { return strings.EqualFold(f, flag) })
	if on {
		e.Flags = append(e.Flags, flag)
	}
}

// Message is a message read in full.
type Message struct {
	From, Sender   Address
	To, Cc, Bcc    []Address
	ReplyTo        []Address
	Subject        string
	Date           time.Time
	MessageID      string
	InReplyTo      string
	References     []string
	Text           string // the text: text/plain, else the HTML as text
	HTML           string // the HTML part, when there is one
	FromHTML       bool   // Text was made from HTML
	Links          []Link // the HTML's links, with their text
	Attachments    []Attachment
	Header         gomail.Header
	Authentication string // Authentication-Results, the first (the user's provider's)
}

// Attachment is a part that is a file.
type Attachment struct {
	Name      string
	Type      string // media type, lower case
	ContentID string // for an image an HTML part shows inline
	Inline    bool   // Content-Disposition: inline
	Data      []byte
}

// Size is the attachment's size.
func (a Attachment) Size() int64 { return int64(len(a.Data)) }

// Image says whether it is an image the window can show.
func (a Attachment) Image() bool {
	switch a.Type {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp":
		return true
	}
	return false
}

// textMax caps a text part read, so a huge one does not take the window.
const textMax = 4 << 20

// Parse reads a message: its header, its text and its attachments.
func Parse(raw []byte) (*Message, error) {
	mr, err := gomail.CreateReader(bytes.NewReader(raw))
	if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
		if mr == nil {
			return nil, err
		}
	}
	m := &Message{Header: mr.Header}
	h := &mr.Header
	m.From = first(addresses(h, "From"))
	m.Sender = first(addresses(h, "Sender"))
	m.To, m.Cc, m.Bcc, m.ReplyTo = addresses(h, "To"), addresses(h, "Cc"), addresses(h, "Bcc"), addresses(h, "Reply-To")
	m.Subject, _ = h.Subject()
	m.Date, _ = h.Date()
	m.MessageID, _ = h.MessageID()
	if ids, _ := h.MsgIDList("In-Reply-To"); len(ids) > 0 {
		m.InReplyTo = ids[0]
	}
	m.References, _ = h.MsgIDList("References")
	m.Authentication = h.Get("Authentication-Results")
	var plain, htmlText string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			if message.IsUnknownCharset(err) || message.IsUnknownEncoding(err) {
				continue
			}
			if plain == "" && htmlText == "" && len(m.Attachments) == 0 {
				return m, fmt.Errorf("reading the message: %w", err)
			}
			break // a broken part at the end: what was read is kept
		}
		switch ph := p.Header.(type) {
		case *gomail.InlineHeader:
			t, params, _ := ph.ContentType()
			name := params["name"]
			disp, dparams, _ := ph.ContentDisposition()
			if name == "" {
				name = dparams["filename"]
			}
			b, _ := io.ReadAll(io.LimitReader(p.Body, textMax))
			switch {
			case t == "text/plain" && name == "" && plain == "":
				plain = string(b)
			case t == "text/html" && name == "" && m.HTML == "":
				m.HTML = string(b)
			case t == "text/plain" || t == "text/html" || strings.HasPrefix(t, "multipart/"):
				// a second text, or an alternative's other side: skipped
				if name != "" {
					m.Attachments = append(m.Attachments, Attachment{Name: name, Type: t, Data: b, Inline: disp == "inline"})
				}
			default:
				cid := strings.Trim(ph.Get("Content-Id"), "<> ")
				if name == "" {
					name = unnamed(t)
				}
				m.Attachments = append(m.Attachments, Attachment{Name: name, Type: t, ContentID: cid, Inline: true, Data: b})
			}
		case *gomail.AttachmentHeader:
			name, _ := ph.Filename()
			t, _, _ := ph.ContentType()
			b, _ := io.ReadAll(p.Body)
			if name == "" {
				name = unnamed(t)
			}
			m.Attachments = append(m.Attachments, Attachment{
				Name: filepath.Base(name), Type: strings.ToLower(t), ContentID: strings.Trim(ph.Get("Content-Id"), "<> "), Data: b,
			})
		}
	}
	if m.HTML != "" {
		htmlText, m.Links = HTMLToText(m.HTML)
	}
	switch {
	case strings.TrimSpace(plain) != "":
		m.Text = cleanText(plain)
	case htmlText != "":
		m.Text, m.FromHTML = htmlText, true
	}
	return m, nil
}

// unnamed names an attachment that came without a name, by its type.
func unnamed(t string) string {
	ext := ""
	if exts, _ := mime.ExtensionsByType(t); len(exts) > 0 {
		ext = exts[0]
	}
	switch t {
	case "text/calendar":
		ext = ".ics"
	case "message/rfc822":
		ext = ".eml"
	}
	return "attachment" + ext
}

// Shown are the attachments the reader lists: not the images an HTML
// part shows inline, since the text shows where they were.
func (m *Message) Shown() []Attachment {
	var out []Attachment
	for _, a := range m.Attachments {
		if a.ContentID != "" && a.Inline && a.Image() && m.HTML != "" && strings.Contains(m.HTML, "cid:"+a.ContentID) && a.Size() < 64<<10 {
			continue // a logo or a signature's picture
		}
		out = append(out, a)
	}
	return out
}

func addresses(h *gomail.Header, key string) []Address {
	list, err := h.AddressList(key)
	if err != nil {
		// a list go-message cannot parse: net/mail is more forgiving with names
		if l, err2 := mail.ParseAddressList(h.Get(key)); err2 == nil {
			for _, a := range l {
				list = append(list, &gomail.Address{Name: a.Name, Address: a.Address})
			}
		}
	}
	out := make([]Address, 0, len(list))
	for _, a := range list {
		out = append(out, Address{Name: a.Name, Addr: a.Address})
	}
	return out
}

func first(l []Address) Address {
	if len(l) == 0 {
		return Address{}
	}
	return l[0]
}

var blankLines = regexp.MustCompile(`\n{3,}`)

// cleanText makes line ends \n, drops trailing spaces and runs of blank
// lines.
func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	s = blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	return strings.TrimSpace(s)
}

// Snippet is the start of a text for a list: quotes, signatures and
// blank lines dropped, one line.
func Snippet(text string, max int) string {
	var b strings.Builder
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l == "--" || l == "-- " {
			break
		}
		if l == "" || strings.HasPrefix(l, ">") || quoteHead.MatchString(l) {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(l)
		if b.Len() >= max {
			break
		}
	}
	return clip(strings.Join(strings.Fields(b.String()), " "), max)
}

// clip cuts s at max bytes on a rune boundary, with an ellipsis.
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimSpace(s[:cut]) + "…"
}

// quoteHead is the line a reply puts over what it quotes.
var quoteHead = regexp.MustCompile(`(?i)^(on .{6,80}wrote:|le .{6,80}a écrit ?:|am .{6,80}schrieb .{1,60}:|el .{6,80}escribió:|.{4,60}于.{4,40}写道[:：]|-----\s*original message\s*-----)$`)

// Split cuts a message's text into what was written and what it quotes
// (with the line over it, or a signature), so the reader can fold the
// rest.
func Split(text string) (written, rest string) {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "-- " || t == "--" || quoteHead.MatchString(t) ||
			(strings.HasPrefix(t, ">") && quotedFromHere(lines[i:])) {
			return strings.TrimRight(strings.Join(lines[:i], "\n"), "\n "), strings.Join(lines[i:], "\n")
		}
	}
	return text, ""
}

// quotedFromHere says whether the lines left are all quotes or blank:
// a quote inside a reply's own text is not folded.
func quotedFromHere(lines []string) bool {
	for _, l := range lines {
		if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, ">") {
			return false
		}
	}
	return true
}

// Subject without its Re: and Fwd: marks, for comparing.
func BaseSubject(s string) string {
	for {
		t := strings.TrimSpace(s)
		l := strings.ToLower(t)
		cut := false
		for _, p := range []string{"re:", "fwd:", "fw:", "aw:", "wg:", "tr:", "rv:", "回复:", "回复：", "转发:", "答复:"} {
			if strings.HasPrefix(l, p) {
				t, cut = t[len(p):], true
				break
			}
		}
		if !cut {
			return t
		}
		s = t
	}
}

// ErrNotFound is a message no longer on the server.
var ErrNotFound = errors.New("the message is no longer there")
