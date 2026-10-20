package mailui

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/mail"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// openConversation shows a conversation: what is kept at once, the rest
// read from the server; the messages not read are marked read.
func (w *window) openConversation(c *mail.Conversation) {
	w.open, w.openID, w.actErr = c, c.ID, ""
	w.dropWebs(func(k mail.Key) bool {
		return slices.ContainsFunc(c.Messages, func(m mail.Item) bool { return m.Key() == k })
	})
	w.lastDrawn = ""
	w.readerStack.SetVisibleChildName("read")
	w.readerScroll.GetVadjustment().SetValue(0)
	if !w.settings.Reading.KeepUnread {
		w.markRead(c)
	}
	w.drawReader()
}

// load reads a message of the open conversation, unless it is read.
func (w *window) load(it mail.Item) {
	k := it.Key()
	if rd := w.reading[k]; rd != nil && (rd.m != nil || rd.loading) {
		return
	}
	x := w.acct(it.Account)
	if x == nil {
		return
	}
	rd := &reading{loading: true}
	w.reading[k] = rd
	go func() {
		m, err := x.s.Message(w.ctx, it.Mailbox, it.UID)
		var j mail.Judgment
		if err == nil && !w.mine(m.From.Addr) {
			j = mail.Judge(m, w.knownSender)
		}
		gtkx.Post(func() {
			rd.loading = false
			if err != nil {
				rd.err = i18n.Tf("Could not read this message: %v", err)
			} else {
				rd.m, rd.judged = m, j
			}
			if w.open != nil && w.open.ID == w.openID {
				w.drawReader()
			}
			for _, r := range w.rows {
				w.fillRow(r) // a suspicious mark
			}
		})
	}()
}

// knownSender says whether the user has written to an address, from the
// Sent mailboxes' indexes. It runs off the main thread.
func (w *window) knownSender(addr string) bool {
	addr = strings.ToLower(addr)
	ok, _ := gtkx.Call(2*time.Second, func() bool {
		for _, x := range w.accts {
			sent := x.s.Role(mail.RoleSent)
			if sent == "" {
				continue
			}
			for _, it := range x.s.Items(sent) {
				for _, a := range append(append([]mail.Address{}, it.To...), it.Cc...) {
					if strings.ToLower(a.Addr) == addr {
						return true
					}
				}
			}
		}
		return false
	})
	return ok
}

// markRead marks a conversation's unread messages read, on the server too.
func (w *window) markRead(c *mail.Conversation) {
	byBox := map[[2]string][]uint32{}
	for _, m := range c.Messages {
		if !m.Has(mail.Seen) {
			k := [2]string{m.Account, m.Mailbox}
			byBox[k] = append(byBox[k], m.UID)
		}
	}
	for k, uids := range byBox {
		x := w.acct(k[0])
		if x == nil {
			continue
		}
		mailbox := k[1]
		go func() {
			err := x.s.SetFlag(w.ctx, mailbox, uids, mail.Seen, true)
			gtkx.Post(func() {
				if err != nil {
					w.actErr = i18n.Tf("Could not mark it read: %v", err)
				}
				w.counted(x, mailbox)
				w.drawSide()
				w.refresh()
			})
		}()
	}
}

// signature sums up what the reader shows, so a refresh that changed
// nothing in it does not draw it again (and lose the scroll and a
// selection).
func (w *window) signature() string {
	var b strings.Builder
	c := w.open
	fmt.Fprintf(&b, "%s|%s|", c.ID, w.actErr)
	for _, m := range c.Messages {
		k := m.Key()
		rd := w.reading[k]
		fmt.Fprintf(&b, "%v:%v:%v:%v:%v:%v;", k, m.Flags, rd != nil && rd.m != nil, rd != nil && rd.err != "", w.folded[k], w.quotes[k])
		fmt.Fprintf(&b, "%v%v%v", w.checks[k], w.judged[k] == nil, w.answers[k].sig())
		if rd != nil && rd.m != nil {
			for _, a := range rd.m.Shown() {
				b.WriteString(w.fileSums[fileKey(m, a)].sig())
			}
		}
	}
	fmt.Fprintf(&b, "|%v|%s", w.assistOpen, w.quick[c.ID].sig()) // the assistant's answers are the panel's
	return b.String()
}

// drawReader makes the conversation's view again: its actions in the bar
// over it, then its subject, a warning when it needs one, the assistant,
// the messages and the reply field.
func (w *window) drawReader() {
	c := w.open
	if c == nil {
		return
	}
	for _, m := range c.Messages {
		w.load(m) // one that joined it since it opened (a reply sent, new mail)
	}
	if sig := w.signature(); sig == w.lastDrawn {
		return
	} else {
		w.lastDrawn = sig
	}
	w.h.forget(w.readerBtns)
	w.readerBtns = nil
	gtkx.Clear(&w.reader)
	w.drawBar(c)

	head := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	head.AddCssClass("mail-read-head")
	subject := c.Subject()
	if subject == "" {
		subject = i18n.T("(no subject)")
	}
	t := gtk.NewLabel(subject)
	t.AddCssClass("mail-read-subject")
	t.SetXalign(0)
	t.SetWrap(true)
	t.SetWrapMode(2)
	t.SetSelectable(true)
	gtkx.Append(head, &t.Widget)
	gtkx.Append(&w.reader, &head.Widget)
	if w.actErr != "" {
		e := gtk.NewLabel(w.actErr)
		e.AddCssClass("mail-error")
		e.SetXalign(0)
		e.SetWrap(true)
		gtkx.Append(&w.reader, &e.Widget)
	}
	w.drawWarning(c)
	last := len(c.Messages) - 1
	for i, m := range c.Messages {
		w.drawMessage(m, i == last)
	}
	if !w.isDraft(c.Latest()) {
		w.drawQuick(c)
	}
	w.drawPanel()
}

