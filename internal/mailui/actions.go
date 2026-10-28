package mailui

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/mail"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// byBox groups a conversation's messages by account and mailbox, those
// in skip left out (the user's Sent, when acting on a conversation seen
// from the inbox).
func (w *window) byBox(c *mail.Conversation, skip ...mail.Role) map[[2]string][]uint32 {
	out := map[[2]string][]uint32{}
	if c == nil {
		return out
	}
	for _, m := range c.Messages {
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

// pending is an action the user can still undo: its messages are out of
// the list, and the server is told when the undo bar goes.
type pending struct {
	moves []heldMove
	open  string // the conversation it took, to choose again on undo
}

type heldMove struct {
	x       *acct
	mailbox string
	to      string
	held    []mail.Envelope
}

// moveConv moves a conversation to a mailbox (to gives it for an account; ""
// when the account has none, said with missing), out of the list at
// once, with done in the undo bar.
func (w *window) moveConv(c *mail.Conversation, to func(*acct) string, missing, done string) {
	if c == nil {
		return
	}
	groups := w.byBox(c, mail.RoleSent)
	p := &pending{}
	for k := range groups {
		x := w.acct(k[0])
		dest := to(x)
		if dest == "" {
			w.actErr = missing
			w.drawReader()
			return
		}
		if dest == k[1] {
			continue
		}
		p.moves = append(p.moves, heldMove{x: x, mailbox: k[1], to: dest})
	}
	if len(p.moves) == 0 {
		return
	}
	w.commitPending() // one action waits at a time
	for i := range p.moves {
		m := &p.moves[i]
		m.held = m.x.s.Hold(m.mailbox, groups[[2]string{m.x.s.Acc.ID, m.mailbox}])
		w.counted(m.x, m.mailbox)
	}
	if c.ID == w.openID {
		p.open = c.ID
		w.next()
	} else {
		w.refresh()
	}
	w.pending = p
	w.undoText.SetText(done)
	w.undoBar.SetVisible(true)
	w.h.later(6000, func() {
		if w.pending == p {
			w.commitPending()
		}
	})
}

// commitPending tells the server what the action waiting did.
func (w *window) commitPending() {
	p := w.pending
	if p == nil {
		return
	}
	w.pending = nil
	w.undoBar.SetVisible(false)
	for _, m := range p.moves {
		go func() {
			err := m.x.s.MoveHeld(w.ctx, m.mailbox, m.held, m.to)
			gtkx.Post(func() {
				if err != nil {
					w.say(&w.listError, i18n.Tf("The server did not move it: %v", err))
				}
				w.counted(m.x, m.mailbox)
				w.refresh()
			})
		}()
	}
}

// undo takes the action waiting back: its messages return, the server
// untouched.
func (w *window) undo() {
	p := w.pending
	if p == nil {
		return
	}
	w.pending = nil
	w.undoBar.SetVisible(false)
	for _, m := range p.moves {
		m.x.s.Release(m.mailbox, m.held)
		w.counted(m.x, m.mailbox)
	}
	w.refresh()
	if p.open != "" {
		if i := slices.Index(w.ids, p.open); i >= 0 {
			w.sel.SetSelected(uint32(i))
		}
	}
}

// flushPending tells the server before Mail quits, waiting a little.
func (w *window) flushPending() {
	p := w.pending
	if p == nil {
		return
	}
	w.pending = nil
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for _, m := range p.moves {
		wg.Add(1)
		go func() { defer wg.Done(); m.x.s.MoveHeld(ctx, m.mailbox, m.held, m.to) }()
	}
	wg.Wait()
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

func roleOf(r mail.Role) func(*acct) string {
	return func(x *acct) string { return x.s.Role(r) }
}

func (w *window) archive() { w.archiveConv(w.open) }

func (w *window) archiveConv(c *mail.Conversation) {
	w.moveConv(c, roleOf(mail.RoleArchive), i18n.T("This account has no Archive folder."), i18n.T("Archived"))
}

func (w *window) spam() {
	w.moveConv(w.open, roleOf(mail.RoleJunk), i18n.T("This account has no Spam folder."), i18n.T("Moved to Spam"))
}

// trash moves the open conversation to the Trash; in the Trash, it asks,
// then deletes it permanently.
func (w *window) trash() { w.trashConv(w.open) }

func (w *window) trashConv(c *mail.Conversation) {
	if c == nil {
		return
	}
	latest := c.Latest()
	x := w.acct(latest.Account)
	if x == nil {
		return
	}
	if latest.Mailbox == x.s.Role(mail.RoleTrash) || x.s.Role(mail.RoleTrash) == "" {
		w.deletePermanentlyConv(c)
		return
	}
	w.moveConv(c, roleOf(mail.RoleTrash), "", i18n.T("Moved to Trash"))
}

// deletePermanently asks, then deletes the open conversation for good,
// from whatever folder it is in.
func (w *window) deletePermanently() { w.deletePermanentlyConv(w.open) }

func (w *window) deletePermanentlyConv(c *mail.Conversation) {
	if c == nil {
		return
	}
	n := len(c.Messages)
	w.confirm(i18n.T("Permanently Delete?"), c.Subject(),
		i18n.Nf("Its %d message is deleted from the server, not moved to the Trash. This cannot be undone.", "Its %d messages are deleted from the server, not moved to the Trash. This cannot be undone.", n, n),
		i18n.T("Permanently Delete"), true, func() { w.deleteConv(c) })
}

// deleteConv deletes a conversation for good; the user's Sent stays when
// it is seen from elsewhere.
func (w *window) deleteConv(c *mail.Conversation) {
	for k, uids := range w.byBox(c, mail.RoleSent) {
		x := w.acct(k[0])
		mailbox, uids := k[1], uids
		go func() {
			err := x.s.DeletePermanently(w.ctx, mailbox, uids)
			gtkx.Post(func() {
				if err != nil {
					w.actErr = i18n.Tf("The server did not delete it: %v", err)
				}
				w.counted(x, mailbox)
				w.refresh()
			})
		}()
	}
	if c.ID == w.openID {
		w.next()
	} else {
		w.refresh()
	}
}

// emptyBox asks, then deletes every message in the mailbox shown (the
// Trash or Spam) for good, those not fetched yet too.
func (w *window) emptyBox() {
	v := w.view
	x := w.acct(v.account)
	if v.kind != viewBox || x == nil {
		return
	}
	title, done := i18n.T("Empty the Trash?"), i18n.T("Empty Trash")
	if v.mailbox == x.s.Role(mail.RoleJunk) {
		title, done = i18n.T("Empty Spam?"), i18n.T("Empty Spam")
	}
	w.confirm(title, x.s.Acc.Address,
		i18n.T("Every message in it is permanently deleted from the server. This cannot be undone."),
		done, true, func() {
			w.commitPending()
			go func() {
				err := x.s.Empty(w.ctx, v.mailbox)
				gtkx.Post(func() {
					if err != nil {
						w.say(&w.listError, i18n.Tf("The server did not empty it: %v", err))
					}
					if w.open != nil && w.open.Latest().Account == x.s.Acc.ID && w.open.Latest().Mailbox == v.mailbox {
						w.open, w.openID = nil, ""
						w.readerStack.SetVisibleChildName("empty")
					}
					w.counted(x, v.mailbox)
					w.refresh()
				})
			}()
		})
}

// star stars the newest message, or unstars the conversation.
func (w *window) star() { w.starConv(w.open) }

func (w *window) starConv(c *mail.Conversation) {
	if c == nil {
		return
	}
	on := !c.Starred()
	targets := map[[2]string][]uint32{}
	if on {
		m := c.Latest()
		targets[[2]string{m.Account, m.Mailbox}] = []uint32{m.UID}
	} else {
		for _, m := range c.Messages {
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

// toggleRead marks a conversation read, or its newest message unread.
func (w *window) toggleRead(c *mail.Conversation) {
	if c == nil {
		return
	}
	if c.Unread() == 0 {
		m := c.Latest()
		w.setFlags(map[[2]string][]uint32{{m.Account, m.Mailbox}: {m.UID}}, mail.Seen, false)
		return
	}
	targets := map[[2]string][]uint32{}
	for _, m := range c.Messages {
		if !m.Has(mail.Seen) {
			k := [2]string{m.Account, m.Mailbox}
			targets[k] = append(targets[k], m.UID)
		}
	}
	w.setFlags(targets, mail.Seen, true)
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
	if c != nil {
		c.writeAI(idea) // as a suggestion, its panel open, to keep or not
	}
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
	// None read yet: nothing to answer, its buttons are off until one is.
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
// open (a click on one) or discard; Unsent is the index's item chosen
// while they show.
func (w *window) showUnsent() {
	ds, err := w.drafts.List()
	w.open, w.openID = nil, ""
	w.sel.UnselectAll()
	w.h.forget(w.readerBtns)
	w.readerBtns = nil
	w.lastDrawn = ""
	gtkx.Clear(&w.readerBar) // the actions of the conversation that was open
	gtkx.Clear(&w.reader)
	w.readerStack.SetVisibleChildName("read")
	if !w.unsent {
		w.unsent = true
		w.drawSide()
	}
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
		open := gtk.NewButton()
		open.AddCssClass("mail-unsent-open")
		open.SetHexpand(true)
		open.SetTooltipText(i18n.T("Open"))
		texts := gtk.NewBox(gtk.OrientationVerticalValue, 2)
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
		open.SetChild(&texts.Widget)
		dd := d
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

// leaveUnsent ends the drafts' page: the reader is empty until a
// conversation opens.
func (w *window) leaveUnsent() {
	w.unsent = false
	if w.open == nil {
		w.readerStack.SetVisibleChildName("empty")
	}
}

// --- notifications ---

var notifyConn *dbus.Conn

// notifyNew says new mail came, when the window is not in front;
// newsletters wait without a word.
func (w *window) notifyNew(x *acct, inbox string, uids []uint32) {
	uids = slices.DeleteFunc(slices.Clone(uids), func(u uint32) bool {
		for _, it := range x.s.Items(inbox) {
			if it.UID == u {
				return mail.IsNews(*it.Envelope)
			}
		}
		return false
	})
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
	if len(left) > 0 && w.settings.Assistant.Sort && w.settings.AI() {
		if len(left) > 30 {
			left = left[:30] // the newest; older ones stay unsorted
		}
		w.sortWithAssistant(x, inbox, left)
	}
}
