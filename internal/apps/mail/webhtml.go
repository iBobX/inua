package mail

import (
	"bytes"
	"encoding/base64"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// WebPage is a message's HTML as a web view shows it: safe to load, and
// what it holds back.
type WebPage struct {
	HTML   string
	Remote int  // the pictures and styles it would load from the web
	Quoted bool // it has a part quoting earlier mail (folded by Quotes)
}

// WebOptions say how a message's HTML is shown.
type WebOptions struct {
	Remote bool // load its pictures and styles from the web
	Quotes bool // show the part quoting earlier mail
}

var remoteURL = regexp.MustCompile(`(?i)url\(\s*['"]?\s*https?:`)

// PrepareWeb makes a message's HTML safe for a web view: no scripts,
// frames, forms, refreshes or event attributes; its parts' pictures
// inlined as data; nothing loaded from the network unless o.Remote (a
// content policy says so, which the web view enforces, and the pictures'
// addresses are dropped too). Links stay; the window opens them in the
// browser. A white page in the reader's type for mail that sets none.
func PrepareWeb(m *Message, o WebOptions) WebPage {
	doc, err := html.Parse(strings.NewReader(m.HTML))
	if err != nil {
		return WebPage{HTML: "<pre>" + markupEscape(m.Text) + "</pre>"}
	}
	parts := map[string]Attachment{}
	for _, a := range m.Attachments {
		if a.ContentID != "" && a.Image() {
			parts[strings.Trim(a.ContentID, "<>")] = a
		}
	}
	p := &WebPage{}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; {
			next := c.NextSibling
			if c.Type == html.ElementNode && unsafeElement(c) {
				n.RemoveChild(c)
			} else {
				if c.Type == html.ElementNode {
					clean(c, p, parts, o.Remote)
					if !p.Quoted && quoteStart(c) {
						p.Quoted = true
					}
				}
				walk(c)
			}
			c = next
		}
	}
	walk(doc)
	var b bytes.Buffer
	if err := html.Render(&b, doc); err != nil {
		return WebPage{HTML: "<pre>" + markupEscape(m.Text) + "</pre>"}
	}
	remote := "'none'"
	if o.Remote {
		remote = "https: http:"
	}
	head := `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; ` +
		`img-src data: ` + strings.TrimPrefix(remote, "'none'") + `; ` +
		`style-src 'unsafe-inline' ` + strings.TrimPrefix(remote, "'none'") + `; ` +
		`font-src data: ` + strings.TrimPrefix(remote, "'none'") + `; form-action 'none'; base-uri 'none'">` +
		`<style>` + webBase
	if !o.Quotes && p.Quoted {
		head += webFolded
	}
	head += `</style>`
	p.HTML = head + b.String()
	return *p
}

// webBase is the page under the sender's styles: white, the reader's type,
// pictures no wider than the column, long words broken.
const webBase = `html{background:#fff;color:#1d1f23}` +
	`body{margin:18px 20px;font:14px/1.45 "IBM Plex Sans","Noto Sans","Noto Sans CJK SC",sans-serif;overflow-wrap:anywhere}` +
	`img{max-width:100%;height:auto}` +
	`table{max-width:100%}` +
	`pre{white-space:pre-wrap}` +
	`blockquote{margin:0 0 0 4px;padding-left:12px;border-left:2px solid #d5d8de;color:#555}`

// webFolded hides the part of a reply quoting earlier mail, as Gmail,
// Outlook, Apple Mail and Thunderbird mark it.
const webFolded = `.gmail_quote,.gmail_quote_container,.yahoo_quoted,.moz-cite-prefix,.protonmail_quote,` +
	`blockquote[type=cite],#divRplyFwdMsg,#divRplyFwdMsg~*,#appendonsend~*{display:none!important}`

// unsafeElement says whether an element goes with all it holds.
func unsafeElement(n *html.Node) bool {
	switch n.DataAtom {
	case atom.Script, atom.Noscript, atom.Iframe, atom.Frame, atom.Frameset, atom.Object, atom.Embed,
		atom.Applet, atom.Form, atom.Input, atom.Button, atom.Select, atom.Textarea, atom.Base, atom.Template:
		return true
	case atom.Meta:
		_, equiv := attrOK(n, "http-equiv")
		return equiv // refreshes, the sender's own policies
	case atom.Link:
		return true // style sheets from the web: the sender's <style> stays
	}
	return false
}

// clean drops an element's event attributes and script links, inlines
// its parts' pictures, and counts (and, unless remote, drops) what it
// would load from the web.
func clean(n *html.Node, p *WebPage, parts map[string]Attachment, remote bool) {
	out := n.Attr[:0]
	for _, a := range n.Attr {
		key := strings.ToLower(a.Key)
		val := strings.TrimSpace(a.Val)
		low := strings.ToLower(val)
		switch {
		case strings.HasPrefix(key, "on"):
			continue
		case (key == "href" || key == "src" || key == "action") && strings.HasPrefix(low, "javascript:"):
			continue
		case key == "src" && strings.HasPrefix(low, "cid:"):
			if part, ok := parts[strings.Trim(val[4:], "<>")]; ok {
				a.Val = "data:" + part.Type + ";base64," + base64.StdEncoding.EncodeToString(part.Data)
			} else {
				continue
			}
		case (key == "src" || key == "background" || key == "srcset") && (strings.HasPrefix(low, "http:") || strings.HasPrefix(low, "https:")):
			if w, h := size(n); (w > 0 && w <= 3) || (h > 0 && h <= 3) {
				continue // a tracking pixel: never loaded
			}
			p.Remote++
			if !remote {
				continue
			}
		case key == "style" && remoteURL.MatchString(val):
			p.Remote++
		}
		out = append(out, a)
	}
	n.Attr = out
	if n.DataAtom == atom.Style && n.FirstChild != nil && remoteURL.MatchString(n.FirstChild.Data) {
		p.Remote++
	}
}
