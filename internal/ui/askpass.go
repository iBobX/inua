package ui

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/pinentry"
)

// Asking for a passphrase for gpg (inua-pinentry), in the polkit dialog's
// card: `call password getpin|confirm|message` with the request as JSON.
// The answer is the reply: the passphrase as its result, or the error
// "cancelled" (or "not ok", the third button of a question). The shell
// keeps nothing; the reply goes back over the user's own socket.

type passPrompts struct {
	slot chan struct{} // held by the dialog on screen
	cur  *passDialog
}

type passDialog struct {
	q    pinentry.Request
	done chan ipc.Response

	win                          gtk.Window
	title, message, prompt, info gtk.Label
	password, password2          gtk.PasswordEntry
	refs                         gtkx.Scope
	closed                       bool
}

// askPassword answers a `password` request, waiting for the user: it runs
// on the request's goroutine, the dialog on the main loop.
func (s *shellUI) askPassword(req ipc.Request) ipc.Response {
	var q pinentry.Request
	if len(req.Args) != 1 || json.Unmarshal([]byte(req.Args[0]), &q) != nil {
		return ipc.Response{Error: i18n.T("password: want one JSON request")}
	}
	q.Mode = pinentry.Mode(req.Function)
	switch q.Mode {
	case pinentry.GetPin, pinentry.Confirm, pinentry.Message:
	default:
		return ipc.Response{Error: "password: unknown mode " + req.Function}
	}
	p := &s.pass
	p.slot <- struct{}{} // one at a time
	defer func() { <-p.slot }()
	d := &passDialog{q: q, done: make(chan ipc.Response, 1)}
	gtkx.Post(func() { s.openPass(d) })
	if q.Timeout > 0 {
		select {
		case r := <-d.done:
			return r
		case <-time.After(time.Duration(q.Timeout) * time.Second):
			gtkx.Post(func() { s.finishPass(d, ipc.Response{Error: "cancelled"}) })
		}
	}
	return <-d.done
}

func (s *shellUI) bindPassHandlers() {
	later := func(f func()) { gtkx.Post(f) } // the dialog sent the signal
	s.onPassOK = func(gtk.Button) { later(s.answerPass) }
	s.onPassActivate = func(e gtk.PasswordEntry) {
		d := s.pass.cur
		if d != nil && d.q.Repeat != "" && e.GoPointer() == d.password.GoPointer() {
			d.password2.GrabFocus() // then the repeat
			return
		}
		later(s.answerPass)
	}
	s.onPassCancel = func(gtk.Button) {
		if d := s.pass.cur; d != nil {
			later(func() { s.finishPass(d, ipc.Response{Error: "cancelled"}) })
		}
	}
	s.onPassNotOK = func(gtk.Button) {
		if d := s.pass.cur; d != nil {
			later(func() { s.finishPass(d, ipc.Response{Error: i18n.T("not ok")}) })
		}
	}
	s.onPassKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		d := s.pass.cur
		if keyval != keyEscape || d == nil {
			return false
		}
		later(func() { s.finishPass(d, ipc.Response{Error: "cancelled"}) })
		return true
	}
}

