package mailui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lyricat/inua/internal/apps/mail"
	"github.com/lyricat/inua/internal/assistant"
	"github.com/lyricat/inua/internal/assistant/keys"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// aiState is the assistant, made the first time it is asked.
type aiState struct {
	asking sync.Mutex // one question at a time: the assistant keeps one conversation
	mu     sync.Mutex
	as     *assistant.Assistant
	ctx    context.Context
	cancel context.CancelFunc
}

// get makes the assistant, or says why it cannot; off the main thread.
func (a *aiState) get() (*assistant.Assistant, context.Context, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ctx == nil {
		a.ctx, a.cancel = context.WithCancel(context.Background())
	}
	if a.as != nil {
		return a.as, a.ctx, nil
	}
	cfg, _, err := config.Load(config.DefaultPath())
	if err != nil {
		cfg = config.Default()
	}
	if !cfg.Assistant.Enable {
		return nil, a.ctx, errors.New(i18n.T("The assistant is turned off. Turn it on in Settings → AI."))
	}
	c := cfg.Assistant
	c.ServeEnable = false
	set, err := assistant.Load(c, keys.New())
	if err != nil {
		return nil, a.ctx, err
	}
	set.Tools = nil // it only answers
	as, err := assistant.New(set, nil, nil)
	if err != nil {
		return nil, a.ctx, err
	}
	a.as = as
	return as, a.ctx, nil
}

func (a *aiState) close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
}

// ask asks one question on its own, streaming the answer to each (on the
// main thread); it returns the whole answer.
func (a *aiState) ask(prompt string, timeout time.Duration, each func(string)) (string, error) {
	as, ctx, err := a.get()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	a.asking.Lock()
	defer a.asking.Unlock()
	as.Reset()
	ev := assistant.Events{}
	if each != nil {
		ev.Answer = func(text string) { gtkx.Post(func() { each(text) }) }
	}
	return as.Ask(ctx, prompt, ev)
}

// aiText is an answer of the assistant's as it comes.
type aiText struct {
	text string
	err  string
	busy bool
}

func (a *aiText) sig() string {
	if a == nil {
		return "-"
	}
	return fmt.Sprintf("%v%d%s", a.busy, len(a.text), a.err)
}

func failed(prefix string, err error) string {
	if errors.Is(err, context.Canceled) {
		return ""
	}
	return i18n.Tf("%s: %v", prefix, err)
}

// --- the conversation ---

// keptSummary is a conversation's summary kept from before, when it has
// had no message since.
func (w *window) keptSummary(c *mail.Conversation) string {
	latest := c.Latest()
	x := w.acct(latest.Account)
	if x == nil {
		return ""
	}
	return x.s.Summary(latest.Mailbox, summaryKey(c))
}

// summaryKey names a conversation as it is now: a new message makes the
// summary kept old.
func summaryKey(c *mail.Conversation) string {
	return fmt.Sprintf("%s#%d", c.ID, len(c.Messages))
}

func (w *window) summarize(c *mail.Conversation) {
	msgs := w.said(c)
	if len(msgs) < len(c.Messages) {
		w.actErr = i18n.T("The messages are still loading.")
		w.drawReader()
		return
	}
	a := &aiText{busy: true}
	w.sumState[c.ID] = a
	w.drawReader()
	latest := c.Latest()
	user := latest.Account
	if x := w.acct(latest.Account); x != nil {
		user = x.s.Acc.Address
	}
	prompt := mail.SummaryPrompt(msgs, user, time.Now(), i18n.Lang())
	go func() {
		text, err := w.ai.ask(prompt, 2*time.Minute, func(t string) {
			a.text = strings.TrimSpace(t)
			if w.openID == c.ID {
				w.drawReader()
			}
		})
		gtkx.Post(func() {
			a.busy = false
			if err != nil {
				a.err = failed(i18n.T("The assistant could not sum it up"), err)
			} else {
				a.text = strings.TrimSpace(text)
				if x := w.acct(latest.Account); x != nil {
					x.s.SaveSummary(latest.Mailbox, summaryKey(c), a.text)
				}
			}
			if w.openID == c.ID {
				w.drawReader()
			}
		})
	}()
}

