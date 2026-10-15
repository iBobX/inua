package viewer

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Markdown is read with goldmark (CommonMark and GitHub's tables, task
// lists, strikethrough and bare links) into blocks for the window to lay
// out as a text view: no HTML, no browser.

// Style is how a run of text is set.
type Style uint8

const (
	Strong Style = 1 << iota
	Emph
	Code
	Strike
	Linked
)

// Span is a run of text in one style, a link, or an image.
type Span struct {
	Text  string
	Style Style
	Link  string // a link's destination, as written
	Image string // an image's source, as written; Text is what it shows
}

// BlockKind is what a block is.
type BlockKind int

const (
	Para BlockKind = iota
	Heading
	CodeBlock
	Rule
	Table
)

// Block is a paragraph, heading, code block, rule or table, with where
// it stands: in how many lists and quotes.
type Block struct {
	Kind   BlockKind
	Level  int    // a heading's level, 1 to 6
	Anchor string // a heading's id, for "#…" links
	Spans  []Span
	Code   string // a code block's text
	Lang   string
	Rows   [][][]Span // a table's rows of cells, the header first

	Depth  int    // how many lists it is in
	Quote  int    // how many quotes it is in
	Marker string // a list item's first block: "•", "3.", "☐" …
	Tight  bool   // in a tight list: no room after it
}

var md = goldmark.New(goldmark.WithExtensions(extension.GFM))

// ParseMarkdown reads Markdown into blocks. YAML front matter becomes a
// code block.
func ParseMarkdown(src string) []Block {
	var blocks []Block
	if front, rest, ok := frontMatter(src); ok {
		blocks = append(blocks, Block{Kind: CodeBlock, Code: front, Lang: "yaml"})
		src = rest
	}
	source := []byte(src)
	r := &mdReader{src: source, anchors: map[string]int{}}
	r.blocks = blocks
	r.walk(md.Parser().Parse(text.NewReader(source)), mdPlace{})
	return r.blocks
}

// frontMatter splits off a YAML header between "---" lines.
func frontMatter(src string) (front, rest string, ok bool) {
	s := strings.TrimPrefix(src, "\ufeff")
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return "", src, false
	}
	body := s[strings.Index(s, "\n")+1:]
	for i := 0; i < len(body); {
		end := strings.IndexByte(body[i:], '\n')
		line := body[i:]
		if end >= 0 {
			line = body[i : i+end]
		}
		if t := strings.TrimRight(line, "\r"); t == "---" || t == "..." {
			if end < 0 {
				return strings.TrimRight(body[:i], "\n"), "", true
			}
			return strings.TrimRight(body[:i], "\n"), body[i+end+1:], true
		}
		if end < 0 {
			break
		}
		i += end + 1
	}
	return "", src, false
}

type mdReader struct {
	src     []byte
	blocks  []Block
	anchors map[string]int // ids given, for numbering repeats
}

// mdPlace is where a block stands while the tree is walked.
type mdPlace struct {
	depth, quote int
	marker       *string // the list item's marker, until its first block takes it
	tight        bool
}

func (r *mdReader) add(b Block, at mdPlace) {
	b.Depth, b.Quote, b.Tight = at.depth, at.quote, at.tight
	if at.marker != nil && *at.marker != "" {
		b.Marker, *at.marker = *at.marker, ""
	}
	r.blocks = append(r.blocks, b)
}

func (r *mdReader) walk(n ast.Node, at mdPlace) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *ast.Paragraph, *ast.TextBlock:
			spans, check := r.inlines(c)
			if check != "" && at.marker != nil {
				*at.marker = check
			}
			if len(spans) > 0 {
				r.add(Block{Kind: Para, Spans: spans}, at)
			}
		case *ast.Heading:
			spans, _ := r.inlines(c)
			r.add(Block{Kind: Heading, Level: c.Level, Spans: spans, Anchor: r.anchor(spans)}, at)
		case *ast.FencedCodeBlock:
			r.add(Block{Kind: CodeBlock, Code: r.lines(c), Lang: string(c.Language(r.src))}, at)
		case *ast.CodeBlock:
			r.add(Block{Kind: CodeBlock, Code: r.lines(c)}, at)
		case *ast.ThematicBreak:
			r.add(Block{Kind: Rule}, at)
		case *ast.Blockquote:
			in := at
			in.quote++
			r.walk(c, in)
		case *ast.List:
			n := c.Start
			for item := c.FirstChild(); item != nil; item = item.NextSibling() {
				marker := []string{"•", "◦", "▪"}[min(at.depth, 2)]
				if c.IsOrdered() {
					marker = fmt.Sprintf("%d.", n)
					n++
				}
				in := mdPlace{depth: at.depth + 1, quote: at.quote, marker: &marker, tight: c.IsTight}
				r.walk(item, in)
				if marker != "" { // an empty item
					r.add(Block{Kind: Para}, in)
				}
			}
		case *ast.HTMLBlock:
			// Raw HTML is left out but for its pictures, as READMEs put
			// their logos and badges.
			var spans []Span
			for _, m := range htmlImage.FindAllStringSubmatch(r.lines(c), -1) {
				alt := ""
				if a := htmlAlt.FindStringSubmatch(m[0]); a != nil {
					alt = a[1]
				}
				spans = append(spans, Span{Text: alt, Image: m[1]})
			}
			if len(spans) > 0 {
				r.add(Block{Kind: Para, Spans: spans}, at)
			}
		case *east.Table:
			var rows [][][]Span
			for row := c.FirstChild(); row != nil; row = row.NextSibling() {
				var cells [][]Span
				for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
					spans, _ := r.inlines(cell)
					cells = append(cells, spans)
				}
				rows = append(rows, cells)
			}
			r.add(Block{Kind: Table, Rows: rows}, at)
		default:
			r.walk(c, at)
		}
	}
}

