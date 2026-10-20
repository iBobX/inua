package mailui

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/mail"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// The composer's AI, in a panel at its right edge (✦, Ctrl+J), as beside
// a conversation in Mail: a line of one's own and the conversation's
// reply ideas while nothing is written; one's own words, Shorter,
// Friendlier, More formal, Fix spelling and Translate once something is,
// for the selection only when there is one. What AI writes comes into
// the text tinted, a suggestion: Keep (Enter), Try again, Discard (Esc),
// each version kept to go back to, and one's own text to compare. Send
// waits while a suggestion does.

// composeAppID is the composer's window class: Hyprland floats it over
// Mail (config/hypr/inua.conf), the dock counts it as Mail's.
const composeAppID = "io.github.lyricat.Inua.Mail.Compose"

const (
	aiIdle    = iota
	aiWriting // the answer coming
	aiReview  // a suggestion, to keep or not
)

// composeAI is the panel's state and the suggestion's.
type composeAI struct {
	phase          int
	asked          string   // what was asked, as the panel says it
	versions       []string // each answer to it, Try again adding one
	vi             int      // the one shown
	prefix, suffix string   // the text around the suggestion
	before         string   // the whole text before it, for Discard and the draft kept
	part           bool     // a selection is being changed
	again          func()   // asks the same again
	seq            int      // the request's number: a stopped one's answers are dropped
	flipped        bool     // one's own text shown in place of the suggestion
	err            string
	tag            *gtk.TextTag

	ideas      []string // the conversation's reply ideas
	ideasAsked bool
	ideasBusy  bool

	listKey  string // what the list holds, to make it again only when that changes
	listPtrs []uintptr
}

func (c *composer) buildAI() {
	h := c.w.h
	c.ptrs = append(c.ptrs,
		h.click(&c.aiToggle, c.toggleAI),
		h.click(&c.aiClose, c.toggleAI),
		h.click(&c.aiStop, c.stopAI),
		h.click(&c.aiPrev, func() { c.showVersion(c.ai.vi - 1) }),
		h.click(&c.aiNext, func() { c.showVersion(c.ai.vi + 1) }),
		h.click(&c.aiKeep, c.keepAI),
		h.click(&c.aiRetry, c.retryAI),
		h.click(&c.aiDiscard, c.discardAI),
		h.click(&c.aiFlip, func() { c.ai.flipped = !c.ai.flipped; c.showSuggestion(); c.drawAI() }),
		h.enter(&c.aiSay, c.askAI),
		h.notify(c.buf, "has-selection", func() {
			if c.aiReveal.GetRevealChild() {
				c.drawAI()
			}
		}),
	)
	for _, l := range mail.SummaryLanguages {
		b := gtk.NewButtonWithLabel(l.Name)
		b.AddCssClass("mail-compose-ai-lang")
		code, name := l.Code, l.Name
		c.ptrs = append(c.ptrs, h.click(b, func() {
			c.changeAI(mail.Translate, code, i18n.Tf("Translate into %s", name))
		}))
		c.aiLangs.Append(&b.Widget)
	}
	c.ai.tag = gtk.NewTextTag("ai-suggestion")
	c.ai.tag.SetPropertyBackground(tint(c.w.c.Colors["accent"], 0.14))
	c.buf.GetTagTable().Add(c.ai.tag)
	c.say("", false)
	c.aiChanged()
	if c.w.settings.AI() && c.w.settings.Window.ComposeAI {
		c.aiReveal.SetRevealChild(true)
	}
	c.drawAI()
}

// tint is a theme colour (#rrggbb) at alpha, for a text tag.
func tint(hex string, alpha float64) string {
	v, err := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	if err != nil || len(hex) != 7 {
		return fmt.Sprintf("rgba(128,128,128,%.2f)", alpha)
	}
	return fmt.Sprintf("rgba(%d,%d,%d,%.2f)", v>>16&0xff, v>>8&0xff, v&0xff, alpha)
}

// aiChanged follows Mail's AI being turned on or off.
func (c *composer) aiChanged() {
	on := c.w.settings.AI()
	c.aiToggle.SetVisible(on)
	if !on {
		if c.ai.phase != aiIdle {
			c.discardAI()
		}
		c.aiReveal.SetRevealChild(false)
	}
}

