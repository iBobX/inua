package mailui

import (
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/mail"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// The assistant's panel, at the window's right edge when asked for (✦ in
// the reader's bar): for the conversation open, its summary, replies to
// pick or a line of what to say (drafted into the quick reply under the
// conversation, to read and send), and its judgment of a message the
// phishing checks doubt.

// toggleAssist opens or closes the panel.
// The sidebar makes room for it, and comes back when it closes.
func (w *window) toggleAssist() {
	w.assistOpen = !w.assistOpen
	w.assistReveal.SetRevealChild(w.assistOpen)
	switch {
	case w.assistOpen && w.indexCol.GetVisible():
		w.toggleSidebar()
		w.sidebarForPanel = true
	case !w.assistOpen && w.sidebarForPanel:
		w.sidebarForPanel = false
		if !w.indexCol.GetVisible() {
			w.toggleSidebar()
		}
	}
	w.drawPanel()
	w.lastDrawn = ""
	w.drawReader() // the bar's ✦
}

// openAssist opens the panel, if it is not, and runs f.
func (w *window) openAssist(f func()) {
	if !w.assistOpen {
		w.toggleAssist()
	}
	f()
}

// drawPanel makes the panel's sections again for the conversation open.
func (w *window) drawPanel() {
	w.h.forget(w.panelBtns)
	w.panelBtns, w.panelSum = nil, nil
	gtkx.Clear(&w.assist)
	if !w.assistOpen {
		return
	}
	c := w.open
	if c == nil {
		w.panelNote(&w.assist, i18n.T("Open a conversation: the assistant sums it up, suggests replies and writes them, and judges a message that looks suspicious."))
		return
	}
	w.panelSummary(c)
	if !w.isDraft(c.Latest()) {
		w.panelReply(c)
	}
	w.panelSafety(c)
}

// panelSection adds a section: its kicker, and the box its lines go in.
func (w *window) panelSection(kicker string) *gtk.Box {
	sec := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	sec.AddCssClass("mail-assist-section")
	k := gtk.NewLabel(kicker)
	k.AddCssClass("kicker")
	k.SetXalign(0)
	gtkx.Append(sec, &k.Widget)
	gtkx.Append(&w.assist, &sec.Widget)
	return sec
}

func (w *window) panelNote(box *gtk.Box, text string) *gtk.Label {
	l := gtk.NewLabel(text)
	l.AddCssClass("mail-assist-note")
	l.SetXalign(0)
	l.SetWrap(true)
	l.SetWrapMode(2)
	l.SetMaxWidthChars(36)
	gtkx.Append(box, &l.Widget)
	return l
}

// panelButton is a full-width button of the panel's.
func (w *window) panelButton(box *gtk.Box, label string, f func()) *gtk.Button {
	b := gtk.NewButtonWithLabel(label)
	b.AddCssClass("outlined")
	b.AddCssClass("mail-assist-button")
	w.panelBtns = append(w.panelBtns, w.h.click(b, f))
	gtkx.Append(box, &b.Widget)
	return b
}

// panelLink is a quiet text button of the panel's.
func (w *window) panelLink(box *gtk.Box, label string, f func()) {
	b := gtk.NewButtonWithLabel(label)
	b.AddCssClass("mail-plain")
	b.SetHalign(gtk.AlignStartValue)
	w.panelBtns = append(w.panelBtns, w.h.click(b, f))
	gtkx.Append(box, &b.Widget)
}

func (w *window) panelError(box *gtk.Box, text string) {
	l := w.panelNote(box, text)
	l.AddCssClass("mail-error")
}

func (w *window) panelSummary(c *mail.Conversation) {
	sec := w.panelSection(i18n.T("SUMMARY"))
	sum := w.sumState[c.ID]
	if sum == nil {
		if text := w.keptSummary(c); text != "" {
			sum = &aiText{text: text}
			w.sumState[c.ID] = sum
		}
	}
	switch {
	case sum == nil:
		w.panelNote(sec, i18n.T("Who wants what, what was decided, and what is asked of you."))
		w.panelButton(sec, i18n.T("Sum it up"), func() { w.summarize(c) })
	case sum.err != "":
		w.panelError(sec, sum.err)
		w.panelLink(sec, i18n.T("Try again"), func() { w.summarize(c) })
	case sum.text == "" && sum.busy:
		w.panelNote(sec, i18n.T("Reading the conversation…")).AddCssClass("mail-ai-busy")
	default:
		l := gtk.NewLabel(sum.text)
		l.AddCssClass("mail-assist-text")
		l.SetXalign(0)
		l.SetWrap(true)
		l.SetWrapMode(2)
		l.SetMaxWidthChars(36)
		l.SetSelectable(true)
		gtkx.Append(sec, &l.Widget)
		w.panelSum = l
		if !sum.busy {
			w.panelLink(sec, i18n.T("Sum it up again"), func() { w.summarize(c) })
		}
	}
}

func (w *window) panelReply(c *mail.Conversation) {
	sec := w.panelSection(i18n.T("REPLY"))
	ideas := w.ideas[c.ID]
	q := w.quick[c.ID]
	if q != nil && q.state == "drafting" {
		w.panelNote(sec, i18n.T("Writing it in the reply under the conversation…")).AddCssClass("mail-ai-busy")
	}
	switch {
	case ideas == nil:
		w.panelNote(sec, i18n.T("Three short replies to pick from; the one picked is written out in the reply under the conversation, for you to read and send."))
		w.panelButton(sec, i18n.T("Suggest replies"), func() { w.askIdeas(c) })
	case ideas.busy:
		w.panelNote(sec, i18n.T("Thinking of replies…")).AddCssClass("mail-ai-busy")
	case ideas.err != "":
		w.panelError(sec, ideas.err)
		w.panelLink(sec, i18n.T("Try again"), func() { w.askIdeas(c) })
	default:
		for _, idea := range strings.Split(ideas.text, "\n") {
			if idea = strings.TrimSpace(idea); idea == "" {
				continue
			}
			b := gtk.NewButton()
			b.AddCssClass("mail-assist-idea")
			l := gtk.NewLabel(idea)
			l.SetXalign(0)
			l.SetWrap(true)
			l.SetWrapMode(2)
			l.SetMaxWidthChars(34)
			b.SetChild(&l.Widget)
			b.SetTooltipText(i18n.T("Write this reply"))
			b.SetSensitive(q == nil || q.state == "")
			w.panelBtns = append(w.panelBtns, w.h.click(b, func() { w.draftQuick(c, idea) }))
			gtkx.Append(sec, &b.Widget)
		}
		w.panelLink(sec, i18n.T("Other replies"), func() { w.askIdeas(c) })
	}
	// Or a line of what to say.
	say := gtk.NewEntry()
	say.AddCssClass("mail-assist-say")
	say.SetPlaceholderText(i18n.T("Or say what to reply, then Enter"))
	say.SetSensitive(q == nil || q.state == "")
	w.panelBtns = append(w.panelBtns, w.h.enter(say, func() {
		if what := strings.TrimSpace(say.GetText()); what != "" {
			w.draftQuick(c, what)
		}
	}))
	gtkx.Append(sec, &say.Widget)
}

func (w *window) panelSafety(c *mail.Conversation) {
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
	sec := w.panelSection(i18n.T("IS IT SAFE?"))
	a := w.judged[k]
	switch {
	case a == nil:
		w.panelNote(sec, i18n.T("The checks doubt this message. The assistant reads what they found and says what it makes of it."))
		w.panelButton(sec, i18n.T("Ask the assistant"), func() { w.askJudge(k) })
	case a.busy:
		w.panelNote(sec, i18n.T("Thinking…")).AddCssClass("mail-ai-busy")
	case a.err != "":
		w.panelError(sec, a.err)
		w.panelLink(sec, i18n.T("Try again"), func() { w.askJudge(k) })
	default:
		l := w.panelNote(sec, a.text)
		l.AddCssClass("mail-assist-text")
	}
}

// draftQuick has the assistant write a reply from a line of what to say,
// into the quick reply under the conversation, as it comes.
func (w *window) draftQuick(c *mail.Conversation, what string) {
	q := w.quick[c.ID]
	if q == nil {
		q = &quickReply{}
		w.quick[c.ID] = q
	}
	if q.state != "" {
		return
	}
	m, it := w.answeringQuiet(c)
	x := w.acct(it.Account)
	if m == nil || x == nil {
		return
	}
	user := x.s.Acc.Name
	if user == "" {
		user = x.s.Acc.Address
	}
	prompt := mail.DraftPrompt(what, w.said(c), w.voice(x), user, i18n.Lang())
	q.before, q.state, q.err, q.drafted = q.text, "drafting", "", false
	w.lastDrawn = ""
	w.drawReader()
	w.drawPanel()
	id := c.ID
	go func() {
		text, err := w.ai.ask(prompt, 2*time.Minute, func(t string) {
			q.text = strings.TrimSpace(t)
			if w.quickFor == id && w.quickBuf != nil {
				w.quickBuf.SetText(q.text, -1)
			}
		})
		gtkx.Post(func() {
			q.state = ""
			if err != nil {
				q.text = q.before
				q.err = failed(i18n.T("The assistant could not write it"), err)
			} else {
				q.text, q.drafted, q.focused = strings.TrimSpace(text), true, true
			}
			w.lastDrawn = ""
			if w.openID == id {
				w.drawReader()
			}
			w.drawPanel()
		})
	}()
}