// tool adds an icon button to the reader's bar.
func (w *window) tool(box *gtk.Box, icon, tip string, f func()) *gtk.Button {
	b := gtk.NewButtonFromIconName(icon)
	b.AddCssClass("mail-tool")
	b.SetTooltipText(tip)
	w.readerBtns = append(w.readerBtns, w.h.click(b, f))
	gtkx.Append(box, &b.Widget)
	return b
}

// drawBar fills the bar over the conversation: answering it on the left,
// filing it on the right, and a menu of the rest; a draft's own on a
// draft.
func (w *window) drawBar(c *mail.Conversation) {
	bar := &w.readerBar
	gtkx.Clear(bar)
	if w.isDraft(c.Latest()) {
		b := gtk.NewButtonWithLabel(i18n.T("Continue writing"))
		b.AddCssClass("suggested-action")
		b.AddCssClass("mail-bar-button")
		w.readerBtns = append(w.readerBtns, w.h.click(b, w.continueDraft))
		gtkx.Append(bar, &b.Widget)
		gap := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		gap.SetHexpand(true)
		gtkx.Append(bar, &gap.Widget)
		w.tool(bar, "user-trash-symbolic", i18n.T("Discard the draft"), w.trash)
		ai := w.tool(bar, "applications-science-symbolic", i18n.T("Assistant"), w.toggleAssist)
		if w.assistOpen {
			ai.AddCssClass("on")
		}
		return
	}
	w.tool(bar, "mail-reply-symbolic", i18n.T("Reply (R)"), func() { w.reply(false) })
	w.tool(bar, "mail-reply-all-symbolic", i18n.T("Reply all (A)"), func() { w.reply(true) })
	w.tool(bar, "mail-forward-symbolic", i18n.T("Forward (F)"), w.forward)
	gap := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	gap.SetHexpand(true)
	gtkx.Append(bar, &gap.Widget)
	latest := c.Latest()
	x := w.acct(latest.Account)
	w.tool(bar, "mail-archive-symbolic", i18n.T("Archive (E)"), w.archive)
	w.tool(bar, "user-trash-symbolic", i18n.T("Move to Trash (#)"), w.trash)
	if x != nil && x.s.Role(mail.RoleJunk) != "" && latest.Mailbox != x.s.Role(mail.RoleJunk) {
		w.tool(bar, "mail-mark-junk-symbolic", i18n.T("Move to Spam (!)"), w.spam)
	}
	w.tool(bar, "mail-move-symbolic", i18n.T("Move to a folder (V)"), w.moveTo)
	starred := slices.ContainsFunc(c.Messages, func(m mail.Item) bool { return m.Has(mail.Flagged) })
	star := w.tool(bar, "mail-starred-symbolic", i18n.T("Star (S)"), w.star)
	if starred {
		star.AddCssClass("on")
		star.SetTooltipText(i18n.T("Unstar (S)"))
	}
	w.tool(bar, "mail-unread-symbolic", i18n.T("Mark as unread (U)"), w.markUnread)
	ai := w.tool(bar, "applications-science-symbolic", i18n.T("Assistant"), w.toggleAssist)
	if w.assistOpen {
		ai.AddCssClass("on")
	}

	more := gtk.NewMenuButton()
	more.SetIconName("view-more-symbolic")
	more.AddCssClass("mail-tool")
	more.SetTooltipText(i18n.T("More"))
	pop := gtk.NewPopover()
	pop.AddCssClass("mail-menu")
	box := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	item := func(label string, f func()) {
		b := gtk.NewButtonWithLabel(label)
		b.AddCssClass("mail-menu-item")
		if l := b.GetChild(); l != nil {
			(&gtk.Label{Widget: *l}).SetXalign(0)
		}
		w.readerBtns = append(w.readerBtns, w.h.click(b, func() { pop.Popdown(); f() }))
		gtkx.Append(box, &b.Widget)
	}
	// The view of the newest message that has HTML.
	for i := len(c.Messages) - 1; i >= 0; i-- {
		it := c.Messages[i]
		rd := w.reading[it.Key()]
		if rd == nil || rd.m == nil || rd.m.HTML == "" {
			continue
		}
		k, m := it.Key(), rd.m
		label := i18n.T("Show as text")
		if !w.formatted(k, m) {
			label = i18n.T("Show formatted")
		}
		item(label, func() { w.flipped[k] = !w.flipped[k]; w.lastDrawn = ""; w.drawReader() })
		item(i18n.T("Open in browser"), func() { w.openHTML(m) })
		break
	}
	item(i18n.T("Keys"), w.showKeys)
	pop.SetChild(&box.Widget)
	more.SetPopover(pop)
	gtkx.Append(bar, &more.Widget)
}