func (s *shellUI) openPass(d *passDialog) {
	q := d.q
	var icon gtk.Image
	var command gtk.Label
	var who gtk.Box
	var ok, cancel, notok gtk.Button
	ui := s.load("polkit.ui", &d.refs)
	ui.Get("polkit_dialog", &d.win)
	ui.Get("icon", &icon)
	ui.Get("title", &d.title)
	ui.Get("message", &d.message)
	ui.Get("command", &command)
	ui.Get("who", &who)
	ui.Get("prompt", &d.prompt)
	ui.Get("password", &d.password)
	ui.Get("password2", &d.password2)
	ui.Get("info", &d.info)
	ui.Get("ok", &ok)
	ui.Get("cancel", &cancel)
	ui.Get("notok", &notok)
	ui.Close()

	who.SetVisible(false)
	// Who wants the key (inua-pinentry tells): the app, or the processes.
	command.SetText(i18n.Tf("Asked by %s", q.Requester))
	command.SetSelectable(false) // a selectable label opens selected
	command.SetVisible(q.Requester != "")
	title := q.Title
	if title == "" || strings.EqualFold(title, "pinentry") {
		title = map[pinentry.Mode]string{pinentry.GetPin: i18n.T("Passphrase Required"), pinentry.Confirm: i18n.T("Confirm"), pinentry.Message: i18n.T("Message")}[q.Mode]
	}
	d.title.SetText(title)
	d.message.SetText(q.Desc)
	d.message.SetVisible(q.Desc != "")
	getpin := q.Mode == pinentry.GetPin
	if !getpin {
		icon.SetFromIconName("dialog-information-symbolic")
	}
	// gpg's prompt ("Passphrase:", "PIN:") goes in the field, as polkit's
	// "Password" does; a longer one stays above it.
	prompt := strings.TrimSuffix(strings.TrimSpace(q.Prompt), ":")
	if prompt == "" {
		prompt = i18n.T("Passphrase")
	}
	if len(prompt) <= 24 {
		var v gobject.Value
		v.Init(gobject.TypeStringVal)
		v.SetString(prompt)
		d.password.SetProperty("placeholder-text", &v)
		v.Unset()
		if q.Repeat != "" {
			v.Init(gobject.TypeStringVal)
			v.SetString(strings.TrimSuffix(strings.TrimSpace(q.Repeat), ":"))
			d.password2.SetProperty("placeholder-text", &v)
			v.Unset()
		}
	} else {
		d.prompt.SetText(strings.TrimSpace(q.Prompt))
		d.prompt.SetVisible(getpin)
	}
	d.password.SetVisible(getpin)
	d.password2.SetVisible(getpin && q.Repeat != "")
	s.showPassInfo(d, q.Error, true)

	ok.SetLabel(labelOr(q.OK, i18n.T("OK")))
	cancel.SetLabel(labelOr(q.Cancel, i18n.T("Cancel")))
	cancel.SetVisible(q.Mode != pinentry.Message)
	notok.SetLabel(q.NotOK)
	notok.SetVisible(q.Mode == pinentry.Confirm && q.NotOK != "")
	ok.ConnectClicked(&s.onPassOK)
	cancel.ConnectClicked(&s.onPassCancel)
	notok.ConnectClicked(&s.onPassNotOK)
	d.password.ConnectActivate(&s.onPassActivate)
	d.password2.ConnectActivate(&s.onPassActivate)
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(&s.onPassKey)
	d.win.AddController(&keys.EventController)

	d.win.SetApplication(s.app)
	w := d.win.GoPointer()
	if s.layer {
		layershell.InitForWindow(w)
		layershell.SetNamespace(w, "inua-polkit")
		layershell.SetLayer(w, layershell.LayerOverlay)
		layershell.SetKeyboardMode(w, layershell.KeyboardExclusive)
		for _, e := range []layershell.Edge{layershell.EdgeTop, layershell.EdgeBottom, layershell.EdgeLeft, layershell.EdgeRight} {
			layershell.SetAnchor(w, e, true)
		}
		layershell.SetExclusiveZone(w, -1)
		if mon := s.monitorByName(s.snap.FocusedMonitor); mon != nil {
			layershell.SetMonitor(w, mon.GoPointer())
		}
	} else {
		d.win.SetTitle(title)
	}
	s.pass.cur = d
	d.win.Present()
	if getpin {
		d.password.GrabFocus()
	} else {
		ok.GrabFocus()
	}
	log.Printf("pinentry: asking (%s)", q.Mode)
}

func labelOr(s, def string) string {
	if s = strings.TrimSpace(s); s != "" {
		return s
	}
	return def
}

func (s *shellUI) showPassInfo(d *passDialog, text string, bad bool) {
	d.info.SetText(text)
	d.info.SetVisible(text != "")
	toggle(&d.info.Widget, "error", bad)
}

// answerPass gives the agent what was typed, or OK.
func (s *shellUI) answerPass() {
	d := s.pass.cur
	if d == nil || d.closed {
		return
	}
	if d.q.Mode != pinentry.GetPin {
		s.finishPass(d, ipc.Response{OK: true})
		return
	}
	pin := d.password.GetText()
	if d.q.Repeat != "" && pin != d.password2.GetText() {
		d.password.SetText("")
		d.password2.SetText("")
		s.showPassInfo(d, labelOr(d.q.NoMatch, i18n.T("The passphrases do not match.")), true)
		d.password.GrabFocus()
		return
	}
	s.finishPass(d, ipc.Response{OK: true, Result: pin})
}

func (s *shellUI) finishPass(d *passDialog, r ipc.Response) {
	if d.closed {
		return
	}
	d.closed = true
	if s.pass.cur == d {
		s.pass.cur = nil
	}
	d.password.SetText("")
	d.password2.SetText("")
	d.win.Destroy()
	d.refs.Release()
	d.done <- r
}
