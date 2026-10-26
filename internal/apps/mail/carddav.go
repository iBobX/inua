package mail

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// The address book is read over CardDAV (RFC 6352), as Thunderbird reads
// Google's: its cards are listed with their ETags, and only those new or
// changed since the last reading are fetched. inua never writes to it.

// GoogleBook is the address of a Google account's contacts, %s its
// address.
const GoogleBook = "https://www.googleapis.com/carddav/v1/principals/%s/lists/default/"

// ErrNoBook is an account with no address book inua may read: a provider
// with none, or a sign-in from before inua asked for the contacts, or one
// whose user left them out on the consent page.
var ErrNoBook = errors.New("no address book")

// Book is an account's address book as last read, kept in the cache.
type Book struct {
	Cards map[string]Card `json:"cards"` // by the card's href
}

// Card is one contact's card: the addresses on it, with its name.
type Card struct {
	ETag   string    `json:"etag"`
	People []Address `json:"people,omitempty"`
}

// People is everyone in the book, by address, once each.
func (b *Book) People() []Address {
	seen := map[string]bool{}
	var out []Address
	for _, c := range b.Cards {
		for _, a := range c.People {
			k := strings.ToLower(a.Addr)
			if !seen[k] {
				seen[k] = true
				out = append(out, a)
			}
		}
	}
	return out
}

// CachedBook is the account's address book as last read; empty when it
// was never read. It reads the cache: call it off the main thread.
func (s *Session) CachedBook() []Address {
	b, _ := s.store.Book(s.Acc.ID)
	return b.People()
}

// SyncBook reads the account's address book again, fetching only the
// cards that changed, keeps it, and gives everyone in it. ErrNoBook when
// the account has none inua may read.
func (s *Session) SyncBook(ctx context.Context) ([]Address, error) {
	if s.ep.Book == "" {
		return nil, ErrNoBook
	}
	a, err := s.signIn()
	if err != nil {
		return nil, err
	}
	if a.Tokens == nil {
		return nil, ErrNoBook
	}
	old, _ := s.store.Book(s.Acc.ID)
	d := dav{url: fmt.Sprintf(s.ep.Book, url.PathEscape(s.Acc.Address)), tokens: a.Tokens}
	b, err := d.sync(ctx, old)
	if err != nil {
		return nil, err
	}
	if err := s.store.SaveBook(s.Acc.ID, b); err != nil {
		return nil, err
	}
	return b.People(), nil
}

// dav is an address book on a CardDAV server, read with a bearer token.
type dav struct {
	url    string
	tokens *Tokens
}

// multiget is how many cards one REPORT asks for.
const multiget = 100

func (d dav) sync(ctx context.Context, old *Book) (*Book, error) {
	const list = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:"><d:prop><d:getetag/><d:resourcetype/></d:prop></d:propfind>`
	ms, err := d.do(ctx, "PROPFIND", "1", list)
	if err != nil {
		return nil, err
	}
	base, _ := url.Parse(d.url)
	b := &Book{Cards: map[string]Card{}}
	var want []string
	for _, r := range ms.Responses {
		p, ok := r.ok()
		if !ok || p.Type.Collection != nil || samePath(base, r.Href) {
			continue
		}
		if c, ok := old.Cards[r.Href]; ok && c.ETag == p.ETag && p.ETag != "" {
			b.Cards[r.Href] = c
			continue
		}
		want = append(want, r.Href)
	}
	for len(want) > 0 {
		n := min(len(want), multiget)
		var q strings.Builder
		q.WriteString(`<?xml version="1.0" encoding="utf-8"?>
<c:addressbook-multiget xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav"><d:prop><d:getetag/><c:address-data/></d:prop>`)
		for _, h := range want[:n] {
			q.WriteString("<d:href>")
			xml.EscapeText(&q, []byte(h))
			q.WriteString("</d:href>")
		}
		q.WriteString("</c:addressbook-multiget>")
		want = want[n:]
		ms, err := d.do(ctx, "REPORT", "1", q.String())
		if err != nil {
			return nil, err
		}
		for _, r := range ms.Responses {
			if p, ok := r.ok(); ok && p.Data != "" {
				b.Cards[r.Href] = Card{ETag: p.ETag, People: vcardPeople(p.Data)}
			}
		}
	}
	return b, nil
}

