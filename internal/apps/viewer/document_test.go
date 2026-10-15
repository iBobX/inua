package viewer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestKind(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for name, want := range map[string]Kind{
		"a.PNG": Image, "b.pdf": PDF, "c.md": Markdown, "d.markdown": Markdown,
		"e.go": Text, "f.TXT": Text, "g.exe": None, "README": None,
	} {
		if got := KindOf(name); got != want {
			t.Errorf("KindOf(%s) = %v, want %v", name, got, want)
		}
	}
	if k := Sniff(write("README", "Read me.\n")); k != Text {
		t.Errorf("README sniffed as %v", k)
	}
	if k := Sniff(write("LICENSE", "© 2026 "+strings.Repeat("ü", 3000))); k != Text {
		t.Errorf("long UTF-8 sniffed as %v", k) // the 4 KB read cuts a character
	}
	if k := Sniff(write("blob", "ELF\x00\x01")); k != None {
		t.Errorf("binary sniffed as %v", k)
	}
	if k := Sniff(write("latin1", "caf\xe9")); k != None {
		t.Errorf("Latin-1 sniffed as %v", k)
	}
	if m := Mime("x.md"); m != "text/markdown" {
		t.Errorf("Mime(x.md) = %q", m)
	}
	if m := Mime("x.go"); m != "text/plain" {
		t.Errorf("Mime(x.go) = %q", m)
	}
}

func TestListByKind(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"b.pdf", "a.pdf", "c.png", "notes.md", "todo.md", "README"} {
		os.WriteFile(filepath.Join(dir, n), []byte("text"), 0o644)
	}
	base := func(fs []string) (out []string) {
		for _, f := range fs {
			out = append(out, filepath.Base(f))
		}
		return out
	}
	files, i := List([]string{filepath.Join(dir, "b.pdf")})
	if got := base(files); !slices.Equal(got, []string{"a.pdf", "b.pdf"}) || i != 1 {
		t.Errorf("a PDF: %v at %d", got, i)
	}
	files, i = List([]string{filepath.Join(dir, "todo.md")})
	if got := base(files); !slices.Equal(got, []string{"notes.md", "todo.md"}) || i != 1 {
		t.Errorf("Markdown: %v at %d", got, i)
	}
	files, _ = List([]string{filepath.Join(dir, "README")})
	if got := base(files); !slices.Equal(got, []string{"README"}) {
		t.Errorf("README alone: %v", got)
	}
	files, _ = List([]string{dir})
	if got := base(files); !slices.Equal(got, []string{"c.png"}) {
		t.Errorf("a folder shows its images: %v", got)
	}
}

func TestReadText(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	os.WriteFile(p, []byte("\xef\xbb\xbfone two\nthree\n"), 0o644)
	s, err := ReadText(p)
	if err != nil || s != "one two\nthree\n" {
		t.Fatalf("ReadText = %q, %v", s, err)
	}
	if l, w, c := TextStats(s); l != 2 || w != 3 || c != 14 {
		t.Errorf("TextStats = %d lines, %d words, %d chars", l, w, c)
	}
	if l, _, _ := TextStats("no newline"); l != 1 {
		t.Errorf("a last line without a newline: %d lines", l)
	}
	os.WriteFile(p, []byte{'a', 0, 'b'}, 0o644)
	if _, err := ReadText(p); !errors.Is(err, ErrNotText) {
		t.Errorf("binary: %v", err)
	}
}

