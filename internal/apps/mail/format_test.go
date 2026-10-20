package mail

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"testing"
)

func TestFormat(t *testing.T) {
	bs := Format(`<html><head><style>p{}</style></head><body>
<div style="display:none">preheader</div>
<h2>Hello <b>there</b></h2>
<p>Read <a href="https://example.com/x?a=1&b=2">the <i>post</i></a> &amp; more.</p>
<ul><li>one</li><li>two <strong>bold</strong></li></ul>
<ol start="3"><li>three</li></ol>
<img src="https://t.example/p.gif" width="1" height="1">
<a href="https://shop.example"><img src="https://img.example/a.png" alt="A" width="300"></a>
<img src="cid:logo@x">
<pre>a  b
c</pre>
<hr>
<div class="gmail_quote">On Monday someone wrote:<blockquote>old</blockquote></div>
</body></html>`)
	var got []string
	for _, b := range bs {
		switch b.Kind {
		case BlockImage:
			got = append(got, "img:"+b.Image.Src+"|"+b.Image.Link)
		case BlockRule:
			got = append(got, "hr")
		default:
			s := b.Markup
			if b.Mark != "" {
				s = b.Mark + " " + s
			}
			if b.Quoted {
				s = "q:" + s
			}
			got = append(got, s)
		}
	}
	want := []string{
		"Hello <b>there</b>",
		`Read <a href="https://example.com/x?a=1&amp;b=2">the <i>post</i></a> &amp; more.`,
		"• one",
		"• two <b>bold</b>",
		"3. three",
		"img:https://img.example/a.png|https://shop.example",
		"img:cid:logo@x|",
		"a  b\nc",
		"hr",
		"q:On Monday someone wrote:",
		"q:old",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if bs[0].Kind != BlockHeading || bs[0].Level != 2 {
		t.Fatalf("heading: %+v", bs[0])
	}
}

func TestFormatInlineAcrossBlocks(t *testing.T) {
	// A <b> around block elements stays balanced in each block.
	bs := Format(`<b><p>one</p><p>two</p></b>`)
	if len(bs) != 2 || bs[0].Markup != "<b>one</b>" || bs[1].Markup != "<b>two</b>" {
		t.Fatalf("%+v", bs)
	}
}

func TestScalePicture(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 400, 200))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	got, err := ScalePicture(buf.Bytes(), 100)
	if err != nil || got.Bounds().Dx() != 100 || got.Bounds().Dy() != 50 {
		t.Fatalf("%v %v", got.Bounds(), err)
	}
	got, _ = ScalePicture(buf.Bytes(), 1000) // never larger
	if got.Bounds().Dx() != 400 {
		t.Fatalf("%v", got.Bounds())
	}
}
