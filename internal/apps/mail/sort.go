package mail

import (
	"fmt"
	"strings"

	"github.com/lyricat/inua/internal/i18n"
)

// Tag is what the sorting makes of a message in the inbox
// (docs/feat/inua-mail.md, 7.2).
type Tag string

const (
	TagNone    Tag = ""
	TagReply   Tag = "reply"   // a person asks the user something
	TagWait    Tag = "wait"    // the user asked and waits
	TagFYI     Tag = "fyi"     // for the user to know
	TagNews    Tag = "news"    // a newsletter
	TagReceipt Tag = "receipt" // an order, an invoice, a booking
	TagNotice  Tag = "notice"  // a service's notification
)

// Tags are the tags in the order they are offered.
var Tags = []Tag{TagReply, TagWait, TagFYI, TagNews, TagReceipt, TagNotice}

// Mark is a tag as the list shows it, in mono capitals.
func (t Tag) Mark() string {
	switch t {
	case TagReply:
		return i18n.T("REPLY")
	case TagWait:
		return i18n.T("WAITING")
	case TagFYI:
		return i18n.T("FYI")
	case TagNews:
		return i18n.T("NEWS")
	case TagReceipt:
		return i18n.T("RECEIPT")
	case TagNotice:
		return i18n.T("NOTICE")
	}
	return ""
}

// Name is a tag in words.
func (t Tag) Name() string {
	switch t {
	case TagReply:
		return i18n.T("Needs a reply")
	case TagWait:
		return i18n.T("Waiting for an answer")
	case TagFYI:
		return i18n.T("For your information")
	case TagNews:
		return i18n.T("Newsletter")
	case TagReceipt:
		return i18n.T("Receipt or booking")
	case TagNotice:
		return i18n.T("Notification")
	}
	return i18n.T("Not sorted")
}

var noReply = []string{"noreply", "no-reply", "donotreply", "do-not-reply", "notifications", "notification", "mailer-daemon", "bounce", "alerts", "notify"}

var receiptWords = []string{"receipt", "invoice", "your order", "order confirmation", "booking", "reservation", "payment received",
	"rechnung", "facture", "factura", "领收", "发票", "订单", "領収書", "請求書", "ご注文"}

// Rules sorts a message without the assistant when its headers settle
// it; false when they do not. mine says whether the user's own message is
// the conversation's last; user is the account's address. Whether a
// person's message asks something of the user (REPLY) is never settled
// here: the assistant's sorting decides it.
func Rules(e Envelope, mine bool, user string) (Tag, bool) {
	local, _, _ := strings.Cut(strings.ToLower(e.From.Addr), "@")
	automated := e.Auto || containsAny(local, noReply)
	subject := strings.ToLower(e.Subject)
	switch {
	case mine:
		return TagWait, true
	case automated && containsAny(subject, receiptWords):
		return TagReceipt, true
	case e.Bulk && !automated:
		return TagNews, true
	case automated:
		return TagNotice, true
	}
	return TagNone, false
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// SortInstructions tell the decision profile how to sort a message.
const SortInstructions = "Sort one e-mail in the user's inbox into exactly one category: " +
	"reply = a person (not a service) writes to the user and asks a question or for an action; " +
	"fyi = a person writes but asks nothing of the user; " +
	"news = a newsletter or marketing; " +
	"receipt = an order, invoice, payment, booking or shipping message; " +
	"notice = an automated notification from a service (security alerts, CI, social, calendar)."

// SortOptions are what Choose may answer, in Tags' terms.
var SortOptions = []string{"reply", "fyi", "news", "receipt", "notice"}

// SortTags are SortOptions as tags.
var SortTags = []Tag{TagReply, TagFYI, TagNews, TagReceipt, TagNotice}

// SortState is what the decision profile is given of a message: the
// sender, the subject and the start of the text, nothing else.
func SortState(e Envelope, user string) string {
	return fmt.Sprintf("The user: %s\nFrom: %s\nTo: %s\nSubject: %s\nList mail: %v\nStart of the text: %s",
		user, e.From.String(), joinAddrs(e.To), e.Subject, e.Bulk, clip(e.Snippet, 400))
}

func joinAddrs(l []Address) string {
	s := make([]string, 0, len(l))
	for _, a := range l {
		s = append(s, a.String())
	}
	return strings.Join(s, ", ")
}
