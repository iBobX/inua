package mail

import (
	"slices"
	"strings"
)

// Item is a message in a mailbox of an account.
type Item struct {
	Account string
	Mailbox string
	*Envelope
}

// Key names an item: account, mailbox and UID.
type Key struct {
	Account, Mailbox string
	UID              uint32
}

// Key is the item's key.
func (it Item) Key() Key { return Key{it.Account, it.Mailbox, it.UID} }

// Conversation is messages that answer one another.
type Conversation struct {
	ID       string // the first message's Message-ID, or account/mailbox/UID
	Messages []Item // oldest first
}

// Latest is the newest message.
func (c *Conversation) Latest() Item { return c.Messages[len(c.Messages)-1] }

// Subject is the first message's subject without Re: and Fwd:.
func (c *Conversation) Subject() string { return BaseSubject(c.Messages[0].Subject) }

// Unread counts the messages not seen.
func (c *Conversation) Unread() int {
	n := 0
	for _, m := range c.Messages {
		if !m.Has(Seen) {
			n++
		}
	}
	return n
}

// Starred says whether a message in it is starred.
func (c *Conversation) Starred() bool {
	return slices.ContainsFunc(c.Messages, func(m Item) bool { return m.Has(Flagged) })
}

// Attach says whether a message in it has attachments.
func (c *Conversation) Attach() bool {
	return slices.ContainsFunc(c.Messages, func(m Item) bool { return m.Attach })
}

// Tag is the newest tagged message's tag.
func (c *Conversation) Tag() Tag {
	for i := len(c.Messages) - 1; i >= 0; i-- {
		if t := c.Messages[i].Tag; t != "" {
			return t
		}
	}
	return ""
}

// People are the senders, newest first, each once; mine are left out
// unless no one else wrote.
func (c *Conversation) People(mine func(addr string) bool) []Address {
	var out []Address
	seen := map[string]bool{}
	for i := len(c.Messages) - 1; i >= 0; i-- {
		a := c.Messages[i].From
		k := strings.ToLower(a.Addr)
		if seen[k] || (mine != nil && mine(a.Addr)) {
			continue
		}
		seen[k] = true
		out = append(out, a)
	}
	if len(out) == 0 && len(c.Messages) > 0 {
		out = append(out, c.Messages[len(c.Messages)-1].From)
	}
	return out
}

// Group puts items into conversations by References and In-Reply-To.
// joining items (the user's Sent) only join a conversation that the
// items started, so a mailbox's list shows its own conversations with
// the replies in place. The newest conversation comes first.
func Group(items, joining []Item) []*Conversation {
	byID := map[string]*Conversation{}
	var out []*Conversation
	place := func(it Item, create bool) {
		var c *Conversation
		for _, ref := range refs(it.Envelope) {
			if c = byID[ref]; c != nil {
				break
			}
		}
		if c == nil && it.MessageID != "" {
			c = byID[it.MessageID] // the message itself came earlier (a copy in another mailbox)
		}
		if c == nil {
			if !create {
				return
			}
			id := it.MessageID
			if id == "" {
				id = it.Account + "/" + it.Mailbox + "/" + itoa(int(it.UID))
			}
			c = &Conversation{ID: id}
			out = append(out, c)
		} else if it.MessageID != "" && slices.ContainsFunc(c.Messages, func(m Item) bool { return m.MessageID == it.MessageID }) {
			return // the same message twice (Inbox and All Mail)
		}
		c.Messages = append(c.Messages, it)
		if it.MessageID != "" {
			byID[it.MessageID] = c
		}
		for _, ref := range refs(it.Envelope) {
			if byID[ref] == nil {
				byID[ref] = c
			}
		}
	}
	sorted := slices.Clone(items)
	slices.SortStableFunc(sorted, func(a, b Item) int { return a.Date.Compare(b.Date) })
	for _, it := range sorted {
		place(it, true)
	}
	if len(joining) > 0 {
		js := slices.Clone(joining)
		slices.SortStableFunc(js, func(a, b Item) int { return a.Date.Compare(b.Date) })
		for _, it := range js {
			place(it, false)
		}
	}
	for _, c := range out {
		slices.SortStableFunc(c.Messages, func(a, b Item) int { return a.Date.Compare(b.Date) })
	}
	slices.SortStableFunc(out, func(a, b *Conversation) int { return b.Latest().Date.Compare(a.Latest().Date) })
	return out
}

// refs are what a message answers, nearest first.
func refs(e *Envelope) []string {
	out := make([]string, 0, len(e.References)+1)
	if e.InReplyTo != "" {
		out = append(out, e.InReplyTo)
	}
	for i := len(e.References) - 1; i >= 0; i-- {
		out = append(out, e.References[i])
	}
	return out
}
