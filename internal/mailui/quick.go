package mailui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/mail"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// quickReply is a reply written at the foot of a conversation, without a
// window: its text, kept across the reader's redraws, and its send.
type quickReply struct {
	text    string
	focused bool   // its field had the focus when the reader was last drawn
	state   string // "", "drafting" (the assistant writes it), "waiting" (the undo's seconds), "sending", "sent"
	err     string
	before  string             // the text before the assistant wrote it
	drafted bool               // the assistant wrote the text
	cancel  context.CancelFunc // stops a send in its seconds
	left    int
}

func (q *quickReply) sig() string {
	if q == nil {
		return "-"
	}
	return fmt.Sprint(q.state, q.err, q.drafted)
}

// drawQuick draws the reply field under the conversation.
func (w *window) drawQuick(c *mail.Conversation) {
	q := w.quick[c.ID]
	if q == nil {
		q = &quickReply{}
		w.quick[c.ID] = q
	}
	m, _ := w.answeringQuiet(c)
	if m == nil {
		return // still loading
	}
	box := gtk.NewBox(gtk.OrientationVerticalValue, 6)
	box.AddCssClass("mail-quick")
	if q.state == "sent" {
		l := gtk.NewLabel(i18n.Tf("Sent to %s.", m.From.Short()))
		l.AddCssClass("mail-quick-note")
		l.SetXalign(0)
		gtkx.Append(box, &l.Widget)
		gtkx.Append(&w.reader, &box.Widget)
		return
	}
	over := gtk.NewOverlay()
	view := gtk.NewTextView()
	view.AddCssClass("mail-quick-text")
	view.SetWrapMode(gtk.WrapWordCharValue)
	view.SetTopMargin(9)
	view.SetBottomMargin(9)
	view.SetLeftMargin(12)
	view.SetRightMargin(12)
	view.SetEditable(q.state == "")
	buf := view.GetBuffer()
	buf.SetText(q.text, -1)
	w.quickBuf, w.quickFor = buf, c.ID // the assistant writes into it as it comes
	over.SetChild(&view.Widget)
	hint := gtk.NewLabel(i18n.Tf("Reply to %s…", firstName(m.From)))
	hint.AddCssClass("mail-quick-hint")
	hint.SetXalign(0)
	hint.SetValign(gtk.AlignStartValue)
	hint.SetCanTarget(false)
	hint.SetVisible(q.text == "")
	over.AddOverlay(&hint.Widget)
	gtkx.Append(box, &over.Widget)
	id := c.ID
	w.readerBtns = append(w.readerBtns, w.h.edit(buf, func() {
		var s, e gtk.TextIter
		buf.GetBounds(&s, &e)
		q.text = buf.GetText(&s, &e, true)
		hint.SetVisible(q.text == "")
	}))
	w.readerBtns = append(w.readerBtns, w.h.key(&view.Widget, true, func(keyval uint32, state gdk.ModifierType) bool {
		switch {
		case state&gdk.ControlMaskValue != 0 && (keyval == uint32(gdk.KEY_Return) || keyval == uint32(gdk.KEY_KP_Enter)):
			w.sendQuick(id)
			return true
		case keyval == uint32(gdk.KEY_Escape):
			w.list.GrabFocus()
			return true
		}
		return false
	}))
	w.readerBtns = append(w.readerBtns, w.h.notify(view, "has-focus", func() { q.focused = view.HasFocus() }))

	row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	row.AddCssClass("mail-quick-foot")
	note := ""
	switch {
	case q.err != "":
		note = q.err
	case q.state == "drafting":
		note = i18n.T("The assistant is writing…")
	case q.drafted:
		note = i18n.T("Drafted by the assistant")
	case q.state == "waiting":
		note = i18n.Nf("Sending in %d second…", "Sending in %d seconds…", q.left, q.left)
	case q.state == "sending":
		note = i18n.T("Sending…")
	default:
		note = i18n.T("Ctrl+Enter sends")
	}
	l := gtk.NewLabel(note)
	l.AddCssClass("mail-keys")
	if q.err != "" {
		l.AddCssClass("mail-error")
	}
	l.SetXalign(0)
	l.SetHexpand(true)
	l.SetEllipsize(3)
	if q.drafted {
		l.SetTooltipText(i18n.T("Read it before you send: the assistant can be wrong."))
	}
	gtkx.Append(row, &l.Widget)
	if q.state == "" && q.drafted {
		w.link(row, i18n.T("Undo"), func() {
			q.text, q.drafted = q.before, false
			w.lastDrawn = ""
			w.drawReader()
		})
	}
	if q.state == "" {
		w.link(row, i18n.T("Open in a window"), func() { w.quickToWindow(id) })
		send := gtk.NewButtonWithLabel(i18n.T("Send"))
		send.AddCssClass("outlined")
		w.readerBtns = append(w.readerBtns, w.h.click(send, func() { w.sendQuick(id) }))
		gtkx.Append(row, &send.Widget)
	} else if q.state == "waiting" {
		u := gtk.NewButtonWithLabel(i18n.T("Undo"))
		u.AddCssClass("outlined")
		w.readerBtns = append(w.readerBtns, w.h.click(u, func() {
			if q.cancel != nil {
				q.cancel()
			}
		}))
		gtkx.Append(row, &u.Widget)
	}
	gtkx.Append(box, &row.Widget)
	gtkx.Append(&w.reader, &box.Widget)
	if q.focused && q.state == "" {
		view.GrabFocus()
		var end gtk.TextIter
		buf.GetEndIter(&end)
		buf.PlaceCursor(&end)
	}
}

