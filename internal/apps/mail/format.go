package mail

import (
	"encoding/base64"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// BlockKind is what a block of a formatted message is.
type BlockKind int

const (
	BlockPara BlockKind = iota
	BlockHeading
	BlockItem // a list item: Mark and Depth say where
	BlockPre
	BlockRule
	BlockImage
)

// Block is a piece of an HTML message the reader draws on its own: a
// paragraph, heading or list item as Pango markup, preformatted text, a
// rule, or a picture.
type Block struct {
	Kind   BlockKind
	Markup string // Pango markup: <b>, <i>, <u>, <s>, <tt>, <small>, <a href>
	Level  int    // a heading's, 1 to 6
	Mark   string // a list item's: "•" or "3."
	Depth  int    // lists deep, for an item; blockquotes deep, for every block
	Quoted bool   // in the part that quotes earlier mail
	Image  Picture
}

// Picture is an image of a message: where it comes from and how it is
// meant to be shown.
type Picture struct {
	Src           string // http(s) for a remote one, cid: for a part's
	Data          []byte // a data: URL's bytes
	Alt, Link     string
	Width, Height int // as the HTML says, 0 when it does not
}

// Remote says whether the picture is loaded from the web.
func (p Picture) Remote() bool {
	return strings.HasPrefix(p.Src, "https://") || strings.HasPrefix(p.Src, "http://")
}

// Format turns an HTML message into blocks the reader draws: text with
// its emphasis and links, headings, lists, quotes, preformatted text and
// pictures. Layout tables become the blocks of their cells; styles,
// scripts, forms and tracking pixels are dropped.
func Format(s string) []Block {
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return nil
	}
	f := &formatter{}
	f.walk(doc)
	f.flush()
	return f.out
}

type formatter struct {
	out    []Block
	b      strings.Builder // the markup of the block being written
	space  bool            // a space is owed before the next word
	open   []string        // the inline tags open, to close and reopen at a block's edges
	quote  int
	pre    int
	list   []int // the numbers of ordered lists; -1 for bullets
	quoted bool
	link   string // the href of the link being written, for its pictures
	head   int    // the heading level being written
	item   string // the mark of the list item just begun
}

// flush ends the block being written.
func (f *formatter) flush() {
	text := f.b.String()
	for i := len(f.open) - 1; i >= 0; i-- {
		text += "</" + tagName(f.open[i]) + ">"
	}
	f.b.Reset()
	for _, t := range f.open {
		f.b.WriteString(t)
	}
	f.space = false
	if strings.TrimSpace(stripTags(text)) == "" && f.item == "" {
		return
	}
	bl := Block{Kind: BlockPara, Markup: strings.TrimSpace(text), Depth: f.quote, Quoted: f.quoted}
	switch {
	case f.head > 0:
		bl.Kind, bl.Level = BlockHeading, f.head
	case f.item != "":
		bl.Kind, bl.Mark, bl.Depth = BlockItem, f.item, max(len(f.list)-1, 0)
		f.item = ""
	}
	f.out = append(f.out, bl)
}

func tagName(open string) string {
	n := strings.TrimPrefix(open, "<")
	if i := strings.IndexAny(n, " >"); i >= 0 {
		n = n[:i]
	}
	return n
}

func stripTags(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (f *formatter) text(s string) {
	if f.pre > 0 {
		f.b.WriteString(markupEscape(s))
		return
	}
	lead := len(s) > 0 && isSpace(s[0])
	trail := len(s) > 0 && isSpace(s[len(s)-1])
	words := strings.Fields(strings.ReplaceAll(s, " ", " "))
	if len(words) == 0 {
		if lead || trail {
			f.space = true
		}
		return
	}
	if (lead || f.space) && f.b.Len() > len(strings.Join(f.open, "")) {
		f.b.WriteByte(' ')
	}
	f.b.WriteString(markupEscape(strings.Join(words, " ")))
	f.space = trail
}

// inline opens a tag for the children of n, and closes it after them.
func (f *formatter) inline(tag string, n *html.Node) {
	if f.space && f.b.Len() > 0 {
		f.b.WriteByte(' ')
		f.space = false
	}
	f.b.WriteString(tag)
	f.open = append(f.open, tag)
	f.children(n)
	f.open = f.open[:len(f.open)-1]
	f.b.WriteString("</" + tagName(tag) + ">")
}

func (f *formatter) children(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		f.walk(c)
	}
}

