package mail

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	gomail "github.com/emersion/go-message/mail"
)

// Invite is a calendar invitation a message carries (iMIP, RFC 6047): a
// text/calendar part with a VEVENT.
type Invite struct {
	Method    string // REQUEST, CANCEL, REPLY …
	UID       string
	Sequence  string
	Summary   string
	Location  string
	URL       string
	Start     time.Time
	End       time.Time
	AllDay    bool
	Organizer Address
	Attendees []Attendee
	// raw keeps the event's own lines a reply copies (DTSTART, DTEND,
	// ORGANIZER, SUMMARY …) as they came.
	raw map[string]string
	// Data is the calendar file, to open with the desktop's calendar app.
	Data []byte
}

// Attendee is someone invited, and their answer so far.
type Attendee struct {
	Address
	Status string // NEEDS-ACTION, ACCEPTED, TENTATIVE, DECLINED
}

// Answers a reply can give.
const (
	Accepted  = "ACCEPTED"
	Tentative = "TENTATIVE"
	Declined  = "DECLINED"
)

// Invitation finds the invitation in a message; nil when it has none.
func (m *Message) Invitation() *Invite {
	for _, a := range m.Attachments {
		if a.Type == "text/calendar" || a.Type == "application/ics" || strings.HasSuffix(strings.ToLower(a.Name), ".ics") {
			if inv := ParseInvite(a.Data); inv != nil {
				return inv
			}
		}
	}
	return nil
}

// ParseInvite reads a calendar file's first event; nil when it has none.
func ParseInvite(data []byte) *Invite {
	lines := unfold(string(data))
	inv := &Invite{raw: map[string]string{}, Data: data}
	in := false
	found := false
	for _, l := range lines {
		name, params, value := icsLine(l)
		switch {
		case name == "METHOD" && !in:
			inv.Method = strings.ToUpper(value)
		case name == "BEGIN" && value == "VEVENT":
			if found {
				return inv // the first event only: a series' exceptions follow it
			}
			in, found = true, true
		case name == "END" && value == "VEVENT":
			in = false
		case !in:
		case name == "UID":
			inv.UID = value
		case name == "SEQUENCE":
			inv.Sequence = value
		case name == "SUMMARY":
			inv.Summary = icsText(value)
			inv.raw[name] = l
		case name == "LOCATION":
			inv.Location = icsText(value)
		case name == "URL":
			inv.URL = value
		case name == "DTSTART":
			inv.Start, inv.AllDay = icsTime(value, params)
			inv.raw[name] = l
		case name == "DTEND":
			inv.End, _ = icsTime(value, params)
			inv.raw[name] = l
		case name == "ORGANIZER":
			inv.Organizer = icsPerson(value, params)
			inv.raw[name] = l
		case name == "ATTENDEE":
			inv.Attendees = append(inv.Attendees, Attendee{icsPerson(value, params), strings.ToUpper(params["PARTSTAT"])})
		}
	}
	if !found {
		return nil
	}
	if inv.Method == "" {
		inv.Method = "PUBLISH"
	}
	if inv.End.IsZero() && !inv.Start.IsZero() {
		if inv.AllDay {
			inv.End = inv.Start.AddDate(0, 0, 1)
		} else {
			inv.End = inv.Start.Add(time.Hour)
		}
	}
	if inv.URL == "" {
		inv.URL = meetingLink(inv.Location)
	}
	return inv
}

// Status is the answer an attendee gave so far; "" when they are not
// invited.
func (inv *Invite) Status(addr string) string {
	for _, a := range inv.Attendees {
		if strings.EqualFold(a.Addr, addr) {
			return a.Status
		}
	}
	return ""
}

// Asks says whether the invitation wants an answer: a request to an
// attendee.
func (inv *Invite) Asks() bool { return inv.Method == "REQUEST" }