// do sends a WebDAV request and reads its multistatus, once more with a
// new access token when the server refused the old one.
func (d dav) do(ctx context.Context, method, depth, body string) (*multistatus, error) {
	for try := 0; ; try++ {
		access, err := d.tokens.Access(ctx)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, method, d.url, strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+access)
		req.Header.Set("Content-Type", `application/xml; charset="utf-8"`)
		req.Header.Set("Depth", depth)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		switch {
		case resp.StatusCode == http.StatusUnauthorized && try == 0:
			d.tokens.Expire()
			continue
		case resp.StatusCode == http.StatusForbidden:
			return nil, ErrNoBook // the token was not given the contacts, or the API is off for the client
		case resp.StatusCode != http.StatusMultiStatus:
			return nil, fmt.Errorf("the address book: %s %s", method, resp.Status)
		}
		ms := &multistatus{}
		if err := xml.NewDecoder(bytes.NewReader(b)).Decode(ms); err != nil {
			return nil, fmt.Errorf("the address book: %w", err)
		}
		return ms, nil
	}
}

type multistatus struct {
	Responses []davResponse `xml:"DAV: response"`
}

type davResponse struct {
	Href     string `xml:"DAV: href"`
	Propstat []struct {
		Status string  `xml:"DAV: status"`
		Prop   davProp `xml:"DAV: prop"`
	} `xml:"DAV: propstat"`
}

type davProp struct {
	ETag string `xml:"DAV: getetag"`
	Data string `xml:"urn:ietf:params:xml:ns:carddav address-data"`
	Type struct {
		Collection *struct{} `xml:"DAV: collection"`
	} `xml:"DAV: resourcetype"`
}

// ok is the properties the server found; false when it found none.
func (r davResponse) ok() (davProp, bool) {
	for _, p := range r.Propstat {
		if strings.Contains(p.Status, " 200 ") {
			return p.Prop, true
		}
	}
	return davProp{}, false
}

// samePath says whether href names the collection at base itself.
func samePath(base *url.URL, href string) bool {
	u, err := base.Parse(href)
	if err != nil {
		return false
	}
	return strings.TrimSuffix(u.Path, "/") == strings.TrimSuffix(base.Path, "/")
}

// vcardPeople reads the addresses on a vCard (RFC 6350, and the 3.0 of
// RFC 2426), each with the card's name.
func vcardPeople(card string) []Address {
	card = strings.ReplaceAll(card, "\r\n", "\n")
	card = strings.ReplaceAll(card, "\n ", "")
	card = strings.ReplaceAll(card, "\n\t", "")
	var out []Address
	var name, nParts string
	var addrs []string
	flush := func() {
		if name == "" {
			name = nParts
		}
		for _, a := range addrs {
			out = append(out, Address{Name: name, Addr: a})
		}
		name, nParts, addrs = "", "", nil
	}
	for line := range strings.SplitSeq(card, "\n") {
		key, value, ok := vcardLine(line)
		if !ok {
			continue
		}
		switch key {
		case "BEGIN":
			name, nParts, addrs = "", "", nil
		case "END":
			flush()
		case "FN":
			name = strings.TrimSpace(vcardText(value))
		case "N":
			// family;given;additional;prefixes;suffixes
			f := strings.Split(value, ";")
			var parts []string
			for _, i := range []int{1, 0} {
				if i < len(f) {
					if p := strings.TrimSpace(vcardText(f[i])); p != "" {
						parts = append(parts, p)
					}
				}
			}
			nParts = strings.Join(parts, " ")
		case "EMAIL":
			a := strings.TrimSpace(vcardText(value))
			a = strings.TrimPrefix(a, "mailto:")
			if strings.Contains(a, "@") && !strings.ContainsAny(a, " <>,") {
				addrs = append(addrs, a)
			}
		}
	}
	if len(addrs) > 0 {
		flush() // a card with no END
	}
	return out
}

// vcardLine splits an unfolded content line into its property's name,
// without group or parameters, and its value.
func vcardLine(line string) (key, value string, ok bool) {
	quoted := false
	for i, r := range line {
		switch r {
		case '"':
			quoted = !quoted
		case ':':
			if quoted {
				continue
			}
			key = line[:i]
			if j := strings.IndexByte(key, ';'); j >= 0 {
				key = key[:j]
			}
			if j := strings.LastIndexByte(key, '.'); j >= 0 {
				key = key[j+1:] // item1.EMAIL
			}
			return strings.ToUpper(strings.TrimSpace(key)), strings.TrimSuffix(line[i+1:], "\r"), true
		}
	}
	return "", "", false
}

// vcardText undoes a text value's escapes.
func vcardText(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n', 'N':
				b.WriteByte(' ')
			default:
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
