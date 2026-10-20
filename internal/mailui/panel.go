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
		w.setSidebar(false)
		w.sidebarForPanel = true
	case !w.assistOpen && w.sidebarForPanel:
		w.sidebarForPanel = false
		w.setSidebar(true)
	}
	w.keepLayout()
	w.drawPanel()
	w.lastDrawn = ""
	w.drawReader() // the bar's ✦
}

// panelLook is the right panel's icon and title: AI's, or, with AI off,
// the checks', all it then shows.
func panelLook(ai bool) (icon, title string) {
	if ai {
		return "applications-science-symbolic", i18n.T("AI")
	}
	return "security-high-symbolic", i18n.T("Checks")
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
		if w.settings.AI() {
			w.panelNote(&w.assist, i18n.T("Open a conversation: AI sums it up, suggests replies and writes them, and judges a message that looks suspicious."))
		} else {
			w.panelNote(&w.assist, i18n.T("Open a conversation to see what the checks found."))
		}
		return
	}
	if w.settings.AI() {
		draft := w.isDraft(c.Latest())
		w.panelActions(c, draft)
		w.panelSummary(c)
		if !draft {
			w.panelReply(c)
		}
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

// panelActions are what the assistant can be asked for the conversation:
// side by side, a summary and replies to pick from; under them, a line of
// what to reply. The answers come in the sections below.
func (w *window) panelActions(c *mail.Conversation, draft bool) {
	box := gtk.NewBox(gtk.OrientationVerticalValue, 8)
	box.AddCssClass("mail-assist-actions")
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	row.SetHomogeneous(true)
	sum := w.summaryOf(c)
	label := i18n.T("Sum it up")
	if sum != nil && sum.text != "" {
		label = i18n.T("Sum it up again")
	}
	b := w.panelAction(row, label, func() { w.summarize(c) })
	b.SetSensitive(sum == nil || !sum.busy)
	q := w.quick[c.ID]
	idle := q == nil || q.state == ""
	if !draft {
		ideas := w.ideas[c.ID]
		label := i18n.T("Suggest replies")
		if ideas != nil && ideas.text != "" {
			label = i18n.T("Other replies")
		}
		b := w.panelAction(row, label, func() { w.askIdeas(c) })
		b.SetSensitive((ideas == nil || !ideas.busy) && idle)
	}
	gtkx.Append(box, &row.Widget)
	if !draft {
		say := gtk.NewEntry()
		say.AddCssClass("mail-assist-say")
		say.SetPlaceholderText(i18n.T("Or say what to reply, then Enter"))
		say.SetSensitive(idle)
		w.panelBtns = append(w.panelBtns, w.h.enter(say, func() {
			if what := strings.TrimSpace(say.GetText()); what != "" {
				w.draftQuick(c, what)
			}
		}))
		gtkx.Append(box, &say.Widget)
	}
	gtkx.Append(&w.assist, &box.Widget)
}

// panelAction is one of the actions' buttons: the assistant's mark and
// what it does.
func (w *window) panelAction(row *gtk.Box, label string, f func()) *gtk.Button {
	b := gtk.NewButton()
	b.AddCssClass("outlined")
	b.AddCssClass("mail-assist-action")
	in := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
	in.SetHalign(gtk.AlignCenterValue)
	icon := gtk.NewImageFromIconName("applications-science-symbolic")
	gtkx.Append(in, &icon.Widget)
	l := gtk.NewLabel(label)
	l.SetEllipsize(3)
	gtkx.Append(in, &l.Widget)
	b.SetChild(&in.Widget)
	w.panelBtns = append(w.panelBtns, w.h.click(b, f))
	gtkx.Append(row, &b.Widget)
	return b
}

// summaryOf is the conversation's summary: asked for here, or kept from
// before.
func (w *window) summaryOf(c *mail.Conversation) *aiText {
	sum := w.sumState[c.ID]
	if sum == nil {
		if text := w.keptSummary(c); text != "" {
			sum = &aiText{text: text}
			w.sumState[c.ID] = sum
		}
	}
	return sum
}

// panelSummary shows the summary, once asked for.
func (w *window) panelSummary(c *mail.Conversation) {
	sum := w.summaryOf(c)
	if sum == nil {
		return
	}
	sec := w.panelSection(i18n.T("SUMMARY"))
	switch {
	case sum.err != "":
		w.panelError(sec, sum.err)
	case sum.text == "" && sum.busy:
		l := loader("")
		l.SetHalign(gtk.AlignStartValue)
		gtkx.Append(sec, &l.Widget)
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
	}
}

// panelReply shows the replies to pick from, once asked for, and the
// reply being written.
func (w *window) panelReply(c *mail.Conversation) {
	ideas := w.ideas[c.ID]
	q := w.quick[c.ID]
	drafting := q != nil && q.state == "drafting"
	if ideas == nil && !drafting {
		return
	}
	sec := w.panelSection(i18n.T("REPLIES"))
	if drafting {
		l := loader("")
		l.SetHalign(gtk.AlignStartValue)
		gtkx.Append(sec, &l.Widget)
	}
	switch {
	case ideas == nil:
	case ideas.busy:
		l := loader("")
		l.SetHalign(gtk.AlignStartValue)
		gtkx.Append(sec, &l.Widget)
	case ideas.err != "":
		w.panelError(sec, ideas.err)
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
	}
}

func (w *window) panelSafety(c *mail.Conversation) {
	sec := w.panelSection(i18n.T("IS IT SAFE?"))
	// The newest message read that is not the user's own.
	var k mail.Key
	var rd *reading
	for i := len(c.Messages) - 1; i >= 0; i-- {
		it := c.Messages[i]
		if r := w.reading[it.Key()]; r != nil && r.m != nil && !w.mine(r.m.From.Addr) {
			k, rd = it.Key(), r
			break
		}
	}
	if rd == nil {
		w.panelNote(sec, i18n.T("Nothing to check: the messages read here are yours."))
		return
	}
	drawChecks(sec, rd.judged)

	if !w.settings.AI() {
		return
	}
	ai := gtk.NewBox(gtk.OrientationVerticalValue, 8)
	ai.AddCssClass("mail-safety-ai")
	a := w.judged[k]
	switch {
	case a == nil:
		w.panelNote(ai, i18n.T("AI reads what the checks found, with the sender, subject, links and text, and says what it makes of it."))
		w.panelButton(ai, i18n.T("Ask AI"), func() { w.askJudge(k) })
	case a.busy:
		gtkx.Append(ai, &loader("").Widget)
	case a.err != "":
		w.panelError(ai, a.err)
		w.panelLink(ai, i18n.T("Try again"), func() { w.askJudge(k) })
	case a.judge != nil:
		verdictLine(ai, a.judge.Verdict, i18n.T("AI"))
		for _, r := range a.judge.Reasons {
			row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
			row.AddCssClass("mail-safety-reason")
			dot := gtk.NewLabel("·")
			dot.AddCssClass("mail-safety-dot")
			dot.SetValign(gtk.AlignStartValue)
			gtkx.Append(row, &dot.Widget)
			l := gtk.NewLabel(r)
			l.SetXalign(0)
			l.SetWrap(true)
			l.SetWrapMode(2)
			l.SetMaxWidthChars(34)
			gtkx.Append(row, &l.Widget)
			gtkx.Append(ai, &row.Widget)
		}
		if a.judge.Advice != "" {
			l := gtk.NewLabel(a.judge.Advice)
			l.AddCssClass("mail-safety-advice")
			l.SetXalign(0)
			l.SetWrap(true)
			l.SetWrapMode(2)
			l.SetMaxWidthChars(34)
			gtkx.Append(ai, &l.Widget)
		}
		w.panelLink(ai, i18n.T("Ask again"), func() { w.askJudge(k) })
	default:
		w.panelNote(ai, a.text)
	}
	gtkx.Append(sec, &ai.Widget)
}

// verdictLine is a verdict as a coloured mark and its name, with who
// gave it.
func verdictLine(box *gtk.Box, v mail.Verdict, by string) {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	row.AddCssClass("mail-verdict")
	row.AddCssClass(v.ID())
	mark := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	mark.AddCssClass("mark")
	mark.AddCssClass(v.ID())
	mark.SetValign(gtk.AlignCenterValue)
	gtkx.Append(row, &mark.Widget)
	name := gtk.NewLabel(v.Name())
	name.AddCssClass("mail-verdict-name")
	gtkx.Append(row, &name.Widget)
	who := gtk.NewLabel(by)
	who.AddCssClass("mail-verdict-by")
	who.SetHexpand(true)
	who.SetXalign(1)
	who.SetEllipsize(3)
	gtkx.Append(row, &who.Widget)
	gtkx.Append(box, &row.Widget)
}

// drawChecks lists the checks: each its mark, its question and what it
// found.
func drawChecks(box *gtk.Box, j mail.Judgment) {
	list := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	list.AddCssClass("mail-checks")
	for _, s := range j.Steps {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		row.AddCssClass("mail-check")
		m := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		m.AddCssClass("mark")
		if c := map[mail.Level]string{mail.LevelGood: "fine", mail.LevelWarn: "careful", mail.LevelBad: "suspicious"}[s.Level]; c != "" {
			m.AddCssClass(c)
		}
		m.SetValign(gtk.AlignStartValue)
		gtkx.Append(row, &m.Widget)
		texts := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		q := gtk.NewLabel(s.Check)
		q.AddCssClass("mail-check-q")
		q.SetXalign(0)
		q.SetWrap(true)
		q.SetMaxWidthChars(40)
		gtkx.Append(texts, &q.Widget)
		f := gtk.NewLabel(s.Found)
		f.AddCssClass("mail-check-found")
		f.SetXalign(0)
		f.SetWrap(true)
		f.SetWrapMode(2)
		f.SetMaxWidthChars(40)
		gtkx.Append(texts, &f.Widget)
		gtkx.Append(row, &texts.Widget)
		gtkx.Append(list, &row.Widget)
	}
	gtkx.Append(box, &list.Widget)
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