// unfold joins a calendar file's folded lines (a line that starts with a
// space or tab goes on the one before).
func unfold(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && len(out) > 0 {
			out[len(out)-1] += l[1:]
			continue
		}
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// icsLine splits NAME;PARAM=V;…:VALUE, quotes kept out of the split.
func icsLine(l string) (name string, params map[string]string, value string) {
	params = map[string]string{}
	quoted, colon := false, -1
	for i, r := range l {
		if r == '"' {
			quoted = !quoted
		}
		if r == ':' && !quoted {
			colon = i
			break
		}
	}
	if colon < 0 {
		return strings.ToUpper(l), params, ""
	}
	head, value := l[:colon], l[colon+1:]
	parts := strings.Split(head, ";")
	name = strings.ToUpper(parts[0])
	for _, p := range parts[1:] {
		k, v, _ := strings.Cut(p, "=")
		params[strings.ToUpper(k)] = strings.Trim(v, `"`)
	}
	return name, params, value
}

// icsText undoes a text value's escapes.
func icsText(v string) string {
	return strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`).Replace(v)
}

func icsPerson(value string, params map[string]string) Address {
	addr := value
	if i := strings.Index(strings.ToLower(addr), "mailto:"); i >= 0 {
		addr = addr[i+len("mailto:"):]
	}
	return Address{Name: params["CN"], Addr: addr}
}

// icsTime reads a date or a time: UTC (…Z), in a named zone (TZID), or
// floating (here); a date alone is a day's event.
func icsTime(v string, params map[string]string) (time.Time, bool) {
	if params["VALUE"] == "DATE" || len(v) == 8 {
		t, err := time.ParseInLocation("20060102", v, time.Local)
		return t, err == nil
	}
	if strings.HasSuffix(v, "Z") {
		t, _ := time.Parse("20060102T150405Z", v)
		return t, false
	}
	loc := time.Local
	if tz := params["TZID"]; tz != "" {
		if l, err := time.LoadLocation(windowsZone(tz)); err == nil {
			loc = l
		}
	}
	t, _ := time.ParseInLocation("20060102T150405", v, loc)
	return t, false
}

// windowsZone turns the Windows names Outlook gives zones into IANA names.
func windowsZone(tz string) string {
	if z, ok := windowsZones[tz]; ok {
		return z
	}
	return tz
}

var windowsZones = map[string]string{
	"Pacific Standard Time": "America/Los_Angeles", "Mountain Standard Time": "America/Denver",
	"Central Standard Time": "America/Chicago", "Eastern Standard Time": "America/New_York",
	"GMT Standard Time": "Europe/London", "W. Europe Standard Time": "Europe/Berlin",
	"Romance Standard Time": "Europe/Paris", "Central Europe Standard Time": "Europe/Budapest",
	"China Standard Time": "Asia/Shanghai", "Tokyo Standard Time": "Asia/Tokyo",
	"Korea Standard Time": "Asia/Seoul", "Taipei Standard Time": "Asia/Taipei",
	"Singapore Standard Time": "Asia/Singapore", "India Standard Time": "Asia/Kolkata",
	"AUS Eastern Standard Time": "Australia/Sydney", "UTC": "UTC",
}

// meetingLink finds a video meeting's address in a location.
func meetingLink(s string) string {
	for _, f := range strings.Fields(s) {
		for _, host := range []string{"meet.google.com", "zoom.us", "teams.microsoft.com", "whereby.com", "meet.jit.si"} {
			if strings.Contains(f, host) {
				if !strings.HasPrefix(f, "http") {
					f = "https://" + f
				}
				return strings.Trim(f, "<>(),")
			}
		}
	}
	return ""
}

// InviteReply builds the answer to an invitation for its organizer
// (iTIP REPLY): a line of text, and the calendar part with the user as
// the attendee and their answer.
func InviteReply(inv *Invite, acc Account, answer string, now time.Time) (Built, error) {
	if inv.Organizer.Addr == "" {
		return Built{}, ErrNoRecipient
	}
	var cal strings.Builder
	w := func(s string) { cal.WriteString(fold(s) + "\r\n") }
	w("BEGIN:VCALENDAR")
	w("PRODID:-//inua//mail//EN")
	w("VERSION:2.0")
	w("METHOD:REPLY")
	w("BEGIN:VEVENT")
	w("UID:" + inv.UID)
	if inv.Sequence != "" {
		w("SEQUENCE:" + inv.Sequence)
	}
	w("DTSTAMP:" + now.UTC().Format("20060102T150405Z"))
	for _, k := range []string{"ORGANIZER", "DTSTART", "DTEND", "SUMMARY"} {
		if l := inv.raw[k]; l != "" {
			w(l)
		}
	}
	cn := ""
	if acc.Name != "" {
		cn = `;CN="` + strings.ReplaceAll(acc.Name, `"`, "'") + `"`
	}
	w("ATTENDEE;PARTSTAT=" + answer + cn + ":mailto:" + acc.Address)
	w("END:VEVENT")
	w("END:VCALENDAR")

	var h gomail.Header
	h.SetAddressList("From", []*gomail.Address{{Name: acc.Name, Address: acc.Address}})
	h.SetAddressList("To", []*gomail.Address{{Name: inv.Organizer.Name, Address: inv.Organizer.Addr}})
	h.SetSubject(answerWord(answer) + ": " + inv.Summary)
	h.SetDate(now)
	_, domain, _ := strings.Cut(acc.Address, "@")
	if err := h.GenerateMessageIDWithHostname(cmpOr(domain, "inua.local")); err != nil {
		return Built{}, err
	}
	id, _ := h.MessageID()
	h.Set("User-Agent", "inua-mail")
	var buf bytes.Buffer
	mw, err := gomail.CreateInlineWriter(&buf, h) // multipart/alternative
	if err != nil {
		return Built{}, err
	}
	var th gomail.InlineHeader
	th.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
	tw, err := mw.CreatePart(th)
	if err != nil {
		return Built{}, err
	}
	fmt.Fprintf(tw, "%s %s: %s\n", cmpOr(acc.Name, acc.Address), strings.ToLower(answerWord(answer)), inv.Summary)
	tw.Close()
	var ch gomail.InlineHeader
	ch.SetContentType("text/calendar", map[string]string{"charset": "utf-8", "method": "REPLY"})
	ch.Set("Content-Transfer-Encoding", "8bit") // folded to 75 bytes already; calendars read it as it is
	cw, err := mw.CreatePart(ch)
	if err != nil {
		return Built{}, err
	}
	io.WriteString(cw, cal.String())
	cw.Close()
	if err := mw.Close(); err != nil {
		return Built{}, err
	}
	return Built{Raw: buf.Bytes(), MessageID: id, From: acc.Address, Recipients: []string{inv.Organizer.Addr}}, nil
}

// answerWord is the subject's word for an answer, in English as
// calendars expect.
func answerWord(answer string) string {
	switch answer {
	case Accepted:
		return "Accepted"
	case Tentative:
		return "Tentative"
	}
	return "Declined"
}

// fold cuts a calendar line at 75 bytes, on rune boundaries.
func fold(s string) string {
	if len(s) <= 75 {
		return s
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		size := len(string(r))
		if n+size > 75 {
			b.WriteString("\r\n ")
			n = 1
		}
		b.WriteRune(r)
		n += size
	}
	return b.String()
}
