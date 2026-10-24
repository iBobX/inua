package host

// The passphrases gpg asks for, relayed to the web pages: inua-pinentry
// asks the shell's dialog and, through the host, every page open (a
// phone's), and the first answer wins. The host keeps a question only
// while it waits; a passphrase passes through it, never kept or logged.

// Secret is what gpg asks: a passphrase (Mode "getpin"), or a yes or no
// ("confirm"), or a note to read ("message"); the texts as the page shows
// them.
type Secret struct {
	ID     int    `json:"id"` // the host's, set when it is asked
	Mode   string `json:"mode"`
	Title  string `json:"title,omitempty"`  // who asks: "ansible-playbook asks for your passphrase"
	Desc   string `json:"desc,omitempty"`   // gpg's: the key, what for
	Prompt string `json:"prompt,omitempty"` // beside the field
	Error  string `json:"error,omitempty"`  // why it asks again: "Bad Passphrase"
	OK     string `json:"ok,omitempty"`     // the buttons
	Cancel string `json:"cancel,omitempty"`
	NotOK  string `json:"not_ok,omitempty"`
}

// A page's answers.
const (
	AnswerOK     = "ok"
	AnswerNo     = "no" // a confirm's third button (gpg's NOTOK)
	AnswerCancel = "cancel"
)

// The errors a secret's reply has besides the page's own.
const (
	ErrNoPage    = "no page" // no web page open to ask
	ErrCancelled = "cancelled"
	ErrNotOK     = "not ok"
)

// asked is a secret waiting: who asked it, and its request's ID.
type asked struct {
	s     Secret
	asker *client
	id    int
}

// handleSecrets answers secret and answer; false for another message.
func (h *Host) handleSecrets(c *client, m Message) bool {
	switch m.Type {
	case MsgSecret:
		if m.Secret == nil {
			c.message(Message{Type: MsgReply, ID: m.ID, Error: "no secret"})
			return true
		}
		h.mu.Lock()
		pages := h.pagesLocked()
		if len(pages) == 0 {
			h.mu.Unlock()
			c.message(Message{Type: MsgReply, ID: m.ID, Error: ErrNoPage})
			return true
		}
		h.secretIDs++
		s := *m.Secret
		s.ID = h.secretIDs
		h.asked[s.ID] = &asked{s: s, asker: c, id: m.ID}
		h.mu.Unlock()
		for _, p := range pages {
			p.message(Message{Type: MsgSecret, Secret: &s})
		}
		// The reply goes with the answer.
		return true
	case MsgAnswer:
		h.mu.Lock()
		a := h.asked[m.Asked]
		if a != nil && c.web {
			delete(h.asked, m.Asked)
		}
		pages := h.pagesLocked()
		h.mu.Unlock()
		if a == nil || !c.web {
			c.message(Message{Type: MsgReply, ID: m.ID, Error: "no such question"})
			return true
		}
		r := Message{Type: MsgReply, ID: a.id}
		switch m.Answer {
		case AnswerOK:
			r.Pin = m.Pin
			if r.Pin == nil {
				r.Pin = new(string)
			}
		case AnswerNo:
			r.Error = ErrNotOK
		default:
			r.Error = ErrCancelled
		}
		a.asker.message(r)
		for _, p := range pages {
			p.message(Message{Type: MsgWithdraw, Asked: m.Asked})
		}
		c.message(Message{Type: MsgReply, ID: m.ID})
		return true
	}
	return false
}

// pagesLocked are the web pages connected; h.mu is held.
func (h *Host) pagesLocked() []*client {
	var pages []*client
	for c := range h.clients {
		if c.web {
			pages = append(pages, c)
		}
	}
	return pages
}

// secretsFor sends a page that has just come the secrets waiting.
func (h *Host) secretsFor(c *client) {
	h.mu.Lock()
	var waiting []Secret
	for _, a := range h.asked {
		waiting = append(waiting, a.s)
	}
	h.mu.Unlock()
	for i := range waiting {
		c.message(Message{Type: MsgSecret, Secret: &waiting[i]})
	}
}

// withdrawFrom takes back the secrets c asked, which went: the desktop's
// dialog was answered, or gpg gave up.
func (h *Host) withdrawFrom(c *client) {
	h.mu.Lock()
	var gone []int
	for id, a := range h.asked {
		if a.asker == c {
			gone = append(gone, id)
			delete(h.asked, id)
		}
	}
	pages := h.pagesLocked()
	h.mu.Unlock()
	for _, id := range gone {
		for _, p := range pages {
			p.message(Message{Type: MsgWithdraw, Asked: id})
		}
	}
}
