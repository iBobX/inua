package mailui

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/mail"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// composer is a message being written, in a window of its own.
type composer struct {
	w   *window
	win gtk.Window

	from                           gtk.DropDown
	to, cc, bcc, subject           gtk.Entry
	ccRow, bccRow, toRow, fieldBox gtk.Box
	sug                            *gtk.Box       // the suggestions under the field typed in
	sugFor                         *gtk.Entry     // that field
	sugs                           []mail.Contact // what it suggests
	sugPtrs                        []uintptr
	contacts                       []mail.Contact
	body                           gtk.TextView
	buf                            *gtk.TextBuffer
	attachments                    gtk.FlowBox
	showCc, attach, send           gtk.Button
	status                         gtk.Label

	// AI's panel (compose_ai.go).
	aiToggle, aiClose, aiStop                   gtk.Button
	aiPrev, aiNext, aiKeep, aiRetry, aiDiscard  gtk.Button
	aiFlip                                      gtk.Button
	aiReveal                                    gtk.Revealer
	aiScope, aiWorking, aiAsked, aiCount, aiErr gtk.Label
	aiSay                                       gtk.Entry
	aiAsk, aiList, aiTranslate, aiWork, aiRev   gtk.Box
	aiLangs                                     gtk.FlowBox
	ai                                          composeAI

	d        mail.Draft
	conv     []mail.Said // the conversation a reply answers, for the assistant
	accounts []string    // the From list's account IDs
	ptrs     []uintptr   // its registrations in the hub
	dirty    bool        // changed since saved
	saving   bool        // a save is due
	edits    int         // changes made, counted for the copy in Drafts
	served   int         // edits when the copy in Drafts was last made
	sentDone bool        // sent: a copy in Drafts made after it goes
	sending  bool
	undo     context.CancelFunc // stops a send in its seconds
	filling  bool               // the fields are being set from the draft
	closed   bool
}