// answeringQuiet is answering without saying the message is loading.
func (w *window) answeringQuiet(c *mail.Conversation) (*mail.Message, mail.Item) {
	var last *mail.Message
	var lastIt mail.Item
	for i := len(c.Messages) - 1; i >= 0; i-- {
		it := c.Messages[i]
		rd := w.reading[it.Key()]
		if rd == nil || rd.m == nil {
			continue
		}
		if last == nil {
			last, lastIt = rd.m, it
		}
		if !w.mine(rd.m.From.Addr) {
			return rd.m, it
		}
	}
	return last, lastIt
}

// quickDraft is the reply a quick reply makes: the text typed over the
// signature and the quote, to the newest message not the user's.
func (w *window) quickDraft(id string) (mail.Draft, *acct, bool) {
	c := w.open
	q := w.quick[id]
	if c == nil || c.ID != id || q == nil {
		return mail.Draft{}, nil, false
	}
	m, it := w.answeringQuiet(c)
	x := w.acct(it.Account)
	if m == nil || x == nil {
		return mail.Draft{}, nil, false
	}
	d := mail.Reply(m, x.s.Acc, false, w.mine)
	d.Body = strings.TrimRight(q.text, "\n ") + d.Body
	return d, x, true
}

// sendQuick sends the quick reply after the undo's seconds.
func (w *window) sendQuick(id string) {
	q := w.quick[id]
	if q == nil || q.state != "" || strings.TrimSpace(q.text) == "" {
		return
	}
	d, x, ok := w.quickDraft(id)
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(w.ctx)
	q.cancel, q.err, q.state, q.left = cancel, "", "waiting", w.settings.UndoSeconds()
	w.list.GrabFocus() // out of the field, so the keys work again
	var count func()
	count = func() {
		if ctx.Err() != nil {
			q.state, q.cancel = "", nil
			w.lastDrawn = ""
			w.drawReader()
			return
		}
		if q.left > 0 {
			w.lastDrawn = ""
			w.drawReader()
			q.left--
			w.h.later(1000, count)
			return
		}
		q.state = "sending"
		w.lastDrawn = ""
		w.drawReader()
		go func() {
			_, err := x.s.Send(context.WithoutCancel(ctx), d)
			gtkx.Post(func() {
				q.cancel = nil
				if err != nil && !errors.Is(err, mail.ErrNotSaved) {
					q.state, q.err = "", i18n.Tf("Not sent: %v", err)
				} else {
					q.state, q.text = "sent", ""
					go x.s.Answered(w.ctx, d.InReplyTo)
					if sent := x.s.Role(mail.RoleSent); sent != "" {
						w.syncBox(x, sent)
					}
				}
				w.lastDrawn = ""
				w.drawReader()
			})
		}()
	}
	count()
}

// quickToWindow moves the quick reply into a composer of its own.
func (w *window) quickToWindow(id string) {
	d, _, ok := w.quickDraft(id)
	if !ok {
		return
	}
	delete(w.quick, id)
	w.newComposer(d, w.said(w.open))
	w.lastDrawn = ""
	w.drawReader()
}
