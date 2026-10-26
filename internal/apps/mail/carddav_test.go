package mail

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// fakeBook is a CardDAV address book: PROPFIND lists the cards with their
// ETags, REPORT gives the cards asked for.
type fakeBook struct {
	mu      sync.Mutex
	path    string
	cards   map[string]string // href → vCard
	etags   map[string]string
	fetched []string // hrefs REPORT gave
	status  int      // when not 0, every request answers it
}

func (b *fakeBook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.status != 0 {
		http.Error(w, "no", b.status)
		return
	}
	if r.Header.Get("Authorization") != "Bearer a2" {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var out strings.Builder
	out.WriteString(`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav">`)
	switch r.Method {
	case "PROPFIND":
		fmt.Fprintf(&out, `<d:response><d:href>%s</d:href><d:propstat><d:prop><d:resourcetype><d:collection/><c:addressbook/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`, b.path)
		for h, e := range b.etags {
			fmt.Fprintf(&out, `<d:response><d:href>%s</d:href><d:propstat><d:prop><d:getetag>%s</d:getetag><d:resourcetype/></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`, h, e)
		}
	case "REPORT":
		for h := range b.cards {
			if !strings.Contains(string(body), "<d:href>"+h+"</d:href>") {
				continue
			}
			b.fetched = append(b.fetched, h)
			fmt.Fprintf(&out, `<d:response><d:href>%s</d:href><d:propstat><d:prop><d:getetag>%s</d:getetag><c:address-data>%s</c:address-data></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`, h, b.etags[h], b.cards[h])
		}
	default:
		http.Error(w, "no", http.StatusMethodNotAllowed)
		return
	}
	out.WriteString(`</d:multistatus>`)
	w.WriteHeader(http.StatusMultiStatus)
	io.WriteString(w, out.String())
}

func (b *fakeBook) put(href, etag, card string) {
	b.mu.Lock()
	b.cards[href], b.etags[href] = card, etag
	b.mu.Unlock()
}

// The address book is read over CardDAV with the account's token; a
// second reading fetches only the cards whose ETag changed, and forgets
// those gone.
func TestSyncBook(t *testing.T) {
	p := newProvider(t)
	book := &fakeBook{path: "/carddav/v1/principals/me@example.org/lists/default/", cards: map[string]string{}, etags: map[string]string{}}
	srv := httptest.NewServer(book)
	t.Cleanup(srv.Close)
	h := book.path
	book.put(h+"ana.vcf", `"1"`, "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Ana Souza\r\nN:Souza;Ana;;;\r\nEMAIL;TYPE=INTERNET:ana@studio.dev\r\nitem1.EMAIL:ana.s@\r\n home.example\r\nEND:VCARD\r\n")
	book.put(h+"ben.vcf", `"1"`, "BEGIN:VCARD\r\nVERSION:3.0\r\nN:Ito;Ben;;;\r\nEMAIL:ben@studio.dev\r\nEND:VCARD\r\n")
	book.put(h+"shop.vcf", `"1"`, "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Corner Shop\r\nTEL:123\r\nEND:VCARD\r\n")

	acc := Account{ID: "g", Address: "me@example.org", Provider: Google}
	s := NewSession(acc, Settings{}, StoreAt(t.TempDir()), SecretsAt(filepath.Join(t.TempDir(), "s.json")))
	t.Cleanup(s.Close)
	s.ep = p.endpoint()
	s.ep.Book = srv.URL + "/carddav/v1/principals/%s/lists/default/"
	s.UseAuth(Auth{User: acc.Address, Tokens: NewTokens(s.ep, "r1", nil)})
	ctx := context.Background()

	names := func(as []Address) []string {
		var out []string
		for _, a := range as {
			out = append(out, a.Name+" <"+a.Addr+">")
		}
		slices.Sort(out)
		return out
	}
	got, err := s.SyncBook(ctx)
	want := []string{"Ana Souza <ana.s@home.example>", "Ana Souza <ana@studio.dev>", "Ben Ito <ben@studio.dev>"}
	if err != nil || !slices.Equal(names(got), want) {
		t.Fatalf("book %v, %v", names(got), err)
	}
	if !slices.Equal(names(s.CachedBook()), want) {
		t.Fatalf("cached %v", names(s.CachedBook()))
	}

	book.mu.Lock()
	book.fetched = nil
	delete(book.cards, h+"shop.vcf")
	delete(book.etags, h+"shop.vcf")
	book.mu.Unlock()
	book.put(h+"ben.vcf", `"2"`, "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Ben Ito\\, Studio\r\nEMAIL:mailto:ben@studio.dev\r\nEND:VCARD\r\n")
	got, err = s.SyncBook(ctx)
	want = []string{"Ana Souza <ana.s@home.example>", "Ana Souza <ana@studio.dev>", "Ben Ito, Studio <ben@studio.dev>"}
	if err != nil || !slices.Equal(names(got), want) {
		t.Fatalf("again %v, %v", names(got), err)
	}
	if !slices.Equal(book.fetched, []string{h + "ben.vcf"}) {
		t.Fatalf("fetched %v, want the changed card alone", book.fetched)
	}

	// A token without the contacts (a sign-in from before): no book, and
	// the cache kept.
	book.mu.Lock()
	book.status = http.StatusForbidden
	book.mu.Unlock()
	if _, err := s.SyncBook(ctx); !errors.Is(err, ErrNoBook) {
		t.Fatalf("forbidden: %v", err)
	}
	if len(s.CachedBook()) != 3 {
		t.Fatalf("cache after forbidden %v", s.CachedBook())
	}
	// A provider with no address book: nothing asked.
	s.ep.Book = ""
	if _, err := s.SyncBook(ctx); !errors.Is(err, ErrNoBook) {
		t.Fatalf("no book: %v", err)
	}
}

// The book's people join the mail's: the book's name is shown, someone
// only in the book comes after those the user writes to, and the user's
// own addresses are left out.
func TestWithBook(t *testing.T) {
	mine := func(a string) bool { return a == "me@x" }
	cs := []Contact{
		{Address: Address{Name: "ana", Addr: "ana@studio.dev"}, Score: 3},
		{Address: Address{Addr: "old@x.org"}, Score: 0.05},
	}
	got := WithBook(cs, []Address{
		{Name: "Ana Souza", Addr: "ANA@studio.dev"},
		{Name: "Zoe", Addr: "zoe@x.org"},
		{Name: "Zoe", Addr: "zoe@x.org"}, // in two books
		{Name: "Me", Addr: "me@x"},
	}, mine)
	var out []string
	for _, c := range got {
		out = append(out, fmt.Sprintf("%s <%s> %.2f", c.Name, c.Addr, c.Score))
	}
	want := []string{"Ana Souza <ana@studio.dev> 3.60", "Zoe <zoe@x.org> 0.60", " <old@x.org> 0.05"}
	if !slices.Equal(out, want) {
		t.Fatalf("contacts %q", out)
	}
}

func TestVcardPeople(t *testing.T) {
	got := vcardPeople("BEGIN:VCARD\nVERSION:4.0\nFN;LANGUAGE=en:Ana \"A\" Souza\nEMAIL;TYPE=\"work:main\":ana@x.org\nEMAIL:not an address\nEND:VCARD\n")
	if len(got) != 1 || got[0] != (Address{Name: `Ana "A" Souza`, Addr: "ana@x.org"}) {
		t.Fatalf("people %+v", got)
	}
}

// The book's addresses are gathered by name, in the names' order; the
// user's own and the same address twice are left out.
func TestPeople(t *testing.T) {
	mine := func(a string) bool { return a == "me@x" }
	ps := People([]Address{
		{Name: "zoe", Addr: "zoe@x.org"},
		{Name: "Élodie", Addr: "elodie@x.fr"},
		{Name: "Ana Souza", Addr: "ana@studio.dev"},
		{Addr: "bot@x.org"},
		{Name: "ana souza", Addr: "ana@home.example"},
		{Name: "Ana Souza", Addr: "ANA@studio.dev"},
		{Name: "Me", Addr: "me@x"},
	}, mine)
	var got []string
	for _, p := range ps {
		got = append(got, p.Title()+" "+strings.Join(p.Addrs, ","))
	}
	want := []string{"Ana Souza ana@studio.dev,ana@home.example", "bot@x.org bot@x.org", "Élodie elodie@x.fr", "zoe zoe@x.org"}
	if !slices.Equal(got, want) {
		t.Fatalf("people %q", got)
	}
	if !ps[0].Match("sou") || !ps[0].Match("HOME") || ps[0].Match("zoe") || !ps[3].Match("") {
		t.Fatal("match")
	}
}