// chip adds a small rounded button.
func (w *window) chip(box *gtk.Box, icon, label string, f func()) *gtk.Button {
	b := gtk.NewButton()
	b.AddCssClass("mail-chip")
	in := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
	if icon != "" {
		gtkx.Append(in, &gtk.NewImageFromIconName(icon).Widget)
	}
	gtkx.Append(in, &gtk.NewLabel(label).Widget)
	b.SetChild(&in.Widget)
	if f != nil {
		w.readerBtns = append(w.readerBtns, w.h.click(b, f))
	} else {
		b.SetSensitive(false)
	}
	gtkx.Append(box, &b.Widget)
	return b
}

// drawWarning shows the phishing route's verdict for the newest message
// that is not fine: a banner of one line, its checks folded behind it.
func (w *window) drawWarning(c *mail.Conversation) {
	var k mail.Key
	var rd *reading
	for i := len(c.Messages) - 1; i >= 0; i-- {
		kk := c.Messages[i].Key()
		if r := w.reading[kk]; r != nil && r.m != nil && r.judged.Verdict != mail.Fine {
			k, rd = kk, r
			break
		}
	}
	if rd == nil {
		return
	}
	j := rd.judged
	box := gtk.NewBox(gtk.OrientationVerticalValue, 8)
	box.AddCssClass("mail-warning")
	box.AddCssClass(j.Verdict.ID())
	line := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	icon := gtk.NewImageFromIconName("dialog-warning-symbolic")
	icon.AddCssClass("mail-warning-icon")
	icon.SetValign(gtk.AlignStartValue)
	gtkx.Append(line, &icon.Widget)
	text := gtk.NewLabel("")
	text.AddCssClass("mail-warning-text")
	text.SetMarkup("<b>" + gtkEscape(j.Verdict.Name()) + "</b>  " + gtkEscape(j.Worst().Found))
	text.SetXalign(0)
	text.SetWrap(true)
	text.SetHexpand(true)
	gtkx.Append(line, &text.Widget)
	gtkx.Append(box, &line.Widget)
	if w.checks[k] {
		steps := gtk.NewBox(gtk.OrientationVerticalValue, 4)
		steps.AddCssClass("mail-steps")
		for _, s := range j.Steps {
			row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
			m := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
			m.AddCssClass("mark")
			if c := map[mail.Level]string{mail.LevelGood: "fine", mail.LevelWarn: "careful", mail.LevelBad: "suspicious"}[s.Level]; c != "" {
				m.AddCssClass(c)
			}
			m.SetValign(gtk.AlignStartValue)
			gtkx.Append(row, &m.Widget)
			l := gtk.NewLabel(s.Check + " " + s.Found)
			l.AddCssClass("mail-step")
			l.SetXalign(0)
			l.SetWrap(true)
			gtkx.Append(row, &l.Widget)
			gtkx.Append(steps, &row.Widget)
		}
		gtkx.Append(box, &steps.Widget)
	}
	chips := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	chips.AddCssClass("mail-chips")
	more := i18n.Nf("Why: %d check", "Why: %d checks", len(j.Steps), len(j.Steps))
	if w.checks[k] {
		more = i18n.T("Hide the checks")
	}
	w.chip(chips, "", more, func() { w.checks[k] = !w.checks[k]; w.lastDrawn = ""; w.drawReader() })
	if x := w.acct(k.Account); x != nil && x.s.Role(mail.RoleJunk) != "" && k.Mailbox != x.s.Role(mail.RoleJunk) {
		w.chip(chips, "mail-mark-junk-symbolic", i18n.T("Move to Spam"), w.spam)
	}
	if a := w.judged[k]; a == nil || a.err != "" {
		w.chip(chips, "applications-science-symbolic", i18n.T("Ask the assistant"), func() { w.openAssist(func() { w.askJudge(k) }) })
	}
	gtkx.Append(box, &chips.Widget)
	gtkx.Append(&w.reader, &box.Widget)
}

// worthSummary: three messages or more, or a long one.
func (w *window) worthSummary(c *mail.Conversation) bool {
	if len(c.Messages) >= 3 {
		return true
	}
	for _, m := range c.Messages {
		if rd := w.reading[m.Key()]; rd != nil && rd.m != nil && len(rd.m.Text) > 1500 {
			return true
		}
	}
	return false
}

// aiLine shows an answer of the assistant's: a kicker, the text, the
// error, or that it is thinking.
func (w *window) aiLine(box *gtk.Box, a *aiText, kicker string) {
	k := gtk.NewLabel(kicker)
	k.AddCssClass("kicker")
	k.AddCssClass("mail-ai-kicker")
	k.SetXalign(0)
	gtkx.Append(box, &k.Widget)
	switch {
	case a.err != "":
		e := gtk.NewLabel(a.err)
		e.AddCssClass("mail-error")
		e.SetXalign(0)
		e.SetWrap(true)
		gtkx.Append(box, &e.Widget)
	case a.text == "" && a.busy:
		l := gtk.NewLabel(i18n.T("Thinking…"))
		l.AddCssClass("mail-ai-busy")
		l.SetXalign(0)
		gtkx.Append(box, &l.Widget)
	default:
		l := gtk.NewLabel(a.text)
		l.AddCssClass("mail-ai-text")
		l.SetXalign(0)
		l.SetWrap(true)
		l.SetSelectable(true)
		gtkx.Append(box, &l.Widget)
	}
}