// toggleAI opens or closes the panel, and remembers it for the next
// composer.
func (c *composer) toggleAI() {
	if !c.w.settings.AI() {
		return
	}
	open := !c.aiReveal.GetRevealChild()
	if !open {
		c.discardAI() // a suggestion waiting goes with the panel: the text is the user's again
	}
	c.aiReveal.SetRevealChild(open)
	if l := &c.w.settings.Window; l.ComposeAI != open {
		l.ComposeAI = open
		if err := c.w.settings.Save(mail.SettingsPath()); err != nil {
			c.say(i18n.Tf("Not saved: %v", err), true)
		}
	}
	c.drawAI()
	switch {
	case open && c.ai.phase == aiIdle:
		c.aiSay.GrabFocus()
	case !open:
		c.body.GrabFocus()
	}
}

// inEntry says the focus is in one of the composer's entries, where Enter
// is the entry's.
func (c *composer) inEntry() bool {
	f := c.win.GetFocus()
	if f == nil {
		return false
	}
	for _, e := range []*gtk.Entry{&c.to, &c.cc, &c.bcc, &c.subject, &c.aiSay} {
		if f.GoPointer() == e.GoPointer() || f.IsAncestor(&e.Widget) {
			return true
		}
	}
	return false
}

// inPanel says the focus is in the AI panel.
func (c *composer) inPanel() bool {
	f := c.win.GetFocus()
	return f != nil && f.IsAncestor(&c.aiReveal.Widget)
}

// keyAI is Ctrl+J: it opens the panel, goes into it when it is open, and
// closes it from inside it.
func (c *composer) keyAI() {
	switch {
	case !c.aiReveal.GetRevealChild() || c.inPanel():
		c.toggleAI()
	case c.ai.phase == aiReview:
		c.aiKeep.GrabFocus()
	case c.ai.phase == aiWriting:
		c.aiStop.GrabFocus()
	default:
		c.aiSay.GrabFocus()
	}
}

// writing says nothing is written yet: the panel offers to write it.
func (c *composer) writing() bool {
	return splitText(c.text()) == "" && !c.buf.GetHasSelection()
}

// drawAI sets the panel from the state.
func (c *composer) drawAI() {
	a := &c.ai
	pending := a.phase != aiIdle
	setClass(&c.aiToggle.Widget, "on", c.aiReveal.GetRevealChild())
	if !c.sending {
		c.send.SetSensitive(!pending)
	}
	part := a.part
	if !pending {
		part = c.buf.GetHasSelection()
	}
	c.aiScope.SetText(i18n.T("· THE SELECTION"))
	c.aiScope.SetVisible(part)
	write := !pending && c.writing()
	c.aiAsk.SetVisible(!pending)
	c.aiTranslate.SetVisible(!pending && !write)
	c.aiWork.SetVisible(a.phase == aiWriting)
	c.aiRev.SetVisible(a.phase == aiReview)
	c.aiErr.SetText(a.err)
	c.aiErr.SetVisible(a.err != "")
	switch {
	case write && len(c.conv) > 0:
		c.aiSay.SetPlaceholderText(i18n.T("What should the reply say?"))
	case write:
		c.aiSay.SetPlaceholderText(i18n.T("What should it say?"))
	case part:
		c.aiSay.SetPlaceholderText(i18n.T("How should the selection change?"))
	default:
		c.aiSay.SetPlaceholderText(i18n.T("How should it change?"))
	}
	c.aiWorking.SetText(i18n.Tf("Writing… %s", a.asked))
	c.aiAsked.SetText(a.asked)
	c.aiCount.SetText(fmt.Sprintf("%d/%d", a.vi+1, len(a.versions)))
	c.aiPrev.SetSensitive(a.vi > 0)
	c.aiNext.SetSensitive(a.vi < len(a.versions)-1)
	c.aiRetry.SetSensitive(a.again != nil)
	if a.flipped {
		c.aiFlip.SetLabel(i18n.T("Suggestion"))
		c.aiFlip.SetTooltipText(i18n.T("Show AI's suggestion again"))
	} else {
		c.aiFlip.SetLabel(i18n.T("Yours"))
		c.aiFlip.SetTooltipText(i18n.T("Show your text as it was"))
	}
	if c.aiReveal.GetRevealChild() && !pending {
		c.drawAIList(write)
	}
}

