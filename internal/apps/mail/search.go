package mail

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/emersion/go-imap/v2"
)

// Query is a search: words, and operators as Gmail writes them
// (from:ana has:attachment after:2026-09-01 is:unread "exact words").
type Query struct {
	Words   []string // in the subject, the people or the text
	From    []string
	To      []string
	Subject []string
	Attach  bool
	Unread  bool
	Read    bool
	Starred bool
	After   time.Time // on or after
	Before  time.Time // before
}

// Empty says whether the query asks nothing.
func (q Query) Empty() bool {
	return len(q.Words) == 0 && len(q.From) == 0 && len(q.To) == 0 && len(q.Subject) == 0 &&
		!q.Attach && !q.Unread && !q.Read && !q.Starred && q.After.IsZero() && q.Before.IsZero()
}

// ParseQuery reads a search as typed; now is for newer_than and
// older_than.
func ParseQuery(s string, now time.Time) Query {
	var q Query
	for _, tok := range tokens(s) {
		key, val, ok := strings.Cut(tok, ":")
		if !ok || val == "" {
			q.Words = append(q.Words, strings.ToLower(tok))
			continue
		}
		val = strings.ToLower(val)
		switch strings.ToLower(key) {
		case "from":
			q.From = append(q.From, val)
		case "to", "cc":
			q.To = append(q.To, val)
		case "subject":
			q.Subject = append(q.Subject, val)
		case "has":
			if val == "attachment" || val == "attachments" {
				q.Attach = true
			}
		case "is":
			switch val {
			case "unread":
				q.Unread = true
			case "read":
				q.Read = true
			case "starred", "flagged":
				q.Starred = true
			}
		case "after", "since":
			if t, err := time.ParseInLocation("2006-01-02", val, now.Location()); err == nil {
				q.After = t
			}
		case "before":
			if t, err := time.ParseInLocation("2006-01-02", val, now.Location()); err == nil {
				q.Before = t
			}
		case "newer_than":
			if d, ok := span(val, now); ok {
				q.After = d
			}
		case "older_than":
			if d, ok := span(val, now); ok {
				q.Before = d
			}
		default:
			q.Words = append(q.Words, strings.ToLower(tok)) // a word with a colon in it
		}
	}
	return q
}

// span reads 3d, 2w, 6m or 1y back from now.
func span(s string, now time.Time) (time.Time, bool) {
	if len(s) < 2 {
		return time.Time{}, false
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n < 0 {
		return time.Time{}, false
	}
	switch s[len(s)-1] {
	case 'd':
		return now.AddDate(0, 0, -n), true
	case 'w':
		return now.AddDate(0, 0, -7*n), true
	case 'm':
		return now.AddDate(0, -n, 0), true
	case 'y':
		return now.AddDate(-n, 0, 0), true
	}
	return time.Time{}, false
}

// tokens splits on spaces, keeping "quoted words" (also key:"quoted")
// together.
func tokens(s string) []string {
	var out []string
	var b strings.Builder
	quoted := false
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
		case unicode.IsSpace(r) && !quoted:
			if b.Len() > 0 {
				out = append(out, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

// Match says whether a message in the index matches; the words are
// looked for in what the index has (subject, people, snippet).
func (q Query) Match(e *Envelope) bool {
	if q.Attach && !e.Attach || q.Unread && e.Has(Seen) || q.Read && !e.Has(Seen) || q.Starred && !e.Has(Flagged) {
		return false
	}
	if !q.After.IsZero() && e.Date.Before(q.After) || !q.Before.IsZero() && !e.Date.Before(q.Before) {
		return false
	}
	from := strings.ToLower(e.From.Name + " " + e.From.Addr)
	var to strings.Builder
	for _, a := range append(append([]Address{}, e.To...), e.Cc...) {
		to.WriteString(strings.ToLower(a.Name + " " + a.Addr + " "))
	}
	subject := strings.ToLower(e.Subject)
	for _, f := range q.From {
		if !strings.Contains(from, f) {
			return false
		}
	}
	for _, t := range q.To {
		if !strings.Contains(to.String(), t) {
			return false
		}
	}
	for _, s := range q.Subject {
		if !strings.Contains(subject, s) {
			return false
		}
	}
	all := subject + " " + from + " " + to.String() + " " + strings.ToLower(e.Snippet)
	for _, w := range q.Words {
		if !strings.Contains(all, w) {
			return false
		}
	}
	return true
}

// Criteria is the query for the server's SEARCH.
func (q Query) Criteria() *imap.SearchCriteria {
	c := &imap.SearchCriteria{Since: q.After, Before: q.Before, Text: q.Words}
	for _, f := range q.From {
		c.Header = append(c.Header, imap.SearchCriteriaHeaderField{Key: "From", Value: f})
	}
	for _, t := range q.To {
		c.Header = append(c.Header, imap.SearchCriteriaHeaderField{Key: "To", Value: t})
	}
	for _, s := range q.Subject {
		c.Header = append(c.Header, imap.SearchCriteriaHeaderField{Key: "Subject", Value: s})
	}
	if q.Unread {
		c.NotFlag = append(c.NotFlag, imap.FlagSeen)
	}
	if q.Read {
		c.Flag = append(c.Flag, imap.FlagSeen)
	}
	if q.Starred {
		c.Flag = append(c.Flag, imap.FlagFlagged)
	}
	if q.Attach {
		// No standard key: a multipart/mixed message is the usual sign.
		c.Header = append(c.Header, imap.SearchCriteriaHeaderField{Key: "Content-Type", Value: "multipart/mixed"})
	}
	return c
}

// Looks says whether a search reads as a sentence for the assistant to
// turn into operators, rather than words to look for.
func Looks(s string) bool {
	if strings.Contains(s, ":") {
		return false
	}
	words := strings.Fields(s)
	if len(words) >= 4 {
		return true
	}
	// A sentence in a language without spaces.
	n := 0
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			n++
		}
	}
	return n >= 6
}