func (f *formatter) walk(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		f.text(n.Data)
		return
	case html.CommentNode, html.DoctypeNode:
		return
	case html.DocumentNode:
		f.children(n)
		return
	}
	if n.Type != html.ElementNode || hidden(n) {
		return
	}
	if !f.quoted && quoteStart(n) {
		f.flush()
		f.quoted = true
	}
	switch n.DataAtom {
	case atom.Script, atom.Style, atom.Head, atom.Title, atom.Noscript, atom.Template, atom.Svg,
		atom.Form, atom.Input, atom.Button, atom.Select, atom.Textarea, atom.Iframe, atom.Object:
		return
	case atom.Br:
		f.b.WriteByte('\n')
		f.space = false
		return
	case atom.Hr:
		f.flush()
		f.out = append(f.out, Block{Kind: BlockRule, Depth: f.quote, Quoted: f.quoted})
		return
	case atom.Img:
		f.image(n)
		return
	case atom.B, atom.Strong:
		f.inline("<b>", n)
		return
	case atom.I, atom.Em, atom.Cite:
		f.inline("<i>", n)
		return
	case atom.U, atom.Ins:
		f.inline("<u>", n)
		return
	case atom.S, atom.Strike, atom.Del:
		f.inline("<s>", n)
		return
	case atom.Code, atom.Kbd, atom.Samp, atom.Tt:
		f.inline("<tt>", n)
		return
	case atom.Small, atom.Sub, atom.Sup:
		f.inline("<small>", n)
		return
	case atom.A:
		href := strings.TrimSpace(attr(n, "href"))
		u, err := url.Parse(href)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto") {
			f.children(n)
			return
		}
		was := f.link
		f.link = href
		f.inline(`<a href="`+markupEscape(href)+`">`, n)
		f.link = was
		return
	case atom.Pre:
		f.flush()
		f.pre++
		f.children(n)
		f.pre--
		text := f.b.String()
		f.b.Reset()
		if strings.TrimSpace(text) != "" {
			f.out = append(f.out, Block{Kind: BlockPre, Markup: strings.Trim(text, "\n"), Depth: f.quote, Quoted: f.quoted})
		}
		return
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		f.flush()
		f.head = int(n.Data[1] - '0')
		f.children(n)
		f.flush()
		f.head = 0
		return
	case atom.Blockquote:
		f.flush()
		f.quote++
		f.children(n)
		f.flush()
		f.quote--
		return
	case atom.Ul, atom.Ol:
		f.flush()
		if n.DataAtom == atom.Ul {
			f.list = append(f.list, -1)
		} else {
			start, _ := strconv.Atoi(attr(n, "start"))
			f.list = append(f.list, max(start-1, 0))
		}
		f.children(n)
		f.flush()
		f.list = f.list[:len(f.list)-1]
		return
	case atom.Li:
		f.flush()
		f.item = "•"
		if k := len(f.list); k > 0 && f.list[k-1] >= 0 {
			f.list[k-1]++
			f.item = strconv.Itoa(f.list[k-1]) + "."
		}
		f.children(n)
		f.flush()
		return
	}
	if formatBlocks[n.DataAtom] {
		f.flush()
		f.children(n)
		f.flush()
		return
	}
	f.children(n)
}

// image adds a picture as a block of its own; tracking pixels and spacers
// (a side of 3 pixels or less) are dropped.
func (f *formatter) image(n *html.Node) {
	src := strings.TrimSpace(attr(n, "src"))
	w, h := size(n)
	if src == "" || (w > 0 && w <= 3) || (h > 0 && h <= 3) {
		return
	}
	p := Picture{Src: src, Alt: strings.TrimSpace(attr(n, "alt")), Link: f.link, Width: w, Height: h}
	switch {
	case strings.HasPrefix(src, "data:"):
		meta, data, ok := strings.Cut(strings.TrimPrefix(src, "data:"), ",")
		if !ok || !strings.HasPrefix(meta, "image/") || !strings.HasSuffix(meta, ";base64") {
			return
		}
		b, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return
		}
		p.Src, p.Data = "", b
	case strings.HasPrefix(src, "cid:"), p.Remote():
	default:
		return
	}
	f.flush()
	f.out = append(f.out, Block{Kind: BlockImage, Image: p, Depth: f.quote, Quoted: f.quoted})
}

// size is an image's width and height from its attributes or its style.
func size(n *html.Node) (int, int) {
	px := func(s string) int {
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "px"))
		v, err := strconv.Atoi(s)
		if err != nil {
			return 0
		}
		return v
	}
	w, h := px(attr(n, "width")), px(attr(n, "height"))
	for _, decl := range strings.Split(attr(n, "style"), ";") {
		k, v, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "width":
			if w == 0 {
				w = px(v)
			}
		case "height":
			if h == 0 {
				h = px(v)
			}
		}
	}
	return w, h
}

// hidden says whether an element is not shown: display:none, the preheader
// text newsletters hide, or the hidden attribute.
func hidden(n *html.Node) bool {
	if _, ok := attrOK(n, "hidden"); ok {
		return true
	}
	style := strings.ToLower(strings.ReplaceAll(attr(n, "style"), " ", ""))
	return strings.Contains(style, "display:none") || strings.Contains(style, "visibility:hidden") ||
		strings.Contains(style, "max-height:0") && strings.Contains(style, "overflow:hidden")
}

func attrOK(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

// quoteStart says whether an element begins the part of a reply that
// quotes earlier mail, as Gmail, Outlook, Apple Mail and Thunderbird mark
// it.
func quoteStart(n *html.Node) bool {
	class := " " + attr(n, "class") + " "
	for _, c := range []string{" gmail_quote ", " gmail_quote_container ", " moz-cite-prefix ", " yahoo_quoted ", " protonmail_quote "} {
		if strings.Contains(class, c) {
			return true
		}
	}
	id := attr(n, "id")
	if id == "divRplyFwdMsg" || id == "appendonsend" || strings.HasPrefix(id, "mail-editor-reference-message") {
		return true
	}
	_, apple := attrOK(n, "type")
	return n.DataAtom == atom.Blockquote && apple && attr(n, "type") == "cite"
}

var formatBlocks = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Table: true, atom.Tr: true, atom.Td: true, atom.Th: true,
	atom.Section: true, atom.Article: true, atom.Header: true, atom.Footer: true, atom.Center: true,
	atom.Address: true, atom.Main: true, atom.Aside: true, atom.Nav: true, atom.Figure: true,
	atom.Figcaption: true, atom.Dl: true, atom.Dt: true, atom.Dd: true, atom.Body: true, atom.Html: true,
	atom.Caption: true, atom.Tbody: true, atom.Thead: true, atom.Tfoot: true,
}

// markupEscape escapes text for Pango markup.
func markupEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;").Replace(s)
}
