package mail

import (
	"strings"
	"testing"
)

func TestPrepareWeb(t *testing.T) {
	m := &Message{
		HTML: `<html><head><meta http-equiv="refresh" content="0;url=https://evil.example"><link rel="stylesheet" href="https://t.example/a.css"><script>alert(1)</script></head>
<body onload="x()"><p>Hello <a href="javascript:alert(1)">bad</a> <a href="https://ok.example">ok</a></p>
<img src="https://img.example/a.png" width="600"><img src="https://t.example/p.gif" width="1" height="1"><img src="cid:logo">
<form action="https://x.example"><input name="pw"></form><iframe src="https://x.example"></iframe>
<div class="gmail_quote">On Monday someone wrote:</div></body></html>`,
		Attachments: []Attachment{{ContentID: "<logo>", Type: "image/png", Name: "logo.png", Data: []byte{0x89, 'P', 'N', 'G'}}},
	}
	p := PrepareWeb(m, WebOptions{})
	for _, bad := range []string{"<script", "alert(1)", "refresh", "a.css", "onload", "<form", "<iframe", "img.example", "p.gif", "cid:"} {
		if strings.Contains(p.HTML, bad) {
			t.Errorf("kept %q", bad)
		}
	}
	for _, good := range []string{"https://ok.example", "data:image/png;base64,", "default-src 'none'", "display:none"} {
		if !strings.Contains(p.HTML, good) {
			t.Errorf("lost %q", good)
		}
	}
	if p.Remote != 1 || !p.Quoted {
		t.Fatalf("remote %d quoted %v", p.Remote, p.Quoted)
	}
	p = PrepareWeb(m, WebOptions{Remote: true, Quotes: true})
	if !strings.Contains(p.HTML, "https://img.example/a.png") || strings.Contains(p.HTML, "p.gif") || strings.Contains(p.HTML, "display:none") {
		t.Fatal("remote or quotes not as asked")
	}
	if !strings.Contains(p.HTML, "img-src data: https: http:") {
		t.Fatal("the policy does not let the pictures load")
	}
}
