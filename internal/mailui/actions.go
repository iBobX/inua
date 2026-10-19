package mailui

import (
	"net/url"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/mail"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// byBox groups the open conversation's messages by account and mailbox,
// those in skip left out (the user's Sent, when acting on a conversation
// seen from the inbox).
func (w *window) byBox(skip ...mail.Role) map[[2]string][]uint32 {
	out := map[[2]string][]uint32{}
	if w.open == nil {
		return out
	}
	for _, m := range w.open.Messages {
		x := w.acct(m.Account)
		if x == nil {
			continue
		}
		skipped := false
		for _, r := range skip {
			if name := x.s.Role(r); name != "" && name == m.Mailbox && !(w.view.kind == viewBox && w.view.mailbox == name) {
				skipped = true
			}
		}
		if !skipped {
			k := [2]string{m.Account, m.Mailbox}
			out[k] = append(out[k], m.UID)
		}
	}
	return out
}

// moveOpen moves the open conversation to a mailbox of the role, then
// chooses the next one.
func (w *window) moveOpen(role mail.Role, missing string) {
	groups := w.byBox(mail.RoleSent)
	if len(groups) == 0 {
		return
	}
	for k, uids := range groups {
		x := w.acct(k[0])
		to := x.s.Role(role)
		if to == "" {
			w.actErr = missing
			w.drawReader()
			return
		}
		if to == k[1] {
			delete(groups, k)
			continue
		}
		mailbox := k[1]
		uids := uids
		go func() {
			err := x.s.Move(w.ctx, mailbox, uids, to)
			gtkx.Post(func() {
				if err != nil {
					w.actErr = i18n.Tf("The server did not move it: %v", err)
				}
				w.counted(x, mailbox)
				w.refresh()
				if err != nil && w.open != nil {
					w.drawReader()
				}
			})
		}()
	}
	w.next()
}

// next chooses the conversation after the open one, which is leaving the
// list.
func (w *window) next() {
	i := int(w.sel.GetSelected())
	w.open, w.openID = nil, ""
	w.readerStack.SetVisibleChildName("empty")
	// The index changed at once: list again, then choose what took its place.
	w.refresh()
	if len(w.convs) == 0 {
		return
	}
	i = min(i, len(w.convs)-1)
	if i >= 0 {
		w.sel.SetSelected(uint32(i))
	}
}

func (w *window) archive() {
	w.moveOpen(mail.RoleArchive, i18n.T("This account has no Archive folder."))
}

func (w *window) spam() {
	w.moveOpen(mail.RoleJunk, i18n.T("This account has no Spam folder."))
}

// trash moves the open conversation to the Trash; in the Trash, it asks,
// then deletes it for good.
func (w *window) trash() {
	if w.open == nil {
		return
	}
	latest := w.open.Latest()
	x := w.acct(latest.Account)
	if x == nil {
		return
	}
	if latest.Mailbox == x.s.Role(mail.RoleTrash) || x.s.Role(mail.RoleTrash) == "" {
		n := len(w.open.Messages)
		w.confirm(i18n.T("Delete for good?"), w.open.Subject(),
			i18n.Nf("Its %d message is deleted from the server. This cannot be undone.", "Its %d messages are deleted from the server. This cannot be undone.", n, n),
			i18n.T("Delete"), true, w.deleteOpen)
		return
	}
	w.moveOpen(mail.RoleTrash, "")
}

func (w *window) deleteOpen() {
	for k, uids := range w.byBox() {
		x := w.acct(k[0])
		mailbox, uids := k[1], uids
		go func() {
			err := x.s.Delete(w.ctx, mailbox, uids)
			gtkx.Post(func() {
				if err != nil {
					w.actErr = i18n.Tf("The server did not delete it: %v", err)
				}
				w.refresh()
			})
		}()
	}
	w.next()
}

// star stars the newest message, or unstars the conversation.
func (w *window) star() {
	if w.open == nil {
		return
	}
	on := !w.open.Starred()
	targets := map[[2]string][]uint32{}
	if on {
		m := w.open.Latest()
		targets[[2]string{m.Account, m.Mailbox}] = []uint32{m.UID}
	} else {
		for _, m := range w.open.Messages {
			if m.Has(mail.Flagged) {
				k := [2]string{m.Account, m.Mailbox}
				targets[k] = append(targets[k], m.UID)
			}
		}
	}
	w.setFlags(targets, mail.Flagged, on)
}

// markUnread marks the newest message unread and closes the conversation.
func (w *window) markUnread() {
	if w.open == nil {
		return
	}
	m := w.open.Latest()
	w.setFlags(map[[2]string][]uint32{{m.Account, m.Mailbox}: {m.UID}}, mail.Seen, false)
	w.open, w.openID = nil, ""
	w.sel.UnselectAll()
	w.readerStack.SetVisibleChildName("empty")
}

func (w *window) setFlags(targets map[[2]string][]uint32, flag string, on bool) {
	for k, uids := range targets {
		x := w.acct(k[0])
		if x == nil {
			continue
		}
		mailbox, uids := k[1], uids
		go func() {
			err := x.s.SetFlag(w.ctx, mailbox, uids, flag, on)
			gtkx.Post(func() {
				if err != nil {
					w.actErr = i18n.Tf("The server did not take it: %v", err)
				}
				w.counted(x, mailbox)
				w.refresh()
			})
		}()
	}
	w.refresh()
}

// --- writing ---

// reply opens a composer answering the newest message not the user's.
func (w *window) reply(all bool) {
	m, it := w.answering()
	if m == nil {
		return
	}
	x := w.acct(it.Account)
	d := mail.Reply(m, x.s.Acc, all, w.mine)
	w.newComposer(d, w.said(w.open))
}

// replyWith opens a reply drafted from one of the assistant's ideas.
func (w *window) replyWith(idea string) {
	m, it := w.answering()
	if m == nil {
		return
	}
	x := w.acct(it.Account)
	c := w.newComposer(mail.Reply(m, x.s.Acc, false, w.mine), w.said(w.open))
	c.draftWhat.SetText(idea)
	c.draftNow()
}

func (w *window) forward() {
	m, it := w.answering()
	if m == nil {
		return
	}
	x := w.acct(it.Account)
	d, err := mail.Forward(m, x.s.Acc, w.drafts.Dir)
	if err != nil {
		w.actErr = i18n.Tf("Could not forward it: %v", err)
		w.drawReader()
		return
	}
	w.newComposer(d, nil)
}

// answering is the message a reply answers: the newest read one not the
// user's, else the newest.
func (w *window) answering() (*mail.Message, mail.Item) {
	if w.open == nil {
		return nil, mail.Item{}
	}
	var last *mail.Message
	var lastIt mail.Item
	for i := len(w.open.Messages) - 1; i >= 0; i-- {
		it := w.open.Messages[i]
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
	if last == nil {
		w.actErr = i18n.T("The message is still loading.")
		w.drawReader()
	}
	return last, lastIt
}

// said is the conversation's messages read, for the assistant.
func (w *window) said(c *mail.Conversation) []mail.Said {
	if c == nil {
		return nil
	}
	var out []mail.Said
	for _, m := range c.Messages {
		if rd := w.reading[m.Key()]; rd != nil && rd.m != nil {
			out = append(out, mail.SaidOf(rd.m))
		}
	}
	return out
}

// mailto opens a composer from a mailto: link.
func (w *window) mailto(link string) {
	u, err := url.Parse(link)
	if err != nil || len(w.accts) == 0 {
		return
	}
	q := u.Query()
	to := u.Opaque
	if t := q.Get("to"); t != "" {
		to = strings.Trim(to+","+t, ",")
	}
	to, _ = url.PathUnescape(to)
	d := mail.Draft{To: to, Cc: q.Get("cc"), Bcc: q.Get("bcc"), Subject: q.Get("subject"), Body: q.Get("body")}
	w.newComposer(d, nil)
}

// --- accounts ---

// askRemove asks, then signs an account out: its settings, secrets and
// cache go.
func (w *window) askRemove(x *acct) {
	w.confirm(i18n.T("Remove this account?"), x.s.Acc.Address,
		i18n.T("Mail forgets it on this computer: its sign-in and the mail it kept. The mail on the server stays."),
		i18n.T("Remove"), true, func() {
			if x.stop != nil {
				x.stop()
			}
			x.s.Close()
			w.settings.Remove(x.s.Acc.ID)
			if err := w.settings.Save(mail.SettingsPath()); err != nil {
				w.say(&w.listError, err.Error())
			}
			w.secrets.Forget(x.s.Acc.ID)
			w.store.Forget(x.s.Acc.ID)
			w.accts = removeAcct(w.accts, x)
			if w.view.account == x.s.Acc.ID {
				w.view = view{}
			}
			w.open, w.openID = nil, ""
			w.readerStack.SetVisibleChildName("empty")
			w.drawSide()
			w.refresh()
		})
}

func removeAcct(l []*acct, x *acct) []*acct {
	var out []*acct
	for _, y := range l {
		if y != x {
			out = append(out, y)
		}
	}
	return out
}

// showUnsent lists the drafts kept on this computer in the reader, to
// open or discard.
func (w *window) showUnsent() {
	ds, err := w.drafts.List()
	w.open, w.openID = nil, ""
	w.sel.UnselectAll()
	w.h.forget(w.readerBtns)
	w.readerBtns = nil
	w.lastDrawn = ""
	gtkx.Clear(&w.reader)
	w.readerStack.SetVisibleChildName("read")
	t := gtk.NewLabel(i18n.T("Unsent"))
	t.AddCssClass("mail-read-subject")
	t.SetXalign(0)
	gtkx.Append(&w.reader, &t.Widget)
	if err != nil {
		e := gtk.NewLabel(err.Error())
		e.AddCssClass("mail-error")
		gtkx.Append(&w.reader, &e.Widget)
	}
	for _, d := range ds {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		row.AddCssClass("mail-unsent")
		texts := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		texts.SetHexpand(true)
		subject := d.Subject
		if subject == "" {
			subject = i18n.T("(no subject)")
		}
		s := gtk.NewLabel(subject)
		s.AddCssClass("mail-from")
		s.SetXalign(0)
		s.SetEllipsize(3)
		gtkx.Append(texts, &s.Widget)
		to := d.To
		if to == "" {
			to = i18n.T("no recipient yet")
		}
		meta := gtk.NewLabel(i18n.Tf("to %s · %s", to, i18n.Time(d.Saved.Local(), "Mon Jan 2, 15:04")))
		meta.AddCssClass("mail-to")
		meta.SetXalign(0)
		meta.SetEllipsize(3)
		gtkx.Append(texts, &meta.Widget)
		gtkx.Append(row, &texts.Widget)
		dd := d
		open := gtk.NewButtonWithLabel(i18n.T("Open"))
		open.AddCssClass("outlined")
		w.readerBtns = append(w.readerBtns, w.h.click(open, func() { w.newComposer(dd, nil); w.showUnsent() }))
		gtkx.Append(row, &open.Widget)
		discard := gtk.NewButtonWithLabel(i18n.T("Discard"))
		discard.AddCssClass("mail-link")
		w.readerBtns = append(w.readerBtns, w.h.click(discard, func() {
			w.drafts.Delete(dd.ID)
			w.drawSide()
			w.showUnsent()
		}))
		gtkx.Append(row, &discard.Widget)
		gtkx.Append(&w.reader, &row.Widget)
	}
	if len(ds) == 0 {
		l := gtk.NewLabel(i18n.T("Nothing unsent."))
		l.AddCssClass("sub")
		l.SetXalign(0)
		gtkx.Append(&w.reader, &l.Widget)
	}
}

// --- notifications ---

var notifyConn *dbus.Conn

// notifyNew says new mail came, when the window is not in front.
func (w *window) notifyNew(x *acct, inbox string, uids []uint32) {
	var newest *mail.Item
	for _, it := range x.s.Items(inbox) {
		for _, u := range uids {
			if it.UID == u && !it.Has(mail.Seen) && (newest == nil || it.Date.After(newest.Date)) {
				it := it
				newest = &it
			}
		}
	}
	if newest == nil || time.Since(newest.Date) > 24*time.Hour {
		return
	}
	summary := newest.From.Short()
	body := newest.Subject
	if len(uids) > 1 {
		body = i18n.Nf("%s and %d more message", "%s and %d more messages", len(uids)-1, newest.Subject, len(uids)-1)
	}
	go func() {
		if notifyConn == nil {
			c, err := dbus.ConnectSessionBus()
			if err != nil {
				return
			}
			notifyConn = c
		}
		notifyConn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications").Call(
			"org.freedesktop.Notifications.Notify", 0,
			"Mail", uint32(0), "io.github.lyricat.Inua.Mail", summary, body, []string{},
			map[string]dbus.Variant{"desktop-entry": dbus.MakeVariant("io.github.lyricat.Inua.Mail"), "category": dbus.MakeVariant("email.arrived")},
			int32(-1))
	}()
}

// --- sorting ---

// sortNew tags the inbox's new messages (uids; nil: every one not
// sorted yet): by the rules at once, by the assistant (when mail.toml
// turns it on) for those the rules leave.
func (w *window) sortNew(x *acct, uids []uint32) {
	if uids != nil && len(uids) == 0 {
		return
	}
	inbox := x.s.Role(mail.RoleInbox)
	var left []mail.Envelope
	for _, it := range x.s.Items(inbox) {
		if it.Tag != "" || time.Since(it.Date) > 30*24*time.Hour {
			continue
		}
		fresh := uids == nil
		for _, u := range uids {
			fresh = fresh || u == it.UID
		}
		if !fresh {
			continue
		}
		if t, ok := mail.Rules(*it.Envelope, w.mine(it.From.Addr), x.s.Acc.Address); ok {
			x.s.SetTag(inbox, it.UID, t, false)
		} else {
			left = append(left, *it.Envelope)
		}
	}
	w.refresh()
	if len(left) > 0 && w.settings.Assistant.Sort {
		if len(left) > 30 {
			left = left[:30] // the newest; older ones stay unsorted
		}
		w.sortWithAssistant(x, inbox, left)
	}
}