// drawAIList fills the list under the line: the reply ideas while
// nothing is written, the changes once something is.
func (c *composer) drawAIList(write bool) {
	a := &c.ai
	if write && len(c.conv) > 0 && !a.ideasAsked {
		c.askIdeas()
	}
	key := fmt.Sprint(write, a.ideasBusy, a.ideas)
	if key == a.listKey {
		return
	}
	a.listKey = key
	c.w.h.forget(a.listPtrs)
	a.listPtrs = nil
	gtkx.Clear(&c.aiList)
	item := func(label string, f func()) {
		l := gtk.NewLabel(label)
		l.SetXalign(0)
		l.SetWrap(true)
		b := gtk.NewButton()
		b.SetChild(&l.Widget)
		b.AddCssClass("mail-compose-ai-item")
		a.listPtrs = append(a.listPtrs, c.w.h.click(b, f))
		gtkx.Append(&c.aiList, &b.Widget)
	}
	switch {
	case write && a.ideasBusy:
		l := gtk.NewLabel(i18n.T("Reading the conversation…"))
		l.AddCssClass("sub")
		l.SetXalign(0)
		gtkx.Append(&c.aiList, &l.Widget)
	case write:
		for _, idea := range a.ideas {
			item(idea, func() { c.writeAI(idea) })
		}
	default:
		for _, p := range []struct {
			label string
			how   mail.Polish
		}{
			{i18n.T("Shorter"), mail.Shorter},
			{i18n.T("Friendlier"), mail.Friendlier},
			{i18n.T("More formal"), mail.Formal},
			{i18n.T("Fix spelling"), mail.Fix},
		} {
			item(p.label, func() { c.changeAI(p.how, "", p.label) })
		}
	}
	c.aiList.SetVisible(c.aiList.GetFirstChild() != nil)
}

// askIdeas asks for the conversation's reply ideas, once.
func (c *composer) askIdeas() {
	a := &c.ai
	a.ideasAsked, a.ideasBusy = true, true
	msgs := c.conv
	if len(msgs) > 6 {
		msgs = msgs[len(msgs)-6:]
	}
	user := ""
	if x := c.w.acct(c.d.Account); x != nil {
		user = x.s.Acc.Address
	}
	prompt := mail.ReplyIdeasPrompt(msgs, user, i18n.Lang())
	go func() {
		text, err := c.w.ai.ask(prompt, time.Minute, nil)
		gtkx.Post(func() {
			if c.closed {
				return
			}
			a.ideasBusy = false
			if err == nil {
				a.ideas = mail.ParseIdeas(text)
			}
			c.drawAI()
		})
	}()
}

// askAI runs the panel's line: what to write while nothing is, else how
// to change it.
func (c *composer) askAI() {
	said := strings.TrimSpace(c.aiSay.GetText())
	if said == "" || c.ai.phase != aiIdle {
		return
	}
	asked := "“" + said + "”"
	if c.writing() {
		c.draftAI(said, asked)
	} else {
		c.changeAI(mail.Custom, said, asked)
	}
	c.aiSay.SetText("")
}

// writeAI drafts a reply from one of its ideas, the panel open.
func (c *composer) writeAI(idea string) {
	if !c.w.settings.AI() {
		return
	}
	if !c.aiReveal.GetRevealChild() {
		c.toggleAI()
	}
	c.draftAI(idea, idea)
}

// draftAI asks for the text from what it should say, in the user's
// voice, above the signature and the quote.
func (c *composer) draftAI(what, asked string) {
	user := ""
	var examples []string
	if x := c.w.acct(c.d.Account); x != nil {
		user = x.s.Acc.Name
		if user == "" {
			user = x.s.Acc.Address
		}
		examples = c.w.voice(x)
	}
	_, sig, quote := splitDraft(c.text())
	prompt := mail.DraftPrompt(what, c.conv, examples, user, i18n.Lang())
	c.startAI(prompt, asked, "", tail(sig, quote), false)
}

// changeAI asks for the text changed: the selection, else what is
// written above the signature and the quote.
func (c *composer) changeAI(how mail.Polish, arg, asked string) {
	if c.ai.phase != aiIdle {
		return
	}
	text := c.text()
	var start, end gtk.TextIter
	if c.buf.GetSelectionBounds(&start, &end) {
		r := []rune(text)
		s, e := int(start.GetOffset()), int(end.GetOffset())
		part := string(r[s:e])
		c.startAI(mail.PolishPrompt(part, how, arg, true), asked, string(r[:s]), string(r[e:]), true)
		return
	}
	written, sig, quote := splitDraft(text)
	if written == "" {
		c.ai.err = i18n.T("Write something first, or say what to write.")
		c.drawAI()
		return
	}
	c.startAI(mail.PolishPrompt(written, how, arg, false), asked, "", tail(sig, quote), false)
}

// tail is what follows the text written: the signature and the quote.
func tail(sig, quote string) string {
	if sig == "" && quote == "" {
		return ""
	}
	return joinDraft("", sig, quote)
}

// startAI begins a request: the text around what changes is kept, and
// Try again asks the same.
func (c *composer) startAI(prompt, asked, prefix, suffix string, part bool) {
	a := &c.ai
	if a.phase != aiIdle {
		return
	}
	a.before, a.prefix, a.suffix, a.part, a.asked = c.text(), prefix, suffix, part, asked
	a.versions, a.vi = nil, 0
	a.again = func() { c.runAI(prompt) }
	c.runAI(prompt)
}