// newComposer opens a composer on a draft; conv is the conversation it
// answers, for the assistant's drafting.
func (w *window) newComposer(d mail.Draft, conv []mail.Said) *composer {
	if len(w.accts) == 0 {
		w.showWelcome("", nil)
		return nil
	}
	// A draft open already comes forward.
	for _, c := range w.composers {
		if (d.ID != "" && c.d.ID == d.ID) || (d.Server != 0 && c.d.Server == d.Server && c.d.Account == d.Account) {
			c.win.Present()
			return c
		}
	}
	if d.Account == "" || w.acct(d.Account) == nil {
		acc := w.accts[0].s.Acc
		if w.view.kind == viewBox {
			if x := w.acct(w.view.account); x != nil {
				acc = x.s.Acc
			}
		}
		blank := mail.NewDraft(acc)
		blank.To, blank.Cc, blank.Bcc, blank.Subject = d.To, d.Cc, d.Bcc, d.Subject
		if d.Body != "" {
			blank.Body = d.Body + blank.Body
		}
		d = blank
	}
	if d.ID == "" {
		d.ID = mail.NewDraft(mail.Account{}).ID
	}
	c := &composer{w: w, d: d, conv: conv}
	b := w.c.Load("mail-compose.ui")
	b.Get("compose", &c.win)
	for name, ptr := range map[string]gobject.Ptr{
		"from": &c.from, "to": &c.to, "cc": &c.cc, "bcc": &c.bcc, "subject": &c.subject,
		"cc_row": &c.ccRow, "bcc_row": &c.bccRow, "to_row": &c.toRow, "fields": &c.fieldBox, "body": &c.body, "attachments": &c.attachments,
		"show_cc": &c.showCc, "attach": &c.attach, "send": &c.send, "status": &c.status,
		"ai_toggle": &c.aiToggle, "ai_close": &c.aiClose, "ai_stop": &c.aiStop, "ai_prev": &c.aiPrev, "ai_next": &c.aiNext,
		"ai_keep": &c.aiKeep, "ai_retry": &c.aiRetry, "ai_discard": &c.aiDiscard, "ai_flip": &c.aiFlip,
		"ai_reveal": &c.aiReveal, "ai_scope": &c.aiScope, "ai_working": &c.aiWorking, "ai_asked": &c.aiAsked,
		"ai_count": &c.aiCount, "ai_error": &c.aiErr, "ai_say": &c.aiSay, "ai_ask": &c.aiAsk, "ai_list": &c.aiList,
		"ai_translate": &c.aiTranslate, "ai_work": &c.aiWork, "ai_review": &c.aiRev, "ai_langs": &c.aiLangs,
	} {
		b.Get(name, ptr)
	}
	b.Close()
	c.win.SetApplication(w.c.App)
	c.buf = c.body.GetBuffer()

	names := make([]string, len(w.accts))
	sel := uint32(0)
	for i, x := range w.accts {
		names[i] = x.s.Acc.Address
		if x.s.Acc.Name != "" {
			names[i] = x.s.Acc.Name + " <" + x.s.Acc.Address + ">"
		}
		c.accounts = append(c.accounts, x.s.Acc.ID)
		if x.s.Acc.ID == d.Account {
			sel = uint32(i)
		}
	}
	list := gtk.NewStringList(names)
	c.from.SetModel(list)
	list.Unref()
	c.from.SetSelected(sel)
	c.from.SetVisible(len(names) > 0)

	c.fill()
	h := w.h
	c.ptrs = append(c.ptrs,
		h.click(&c.showCc, func() {
			c.ccRow.SetVisible(true)
			c.bccRow.SetVisible(true)
			c.showCc.SetVisible(false)
			c.cc.GrabFocus()
		}),
		h.click(&c.attach, c.chooseFiles),
		h.click(&c.send, c.sendOrUndo),
		h.edit(&c.to, func() { c.changed(); c.suggest(&c.to, &c.toRow) }),
		h.edit(&c.cc, func() { c.changed(); c.suggest(&c.cc, &c.ccRow) }),
		h.edit(&c.bcc, func() { c.changed(); c.suggest(&c.bcc, &c.bccRow) }),
		h.edit(&c.subject, c.changed),
		h.edit(c.buf, c.changed),
		h.close(&c.win, c.closeRequested),
		h.key(&c.win.Widget, true, c.key),
		h.drop(&c.win.Widget, c.add),
		h.notify(&c.from, "selected", c.fromChanged),
	)
	c.buildAI()
	c.sug = gtk.NewBox(gtk.OrientationVerticalValue, 0)
	c.sug.AddCssClass("mail-suggest")
	c.sug.SetVisible(false)
	c.fieldBox.InsertChildAfter(&c.sug.Widget, &c.toRow.Widget)
	c.contacts = w.contacts()
	for _, e := range []*gtk.Entry{&c.to, &c.cc, &c.bcc} {
		e := e
		c.ptrs = append(c.ptrs, h.key(&e.Widget, true, func(keyval uint32, _ gdk.ModifierType) bool { return c.suggestKey(e, keyval) }))
	}
	w.composers = append(w.composers, c)
	w.h.later(60000, c.serverTick)
	c.win.Present()
	// A class of its own: Hyprland floats it over Mail, which tiles
	// (config/hypr/inua.conf), and the dock counts it as Mail's. Set
	// once the toplevel is made, before its first frame maps it.
	gtkx.SetAppID(&c.win, composeAppID)
	if c.d.To == "" {
		c.to.GrabFocus()
	} else {
		c.body.GrabFocus()
		var start gtk.TextIter
		c.buf.GetStartIter(&start)
		c.buf.PlaceCursor(&start)
	}
	return c
}

// fill sets the fields from the draft.
func (c *composer) fill() {
	c.filling = true
	defer func() { c.filling = false }()
	c.to.SetText(c.d.To)
	c.cc.SetText(c.d.Cc)
	c.bcc.SetText(c.d.Bcc)
	if c.d.Cc != "" || c.d.Bcc != "" {
		c.ccRow.SetVisible(true)
		c.bccRow.SetVisible(true)
		c.showCc.SetVisible(false)
	}
	c.subject.SetText(c.d.Subject)
	c.buf.SetText(c.d.Body, -1)
	c.title()
	c.drawFiles()
}

