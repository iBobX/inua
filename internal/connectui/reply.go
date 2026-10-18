package connectui

import (
	"context"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/connect"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

type reply struct {
	r      *replyApp
	win    *gtk.ApplicationWindow
	title  gtk.Label
	text   gtk.Entry
	cancel gtk.Button
	send   gtk.Button
	err    gtk.Label

	onClick func(gtk.Button)
	onEnter func(gtk.Entry)
}

func openReply(c *appui.Ctx, r *replyApp) *gtk.ApplicationWindow {
	w := &reply{r: r, win: &gtk.ApplicationWindow{}}
	b := c.Load("connectreply.ui")
	b.Get("connect_reply", w.win)
	for name, ptr := range map[string]gobject.Ptr{"reply_title": &w.title, "reply_text": &w.text,
		"reply_cancel": &w.cancel, "reply_send": &w.send, "reply_error": &w.err} {
		b.Get(name, ptr)
	}
	b.Close()
	w.title.SetText(i18n.Tf("Answer %s", r.title))
	w.onClick = func(b gtk.Button) {
		if b.GetName() == "reply_send" {
			w.submit()
		} else {
			w.win.Close()
		}
	}
	w.cancel.ConnectClicked(&w.onClick)
	w.send.ConnectClicked(&w.onClick)
	w.onEnter = func(gtk.Entry) { w.submit() }
	w.text.ConnectActivate(&w.onEnter)
	w.text.GrabFocus()
	return w.win
}

func (w *reply) submit() {
	msg := strings.TrimSpace(w.text.GetText())
	if msg == "" {
		return
	}
	w.send.SetSensitive(false)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := connect.Call(ctx, "Reply", w.r.device, w.r.id, msg).Err
		gtkx.Post(func() {
			if err != nil {
				w.send.SetSensitive(true)
				w.err.SetText(i18n.Tf("Not sent: %v", err))
				w.err.SetVisible(true)
				return
			}
			w.win.Close()
		})
	}()
}
