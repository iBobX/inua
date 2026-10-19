package mailui

import (
	"context"
	"errors"
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
	draftWhat                      gtk.Entry
	ccRow, bccRow                  gtk.Box
	body                           gtk.TextView
	buf                            *gtk.TextBuffer
	attachments                    gtk.FlowBox
	showCc, attach, send, draftBtn gtk.Button
	undoAI                         gtk.Button
	polish                         gtk.MenuButton
	popover                        *gtk.Popover
	status, aiNote                 gtk.Label

	d        mail.Draft
	conv     []mail.Said // the conversation a reply answers, for the assistant
	accounts []string    // the From list's account IDs
	ptrs     []uintptr   // its registrations in the hub
	dirty    bool        // changed since saved
	saving   bool        // a save is due
	before   string      // the text before the assistant changed it
	asking   bool
	sending  bool
	undo     context.CancelFunc // stops a send in its 5 seconds
	filling  bool               // the fields are being set from the draft
	closed   bool
}

// newComposer opens a composer on a draft; conv is the conversation it
// answers, for the assistant's drafting.
func (w *window) newComposer(d mail.Draft, conv []mail.Said) *composer {
	if len(w.accts) == 0 {
		w.showAccounts("", nil)
		return nil
	}
	// A draft open already comes forward.
	for _, c := range w.composers {
		if d.ID != "" && c.d.ID == d.ID {
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
		"from": &c.from, "to": &c.to, "cc": &c.cc, "bcc": &c.bcc, "subject": &c.subject, "draft_what": &c.draftWhat,
		"cc_row": &c.ccRow, "bcc_row": &c.bccRow, "body": &c.body, "attachments": &c.attachments,
		"show_cc": &c.showCc, "attach": &c.attach, "send": &c.send, "draft": &c.draftBtn, "undo_ai": &c.undoAI,
		"polish": &c.polish, "status": &c.status, "ai_note": &c.aiNote,
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
		h.click(&c.draftBtn, c.draftNow),
		h.click(&c.undoAI, c.undoAssistant),
		h.enter(&c.draftWhat, c.draftNow),
		h.edit(&c.to, c.changed), h.edit(&c.cc, c.changed), h.edit(&c.bcc, c.changed), h.edit(&c.subject, c.changed),
		h.edit(c.buf, c.changed),
		h.close(&c.win, c.closeRequested),
		h.key(&c.win.Widget, true, c.key),
		h.drop(&c.win.Widget, c.add),
		h.notify(&c.from, "selected", c.fromChanged),
	)
	c.buildPolish()
	w.composers = append(w.composers, c)
	c.win.Present()
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
	c.dirty = true
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

func (c *composer) say(text string, bad bool) {
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
	switch {
	case ctrl && (keyval == uint32(gdk.KEY_Return) || keyval == uint32(gdk.KEY_KP_Enter)):
		c.sendOrUndo()
	case keyval == uint32(gdk.KEY_Escape) && c.popover != nil && c.popover.GetVisible():
		c.popover.Popdown()
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
	c.close()
	return false
}

func (c *composer) close() {
	c.closed = true
	if c.undo != nil {
		c.undo()
	}
	c.w.h.forget(c.ptrs)
	c.w.composers = slices.DeleteFunc(c.w.composers, func(x *composer) bool { return x == c })
	c.w.drawSide() // Unsent
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

// sendOrUndo sends, after 5 seconds the user can take it back in; pressed
// again in those seconds, it takes it back.
func (c *composer) sendOrUndo() {
	if c.sending {
		if c.undo != nil {
			c.undo()
		}
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
	left := 5
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
	c.w.composers = slices.DeleteFunc(c.w.composers, func(y *composer) bool { return y == c })
	c.win.Destroy()
	c.w.drawSide()
}

// fields makes the fields editable or not, while a send waits.
func (c *composer) fields(on bool) {
	for _, wd := range []*gtk.Widget{&c.to.Widget, &c.cc.Widget, &c.bcc.Widget, &c.subject.Widget, &c.from.Widget, &c.attach.Widget, &c.draftWhat.Widget, &c.draftBtn.Widget, &c.polish.Widget} {
		wd.SetSensitive(on)
	}
	c.body.SetEditable(on)
}

// --- the assistant ---

func (c *composer) buildPolish() {
	c.popover = gtk.NewPopover()
	c.popover.AddCssClass("mail-menu")
	box := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	for _, p := range []struct {
		label string
		how   mail.Polish
		to    string
	}{
		{i18n.T("Shorter"), mail.Shorter, ""},
		{i18n.T("Friendlier"), mail.Friendlier, ""},
		{i18n.T("More formal"), mail.Formal, ""},
		{i18n.T("Fix spelling and grammar"), mail.Fix, ""},
		{"English", mail.Translate, "en"},
		{"中文", mail.Translate, "zh"},
		{"日本語", mail.Translate, "ja"},
		{"한국어", mail.Translate, "ko"},
		{"Español", mail.Translate, "es"},
		{"Français", mail.Translate, "fr"},
		{"Deutsch", mail.Translate, "de"},
	} {
		label := p.label
		if p.how == mail.Translate {
			label = i18n.Tf("Translate into %s", p.label)
		}
		b := gtk.NewButtonWithLabel(label)
		b.AddCssClass("mail-menu-item")
		pp := p
		c.ptrs = append(c.ptrs, c.w.h.click(b, func() { c.popover.Popdown(); c.polishWith(pp.how, pp.to) }))
		gtkx.Append(box, &b.Widget)
	}
	c.popover.SetChild(&box.Widget)
	c.polish.SetPopover(c.popover)
}

// draftNow asks the assistant for the text from the line of what to say.
func (c *composer) draftNow() {
	what := strings.TrimSpace(c.draftWhat.GetText())
	if what == "" {
		c.draftWhat.GrabFocus()
		c.note(i18n.T("Write a line of what to say, then Draft."), false)
		return
	}
	user := ""
	var examples []string
	if x := c.w.acct(c.d.Account); x != nil {
		user = x.s.Acc.Name
		if user == "" {
			user = x.s.Acc.Address
		}
		examples = c.w.voice(x)
	}
	prompt := mail.DraftPrompt(what, c.conv, examples, user, i18n.Lang())
	c.rewrite(prompt, true)
}

func (c *composer) polishWith(how mail.Polish, to string) {
	text := splitText(c.text())
	if text == "" {
		c.note(i18n.T("Write something first, or Draft it."), false)
		return
	}
	c.rewrite(mail.PolishPrompt(text, how, to), false)
}

// rewrite asks the assistant and puts its answer in place of what the user
// wrote, keeping the signature and the quote under it; Undo puts it back.
func (c *composer) rewrite(prompt string, drafting bool) {
	if c.asking {
		return
	}
	c.asking = true
	c.before = c.text()
	_, sig, quote := splitDraft(c.before)
	c.draftBtn.SetSensitive(false)
	c.polish.SetSensitive(false)
	c.note(i18n.T("The assistant is writing…"), false)
	put := func(t string) {
		if c.closed {
			return
		}
		c.filling = true
		c.buf.SetText(joinDraft(strings.TrimSpace(t), sig, quote), -1)
		c.filling = false
	}
	go func() {
		text, err := c.w.ai.ask(prompt, 2*time.Minute, put)
		gtkx.Post(func() {
			if c.closed {
				return
			}
			c.asking = false
			c.draftBtn.SetSensitive(true)
			c.polish.SetSensitive(true)
			if err != nil {
				c.filling = true
				c.buf.SetText(c.before, -1)
				c.filling = false
				c.note(failed(i18n.T("The assistant could not write it"), err), true)
				return
			}
			put(text)
			c.undoAI.SetVisible(true)
			if drafting {
				c.draftWhat.SetText("")
			}
			c.note(i18n.T("Written by the assistant: read it before you send."), false)
			c.changed()
		})
	}()
}

func (c *composer) undoAssistant() {
	c.buf.SetText(c.before, -1)
	c.undoAI.SetVisible(false)
	c.note("", false)
	c.changed()
}

func (c *composer) note(text string, bad bool) {
	c.aiNote.SetText(text)
	c.aiNote.SetVisible(text != "")
	setClass(&c.aiNote.Widget, "error", bad)
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
