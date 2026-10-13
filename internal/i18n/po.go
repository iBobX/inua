package i18n

import (
	"fmt"
	"strconv"
	"strings"
)

// catalog is a language's translations: by msgid (with "ctx\x04" before
// it for a context), its forms: one, or one for each plural form.
type catalog struct {
	lang   string
	msgs   map[string][]string
	plural func(n int) int
}

// Entry is one message of a .po file.
type Entry struct {
	Comments []string // its lines starting #, as they are
	Context  string
	ID       string
	Plural   string
	Str      []string // msgstr, or msgstr[0], msgstr[1] …
}

// Key is how a catalog finds an entry: its msgid, after its context.
func (e Entry) Key() string {
	if e.Context != "" {
		return e.Context + "\x04" + e.ID
	}
	return e.ID
}

// ParsePO reads a .po file's entries; the first, with an empty msgid, is
// its header. Lines of comments alone (obsolete #~ entries) are dropped.
func ParsePO(text string) ([]Entry, error) {
	var out []Entry
	var e Entry
	var last *string // the string a continuation line adds to
	flush := func() {
		if e.ID != "" || len(e.Str) > 0 {
			out = append(out, e)
		}
		e, last = Entry{}, nil
	}
	for n, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		bad := fmt.Errorf("line %d: %q", n+1, line)
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "#"):
			if len(e.Str) > 0 {
				flush()
			}
			e.Comments = append(e.Comments, line)
		case strings.HasPrefix(line, `"`):
			s, err := unquote(line)
			if err != nil || last == nil {
				return nil, bad
			}
			*last += s
		default:
			key, val, ok := strings.Cut(line, " ")
			s, err := unquote(strings.TrimSpace(val))
			if !ok || err != nil {
				return nil, bad
			}
			if (key == "msgctxt" || key == "msgid") && len(e.Str) > 0 {
				flush()
			}
			switch {
			case key == "msgctxt":
				e.Context, last = s, &e.Context
			case key == "msgid":
				e.ID, last = s, &e.ID
			case key == "msgid_plural":
				e.Plural, last = s, &e.Plural
			case key == "msgstr":
				e.Str = append(e.Str, s)
				last = &e.Str[len(e.Str)-1]
			case strings.HasPrefix(key, "msgstr["):
				i, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(key, "msgstr["), "]"))
				if err != nil {
					return nil, bad
				}
				for len(e.Str) <= i {
					e.Str = append(e.Str, "")
				}
				e.Str[i] = s
				last = &e.Str[i]
			default:
				return nil, bad
			}
		}
	}
	flush()
	return out, nil
}

func unquote(s string) (string, error) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", fmt.Errorf("not quoted")
	}
	return strconv.Unquote(s)
}

// Quote writes s as a .po string.
func Quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`)
	return `"` + r.Replace(s) + `"`
}

func parse(text string) (*catalog, error) {
	entries, err := ParsePO(text)
	if err != nil {
		return nil, err
	}
	c := &catalog{msgs: map[string][]string{}, plural: func(n int) int {
		if n == 1 {
			return 0
		}
		return 1
	}}
	for _, e := range entries {
		if e.ID == "" { // the header
			if pf := header(e, "Plural-Forms"); strings.Contains(pf, "nplurals=1") {
				c.plural = func(int) int { return 0 }
			} else if strings.Contains(pf, "n > 1") {
				c.plural = func(n int) int {
					if n > 1 {
						return 1
					}
					return 0
				}
			}
			continue
		}
		if !Fuzzy(e) {
			c.msgs[e.Key()] = e.Str
		}
	}
	return c, nil
}

// header is a field of a .po header's msgstr.
func header(e Entry, field string) string {
	if len(e.Str) == 0 {
		return ""
	}
	for l := range strings.SplitSeq(e.Str[0], "\n") {
		if k, v, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) == field {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// Fuzzy reports whether a translation is marked unchecked: it is not used.
func Fuzzy(e Entry) bool {
	for _, c := range e.Comments {
		if strings.HasPrefix(c, "#,") && strings.Contains(c, "fuzzy") {
			return true
		}
	}
	return false
}
