package mailui

import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	w.lastDrawn = ""
	w.readerStack.SetVisibleChildName("read")
	w.readerScroll.GetVadjustment().SetValue(0)
	w.markRead(c)
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
		fmt.Fprintf(&b, "%v%v", w.checks[k], w.judged[k].sig())
	}
	fmt.Fprintf(&b, "|%s|%s", w.sumState[c.ID].sig(), w.ideas[c.ID].sig())
	return b.String()
}

// drawReader makes the conversation's view again.
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

	subject := c.Subject()
	if subject == "" {
		subject = i18n.T("(no subject)")
	}
	t := gtk.NewLabel(subject)
	t.AddCssClass("mail-read-subject")
	t.SetXalign(0)
	t.SetWrap(true)
	t.SetMaxWidthChars(70)
	gtkx.Append(&w.reader, &t.Widget)
	if w.actErr != "" {
		e := gtk.NewLabel(w.actErr)
		e.AddCssClass("mail-error")
		e.SetXalign(0)
		e.SetWrap(true)
		gtkx.Append(&w.reader, &e.Widget)
	}
	w.drawWarning(c)
	w.drawAssist(c)

	last := len(c.Messages) - 1
	for i, m := range c.Messages {
		w.drawMessage(m, i == last)
	}
	acts := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	acts.AddCssClass("mail-actions")
	for _, a := range []struct {
		label, tip string
		main       bool
		f          func()
	}{
		{i18n.T("Reply"), "R", true, func() { w.reply(false) }},
		{i18n.T("Reply all"), "A", false, func() { w.reply(true) }},
		{i18n.T("Forward"), "F", false, w.forward},
	} {
		b := gtk.NewButtonWithLabel(a.label)
		b.SetTooltipText(a.tip)
		if a.main {
			b.AddCssClass("outlined")
		} else {
			b.AddCssClass("mail-plain")
		}
		w.readerBtns = append(w.readerBtns, w.h.click(b, a.f))
		gtkx.Append(acts, &b.Widget)
	}
	hint := gtk.NewLabel(i18n.T("E archive · # delete · S star · U unread"))
	hint.AddCssClass("mail-keys")
	hint.SetHexpand(true)
	hint.SetXalign(1)
	hint.SetEllipsize(3) // it gives way before the window grows
	gtkx.Append(acts, &hint.Widget)
	gtkx.Append(&w.reader, &acts.Widget)
}

// drawWarning shows the phishing route's verdict for the newest message
// that is not fine: one line, its checks folded behind it.
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
	box := gtk.NewBox(gtk.OrientationVerticalValue, 6)
	box.AddCssClass("mail-strip")
	box.AddCssClass("mail-warning")
	line := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	mark := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	mark.AddCssClass("mark")
	mark.AddCssClass(j.Verdict.ID())
	mark.SetValign(gtk.AlignCenterValue)
	gtkx.Append(line, &mark.Widget)
	worst := j.Worst()
	text := gtk.NewLabel(j.Verdict.Name() + ": " + worst.Found)
	text.AddCssClass("mail-warning-text")
	text.AddCssClass(j.Verdict.ID())
	text.SetXalign(0)
	text.SetWrap(true)
	text.SetHexpand(true)
	text.SetMaxWidthChars(80)
	gtkx.Append(line, &text.Widget)
	gtkx.Append(box, &line.Widget)

	links := gtk.NewBox(gtk.OrientationHorizontalValue, 16)
	links.AddCssClass("mail-links")
	more := i18n.Nf("%d check ▸", "%d checks ▸", len(j.Steps), len(j.Steps))
	if w.checks[k] {
		more = i18n.T("Hide the checks")
	}
	w.link(links, more, func() { w.checks[k] = !w.checks[k]; w.drawReader() })
	if x := w.acct(k.Account); x != nil && x.s.Role(mail.RoleJunk) != "" && k.Mailbox != x.s.Role(mail.RoleJunk) {
		w.link(links, i18n.T("Move to Spam"), w.spam)
	}
	if a := w.judged[k]; a == nil || a.err != "" {
		w.link(links, i18n.T("Ask the assistant"), func() { w.askJudge(k) })
	}
	if w.checks[k] {
		steps := gtk.NewBox(gtk.OrientationVerticalValue, 4)
		steps.AddCssClass("mail-steps")
		for _, s := range j.Steps {
			row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
			m := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
			m.AddCssClass("mark")
			m.AddCssClass(map[mail.Level]string{mail.LevelGood: "fine", mail.LevelWarn: "careful", mail.LevelBad: "suspicious"}[s.Level])
			m.SetValign(gtk.AlignStartValue)
			gtkx.Append(row, &m.Widget)
			l := gtk.NewLabel(s.Check + " " + s.Found)
			l.AddCssClass("mail-step")
			l.SetXalign(0)
			l.SetWrap(true)
			l.SetMaxWidthChars(80)
			gtkx.Append(row, &l.Widget)
			gtkx.Append(steps, &row.Widget)
		}
		gtkx.Append(box, &steps.Widget)
	}
	if a := w.judged[k]; a != nil {
		w.aiLine(box, a, i18n.T("THE ASSISTANT"))
	}
	gtkx.Append(box, &links.Widget)
	gtkx.Append(&w.reader, &box.Widget)
}