func (c *composer) title() {
	t := strings.TrimSpace(c.subject.GetText())
	if t == "" {
		t = i18n.T("New message")
	}
	c.win.SetTitle(t)
}

// read takes the fields into the draft.
func (c *composer) read() {
	c.d.To, c.d.Cc, c.d.Bcc = c.to.GetText(), c.cc.GetText(), c.bcc.GetText()
	c.d.Subject = c.subject.GetText()
	c.d.Body = c.text()
	if c.ai.phase != aiIdle {
		c.d.Body = c.ai.before // what AI suggests is not the user's until kept
	}
	if i := int(c.from.GetSelected()); i < len(c.accounts) {
		c.d.Account = c.accounts[i]
	}
}

func (c *composer) text() string {
	var start, end gtk.TextIter
	c.buf.GetBounds(&start, &end)
	return c.buf.GetText(&start, &end, true)
}

// changed keeps the draft a few seconds after the last change.
func (c *composer) changed() {
	if c.filling || c.closed {
		return
	}
	c.title()
	if c.aiReveal.GetRevealChild() {
		c.drawAI() // written now, or no longer: what the panel offers
	}
	c.dirty = true
	c.edits++
	if c.saving {
		return
	}
	c.saving = true
	c.w.h.later(3000, func() {
		c.saving = false
		if !c.closed && c.dirty {
			c.save()
		}
	})
}

// save keeps the draft on disk; an empty one is not kept.
func (c *composer) save() {
	if c.sending {
		return
	}
	c.read()
	c.dirty = false
	if c.d.Empty() {
		c.w.drafts.Delete(c.d.ID)
		return
	}
	c.d.Saved = time.Now()
	if err := c.w.drafts.Save(c.d); err != nil {
		c.say(i18n.Tf("Could not keep the draft: %v", err), true)
		return
	}
	c.say(i18n.Tf("Draft kept at %s", time.Now().Format("15:04")), false)
}

// serverTick keeps a copy in the account's Drafts folder once a minute
// while the draft changes.
func (c *composer) serverTick() {
	if c.closed {
		return
	}
	if !c.sending && c.edits != c.served {
		c.saveServer(false)
	}
	c.w.h.later(60000, c.serverTick)
}

// saveServer keeps a copy of the draft in the account's Drafts folder, in
// place of the one before. closing: the window has gone, and the draft on
// this computer goes once the server has it.
func (c *composer) saveServer(closing bool) {
	if !closing {
		c.read()
	}
	d := c.d
	x := c.w.acct(d.Account)
	if x == nil {
		return
	}
	if d.Empty() {
		if d.Server != 0 {
			go x.s.DeleteDraft(c.w.ctx, d.Server)
		}
		return
	}
	edits := c.edits
	go func() {
		uid, err := x.s.SaveDraft(c.w.ctx, d)
		gtkx.Post(func() {
			if c.sentDone && uid != 0 {
				go x.s.DeleteDraft(c.w.ctx, uid) // sent while it was being kept
				return
			}
			switch {
			case err == nil:
				c.d.Server, c.served = uid, edits
				if closing {
					c.w.drafts.Delete(d.ID) // the Drafts folder has it
				} else {
					c.w.drafts.Save(c.d) // with its copy's UID
					c.say(i18n.Tf("Draft kept in Drafts at %s", time.Now().Format("15:04")), false)
				}
				if drafts := x.s.Role(mail.RoleDrafts); drafts != "" {
					c.w.syncBox(x, drafts)
				}
			case errors.Is(err, mail.ErrNoDrafts):
			case !closing:
				c.say(i18n.Tf("Draft kept on this computer only: %v", err), false)
			}
			c.w.drawSide()
		})
	}()
}

// say puts what happened in the foot; "" puts the keys back.
func (c *composer) say(text string, bad bool) {
	if text == "" {
		text = i18n.T("Ctrl+Enter sends")
	}
	c.status.SetText(text)
	setClass(&c.status.Widget, "error", bad)
}