// link adds a plain text button.
func (w *window) link(box *gtk.Box, text string, f func()) {
	b := gtk.NewButtonWithLabel(text)
	b.AddCssClass("mail-link")
	w.readerBtns = append(w.readerBtns, w.h.click(b, f))
	gtkx.Append(box, &b.Widget)
}

// senderMark is a sender's initial on a tinted square.
func senderMark(a mail.Address, size int32) *gtk.Widget {
	name := a.Name
	if name == "" {
		name = a.Addr
	}
	l := gtk.NewLabel(initial(name))
	l.AddCssClass("mail-sender-mark")
	l.SetSizeRequest(size, size)
	l.SetValign(gtk.AlignStartValue)
	return &l.Widget
}

// drawMessage shows a message: open (the newest, or one unfolded) or
// folded to its sender and first line.
func (w *window) drawMessage(it mail.Item, newest bool) {
	k := it.Key()
	rd := w.reading[k]
	open := newest || w.folded[k] // folded holds "unfolded by the user" for older ones
	if !newest && !it.Has(mail.Seen) {
		open = true
	}
	box := gtk.NewBox(gtk.OrientationVerticalValue, 14)
	box.AddCssClass("mail-message")
	if !open {
		box.AddCssClass("folded")
	}
	head := gtk.NewButton()
	head.AddCssClass("mail-message-head")
	hb := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	date := gtk.NewLabel(i18n.Time(it.Date.Local(), "Mon Jan 2, 15:04"))
	date.AddCssClass("mail-date")
	date.SetValign(gtk.AlignStartValue)
	if open {
		gtkx.Append(hb, senderMark(it.From, 34))
		who := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		who.SetHexpand(true)
		top := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
		from := gtk.NewLabel(it.From.Short())
		from.AddCssClass("mail-from")
		gtkx.Append(top, &from.Widget) // whole: the address gives way first
		if it.From.Name != "" {
			addr := gtk.NewLabel(it.From.Addr)
			addr.AddCssClass("mail-from-addr")
			addr.SetEllipsize(3)
			addr.SetXalign(0)
			gtkx.Append(top, &addr.Widget)
		}
		gtkx.Append(who, &top.Widget)
		to := gtk.NewLabel(w.toLine(it))
		to.AddCssClass("mail-to")
		to.SetXalign(0)
		to.SetEllipsize(3)
		gtkx.Append(who, &to.Widget)
		gtkx.Append(hb, &who.Widget)
	} else {
		gtkx.Append(hb, senderMark(it.From, 24))
		from := gtk.NewLabel(it.From.Short())
		from.AddCssClass("mail-from")
		from.SetValign(gtk.AlignCenterValue)
		gtkx.Append(hb, &from.Widget)
		snip := gtk.NewLabel(it.Snippet)
		snip.AddCssClass("mail-to")
		snip.SetXalign(0)
		snip.SetHexpand(true)
		snip.SetEllipsize(3)
		snip.SetValign(gtk.AlignCenterValue)
		gtkx.Append(hb, &snip.Widget)
		if it.Attach {
			clip := gtk.NewLabel(i18n.T("FILE"))
			clip.AddCssClass("mail-clip")
			clip.SetValign(gtk.AlignCenterValue)
			gtkx.Append(hb, &clip.Widget)
		}
		date.SetValign(gtk.AlignCenterValue)
	}
	gtkx.Append(hb, &date.Widget)
	head.SetChild(&hb.Widget)
	if !newest {
		head.SetTooltipText(i18n.T("Fold or unfold"))
		w.readerBtns = append(w.readerBtns, w.h.click(head, func() { w.folded[k] = !open; w.lastDrawn = ""; w.drawReader() }))
	} else {
		head.SetCanTarget(false)
	}
	gtkx.Append(box, &head.Widget)
	if !open {
		gtkx.Append(&w.reader, &box.Widget)
		return
	}
	switch {
	case rd == nil || rd.loading:
		l := gtk.NewLabel(i18n.T("Loading…"))
		l.AddCssClass("mail-loading")
		l.SetXalign(0)
		gtkx.Append(box, &l.Widget)
	case rd.err != "":
		e := gtk.NewLabel(rd.err)
		e.AddCssClass("mail-error")
		e.SetXalign(0)
		e.SetWrap(true)
		gtkx.Append(box, &e.Widget)
	default:
		w.drawBody(box, it, rd.m)
	}
	gtkx.Append(&w.reader, &box.Widget)
}

func (w *window) toLine(it mail.Item) string {
	var names []string
	for _, a := range append(append([]mail.Address{}, it.To...), it.Cc...) {
		if w.mine(a.Addr) {
			names = append(names, i18n.T("me"))
		} else {
			names = append(names, firstName(a))
		}
		if len(names) == 6 {
			names = append(names, "…")
			break
		}
	}
	if len(names) == 0 {
		return ""
	}
	return i18n.Tf("to %s", strings.Join(names, ", "))
}