var (
	htmlImage = regexp.MustCompile(`(?is)<img\b[^>]*?\bsrc\s*=\s*["']([^"']+)["'][^>]*>`)
	htmlAlt   = regexp.MustCompile(`(?is)\balt\s*=\s*["']([^"']*)["']`)
)

// lines is a block's text, line by line.
func (r *mdReader) lines(n ast.Node) string {
	var b bytes.Buffer
	l := n.Lines()
	for i := 0; i < l.Len(); i++ {
		seg := l.At(i)
		b.Write(seg.Value(r.src))
	}
	return strings.TrimRight(b.String(), "\n")
}

// inlines are a block's runs of text; a task list item's box comes back
// as its marker.
func (r *mdReader) inlines(n ast.Node) (spans []Span, check string) {
	var walk func(n ast.Node, style Style, link string)
	put := func(s string, style Style, link string) {
		if s == "" {
			return
		}
		if k := len(spans) - 1; k >= 0 && spans[k].Style == style && spans[k].Link == link && spans[k].Image == "" {
			spans[k].Text += s
			return
		}
		spans = append(spans, Span{Text: s, Style: style, Link: link})
	}
	walk = func(n ast.Node, style Style, link string) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch c := c.(type) {
			case *ast.Text:
				put(string(c.Value(r.src)), style, link)
				switch {
				case c.HardLineBreak():
					put("\n", style, link)
				case c.SoftLineBreak():
					put(" ", style, link)
				}
			case *ast.String:
				put(string(c.Value), style, link)
			case *ast.CodeSpan:
				var b strings.Builder
				for t := c.FirstChild(); t != nil; t = t.NextSibling() {
					if t, ok := t.(*ast.Text); ok {
						b.Write(t.Value(r.src))
					}
				}
				put(b.String(), style|Code, link)
			case *ast.Emphasis:
				if c.Level >= 2 {
					walk(c, style|Strong, link)
				} else {
					walk(c, style|Emph, link)
				}
			case *east.Strikethrough:
				walk(c, style|Strike, link)
			case *ast.Link:
				walk(c, style|Linked, string(c.Destination))
			case *ast.AutoLink:
				url := string(c.URL(r.src))
				put(string(c.Label(r.src)), style|Linked, url)
			case *ast.Image:
				var alt strings.Builder
				for t := c.FirstChild(); t != nil; t = t.NextSibling() {
					if t, ok := t.(*ast.Text); ok {
						alt.Write(t.Value(r.src))
					}
				}
				spans = append(spans, Span{Text: alt.String(), Style: style, Link: link, Image: string(c.Destination)})
			case *east.TaskCheckBox:
				check = "☐"
				if c.IsChecked {
					check = "☑"
				}
			case *ast.RawHTML:
				var b strings.Builder
				for i := 0; i < c.Segments.Len(); i++ {
					seg := c.Segments.At(i)
					b.Write(seg.Value(r.src))
				}
				if br.MatchString(b.String()) {
					put("\n", style, link)
				}
			default:
				walk(c, style, link)
			}
		}
	}
	walk(n, 0, "")
	return spans, check
}

var br = regexp.MustCompile(`(?i)^<br\s*/?>$`)

// anchor is a heading's id as GitHub makes it: lower case, spaces as
// hyphens, punctuation left out, and repeats numbered.
func (r *mdReader) anchor(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		for _, c := range strings.ToLower(s.Text) {
			switch {
			case unicode.IsLetter(c) || unicode.IsDigit(c) || c == '-' || c == '_':
				b.WriteRune(c)
			case c == ' ':
				b.WriteByte('-')
			}
		}
	}
	id := b.String()
	n := r.anchors[id]
	r.anchors[id] = n + 1
	if n > 0 {
		id = fmt.Sprintf("%s-%d", id, n)
	}
	return id
}

// PlainText is spans' text alone.
func PlainText(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}
