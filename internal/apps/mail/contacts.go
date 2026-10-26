package mail

import (
	"slices"
	"strings"
	"time"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// Contact is someone the user exchanges mail with, for the composer's
// suggestions.
type Contact struct {
	Address
	Score float64
}

// Contacts gathers the people of the messages given: those the user
// wrote to count most, those who wrote to the user less, lists and
// automated senders not at all; recent mail weighs more. mine says
// whether an address is the user's.
func Contacts(items []Item, mine func(string) bool, now time.Time) []Contact {
	byAddr := map[string]*Contact{}
	add := func(a Address, weight float64, at time.Time) {
		k := strings.ToLower(strings.TrimSpace(a.Addr))
		if k == "" || !strings.Contains(k, "@") || mine(a.Addr) {
			return
		}
		local, _, _ := strings.Cut(k, "@")
		if containsAny(local, noReply) {
			return
		}
		age := now.Sub(at).Hours() / 24
		w := weight / (1 + max(age, 0)/90) // a quarter-year halves it
		c := byAddr[k]
		if c == nil {
			c = &Contact{Address: Address{Addr: a.Addr}}
			byAddr[k] = c
		}
		c.Score += w
		if a.Name != "" && !strings.Contains(a.Name, "@") {
			c.Name = a.Name
		}
	}
	for _, it := range items {
		if mine(it.From.Addr) {
			for _, a := range append(append([]Address{}, it.To...), it.Cc...) {
				add(a, 3, it.Date)
			}
			continue
		}
		if it.Bulk || it.Auto {
			continue
		}
		add(it.From, 1, it.Date)
	}
	out := make([]Contact, 0, len(byAddr))
	for _, c := range byAddr {
		out = append(out, *c)
	}
	sortContacts(out)
	return out
}

func sortContacts(out []Contact) {
	slices.SortFunc(out, func(a, b Contact) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		}
		return strings.Compare(a.Addr, b.Addr)
	})
}

// WithBook adds the people of the user's address books to the contacts
// of their mail: the name the book gives is the one shown, and being in
// the book weighs as much as a message from them a couple of months ago.
func WithBook(cs []Contact, book []Address, mine func(string) bool) []Contact {
	if len(book) == 0 {
		return cs
	}
	at := map[string]int{}
	for i, c := range cs {
		at[strings.ToLower(c.Addr)] = i
	}
	counted := map[string]bool{} // the same address in two books counts once
	for _, a := range book {
		k := strings.ToLower(strings.TrimSpace(a.Addr))
		if k == "" || counted[k] || mine(a.Addr) {
			continue
		}
		counted[k] = true
		i, ok := at[k]
		if !ok {
			i = len(cs)
			at[k] = i
			cs = append(cs, Contact{Address: Address{Addr: a.Addr}})
		}
		cs[i].Score += 0.6
		if a.Name != "" {
			cs[i].Name = a.Name
		}
	}
	sortContacts(cs)
	return cs
}

// Suggest is the contacts matching what is being typed: a word of the
// name or the start of the address, best first, at most n.
func Suggest(cs []Contact, typed string, n int) []Contact {
	typed = strings.ToLower(strings.TrimSpace(typed))
	if typed == "" {
		return nil
	}
	var out []Contact
	for _, c := range cs {
		match := strings.HasPrefix(strings.ToLower(c.Addr), typed)
		for _, w := range strings.Fields(strings.ToLower(c.Name)) {
			match = match || strings.HasPrefix(w, typed)
		}
		match = match || strings.HasPrefix(strings.ToLower(c.Name), typed)
		if match {
			out = append(out, c)
			if len(out) == n {
				break
			}
		}
	}
	return out
}

// Person is someone in the user's address books: a name, and every
// address the books give them.
type Person struct {
	Name  string
	Addrs []string
}

// People gathers address books' addresses by name, in the names' order;
// someone with no name goes by their address. The user's own addresses
// are left out.
func People(book []Address, mine func(string) bool) []Person {
	byName := map[string]*Person{}
	seen := map[string]bool{}
	for _, a := range book {
		addr := strings.TrimSpace(a.Addr)
		k := strings.ToLower(addr)
		if k == "" || seen[k] || mine(addr) {
			continue
		}
		seen[k] = true
		name := strings.TrimSpace(a.Name)
		key := strings.ToLower(name)
		if name == "" {
			key = k
		}
		p := byName[key]
		if p == nil {
			p = &Person{Name: name}
			byName[key] = p
		}
		p.Addrs = append(p.Addrs, addr)
	}
	out := make([]Person, 0, len(byName))
	for _, p := range byName {
		out = append(out, *p)
	}
	// Accents and case aside (Élodie among the E's).
	col := collate.New(language.Und, collate.IgnoreCase, collate.IgnoreDiacritics)
	slices.SortFunc(out, func(a, b Person) int {
		if c := col.CompareString(a.Title(), b.Title()); c != 0 {
			return c
		}
		return strings.Compare(a.Addrs[0], b.Addrs[0])
	})
	return out
}

// Title is how the person is named: their name, else their address.
func (p Person) Title() string {
	if p.Name != "" {
		return p.Name
	}
	return p.Addrs[0]
}

// Match says whether the person is what is being typed: a word of the
// name starts with it, or an address holds it.
func (p Person) Match(typed string) bool {
	typed = strings.ToLower(strings.TrimSpace(typed))
	if typed == "" {
		return true
	}
	name := strings.ToLower(p.Name)
	if strings.HasPrefix(name, typed) {
		return true
	}
	for _, w := range strings.Fields(name) {
		if strings.HasPrefix(w, typed) {
			return true
		}
	}
	for _, a := range p.Addrs {
		if strings.Contains(strings.ToLower(a), typed) {
			return true
		}
	}
	return false
}