// drawBody shows a message's text (what it quotes folded), its
// attachments, and a note when the text was made from HTML.
func (w *window) drawBody(box *gtk.Box, it mail.Item, m *mail.Message) {
	k := it.Key()
	inv := m.Invitation()
	if inv != nil {
		w.drawInvite(box, it, inv)
	}
	formatted := w.formatted(k, m)
	written, rest := mail.Split(m.Text)
	if strings.TrimSpace(written) == "" {
		written, rest = m.Text, ""
	}
	switch {
	case formatted && webOK():
		rest = ""
		if w.webView(box, k, m) {
			rest = "-"
		}
	case formatted:
		rest = ""
		if w.drawFormatted(box, k, m) {
			rest = "-"
		}
	default:
		body := gtk.NewLabel("")
		body.AddCssClass("mail-text")
		body.SetMarkup(linkify(written))
		body.SetXalign(0)
		body.SetWrap(true)
		body.SetWrapMode(2) // word_char: long links
		body.SetSelectable(true)
		body.SetMaxWidthChars(84)
		gtkx.Append(box, &body.Widget)
	}
	links := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	links.AddCssClass("mail-chips")
	if rest != "" {
		tip := i18n.T("Show quoted text")
		if w.quotes[k] {
			tip = i18n.T("Hide quoted text")
		}
		w.chip(links, "", "•••", func() { w.quotes[k] = !w.quotes[k]; w.lastDrawn = ""; w.drawReader() }).SetTooltipText(tip)
	}
	if rest != "" && !formatted && w.quotes[k] {
		q := gtk.NewLabel("")
		q.AddCssClass("mail-quoted")
		q.SetMarkup(linkify(rest))
		q.SetXalign(0)
		q.SetWrap(true)
		q.SetWrapMode(2)
		q.SetSelectable(true)
		q.SetMaxWidthChars(84)
		gtkx.Append(box, &q.Widget)
	}
	shown := m.Shown()
	if inv != nil { // the card stands for its calendar file
		shown = slices.DeleteFunc(shown, func(a mail.Attachment) bool {
			return a.Type == "text/calendar" || a.Type == "application/ics" || strings.HasSuffix(strings.ToLower(a.Name), ".ics")
		})
	}
	if len(shown) > 0 {
		flow := gtk.NewFlowBox()
		flow.AddCssClass("mail-files")
		flow.SetSelectionMode(gtk.SelectionNoneValue)
		flow.SetMaxChildrenPerLine(3)
		flow.SetColumnSpacing(8)
		flow.SetRowSpacing(8)
		flow.SetHalign(gtk.AlignStartValue)
		for _, a := range shown {
			flow.Append(&w.fileCard(it, m, a).Widget)
		}
		gtkx.Append(box, &flow.Widget)
		for _, a := range shown {
			if s := w.fileSums[fileKey(it, a)]; s != nil {
				w.aiLine(box, s, i18n.Tf("SUMMARY · ASSISTANT · %s", a.Name))
			}
		}
	}
	if links.GetFirstChild() != nil {
		gtkx.Append(box, &links.Widget)
	}
}

// fileCard is an attachment: a picture's thumbnail or its type, its name
// and size; a click opens it, Save… keeps it.
func (w *window) fileCard(it mail.Item, m *mail.Message, a mail.Attachment) *gtk.Box {
	card := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	card.AddCssClass("mail-file")
	open := gtk.NewButton()
	open.AddCssClass("mail-file-open")
	open.SetTooltipText(i18n.Tf("Open %s", a.Name))
	ob := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	if a.Image() && a.Size() < 20<<20 {
		if tex, err := gdk.NewTextureFromBytes(glib.NewBytes(a.Data, uint(len(a.Data)))); err == nil {
			pic := gtk.NewPictureForPaintable(tex)
			pic.SetContentFit(gtk.ContentFitCoverValue)
			pic.SetCanShrink(true)
			pic.SetSizeRequest(44, 44)
			pic.SetValign(gtk.AlignCenterValue)
			pic.AddCssClass("mail-thumb")
			tex.Unref()
			gtkx.Append(ob, &pic.Widget)
		}
	} else {
		ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(a.Name), "."))
		if ext == "" || len(ext) > 4 {
			ext = i18n.T("FILE")
		}
		ic := gtk.NewLabel(ext)
		ic.AddCssClass("mail-file-type")
		gtkx.Append(ob, &ic.Widget)
	}
	names := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	n := gtk.NewLabel(a.Name)
	n.AddCssClass("mail-file-name")
	n.SetXalign(0)
	n.SetEllipsize(2)
	n.SetMaxWidthChars(26)
	gtkx.Append(names, &n.Widget)
	sz := gtk.NewLabel(size(a.Size()))
	sz.AddCssClass("mail-file-size")
	sz.SetXalign(0)
	gtkx.Append(names, &sz.Widget)
	gtkx.Append(ob, &names.Widget)
	open.SetChild(&ob.Widget)
	w.readerBtns = append(w.readerBtns, w.h.click(open, func() { w.openFile(a) }))
	gtkx.Append(card, &open.Widget)
	if a.HasText() {
		sum := gtk.NewButtonWithLabel(i18n.T("Sum up"))
		sum.AddCssClass("mail-file-save")
		sum.SetTooltipText(i18n.T("The assistant reads it and sums it up"))
		s := w.fileSums[fileKey(it, a)]
		sum.SetSensitive(s == nil || (!s.busy && s.err != ""))
		w.readerBtns = append(w.readerBtns, w.h.click(sum, func() { w.sumUpFile(it, m, a) }))
		gtkx.Append(card, &sum.Widget)
	}
	save := gtk.NewButtonWithLabel(i18n.T("Save…"))
	save.AddCssClass("mail-file-save")
	w.readerBtns = append(w.readerBtns, w.h.click(save, func() { w.saveFile(a) }))
	gtkx.Append(card, &save.Widget)
	return card
}