// fromChanged moves the signature with the account.
func (c *composer) fromChanged() {
	if c.filling {
		return
	}
	i := int(c.from.GetSelected())
	if i >= len(c.accounts) {
		return
	}
	old, nw := c.w.acct(c.d.Account), c.w.acct(c.accounts[i])
	if old != nil && nw != nil && old.s.Acc.Signature != "" {
		text := c.text()
		if strings.Contains(text, "-- \n"+old.s.Acc.Signature) {
			c.buf.SetText(strings.Replace(text, "-- \n"+old.s.Acc.Signature, "-- \n"+nw.s.Acc.Signature, 1), -1)
		}
	}
	c.d.Account = c.accounts[i]
	c.changed()
}

func (c *composer) key(keyval uint32, state gdk.ModifierType) bool {
	ctrl := state&gdk.ControlMaskValue != 0
	enter := keyval == uint32(gdk.KEY_Return) || keyval == uint32(gdk.KEY_KP_Enter)
	switch {
	case ctrl && enter:
		c.sendOrUndo()
	case ctrl && (keyval == uint32(gdk.KEY_j) || keyval == uint32(gdk.KEY_J)):
		c.keyAI()
	case ctrl && (keyval == uint32(gdk.KEY_w) || keyval == uint32(gdk.KEY_W)):
		c.win.Close() // the draft is kept as it closes
	case c.ai.phase == aiReview && enter && !ctrl && !c.inEntry() && !c.inPanel():
		c.keepAI() // the text cannot be typed in while a suggestion waits; in the panel, Enter is the button's
	case c.ai.phase == aiReview && keyval == uint32(gdk.KEY_Escape):
		c.discardAI()
	default:
		return false
	}
	return true
}

func (c *composer) closeRequested() bool {
	if c.sending {
		return true // it closes when sent, or comes back if it failed
	}
	c.save()
	if c.edits != c.served {
		c.saveServer(true)
	} else if c.d.Server != 0 {
		c.w.drafts.Delete(c.d.ID) // the Drafts folder has it as it is
	}
	c.close()
	return false
}

func (c *composer) close() {
	c.closed = true
	c.ai.seq++ // an answer still coming is dropped
	c.w.h.forget(c.ai.listPtrs)
	if c.undo != nil {
		c.undo()
	}
	c.w.h.forget(c.ptrs)
	c.w.composers = slices.DeleteFunc(c.w.composers, func(x *composer) bool { return x == c })
	c.w.drawSide() // Unsent
	if c.w.unsent {
		c.w.showUnsent() // the draft as it was left, or gone when sent
	}
}

// --- files ---

func (c *composer) chooseFiles() {
	d := gtk.NewFileDialog()
	d.SetTitle(i18n.T("Attach files"))
	id := c.w.h.callback(func(result uintptr) {
		defer d.Unref()
		files, err := d.OpenMultipleFinish(&gio.AsyncResultBase{Ptr: result})
		if err != nil || files == nil {
			return
		}
		var paths []string
		for i := uint32(0); i < files.GetNItems(); i++ {
			f := gio.FileBase{Ptr: files.GetItem(i)}
			if p := f.GetPath(); p != "" {
				paths = append(paths, p)
			}
			(&gobject.Object{Ptr: f.Ptr}).Unref()
		}
		(&gobject.Object{Ptr: files.GoPointer()}).Unref()
		c.add(paths)
	})
	d.OpenMultiple(&c.win, nil, &c.w.h.onAsync, id)
}

// add attaches files, by path; folders and what cannot be read are said.
func (c *composer) add(paths []string) {
	var bad []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			bad = append(bad, filepath.Base(p))
			continue
		}
		if !slices.Contains(c.d.Attachments, p) {
			c.d.Attachments = append(c.d.Attachments, p)
		}
	}
	c.drawFiles()
	c.changed()
	if len(bad) > 0 {
		c.say(i18n.Tf("Not attached (a folder, or not readable): %s", strings.Join(bad, ", ")), true)
	}
}

