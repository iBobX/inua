package host

import (
	"encoding/json"
	"net"
	"testing"
	"time"
)

// fakePage is a web page on the host's socket, as inua-termd serve's
// bridge connects one: its control messages come to msgs.
type fakePage struct {
	conn net.Conn
	msgs chan Message
}

func openPage(t *testing.T, path string) *fakePage {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := writeMessage(conn, Message{Type: MsgHello, Version: Version, Web: true}); err != nil {
		t.Fatal(err)
	}
	p := &fakePage{conn: conn, msgs: make(chan Message, 16)}
	go func() {
		for {
			kind, payload, err := ReadFrame(conn)
			if err != nil {
				close(p.msgs)
				return
			}
			var m Message
			if kind == FrameControl && json.Unmarshal(payload, &m) == nil && m.Type != MsgWelcome {
				p.msgs <- m
			}
		}
	}()
	return p
}

// next is the page's next message of type kind.
func (p *fakePage) next(t *testing.T, kind string) Message {
	t.Helper()
	for {
		select {
		case m, ok := <-p.msgs:
			if !ok {
				t.Fatalf("the page's connection ended waiting for %s", kind)
			}
			if m.Type == kind {
				return m
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no %s came to the page", kind)
		}
	}
}

func (p *fakePage) answer(t *testing.T, asked int, answer, pin string) {
	t.Helper()
	m := Message{Type: MsgAnswer, ID: 99, Asked: asked, Answer: answer}
	if pin != "" {
		m.Pin = &pin
	}
	if err := writeMessage(p.conn, m); err != nil {
		t.Fatal(err)
	}
}

type asking struct {
	answer, pin string
	err         error
}

func ask(c *Client, s Secret) chan asking {
	done := make(chan asking, 1)
	go func() {
		a, pin, err := c.AskSecret(s)
		done <- asking{a, pin, err}
	}()
	return done
}

func wait(t *testing.T, done chan asking) asking {
	t.Helper()
	select {
	case a := <-done:
		return a
	case <-time.After(5 * time.Second):
		t.Fatal("no answer came back")
		return asking{}
	}
}

// With no page open, a secret is answered at once with none: the
// desktop's dialog alone asks.
func TestSecretNoPage(t *testing.T) {
	_, path := startHost(t)
	dial(t, path, newWatcher()) // a window: not a page
	asker := dial(t, path, newWatcher())
	if a := wait(t, ask(asker, Secret{Mode: "getpin"})); a.answer != "" || a.err != nil {
		t.Errorf("no page: %+v", a)
	}
}

// Every page is asked, one that comes later too; the first answer goes
// back with the passphrase, and the others are told it is answered. A
// window cannot answer.
func TestSecretAnswered(t *testing.T) {
	_, path := startHost(t)
	page := openPage(t, path)
	window := dial(t, path, newWatcher())
	asker := dial(t, path, newWatcher())
	time.Sleep(50 * time.Millisecond) // the page is counted in
	done := ask(asker, Secret{Mode: "getpin", Title: "ansible-playbook asks for your passphrase", Desc: "Please enter the passphrase"})
	m := page.next(t, MsgSecret)
	if m.Secret == nil || m.Secret.ID == 0 || m.Secret.Title != "ansible-playbook asks for your passphrase" {
		t.Fatalf("the page got %+v", m.Secret)
	}
	late := openPage(t, path)
	if got := late.next(t, MsgSecret); got.Secret == nil || got.Secret.ID != m.Secret.ID {
		t.Fatalf("a page opened later got %+v", got.Secret)
	}
	// The window's answer is turned away.
	if err := writeMessage(window.conn, Message{Type: MsgAnswer, ID: 1, Asked: m.Secret.ID, Answer: AnswerOK}); err != nil {
		t.Fatal(err)
	}
	select {
	case a := <-done:
		t.Fatalf("a window answered: %+v", a)
	case <-time.After(200 * time.Millisecond):
	}
	page.answer(t, m.Secret.ID, AnswerOK, "correct horse")
	if a := wait(t, done); a.answer != AnswerOK || a.pin != "correct horse" || a.err != nil {
		t.Errorf("answered %+v", a)
	}
	if w := late.next(t, MsgWithdraw); w.Asked != m.Secret.ID {
		t.Errorf("the other page was told %d", w.Asked)
	}
}

// A confirm's No and a Cancel come back as such.
func TestSecretRefused(t *testing.T) {
	_, path := startHost(t)
	page := openPage(t, path)
	asker := dial(t, path, newWatcher())
	time.Sleep(50 * time.Millisecond)
	for _, answer := range []string{AnswerNo, AnswerCancel} {
		done := ask(asker, Secret{Mode: "confirm"})
		m := page.next(t, MsgSecret)
		page.answer(t, m.Secret.ID, answer, "")
		if a := wait(t, done); a.answer != answer || a.pin != "" {
			t.Errorf("%s came back as %+v", answer, a)
		}
	}
}

// The asker gone (the desktop's dialog answered, gpg gave up), the pages
// are told the question is withdrawn.
func TestSecretWithdrawn(t *testing.T) {
	_, path := startHost(t)
	page := openPage(t, path)
	asker := dial(t, path, newWatcher())
	time.Sleep(50 * time.Millisecond)
	ask(asker, Secret{Mode: "getpin"})
	m := page.next(t, MsgSecret)
	asker.Close()
	if w := page.next(t, MsgWithdraw); w.Asked != m.Secret.ID {
		t.Errorf("withdrawn %d, want %d", w.Asked, m.Secret.ID)
	}
}