// size is a byte count in words.
func size(n int64) string {
	switch {
	case n < 1000:
		return i18n.Tf("%d bytes", n)
	case n < 1000*1000:
		return fmt.Sprintf("%.0f KB", float64(n)/1000)
	case n < 1000*1000*1000:
		return fmt.Sprintf("%.1f MB", float64(n)/1e6)
	}
	return fmt.Sprintf("%.1f GB", float64(n)/1e9)
}

// openedDir is where attachments open from: only the user can read it,
// and it is emptied when Mail quits.
func openedDir() string {
	run := os.Getenv("XDG_RUNTIME_DIR")
	if run == "" {
		run = os.TempDir()
	}
	return filepath.Join(run, "inua-mail")
}

func cleanOpened() { os.RemoveAll(openedDir()) }

// writeOpened writes data under a name in a folder of its own and
// returns its path.
func writeOpened(name string, data []byte) (string, error) {
	dir := filepath.Join(openedDir(), strings.ToLower(rand.Text()[:10]))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	p := filepath.Join(dir, mail.SafeName(name))
	return p, os.WriteFile(p, data, 0o600)
}

// openFile opens an attachment with the desktop's app for its type.
func (w *window) openFile(a mail.Attachment) {
	p, err := writeOpened(a.Name, a.Data)
	if err == nil {
		err = openPath(p)
	}
	if err != nil {
		w.actErr = i18n.Tf("Could not open %s: %v", a.Name, err)
		w.drawReader()
	}
}

// openPath opens a file: inua-viewer for what it shows, else xdg-open.
func openPath(p string) error {
	ext := strings.ToLower(filepath.Ext(p))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".pdf", ".md", ".txt":
		if bin, err := exec.LookPath("inua-viewer"); err == nil {
			return desktop.Spawn([]string{bin, p}, "")
		}
	}
	return desktop.Spawn([]string{"xdg-open", p}, "")
}

// openHTML opens a message's HTML in the browser.
func (w *window) openHTML(m *mail.Message) {
	html := m.HTML
	if html == "" {
		html = "<pre>" + gtkEscape(m.Text) + "</pre>"
	}
	p, err := writeOpened("message.html", []byte("<meta charset=\"utf-8\">"+html))
	if err == nil {
		err = desktop.Spawn([]string{"xdg-open", p}, "")
	}
	if err != nil {
		w.actErr = i18n.Tf("Could not open it in the browser: %v", err)
		w.drawReader()
	}
}

// saveFile asks where to keep an attachment, and writes it there.
func (w *window) saveFile(a mail.Attachment) {
	d := gtk.NewFileDialog()
	d.SetTitle(i18n.T("Save the attachment"))
	d.SetInitialName(mail.SafeName(a.Name))
	if home, err := os.UserHomeDir(); err == nil {
		f := gio.FileNewForPath(filepath.Join(home, "Downloads"))
		d.SetInitialFolder(f)
		(&gobject.Object{Ptr: f.GoPointer()}).Unref()
	}
	id := w.h.callback(func(result uintptr) {
		defer d.Unref()
		f, err := d.SaveFinish(&gio.AsyncResultBase{Ptr: result})
		if err != nil || f == nil {
			return // cancelled
		}
		p := f.GetPath()
		(&gobject.Object{Ptr: f.GoPointer()}).Unref()
		if p == "" {
			return
		}
		if err := os.WriteFile(p, a.Data, 0o644); err != nil {
			w.actErr = i18n.Tf("Could not save %s: %v", a.Name, err)
			w.drawReader()
		}
	})
	d.Save(&w.win.Window, nil, &w.h.onAsync, id)
}

var urlRe = regexp.MustCompile(`https?://[^\s<>"'()]+[^\s<>"'().,;:!?]`)

// linkify escapes text for a label's markup and makes its web addresses
// links.
func linkify(text string) string {
	var b strings.Builder
	last := 0
	for _, m := range urlRe.FindAllStringIndex(text, -1) {
		b.WriteString(gtkEscape(text[last:m[0]]))
		u := text[m[0]:m[1]]
		fmt.Fprintf(&b, `<a href="%s">%s</a>`, gtkEscape(u), gtkEscape(u))
		last = m[1]
	}
	b.WriteString(gtkEscape(text[last:]))
	return b.String()
}

func gtkEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;").Replace(s)
}

// isDraft says whether a message is in its account's Drafts folder.
func (w *window) isDraft(it mail.Item) bool {
	x := w.acct(it.Account)
	return x != nil && it.Mailbox == x.s.Role(mail.RoleDrafts)
}