func (c *composer) drawFiles() {
	c.attachments.RemoveAll()
	var total int64
	for _, p := range c.d.Attachments {
		chip := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
		chip.AddCssClass("mail-chip")
		var sz int64
		if info, err := os.Stat(p); err == nil {
			sz = info.Size()
		}
		total += sz
		l := gtk.NewLabel(filepath.Base(p))
		l.SetEllipsize(2)
		l.SetMaxWidthChars(28)
		gtkx.Append(chip, &l.Widget)
		s := gtk.NewLabel(size(sz))
		s.AddCssClass("mail-file-size")
		gtkx.Append(chip, &s.Widget)
		rm := gtk.NewButtonWithLabel("×")
		rm.AddCssClass("mail-chip-remove")
		rm.SetTooltipText(i18n.T("Remove"))
		pp := p
		c.ptrs = append(c.ptrs, c.w.h.click(rm, func() {
			c.d.Attachments = slices.DeleteFunc(c.d.Attachments, func(x string) bool { return x == pp })
			c.drawFiles()
			c.changed()
		}))
		gtkx.Append(chip, &rm.Widget)
		c.attachments.Append(&chip.Widget)
	}
	c.attachments.SetVisible(len(c.d.Attachments) > 0)
	if total > 20<<20 {
		c.say(i18n.Tf("The files come to %s; many servers refuse more than 20 MB.", size(total)), true)
	}
}

// --- sending ---

// sendOrUndo sends, after the seconds (settings) the user can take it back in; pressed
// again in those seconds, it takes it back.
func (c *composer) sendOrUndo() {
	if c.sending {
		if c.undo != nil {
			c.undo()
		}
		return
	}
	if c.ai.phase != aiIdle {
		c.say(i18n.T("Keep or discard AI's suggestion first."), true)
		return
	}
	c.read()
	x := c.w.acct(c.d.Account)
	if x == nil {
		c.say(i18n.T("Choose the account it is sent from."), true)
		return
	}
	if _, err := mail.Build(c.d, x.s.Acc, time.Now()); err != nil {
		if errors.Is(err, mail.ErrNoRecipient) {
			c.say(i18n.T("Add who it goes to."), true)
			c.to.GrabFocus()
		} else {
			c.say(err.Error(), true)
		}
		return
	}
	if strings.TrimSpace(c.d.Subject) == "" && strings.TrimSpace(c.d.Body) == "" {
		c.say(i18n.T("The message is empty."), true)
		return
	}
	c.save()
	c.sending = true
	c.fields(false)
	ctx, cancel := context.WithCancel(c.w.ctx)
	c.undo = cancel
	c.send.SetLabel(i18n.T("Undo"))
	c.send.RemoveCssClass("suggested-action")
	c.send.AddCssClass("outlined")
	left := c.w.settings.UndoSeconds()
	var count func()
	count = func() {
		if ctx.Err() != nil {
			c.sending = false
			c.fields(true)
			c.resetSend()
			c.say(i18n.T("Not sent. The draft is kept."), false)
			return
		}
		if left > 0 {
			c.say(i18n.Nf("Sending in %d second…", "Sending in %d seconds…", left, left), false)
			left--
			c.w.h.later(1000, count)
			return
		}
		c.say(i18n.T("Sending…"), false)
		c.send.SetSensitive(false)
		d := c.d
		go func() {
			b, err := x.s.Send(context.WithoutCancel(ctx), d)
			gtkx.Post(func() { c.sent(x, d, b, err) })
		}()
	}
	count()
}

func (c *composer) resetSend() {
	c.send.SetSensitive(true)
	c.send.SetLabel(i18n.T("Send"))
	c.send.RemoveCssClass("outlined")
	c.send.AddCssClass("suggested-action")
}

