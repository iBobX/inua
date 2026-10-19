package mail

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/lyricat/inua/internal/i18n"
)

// Level is what a check found: good, nothing either way, worth care, bad.
type Level int

const (
	LevelNone Level = iota
	LevelGood
	LevelWarn
	LevelBad
)

// Verdict is the route's: is the message what it says it is.
type Verdict int

const (
	Fine       Verdict = iota // nothing found
	Careful                   // something to look at
	Suspicious                // a sign of phishing
)

// Name is the verdict in words.
func (v Verdict) Name() string {
	switch v {
	case Careful:
		return i18n.T("Be careful")
	case Suspicious:
		return i18n.T("Suspicious")
	}
	return i18n.T("Looks fine")
}

// ID names the verdict for styles.
func (v Verdict) ID() string {
	switch v {
	case Careful:
		return "careful"
	case Suspicious:
		return "suspicious"
	}
	return "fine"
}

// Step is one check of the route and what it found.
type Step struct {
	Check string
	Found string
	Level Level
}

// Judgment is the route's steps and verdict.
type Judgment struct {
	Steps   []Step
	Verdict Verdict
}

// Judge takes a message through the phishing checks
// (docs/feat/inua-mail.md, 7.6). known says whether the user has written
// to an address before.
func Judge(m *Message, known func(addr string) bool) Judgment {
	var j Judgment
	add := func(check, found string, l Level) { j.Steps = append(j.Steps, Step{check, found, l}) }

	// 1. The sender's server vouched for it?
	check := i18n.T("Did the sender's server vouch for it?")
	if m.Authentication == "" {
		add(check, i18n.T("No report from your provider."), LevelNone)
	} else {
		r := authResults(m.Authentication)
		found := fmt.Sprintf("SPF %s · DKIM %s · DMARC %s", word(r["spf"]), word(r["dkim"]), word(r["dmarc"]))
		switch {
		case r["dmarc"] == "fail":
			add(check, found+" — "+i18n.T("the sender's domain says it did not send this."), LevelBad)
		case r["spf"] == "fail" && r["dkim"] != "pass":
			add(check, found+" — "+i18n.T("it came from a server the domain does not use."), LevelBad)
		case r["dkim"] == "pass" || r["dmarc"] == "pass":
			add(check, found, LevelGood)
		case r["spf"] == "softfail" || r["dkim"] == "fail":
			add(check, found, LevelWarn)
		default:
			add(check, found, LevelNone)
		}
	}

	// 2. Who it says it is?
	check = i18n.T("Is it who it says it is?")
	domain := m.From.Domain()
	switch {
	case m.From.Addr == "":
		add(check, i18n.T("It has no sender."), LevelBad)
	case nameAddr(m.From.Name) != "" && !strings.EqualFold(nameAddr(m.From.Name), m.From.Addr):
		add(check, i18n.Tf("Its name shows %s, but it comes from %s.", nameAddr(m.From.Name), m.From.Addr), LevelBad)
	case lookalike(domain) != "":
		add(check, i18n.Tf("%s looks like %s.", domain, lookalike(domain)), LevelBad)
	case brandIn(m.From.Name) != "" && !brandOwns(brandIn(m.From.Name), domain):
		add(check, i18n.Tf("It names %s, but comes from %s.", brandIn(m.From.Name), domain), LevelWarn)
	case mixedScripts(domain):
		add(check, i18n.Tf("%s mixes alphabets.", domain), LevelBad)
	default:
		add(check, i18n.Tf("From %s.", m.From.Addr), LevelNone)
	}

	// 3. Replies go elsewhere?
	check = i18n.T("Do replies go to the sender?")
	other := ""
	for _, a := range m.ReplyTo {
		if site(a.Domain()) != site(domain) {
			other = a.Addr
			break
		}
	}
	if other != "" {
		add(check, i18n.Tf("Replies go to %s, another domain.", other), LevelWarn)
	} else {
		add(check, i18n.T("Yes."), LevelNone)
	}

	// 4. Links where they say?
	check = i18n.T("Do its links go where they say?")
	bad, raw := 0, 0
	example := ""
	for _, l := range m.Links {
		u, err := url.Parse(l.URL)
		if err != nil {
			continue
		}
		host := strings.ToLower(u.Hostname())
		if net.ParseIP(host) != nil {
			raw++
			if example == "" {
				example = i18n.Tf("a link goes to %s, an address with no name", host)
			}
			continue
		}
		if shown := shownHost(l.Text); shown != "" && site(shown) != site(host) {
			bad++
			if example == "" {
				example = i18n.Tf("“%s” goes to %s", clip(l.Text, 40), host)
			}
		}
	}
	switch {
	case bad+raw > 0:
		add(check, i18n.Nf("%d link does not: %s.", "%d links do not: %s.", bad+raw, bad+raw, example), LevelBad)
	case len(m.Links) > 0:
		add(check, i18n.Nf("Its %d link does.", "Its %d links do.", len(m.Links), len(m.Links)), LevelNone)
	default:
		add(check, i18n.T("It has no links."), LevelNone)
	}

	// 5. Known?
	check = i18n.T("Have you written to them?")
	if known != nil && known(m.From.Addr) {
		add(check, i18n.T("Yes: a good sign, not proof."), LevelGood)
	} else {
		add(check, i18n.T("Not from this computer."), LevelNone)
	}

	for _, s := range j.Steps {
		switch {
		case s.Level == LevelBad:
			j.Verdict = Suspicious
		case s.Level == LevelWarn && j.Verdict < Careful:
			j.Verdict = Careful
		}
	}
	return j
}