// continueDraft opens the draft chosen in the composer, its copy in Drafts
// replaced as it is written.
func (w *window) continueDraft() {
	if w.open == nil {
		return
	}
	it := w.open.Latest()
	rd := w.reading[it.Key()]
	x := w.acct(it.Account)
	if rd == nil || rd.m == nil || x == nil {
		w.actErr = i18n.T("The message is still loading.")
		w.drawReader()
		return
	}
	d, err := mail.DraftOf(rd.m, x.s.Acc, it.UID, w.drafts.Dir)
	if err != nil {
		w.actErr = i18n.Tf("Could not open the draft: %v", err)
		w.drawReader()
		return
	}
	w.newComposer(d, w.said(w.open))
}

// --- invitations ---

// drawInvite shows an invitation as an event card: what, when (in the
// user's time), where, who; the answers when it asks for one, and Add to
// calendar, which opens its file with the desktop's calendar.
func (w *window) drawInvite(box *gtk.Box, it mail.Item, inv *mail.Invite) {
	k := it.Key()
	x := w.acct(it.Account)
	card := gtk.NewBox(gtk.OrientationVerticalValue, 5)
	card.AddCssClass("mail-invite")
	kicker := i18n.T("INVITATION")
	switch inv.Method {
	case "CANCEL":
		kicker = i18n.T("CANCELLED")
		card.AddCssClass("cancelled")
	case "REPLY":
		kicker = i18n.T("ANSWER")
	}
	kl := gtk.NewLabel(kicker)
	kl.AddCssClass("kicker")
	kl.AddCssClass("mail-invite-kicker")
	kl.SetXalign(0)
	gtkx.Append(card, &kl.Widget)
	title := gtk.NewLabel(cmpOr(inv.Summary, i18n.T("(no title)")))
	title.AddCssClass("mail-invite-title")
	title.SetXalign(0)
	title.SetWrap(true)
	gtkx.Append(card, &title.Widget)
	if !inv.Start.IsZero() {
		when := gtk.NewLabel(eventTime(inv))
		when.AddCssClass("mail-invite-when")
		when.SetXalign(0)
		gtkx.Append(card, &when.Widget)
	}
	if inv.Location != "" || inv.URL != "" {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		if inv.Location != "" {
			l := gtk.NewLabel(inv.Location)
			l.AddCssClass("mail-invite-where")
			l.SetXalign(0)
			l.SetWrap(true)
			l.SetMaxWidthChars(60)
			gtkx.Append(row, &l.Widget)
		}
		if inv.URL != "" {
			link := gtk.NewLabel("")
			link.SetMarkup(`<a href="` + gtkEscape(inv.URL) + `">` + gtkEscape(i18n.T("Join")) + `</a>`)
			link.AddCssClass("mail-invite-join")
			link.SetTooltipText(inv.URL)
			gtkx.Append(row, &link.Widget)
		}
		gtkx.Append(card, &row.Widget)
	}
	who := gtk.NewLabel(w.inviteWho(inv, x))
	who.AddCssClass("mail-invite-who")
	who.SetXalign(0)
	who.SetWrap(true)
	who.SetMaxWidthChars(70)
	gtkx.Append(card, &who.Widget)

	acts := gtk.NewBox(gtk.OrientationHorizontalValue, 14)
	acts.AddCssClass("mail-invite-actions")
	a := w.answers[k]
	mineNow := ""
	if x != nil {
		mineNow = inv.Status(x.s.Acc.Address)
	}
	if x != nil {
		if kept := x.s.AnswerOf(it.Mailbox, it.UID); kept != "" {
			mineNow = kept
		}
	}
	if a != nil && a.text != "" && !a.busy && a.err == "" {
		mineNow = a.text
	}
	if inv.Asks() && x != nil { // also when invited through a list, not by name
		for _, ans := range []struct{ answer, label string }{
			{mail.Accepted, i18n.T("Accept")}, {mail.Tentative, i18n.T("Maybe")}, {mail.Declined, i18n.T("Decline")},
		} {
			b := gtk.NewButtonWithLabel(ans.label)
			if ans.answer == mail.Accepted {
				b.AddCssClass("outlined")
			} else {
				b.AddCssClass("mail-plain")
			}
			if ans.answer == mineNow {
				b.AddCssClass("chosen")
			}
			b.SetSensitive(a == nil || !a.busy)
			answer := ans.answer
			w.readerBtns = append(w.readerBtns, w.h.click(b, func() { w.answerInvite(it, inv, answer) }))
			gtkx.Append(acts, &b.Widget)
		}
	}
	add := gtk.NewButtonWithLabel(i18n.T("Add to calendar"))
	add.AddCssClass("mail-link")
	add.SetTooltipText(i18n.T("Open it with your calendar app"))
	data := inv.Data
	w.readerBtns = append(w.readerBtns, w.h.click(add, func() {
		w.openFile(mail.Attachment{Name: "invite.ics", Type: "text/calendar", Data: data})
	}))
	if inv.Method != "CANCEL" {
		gtkx.Append(acts, &add.Widget)
	}
	gtkx.Append(card, &acts.Widget)
	if a != nil {
		var note string
		switch {
		case a.busy:
			note = i18n.T("Sending your answer…")
		case a.err != "":
			note = a.err
		default:
			note = i18n.Tf("%s · sent to %s", answerName(a.text), inv.Organizer.Short())
		}
		l := gtk.NewLabel(note)
		l.AddCssClass("mail-invite-note")
		if a.err != "" {
			l.AddCssClass("mail-error")
		}
		l.SetXalign(0)
		l.SetWrap(true)
		gtkx.Append(card, &l.Widget)
	}
	gtkx.Append(box, &card.Widget)
}