// runAI asks, the answer coming into the text as a new version.
func (c *composer) runAI(prompt string) {
	a := &c.ai
	a.seq++
	seq := a.seq
	a.phase, a.flipped, a.err = aiWriting, false, ""
	a.versions = append(a.versions, "")
	a.vi = len(a.versions) - 1
	c.body.SetEditable(false)
	c.drawAI()
	c.showSuggestion()
	c.aiStop.GrabFocus() // the focus on what can be done next: Stop, then Keep
	go func() {
		text, err := c.w.ai.ask(prompt, 2*time.Minute, func(t string) {
			if c.closed || seq != a.seq {
				return
			}
			a.versions[a.vi] = strings.TrimSpace(t)
			c.showSuggestion()
		})
		gtkx.Post(func() {
			if c.closed || seq != a.seq {
				return
			}
			text = strings.TrimSpace(text)
			if err != nil || text == "" {
				if err != nil {
					a.err = failed(i18n.T("AI could not write it"), err)
				} else {
					a.err = i18n.T("AI gave no text.")
				}
				c.dropVersion()
				return
			}
			a.versions[a.vi] = text
			a.phase = aiReview
			c.showSuggestion()
			c.drawAI()
			c.aiKeep.GrabFocus()
		})
	}()
}

// dropVersion takes away the version shown (failed, or stopped before
// any text); with none left, the text is as it was.
func (c *composer) dropVersion() {
	a := &c.ai
	a.versions = append(a.versions[:a.vi], a.versions[a.vi+1:]...)
	if len(a.versions) == 0 {
		err := a.err
		c.discardAI()
		a.err = err
		c.drawAI()
		return
	}
	a.vi = len(a.versions) - 1
	a.phase = aiReview
	c.showSuggestion()
	c.drawAI()
	c.aiKeep.GrabFocus()
}

// stopAI stops the answer where it is: what came is a version.
func (c *composer) stopAI() {
	a := &c.ai
	if a.phase != aiWriting {
		return
	}
	a.seq++ // the rest of the answer is dropped
	if a.versions[a.vi] == "" {
		c.dropVersion()
		return
	}
	a.phase = aiReview
	c.drawAI()
	c.aiKeep.GrabFocus()
}

func (c *composer) retryAI() {
	if c.ai.phase == aiReview && c.ai.again != nil {
		c.ai.again()
	}
}

func (c *composer) showVersion(i int) {
	a := &c.ai
	if a.phase != aiReview || i < 0 || i >= len(a.versions) {
		return
	}
	a.vi, a.flipped = i, false
	c.showSuggestion()
	c.drawAI()
	if !c.inPanel() { // the arrow pressed went out at the first or the last
		c.aiKeep.GrabFocus()
	}
}

// showSuggestion puts the version shown into the text, tinted; or the
// text as it was, when that is asked to compare.
func (c *composer) showSuggestion() {
	a := &c.ai
	c.filling = true
	defer func() { c.filling = false }()
	if a.flipped {
		c.buf.SetText(a.before, -1)
		return
	}
	v := a.versions[a.vi]
	c.buf.SetText(a.prefix+v+a.suffix, -1)
	from := int32(utf8.RuneCountInString(a.prefix))
	var start, end gtk.TextIter
	c.buf.GetIterAtOffset(&start, from)
	c.buf.GetIterAtOffset(&end, from+int32(utf8.RuneCountInString(v)))
	c.buf.ApplyTag(a.tag, &start, &end)
}

// keepAI makes the suggestion the text.
func (c *composer) keepAI() {
	a := &c.ai
	if a.phase != aiReview {
		return
	}
	v := a.versions[a.vi]
	c.endAI(a.prefix + v + a.suffix)
	// The cursor after what came, to go on from there.
	var at gtk.TextIter
	c.buf.GetIterAtOffset(&at, int32(utf8.RuneCountInString(a.prefix+v)))
	c.buf.PlaceCursor(&at)
	c.body.GrabFocus()
	c.changed()
}

// discardAI puts the text back as it was.
func (c *composer) discardAI() {
	if c.ai.phase == aiIdle {
		return
	}
	c.ai.seq++
	c.endAI(c.ai.before)
}

func (c *composer) endAI(text string) {
	a := &c.ai
	a.phase, a.versions, a.vi, a.flipped, a.again, a.err = aiIdle, nil, 0, false, nil, ""
	c.filling = true
	c.buf.SetText(text, -1)
	c.filling = false
	c.body.SetEditable(true)
	c.drawAI()
}