// drawAssist shows the assistant's strip: the summary and the reply ideas
// when asked for, else the quiet links that ask.
func (w *window) drawAssist(c *mail.Conversation) {
	sum, ideas := w.sumState[c.ID], w.ideas[c.ID]
	if sum == nil {
		if text := w.keptSummary(c); text != "" {
			sum = &aiText{text: text}
			w.sumState[c.ID] = sum
		}
	}
	box := gtk.NewBox(gtk.OrientationVerticalValue, 6)
	box.AddCssClass("mail-strip")
	if sum != nil {
		w.aiLine(box, sum, i18n.T("SUMMARY · ASSISTANT"))
	}
	links := gtk.NewBox(gtk.OrientationHorizontalValue, 16)
	links.AddCssClass("mail-links")
	if ideas != nil && ideas.err == "" && !ideas.busy {
		for _, idea := range strings.Split(ideas.text, "\n") {
			if idea = strings.TrimSpace(idea); idea != "" {
				w.link(links, idea, func() { w.replyWith(idea) })
			}
		}
	}
	if sum == nil && w.worthSummary(c) {
		w.link(links, i18n.T("Summarize"), func() { w.summarize(c) })
	}
	if ideas == nil {
		w.link(links, i18n.T("Reply ideas"), func() { w.askIdeas(c) })
	} else if ideas.busy {
		l := gtk.NewLabel(i18n.T("Thinking of replies…"))
		l.AddCssClass("mail-ai-busy")
		gtkx.Append(links, &l.Widget)
	} else if ideas.err != "" {
		e := gtk.NewLabel(ideas.err)
		e.AddCssClass("mail-error")
		e.SetWrap(true)
		gtkx.Append(box, &e.Widget)
	}
	gtkx.Append(box, &links.Widget)
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
		l.SetMaxWidthChars(80)
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

// drawMessage shows a message: open (the newest, or one unfolded) or
// folded to its sender and first line.
func (w *window) drawMessage(it mail.Item, newest bool) {
	k := it.Key()
	rd := w.reading[k]
	open := newest || w.folded[k] // folded holds "unfolded by the user" for older ones
	if !newest && !it.Has(mail.Seen) {
		open = true
	}
	box := gtk.NewBox(gtk.OrientationVerticalValue, 8)
	box.AddCssClass("mail-message")
	if !open {
		box.AddCssClass("folded")
	}
	head := gtk.NewButton()
	head.AddCssClass("mail-message-head")
	hb := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	from := gtk.NewLabel(it.From.Short())
	from.AddCssClass("mail-from")
	from.SetTooltipText(it.From.Addr)
	gtkx.Append(hb, &from.Widget)
	to := gtk.NewLabel("")
	to.AddCssClass("mail-to")
	to.SetXalign(0)
	to.SetHexpand(true)
	to.SetEllipsize(3)
	if open {
		to.SetText(w.toLine(it))
	} else {
		to.SetText(it.Snippet)
	}
	gtkx.Append(hb, &to.Widget)
	if it.Attach && !open {
		clip := gtk.NewLabel(i18n.T("FILE"))
		clip.AddCssClass("mail-clip")
		gtkx.Append(hb, &clip.Widget)
	}
	date := gtk.NewLabel(i18n.Time(it.Date.Local(), "Mon Jan 2, 15:04"))
	date.AddCssClass("mail-date")
	gtkx.Append(hb, &date.Widget)
	head.SetChild(&hb.Widget)
	if !newest {
		w.readerBtns = append(w.readerBtns, w.h.click(head, func() { w.folded[k] = !open; w.drawReader() }))
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
	written, rest := mail.Split(m.Text)
	if strings.TrimSpace(written) == "" {
		written, rest = m.Text, ""
	}
	body := gtk.NewLabel("")
	body.AddCssClass("mail-text")
	body.SetMarkup(linkify(written))
	body.SetXalign(0)
	body.SetWrap(true)
	body.SetWrapMode(2) // word_char: long links
	body.SetSelectable(true)
	body.SetMaxWidthChars(84)
	gtkx.Append(box, &body.Widget)
	links := gtk.NewBox(gtk.OrientationHorizontalValue, 16)
	links.AddCssClass("mail-links")
	if rest != "" {
		label := i18n.T("Show quoted text")
		if w.quotes[k] {
			label = i18n.T("Hide quoted text")
		}
		w.link(links, label, func() { w.quotes[k] = !w.quotes[k]; w.drawReader() })
	}
	if m.FromHTML || m.HTML != "" {
		b := gtk.NewButtonWithLabel(i18n.T("Open in browser"))
		b.AddCssClass("mail-link")
		b.SetTooltipText(i18n.T("Shown here as text. The browser shows it as sent, and may load its remote images, which tell the sender you opened it."))
		w.readerBtns = append(w.readerBtns, w.h.click(b, func() { w.openHTML(m) }))
		gtkx.Append(links, &b.Widget)
	}
	if rest != "" && w.quotes[k] {
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
	if shown := m.Shown(); len(shown) > 0 {
		flow := gtk.NewFlowBox()
		flow.AddCssClass("mail-files")
		flow.SetSelectionMode(gtk.SelectionNoneValue)
		flow.SetMaxChildrenPerLine(3)
		flow.SetColumnSpacing(8)
		flow.SetRowSpacing(8)
		flow.SetHalign(gtk.AlignStartValue)
		for _, a := range shown {
			flow.Append(&w.fileCard(a).Widget)
		}
		gtkx.Append(box, &flow.Widget)
	}
	gtkx.Append(box, &links.Widget)
}

// fileCard is an attachment: a picture's thumbnail or its type, its name
// and size; a click opens it, Save… keeps it.
func (w *window) fileCard(a mail.Attachment) *gtk.Box {
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