// eventTime is when an event is, in the user's time: the day, then the
// hours, or the days of an event that takes whole days.
func eventTime(inv *mail.Invite) string {
	s, e := inv.Start.Local(), inv.End.Local()
	if inv.AllDay {
		last := e.AddDate(0, 0, -1)
		if !last.After(s) {
			return i18n.Time(s, "Mon Jan 2") + " · " + i18n.T("all day")
		}
		return i18n.Time(s, "Mon Jan 2") + " – " + i18n.Time(last, "Mon Jan 2")
	}
	day := i18n.Time(s, "Mon Jan 2")
	if s.YearDay() != e.YearDay() || s.Year() != e.Year() {
		return day + " " + s.Format("15:04") + " – " + i18n.Time(e, "Mon Jan 2") + " " + e.Format("15:04")
	}
	return day + " · " + s.Format("15:04") + "–" + e.Format("15:04")
}

// inviteWho says who invited, and how the others answered.
func (w *window) inviteWho(inv *mail.Invite, x *acct) string {
	parts := []string{}
	if inv.Organizer.Addr != "" {
		parts = append(parts, i18n.Tf("from %s", inv.Organizer.Short()))
	}
	going, others := 0, 0
	for _, a := range inv.Attendees {
		if x != nil && strings.EqualFold(a.Addr, x.s.Acc.Address) {
			continue
		}
		if strings.EqualFold(a.Addr, inv.Organizer.Addr) {
			continue
		}
		others++
		if a.Status == mail.Accepted {
			going++
		}
	}
	if others > 0 {
		parts = append(parts, i18n.Nf("%d other invited, %d accepted", "%d others invited, %d accepted", others, others, going))
	}
	if inv.Method == "REPLY" && len(inv.Attendees) > 0 {
		a := inv.Attendees[0]
		parts = append(parts, i18n.Tf("%s: %s", a.Short(), answerName(a.Status)))
	}
	return strings.Join(parts, " · ")
}

// answerName is an answer in words.
func answerName(answer string) string {
	switch answer {
	case mail.Accepted:
		return i18n.T("Accepted")
	case mail.Tentative:
		return i18n.T("Maybe")
	case mail.Declined:
		return i18n.T("Declined")
	}
	return i18n.T("Not answered")
}

// answerInvite sends the user's answer to the invitation's organizer, and
// marks the message answered.
func (w *window) answerInvite(it mail.Item, inv *mail.Invite, answer string) {
	x := w.acct(it.Account)
	if x == nil {
		return
	}
	k := it.Key()
	a := &aiText{busy: true, text: answer}
	w.answers[k] = a
	w.drawReader()
	go func() {
		err := x.s.Answer(w.ctx, inv, answer)
		if err == nil || errors.Is(err, mail.ErrNotSaved) {
			x.s.SaveAnswer(it.Mailbox, it.UID, answer)
			x.s.SetFlag(w.ctx, it.Mailbox, []uint32{it.UID}, mail.Answered, true)
		}
		gtkx.Post(func() {
			a.busy = false
			if err != nil && !errors.Is(err, mail.ErrNotSaved) {
				a.err = i18n.Tf("Your answer was not sent: %v", err)
			}
			w.drawReader()
		})
	}()
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// fileKey names an attachment of a message, for its summary.
func fileKey(it mail.Item, a mail.Attachment) string {
	return fmt.Sprintf("%s/%s/%d/%s", it.Account, it.Mailbox, it.UID, a.Name)
}

// sumUpFile reads an attachment's text and asks the assistant for its
// gist, shown under the files as it comes.
func (w *window) sumUpFile(it mail.Item, m *mail.Message, a mail.Attachment) {
	k := fileKey(it, a)
	s := &aiText{busy: true}
	w.fileSums[k] = s
	w.drawReader()
	about := mail.SaidOf(m)
	go func() {
		text, err := a.Text()
		if err != nil {
			gtkx.Post(func() {
				s.busy = false
				if errors.Is(err, mail.ErrNoText) {
					s.err = i18n.T("Mail found no text in it to read (a scan is pictures of pages).")
				} else {
					s.err = i18n.Tf("Could not read it: %v", err)
				}
				w.drawReader()
			})
			return
		}
		answer, err := w.ai.ask(mail.AttachmentPrompt(a.Name, text, about, i18n.Lang()), 2*time.Minute, func(t string) {
			s.text = strings.TrimSpace(t)
			w.drawReader()
		})
		gtkx.Post(func() {
			s.busy = false
			if err != nil {
				s.err = failed(i18n.T("The assistant could not sum it up"), err)
			} else {
				s.text = strings.TrimSpace(answer)
			}
			w.drawReader()
		})
	}()
}