// sent closes the composer, or says why it was not sent.
func (c *composer) sent(x *acct, d mail.Draft, b mail.Built, err error) {
	c.sending = false
	c.undo = nil
	if err != nil && !errors.Is(err, mail.ErrNotSaved) {
		c.fields(true)
		c.resetSend()
		c.say(i18n.Tf("Not sent: %v", err), true)
		return
	}
	c.w.drafts.Delete(d.ID)
	c.sentDone = true
	if c.d.Server != 0 { // the latest copy: one may have been kept while the send waited
		go x.s.DeleteDraft(c.w.ctx, c.d.Server)
	}
	if d.InReplyTo != "" {
		go x.s.Answered(c.w.ctx, d.InReplyTo)
	}
	if sent := x.s.Role(mail.RoleSent); sent != "" {
		c.w.syncBox(x, sent)
	}
	if err != nil {
		c.w.actErr = i18n.Tf("Sent, but not saved in Sent: %v", err)
	}
	c.closed = true
	c.w.h.forget(c.ptrs)
	c.w.h.forget(c.ai.listPtrs)
	c.w.composers = slices.DeleteFunc(c.w.composers, func(y *composer) bool { return y == c })
	c.win.Destroy()
	c.w.drawSide()
}

// fields makes the fields editable or not, while a send waits.
func (c *composer) fields(on bool) {
	for _, wd := range []*gtk.Widget{&c.to.Widget, &c.cc.Widget, &c.bcc.Widget, &c.subject.Widget, &c.from.Widget, &c.attach.Widget, &c.aiToggle.Widget} {
		wd.SetSensitive(on)
	}
	c.body.SetEditable(on)
}

// splitDraft cuts a draft's text into what the user writes, the
// signature, and the quote of what it answers.
func splitDraft(text string) (written, sig, quote string) {
	written = text
	// The quote: from the line "On …, … wrote:" followed by "> " lines.
	lines := strings.Split(written, "\n")
	for i, l := range lines {
		if strings.HasSuffix(strings.TrimSpace(l), ":") && i+1 < len(lines) && strings.HasPrefix(lines[i+1], ">") {
			quote = strings.Join(lines[i:], "\n")
			written = strings.Join(lines[:i], "\n")
			break
		}
		if strings.HasPrefix(l, "---------- ") { // a forward
			quote = strings.Join(lines[i:], "\n")
			written = strings.Join(lines[:i], "\n")
			break
		}
	}
	if i := strings.LastIndex(written, "\n-- \n"); i >= 0 {
		sig = strings.TrimSpace(written[i+1:])
		written = written[:i]
	} else if strings.HasPrefix(written, "-- \n") {
		sig, written = strings.TrimSpace(written), ""
	}
	return strings.TrimSpace(written), sig, strings.TrimSpace(quote)
}

func splitText(text string) string { w, _, _ := splitDraft(text); return w }

func joinDraft(written, sig, quote string) string {
	parts := []string{written}
	if sig != "" {
		parts = append(parts, sig)
	}
	if quote != "" {
		parts = append(parts, quote)
	}
	return strings.Join(parts, "\n\n")
}

// voice is three of the user's recent messages kept from Sent, for the
// assistant to write as they do.
func (w *window) voice(x *acct) []string {
	sent := x.s.Role(mail.RoleSent)
	if sent == "" {
		return nil
	}
	var out []string
	for _, it := range x.s.Items(sent) {
		raw, ok := x.s.Raw(sent, it.UID)
		if !ok {
			continue
		}
		m, err := mail.Parse(raw)
		if err != nil {
			continue
		}
		if t := splitText(mail.SaidOf(m).Text); len(t) > 40 {
			out = append(out, t)
		}
		if len(out) == 3 {
			break
		}
	}
	return out
}

// --- suggestions ---

// contacts are the people of every account's mail kept and of its
// address book, for the composer's suggestions.
func (w *window) contacts() []mail.Contact {
	var items []mail.Item
	var book []mail.Address
	for _, x := range w.accts {
		for _, r := range []mail.Role{mail.RoleInbox, mail.RoleSent, mail.RoleArchive} {
			if name := x.s.Role(r); name != "" {
				items = append(items, x.s.Items(name)...)
			}
		}
		book = append(book, x.book...)
	}
	return mail.WithBook(mail.Contacts(items, w.mine, time.Now()), book, w.mine)
}

