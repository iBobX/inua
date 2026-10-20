package mail

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Link is a link in an HTML message: what it says and where it goes.
type Link struct {
	Text, URL string
}

// HTMLToText turns an HTML message into text: paragraphs, headings, list
// items with marks, quotes with "> ", links as their text with the
// address after it when it is not the text itself. Styles, scripts and
// images are dropped (an image leaves its alt text).
func HTMLToText(s string) (string, []Link) {
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return "", nil
	}
	t := &textOut{}
	t.walk(doc)
	return cleanText(t.b.String()), t.links
}

type textOut struct {
	b       strings.Builder
	links   []Link
	quote   int   // blockquotes deep
	pre     int   // inside <pre>
	list    []int // the numbers of ordered lists; -1 for bullets
	lineEnd bool  // the last thing written ends a line
	space   bool  // a space is owed before the next word
}

func (t *textOut) newline(n int) {
	s := t.b.String()
	have := len(s) - len(strings.TrimRight(s, "\n"))
	if len(s) == 0 {
		return
	}
	for ; have < n; have++ {
		t.b.WriteByte('\n')
	}
	t.lineEnd, t.space = true, false
}

func (t *textOut) write(s string) {
	if s == "" {
		return
	}
	if t.lineEnd && t.quote > 0 {
		t.b.WriteString(strings.Repeat("> ", t.quote))
	}
	if t.space && !t.lineEnd {
		t.b.WriteByte(' ')
	}
	t.b.WriteString(s)
	t.lineEnd, t.space = false, false
}

func (t *textOut) text(s string) {
	if t.pre > 0 {
		for i, l := range strings.Split(s, "\n") {
			if i > 0 {
				t.b.WriteByte('\n')
				t.lineEnd = true
			}
			t.write(l)
		}
		return
	}
	lead := len(s) > 0 && isSpace(s[0])
	trail := len(s) > 0 && isSpace(s[len(s)-1])
	words := strings.Fields(strings.ReplaceAll(s, " ", " "))
	if len(words) == 0 {
		if lead || trail {
			t.space = true
		}
		return
	}
	if lead {
		t.space = true
	}
	t.write(strings.Join(words, " "))
	t.space = trail
}

func isSpace(c byte) bool { return c == ' ' || c == '\n' || c == '\t' || c == '\r' }

func (t *textOut) walk(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		t.text(n.Data)
		return
	case html.CommentNode, html.DoctypeNode:
		return
	case html.ElementNode:
		if hidden(n) {
			return
		}
		switch n.DataAtom {
		case atom.Script, atom.Style, atom.Head, atom.Title, atom.Noscript, atom.Template, atom.Svg:
			return
		case atom.Img:
			if alt := strings.TrimSpace(attr(n, "alt")); alt != "" && len(alt) < 80 {
				t.text(" [" + alt + "] ")
			}
			return
		case atom.Br:
			t.b.WriteByte('\n')
			t.lineEnd, t.space = true, false
			return
		case atom.Hr:
			t.newline(2)
			t.write("———")
			t.newline(2)
			return
		case atom.A:
			t.link(n)
			return
		}
	}
	block := n.Type == html.ElementNode && blocks[n.DataAtom]
	if block {
		t.newline(gap(n.DataAtom))
	}
	switch n.DataAtom {
	case atom.Blockquote:
		t.quote++
	case atom.Pre:
		t.pre++
	case atom.Ul:
		t.list = append(t.list, -1)
	case atom.Ol:
		t.list = append(t.list, 0)
	case atom.Li:
		mark := "•"
		if k := len(t.list); k > 0 && t.list[k-1] >= 0 {
			t.list[k-1]++
			mark = itoa(t.list[k-1]) + "."
		}
		t.write(strings.Repeat("  ", max(len(t.list)-1, 0)) + mark)
		t.space = true
	case atom.Td, atom.Th:
		if n.PrevSibling != nil {
			t.space = true
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		t.walk(c)
	}
	switch n.DataAtom {
	case atom.Blockquote:
		t.quote--
	case atom.Pre:
		t.pre--
	case atom.Ul, atom.Ol:
		t.list = t.list[:len(t.list)-1]
	}
	if block {
		t.newline(gap(n.DataAtom))
	}
}

// link writes a link's text, and where it goes when that is not the
// text, and keeps it for the phishing route.
func (t *textOut) link(n *html.Node) {
	href := strings.TrimSpace(attr(n, "href"))
	sub := &textOut{}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sub.walk(c)
	}
	text := strings.Join(strings.Fields(sub.b.String()), " ")
	t.links = append(t.links, sub.links...)
	u, err := url.Parse(href)
	web := err == nil && (u.Scheme == "http" || u.Scheme == "https")
	if web {
		t.links = append(t.links, Link{Text: text, URL: href})
	}
	switch {
	case text == "" && web:
		t.text(" " + href + " ")
	case text == "":
	case !web || sameLink(text, href) || strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]"): // a picture's alt text: no address after it
		t.text(" " + text + " ")
	default:
		t.text(" " + text + " ")
		t.write("<" + href + ">")
		t.space = true
	}
}

// sameLink says whether a link's text is its address already.
func sameLink(text, href string) bool {
	trim := func(s string) string {
		s = strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
		return strings.TrimSuffix(strings.TrimPrefix(s, "www."), "/")
	}
	return trim(text) == trim(href) || strings.TrimPrefix(href, "mailto:") == text
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

var blocks = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Ul: true, atom.Ol: true, atom.Li: true, atom.Blockquote: true, atom.Pre: true, atom.Table: true, atom.Tr: true,
	atom.Section: true, atom.Article: true, atom.Header: true, atom.Footer: true, atom.Center: true, atom.Address: true,
}

// gap is how many line ends go around a block: a paragraph's blank line,
// a list item's or a row's one.
func gap(a atom.Atom) int {
	switch a {
	case atom.Li, atom.Tr, atom.Div, atom.Table:
		return 1
	}
	return 2
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