// Worst is the step that decided the verdict; the zero Step when fine.
func (j Judgment) Worst() Step {
	var w Step
	for _, s := range j.Steps {
		if s.Level > w.Level && s.Level >= LevelWarn {
			w = s
		}
	}
	return w
}

func word(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

var authRe = regexp.MustCompile(`(?i)\b(spf|dkim|dmarc)\s*=\s*([a-z]+)`)

// authResults reads Authentication-Results: each method's first result.
func authResults(h string) map[string]string {
	out := map[string]string{}
	for _, m := range authRe.FindAllStringSubmatch(h, -1) {
		k := strings.ToLower(m[1])
		if _, ok := out[k]; !ok || strings.EqualFold(m[2], "pass") {
			out[k] = strings.ToLower(m[2])
		}
	}
	return out
}

var addrInName = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)

func nameAddr(name string) string { return addrInName.FindString(name) }

// brands are names phishing borrows, with the domains that are theirs.
var brands = map[string][]string{
	"paypal": {"paypal.com"}, "apple": {"apple.com", "icloud.com"}, "microsoft": {"microsoft.com", "outlook.com", "office.com", "live.com"},
	"google": {"google.com", "gmail.com", "youtube.com"}, "amazon": {"amazon.com", "amazon.co.uk", "amazon.de", "amazon.co.jp", "amazon.cn"},
	"netflix": {"netflix.com"}, "dhl": {"dhl.com", "dhl.de"}, "fedex": {"fedex.com"}, "github": {"github.com"},
	"facebook": {"facebook.com", "facebookmail.com", "meta.com"}, "instagram": {"instagram.com", "mail.instagram.com"},
	"coinbase": {"coinbase.com"}, "binance": {"binance.com"}, "docusign": {"docusign.com", "docusign.net"},
}

// brandIn names the brand a display name borrows, or "".
func brandIn(name string) string {
	l := strings.ToLower(name)
	keys := make([]string, 0, len(brands))
	for k := range brands {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, b := range keys {
		if regexp.MustCompile(`\b` + b + `\b`).MatchString(l) {
			return b
		}
	}
	return ""
}

func brandOwns(brand, domain string) bool {
	s := site(domain)
	for _, d := range brands[brand] {
		if s == site(d) {
			return true
		}
	}
	return false
}

var homoglyphs = strings.NewReplacer("0", "o", "1", "l", "3", "e", "5", "s", "rn", "m", "vv", "w")

// lookalike names the brand domain a domain imitates, or "".
func lookalike(domain string) string {
	s := site(domain)
	name, _, _ := strings.Cut(s, ".")
	plain := homoglyphs.Replace(name)
	for b, owned := range brands {
		if brandOwns(b, domain) {
			continue
		}
		for _, d := range owned {
			bn, _, _ := strings.Cut(site(d), ".")
			if (plain == bn && name != bn) || (strings.HasPrefix(domain, "xn--") && strings.Contains(plain, bn)) {
				return d
			}
		}
	}
	return ""
}

// mixedScripts says whether a domain mixes Latin with another alphabet,
// or is written in Punycode.
func mixedScripts(domain string) bool {
	if strings.Contains(domain, "xn--") {
		return true
	}
	latin, other := false, false
	for _, r := range domain {
		switch {
		case r < 128:
			if unicode.IsLetter(r) {
				latin = true
			}
		case unicode.IsLetter(r):
			other = true
		}
	}
	return latin && other
}

var twoPart = []string{"co.uk", "co.jp", "com.au", "com.cn", "com.br", "co.kr", "co.nz", "com.tw", "com.hk", "co.in", "org.uk", "ac.uk"}

// site is a domain's registrable part: example.com for mail.example.com,
// example.co.uk for a.example.co.uk.
func site(domain string) string {
	d := strings.TrimSuffix(strings.ToLower(domain), ".")
	labels := strings.Split(d, ".")
	n := 2
	if len(labels) >= 3 && slices.Contains(twoPart, strings.Join(labels[len(labels)-2:], ".")) {
		n = 3
	}
	if len(labels) <= n {
		return d
	}
	return strings.Join(labels[len(labels)-n:], ".")
}

var hostLike = regexp.MustCompile(`^(?:https?://)?((?:[a-z0-9-]+\.)+[a-z]{2,})(?:[/:?#].*)?$`)

// shownHost is the domain a link's text names, when its text is an
// address; "" for ordinary words.
func shownHost(text string) string {
	m := hostLike.FindStringSubmatch(strings.ToLower(strings.TrimSpace(text)))
	if m == nil {
		return ""
	}
	return m[1]
}

// JudgeFacts is what the assistant is given to judge a message, after
// the checks.
func JudgeFacts(m *Message, j Judgment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\nReply-To: %s\nSubject: %s\nDate: %s\n", m.From.String(), joinAddrs(m.ReplyTo), m.Subject, m.Date.Format("2006-01-02 15:04"))
	b.WriteString("\nChecks done:\n")
	for _, s := range j.Steps {
		fmt.Fprintf(&b, "- %s %s\n", s.Check, s.Found)
	}
	if len(m.Links) > 0 {
		b.WriteString("\nLinks (text → target):\n")
		for i, l := range m.Links {
			if i == 15 {
				break
			}
			fmt.Fprintf(&b, "- %s → %s\n", clip(l.Text, 60), l.URL)
		}
	}
	fmt.Fprintf(&b, "\nText:\n%s\n", clip(m.Text, 3000))
	return b.String()
}