func (w *window) askIdeas(c *mail.Conversation) {
	msgs := w.said(c)
	if len(msgs) == 0 {
		return
	}
	a := &aiText{busy: true}
	w.ideas[c.ID] = a
	w.drawReader()
	user := ""
	if x := w.acct(c.Latest().Account); x != nil {
		user = x.s.Acc.Address
	}
	if len(msgs) > 6 {
		msgs = msgs[len(msgs)-6:]
	}
	prompt := mail.ReplyIdeasPrompt(msgs, user, i18n.Lang())
	go func() {
		text, err := w.ai.ask(prompt, time.Minute, nil)
		gtkx.Post(func() {
			a.busy = false
			if err != nil {
				a.err = failed(i18n.T("The assistant could not suggest replies"), err)
			} else if ideas := mail.ParseIdeas(text); len(ideas) > 0 {
				a.text = strings.Join(ideas, "\n")
			} else {
				a.err = i18n.T("The assistant gave no replies.")
			}
			if w.openID == c.ID {
				w.drawReader()
			}
		})
	}()
}

// askJudge asks the assistant whether a message is phishing, with the
// checks' findings.
func (w *window) askJudge(k mail.Key) {
	rd := w.reading[k]
	if rd == nil || rd.m == nil {
		return
	}
	a := &aiText{busy: true}
	w.judged[k] = a
	w.drawReader()
	prompt := mail.JudgePrompt(mail.JudgeFacts(rd.m, rd.judged), i18n.Lang())
	go func() {
		text, err := w.ai.ask(prompt, time.Minute, nil)
		gtkx.Post(func() {
			a.busy = false
			if err != nil {
				a.err = failed(i18n.T("The assistant could not judge it"), err)
			} else if v, why, ok := mail.ParseJudge(text); ok {
				a.text = v.Name()
				if why != "" {
					a.text += ": " + why
				}
			} else {
				a.err = i18n.Tf("The assistant answered %q", strings.TrimSpace(text))
			}
			w.drawReader()
		})
	}()
}

// askSearch turns a sentence into the search's operators, shows them in
// the entry, and searches.
func (w *window) askSearch(sentence string) {
	w.say(&w.searchNote, i18n.T("Asking the assistant what to search for…"))
	prompt := mail.SearchPrompt(sentence, time.Now())
	go func() {
		text, err := w.ai.ask(prompt, time.Minute, nil)
		gtkx.Post(func() {
			if strings.TrimSpace(w.search.GetText()) != sentence {
				return // the user typed on
			}
			if err != nil {
				w.say(&w.searchNote, failed(i18n.T("The assistant could not search"), err))
				return
			}
			q := mail.ParseSearch(text)
			if q == "" {
				w.say(&w.searchNote, i18n.T("The assistant gave no search."))
				return
			}
			w.asked = q
			w.query = q
			w.parsed = mail.ParseQuery(q, time.Now())
			w.search.SetText(q)
			w.search.SetPosition(-1)
			w.say(&w.searchNote, i18n.Tf("Searched for %s", q))
			w.refresh()
			w.searchServer()
		})
	}()
}

// sortWithAssistant asks the decision profile for each message's tag,
// one after another; with no decision profile, the profile that answers
// is not asked (sorting is a cheap call or none).
func (w *window) sortWithAssistant(x *acct, inbox string, es []mail.Envelope) {
	if w.sorting {
		return
	}
	w.sorting = true
	user := x.s.Acc.Address
	go func() {
		defer gtkx.Post(func() { w.sorting = false })
		as, ctx, err := w.ai.get()
		if err != nil || len(as.Choosers()) == 0 {
			return
		}
		for _, e := range es {
			if ctx.Err() != nil {
				return
			}
			c, cancel := context.WithTimeout(ctx, 30*time.Second)
			i, err := as.Choose(c, "", mail.SortInstructions, mail.SortState(e, user), mail.SortOptions)
			cancel()
			if err != nil {
				if errors.Is(err, assistant.ErrNoChoice) {
					continue
				}
				return
			}
			x.s.SetTag(inbox, e.UID, mail.SortTags[i], false)
			gtkx.Post(func() { w.refresh(); w.drawSide() })
		}
	}()
}