// readBook gives the composer an account's address book: the cache's at
// once, then the server's. An account with none, or signed in before
// inua asked for the contacts, goes without.
func (w *window) readBook(x *acct) {
	go func() {
		if b := x.s.CachedBook(); len(b) > 0 {
			gtkx.Post(func() { x.book = b; w.bookChanged() })
		}
		b, err := x.s.SyncBook(w.ctx)
		switch {
		case err == nil:
			gtkx.Post(func() { x.book = b; w.bookChanged() })
		case !errors.Is(err, mail.ErrNoBook) && !errors.Is(err, mail.ErrSignInAgain) && w.ctx.Err() == nil:
			log.Printf("mail: %s's address book: %v", x.s.Acc.ID, err)
		}
	}()
}

// typedPart is the address being typed in a field: after the last comma.
func typedPart(s string) string {
	if i := strings.LastIndexAny(s, ",;"); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(s)
}

// suggest shows, under a field's row, the people whose name or address
// starts with what is being typed.
func (c *composer) suggest(e *gtk.Entry, row *gtk.Box) {
	if c.filling {
		return
	}
	typed := typedPart(e.GetText())
	c.sugs = nil
	if len(typed) >= 1 { // typing changed it: c.filling guards the code's changes
		for _, s := range mail.Suggest(c.contacts, typed, 6) {
			if !strings.EqualFold(s.Addr, typed) {
				c.sugs = append(c.sugs, s)
			}
		}
	}
	c.w.h.forget(c.sugPtrs)
	c.sugPtrs = nil
	gtkx.Clear(c.sug)
	if len(c.sugs) == 0 {
		c.sug.SetVisible(false)
		return
	}
	c.sugFor = e
	c.fieldBox.ReorderChildAfter(&c.sug.Widget, &row.Widget)
	for i, s := range c.sugs {
		b := gtk.NewButton()
		b.AddCssClass("mail-suggestion")
		if i == 0 {
			b.AddCssClass("first")
		}
		box := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		name := gtk.NewLabel(s.Short())
		name.AddCssClass("mail-suggestion-name")
		name.SetXalign(0)
		gtkx.Append(box, &name.Widget)
		if s.Name != "" {
			addr := gtk.NewLabel(s.Addr)
			addr.AddCssClass("mail-suggestion-addr")
			addr.SetXalign(0)
			gtkx.Append(box, &addr.Widget)
		}
		b.SetChild(&box.Widget)
		s := s
		c.sugPtrs = append(c.sugPtrs, c.w.h.click(b, func() { c.pick(s) }))
		gtkx.Append(c.sug, &b.Widget)
	}
	c.sug.SetVisible(true)
}

// pick puts a suggestion in the field, in the place of what was typed.
func (c *composer) pick(s mail.Contact) {
	e := c.sugFor
	if e == nil {
		return
	}
	text := e.GetText()
	head := ""
	if i := strings.LastIndexAny(text, ",;"); i >= 0 {
		head = strings.TrimSpace(text[:i+1]) + " "
	}
	c.sug.SetVisible(false)
	c.sugs = nil
	e.SetText(head + s.String() + ", ")
	e.GrabFocus()
	e.SetPosition(-1)
}

// suggestKey takes the field's keys while suggestions show: Tab or Enter
// takes the first, Down goes into them, Escape closes them.
func (c *composer) suggestKey(e *gtk.Entry, keyval uint32) bool {
	if !c.sug.GetVisible() || c.sugFor != e || len(c.sugs) == 0 {
		return false
	}
	switch keyval {
	case uint32(gdk.KEY_Tab), uint32(gdk.KEY_Return), uint32(gdk.KEY_KP_Enter):
		c.pick(c.sugs[0])
	case uint32(gdk.KEY_Down):
		if first := c.sug.GetFirstChild(); first != nil {
			first.GrabFocus()
		}
	case uint32(gdk.KEY_Escape):
		c.sug.SetVisible(false)
	default:
		return false
	}
	return true
}