func TestParseMarkdown(t *testing.T) {
	src := `---
title: Notes
---
# Hello *world*

Some **bold**, ` + "`code`" + `, ~~gone~~ and [a link](other.md#top).
A soft break.

## Hello world

- one
- [x] done
  1. inner
- [ ] open

> quoted

` + "```go\nfmt.Println()\n```" + `

---

| a | b |
|---|---|
| 1 | **2** |

![logo](img/logo.png)

<p align="center"><img src="banner.svg" alt="Banner"></p>
`
	blocks := ParseMarkdown(src)
	var got []string
	for _, b := range blocks {
		s := fmt.Sprintf("%d d%d q%d %q", b.Kind, b.Depth, b.Quote, b.Marker)
		switch b.Kind {
		case Heading:
			s += fmt.Sprintf(" h%d #%s %q", b.Level, b.Anchor, PlainText(b.Spans))
		case CodeBlock:
			s += fmt.Sprintf(" %s %q", b.Lang, b.Code)
		case Table:
			s += fmt.Sprintf(" %dx%d %q", len(b.Rows), len(b.Rows[0]), PlainText(b.Rows[1][1]))
		default:
			s += fmt.Sprintf(" %q", PlainText(b.Spans))
		}
		got = append(got, s)
	}
	want := []string{
		`2 d0 q0 "" yaml "title: Notes"`,
		`1 d0 q0 "" h1 #hello-world "Hello world"`,
		`0 d0 q0 "" "Some bold, code, gone and a link. A soft break."`, // the soft break a space
		`1 d0 q0 "" h2 #hello-world-1 "Hello world"`,
		`0 d1 q0 "•" "one"`,
		`0 d1 q0 "☑" "done"`,
		`0 d2 q0 "1." "inner"`,
		`0 d1 q0 "☐" "open"`,
		`0 d0 q1 "" "quoted"`,
		`2 d0 q0 "" go "fmt.Println()"`,
		`3 d0 q0 "" ""`,
		`4 d0 q0 "" 2x2 "2"`,
		`0 d0 q0 "" "logo"`,
		`0 d0 q0 "" "Banner"`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("blocks:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	para := blocks[2].Spans
	styles := map[string]Style{}
	for _, s := range para {
		styles[s.Text] = s.Style
		if s.Style&Linked != 0 && s.Link != "other.md#top" {
			t.Errorf("link to %q", s.Link)
		}
	}
	if styles["bold"] != Strong || styles["code"] != Code || styles["gone"] != Strike || styles["a link"] != Linked {
		t.Errorf("styles: %v", styles)
	}
	if img := blocks[12].Spans[0]; img.Image != "img/logo.png" {
		t.Errorf("image: %+v", img)
	}
	if img := blocks[13].Spans[0]; img.Image != "banner.svg" {
		t.Errorf("HTML image: %+v", img)
	}
}

func TestFrontMatterUnclosed(t *testing.T) {
	blocks := ParseMarkdown("---\nnot front matter\n")
	for _, b := range blocks {
		if b.Kind == CodeBlock {
			t.Errorf("unclosed front matter read as a header: %+v", b)
		}
	}
}

// testPDF writes a PDF of two pages: 200×100 points with its left half
// black and a line of text, and 300×400 points blank.
func testPDF(t *testing.T) string {
	content := "0 0 0 rg 0 0 100 100 re f BT /F1 12 Tf 110 50 Td (Hello PDF) Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 400] >>",
		"<< /Title (Test Paper) /Author (Inua) >>",
	}
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	var offsets []int
	for i, o := range objs {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R /Info 7 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	p := filepath.Join(t.TempDir(), "test paper.pdf")
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPDF(t *testing.T) {
	if err := PDFAvailable(); err != nil {
		t.Skip(err)
	}
	d, err := OpenPDF(testPDF(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if d.Pages() != 2 {
		t.Fatalf("%d pages", d.Pages())
	}
	if w, h := d.PageSize(1); w != 300 || h != 400 {
		t.Errorf("page 2 is %v×%v", w, h)
	}
	if info := d.Info(); info.Title != "Test Paper" || info.Author != "Inua" {
		t.Errorf("info: %+v", info)
	}
	if s := d.PageText(0); !strings.Contains(s, "Hello PDF") {
		t.Errorf("page text %q", s)
	}
	px, scale, err := d.Render(0, 2, GoAllocator{})
	if err != nil {
		t.Fatal(err)
	}
	if px.W != 400 || px.H != 200 || scale != 2 || px.Format != BGRAPremult {
		t.Fatalf("rendered %dx%d at %v", px.W, px.H, scale)
	}
	at := func(x, y int) []byte { return px.Buf[y*px.Stride+x*4 : y*px.Stride+x*4+4] }
	if p := at(50, 100); p[0] != 0 || p[1] != 0 || p[2] != 0 || p[3] != 255 {
		t.Errorf("the black half: %v", p)
	}
	if p := at(390, 190); p[0] != 255 || p[1] != 255 || p[2] != 255 || p[3] != 255 {
		t.Errorf("the white paper: %v", p)
	}
	// A huge scale is brought down.
	if px, scale, err := d.Render(1, 1000, GoAllocator{}); err != nil || float64(px.W*px.H) > maxPagePixels*1.01 || scale >= 1000 {
		t.Errorf("huge: %dx%d at %v, %v", px.W, px.H, scale, err)
	}
	if name := PaperName(595.28, 841.89); name != "A4" {
		t.Errorf("A4 is %q", name)
	}
	if name := PaperName(792, 612); name != "Letter" {
		t.Errorf("Letter on its side is %q", name)
	}
}

func TestPDFErrors(t *testing.T) {
	if err := PDFAvailable(); err != nil {
		t.Skip(err)
	}
	p := filepath.Join(t.TempDir(), "broken.pdf")
	os.WriteFile(p, []byte("not a pdf"), 0o644)
	if _, err := OpenPDF(p); err == nil {
		t.Error("a broken PDF opened")
	}
	if _, err := OpenPDF(filepath.Join(t.TempDir(), "missing.pdf")); err == nil {
		t.Error("a missing PDF opened")
	}
}
