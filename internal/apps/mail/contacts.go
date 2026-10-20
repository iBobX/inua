package mail

import (
	"slices"
	"strings"
	"time"
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
	slices.SortFunc(out, func(a, b Contact) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		}
		return strings.Compare(a.Addr, b.Addr)
	})
	return out
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
