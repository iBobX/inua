package diskui

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/disk"
	"github.com/lyricat/inua/internal/assistant"
	"github.com/lyricat/inua/internal/assistant/keys"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// The panel on the right: the item chosen, whether it can go (the route of
// checks, then the assistant's decision when no check settles it), what
// the assistant says it is, and deleting it.

// selectNode shows n in the panel; nil empties it.
func (w *window) selectNode(n *disk.Node) {
	if n != w.sel {
		w.selConfirm.SetVisible(false)
		say(&w.selDeleteErr, "")
		say(&w.selItemError, "")
		say(&w.selAskError, "")
	}
	w.sel = n
	w.detailsEmpty.SetVisible(n == nil)
	w.details.SetVisible(n != nil)
	if n == nil {
		return
	}
	w.selName.SetText(nodeName(n))
	w.selPath.SetText(disk.Tilde(n.Path(), w.home))
	meta := disk.Size(n.Size)
	if n.Dir || n.Rest {
		meta += " · " + i18n.Nf("%s file", "%s files", int(n.Files), count(n.Files))
	}
	if n.Mod > 0 {
		meta += " · " + i18n.Tf("changed %s", i18n.Time(time.Unix(n.Mod, 0), "2006-01-02"))
	}
	if n.Denied {
		meta += " · " + i18n.T("some of it could not be read")
	}
	w.selMeta.SetText(meta)
	tag := ""
	if r := disk.RuleOf(n); r != nil {
		tag = "[" + r.Kind.Tag() + "]"
	}
	w.selTag.SetText(tag)
	w.selMap.SetVisible(w.page == "kind")
	w.selOpen.SetVisible(!n.Rest)
	w.selExplain.SetSensitive(!n.Rest && w.telling == nil)
	w.showExplain()
	if n.Rest {
		w.showRestRoute(n)
		return
	}
	if r := w.routes[n]; r != nil {
		w.showRoute()
		return
	}
	w.selVerdict.SetText(i18n.T("Checking…"))
	setVerdictClass(&w.selVerdict, disk.Unknown)
	gtkx.Clear(&w.selSteps)
	w.selCleanBox.SetVisible(false)
	w.selAskBox.SetVisible(false)
	w.setDeletable(false, "")
	path := n.Path()
	var r disk.Route
	w.background(func() {
		r = disk.Decide(context.Background(), path, w.home)
	}, func() {
		w.routes[n] = &r
		if w.sel == n {
			w.showRoute()
		}
	})
}

func setVerdictClass(l *gtk.Label, v disk.Verdict) {
	for _, c := range []string{"safe", "review", "keep", "unknown"} {
		l.RemoveCssClass(c)
	}
	l.AddCssClass(verdictClass(v))
}

// showRestRoute is the panel of a folder's small files counted together:
// they are not one thing to delete.
func (w *window) showRestRoute(n *disk.Node) {
	gtkx.Clear(&w.selSteps)
	w.selVerdict.SetText(i18n.T("Files of all kinds, each smaller than 1 MB: open the folder in Files to look at them."))
	setVerdictClass(&w.selVerdict, disk.Unknown)
	w.selCleanBox.SetVisible(false)
	w.selAskBox.SetVisible(false)
	w.setDeletable(false, i18n.T("They are not one item to delete."))
}

// showRoute shows how the item chosen came to its verdict.
func (w *window) showRoute() {
	n := w.sel
	r := w.routes[n]
	gtkx.Clear(&w.selSteps)
	for _, s := range r.Steps {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
		row.AddCssClass("disk-step")
		mark := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		mark.AddCssClass("mark")
		if s.Verdict != disk.Unknown {
			mark.AddCssClass("on")
		}
		mark.SetValign(gtk.AlignStartValue)
		gtkx.Append(row, &mark.Widget)
		text := gtk.NewBox(gtk.OrientationVerticalValue, 1)
		text.SetHexpand(true)
		q := gtk.NewLabel(s.Check)
		q.AddCssClass("disk-step-check")
		q.SetXalign(0)
		q.SetWrap(true)
		gtkx.Append(text, &q.Widget)
		a := gtk.NewLabel(s.Found)
		a.AddCssClass("sub")
		a.SetXalign(0)
		a.SetWrap(true)
		gtkx.Append(text, &a.Widget)
		gtkx.Append(row, &text.Widget)
		gtkx.Append(&w.selSteps, &row.Widget)
	}
	verdict := r.Verdict
	if v, ok := w.asked[n]; ok && verdict == disk.Unknown {
		verdict = v
	}
	if r.Verdict == disk.Unknown {
		w.selVerdict.SetText(i18n.T("No check settles it."))
	} else {
		w.selVerdict.SetText(r.Verdict.Name())
	}
	setVerdictClass(&w.selVerdict, r.Verdict)
	w.selClean.SetText(r.Clean)
	w.selCleanBox.SetVisible(r.Clean != "")

	w.selAskBox.SetVisible(r.Verdict == disk.Unknown)
	w.selAsk.SetSensitive(!w.asking[n])
	if w.asking[n] {
		w.selAsk.SetLabel(i18n.T("Asking…"))
	} else {
		w.selAsk.SetLabel(i18n.T("Ask the assistant"))
	}
	if v, ok := w.asked[n]; ok {
		text := i18n.Tf("The assistant: %s", v.Name())
		if why := w.reasons[n]; why != "" {
			text += "\n" + why
		}
		w.selAI.SetText(text)
		setVerdictClass(&w.selAI, v)
		w.selAI.SetVisible(true)
	} else {
		w.selAI.SetVisible(false)
	}

	switch {
	case n.Parent == nil:
		w.setDeletable(false, i18n.T("It is the place scanned: choose what is in it."))
	case verdict == disk.Keep:
		w.setDeletable(false, i18n.T("Kept: the checks above say why."))
	case verdict == disk.Safe:
		w.setDeletable(true, i18n.T("It comes back when needed. The trash keeps it until emptied; Delete frees the room now."))
	case verdict == disk.Review:
		w.setDeletable(true, i18n.T("Look inside before deleting: it may hold your work, settings or history."))
	default:
		w.setDeletable(true, i18n.T("No check settled it: look inside first, or ask the assistant."))
	}
}

func (w *window) setDeletable(on bool, note string) {
	w.selTrash.SetSensitive(on)
	w.selDelete.SetSensitive(on)
	if !on {
		w.selConfirm.SetVisible(false)
	}
	w.selDeleteNote.SetText(note)
	w.selDeleteNote.SetVisible(note != "")
}

// --- the assistant ---

// aiState is the assistant, made once it is first needed: reading its keys
// may ask for the keyring's passphrase.
type aiState struct {
	asking sync.Mutex // a question at a time: the conversation answers one
	mu     sync.Mutex
	as     *assistant.Assistant
	err    error
	cancel context.CancelFunc
	ctx    context.Context
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

// askDecision asks the assistant's decision profile whether the item
// chosen can go, when no check settled it.
func (w *window) askDecision() {
	n := w.sel
	r := w.routes[n]
	if n == nil || r == nil || w.asking[n] {
		return
	}
	w.asking[n] = true
	say(&w.selAskError, "")
	w.showRoute()
	facts := disk.Facts(n, *r, w.home)
	var v disk.Verdict
	var why string
	var err error
	w.background(func() {
		as, ctx, e := w.ai.get()
		if e != nil {
			err = e
			return
		}
		ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		// The decision route's profile, the one for quick choices; else
		// the one that answers, asked for a word and a reason.
		if len(as.Choosers()) > 0 {
			i, e := as.Choose(ctx, "", disk.ChooseInstructions, facts, disk.ChooseOptions)
			if e == nil {
				v = disk.Verdicts[i]
				return
			}
			if !errors.Is(e, assistant.ErrNoChoice) {
				err = e
				return
			}
		}
		w.ai.asking.Lock()
		defer w.ai.asking.Unlock()
		as.Reset()
		answer, e := as.Ask(ctx, disk.DecidePrompt(facts, i18n.Lang()), assistant.Events{})
		if e != nil {
			err = e
			return
		}
		var ok bool
		if v, why, ok = disk.ParseDecision(answer); !ok {
			err = errors.New(i18n.Tf("it answered %q", answer))
		}
	}, func() {
		delete(w.asking, n)
		if err == nil {
			w.asked[n] = v
			w.reasons[n] = why
		}
		if w.sel == n {
			if err != nil {
				say(&w.selAskError, i18n.Tf("The assistant could not decide: %v", err))
			}
			w.showRoute()
		}
	})
}

// explain asks the assistant what the item chosen is, the answer showing
// as it comes.
func (w *window) explain() {
	n := w.sel
	if n == nil || n.Rest || w.telling != nil {
		return
	}
	r := w.routes[n]
	if r == nil {
		r = &disk.Route{}
	}
	w.telling = n
	delete(w.failed, n)
	w.explains[n] = ""
	w.selExplain.SetSensitive(false)
	w.selExplain.SetLabel(i18n.T("Asking…"))
	w.showExplain()
	prompt := disk.ExplainPrompt(disk.Facts(n, *r, w.home), i18n.Lang())
	var err error
	w.background(func() {
		as, ctx, e := w.ai.get()
		if e != nil {
			err = e
			return
		}
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		w.ai.asking.Lock()
		defer w.ai.asking.Unlock()
		as.Reset() // each question on its own
		_, err = as.Ask(ctx, prompt, assistant.Events{Answer: func(text string) {
			gtkx.Post(func() {
				if w.closed || w.telling != n {
					return
				}
				w.explains[n] = text
				if w.sel == n {
					w.showExplain()
				}
			})
		}})
	}, func() {
		w.telling = nil
		if err != nil {
			w.failed[n] = i18n.Tf("The assistant could not answer: %v", err)
			if w.explains[n] == "" {
				delete(w.explains, n)
			}
		}
		w.selExplain.SetLabel(i18n.T("Explain"))
		if w.sel != nil {
			w.selExplain.SetSensitive(!w.sel.Rest)
		}
		if w.sel == n {
			w.showExplain()
		}
	})
}

func (w *window) showExplain() {
	n := w.sel
	text, ok := w.explains[n]
	switch {
	case ok && text == "":
		w.selExplainText.SetText(i18n.T("Asking…"))
		w.selExplainText.SetVisible(true)
	case ok:
		w.selExplainText.SetText(text)
		w.selExplainText.SetVisible(true)
	default:
		w.selExplainText.SetVisible(false)
	}
	say(&w.selExplainError, w.failed[n])
	if w.telling == nil {
		if ok && text != "" {
			w.selExplain.SetLabel(i18n.T("Explain again"))
		} else {
			w.selExplain.SetLabel(i18n.T("Explain"))
		}
	}
}

// --- acting on it ---

// openInFiles shows the item in Files: a folder opened, a file selected
// in its folder.
func (w *window) openInFiles() {
	n := w.sel
	if n == nil {
		return
	}
	path := n.Path()
	say(&w.selItemError, "")
	if bin, err := exec.LookPath("inua-files"); err == nil {
		if err := desktop.Spawn([]string{bin, path}, ""); err == nil {
			return
		}
	}
	dir := path
	if !n.Dir {
		dir = filepath.Dir(path)
	}
	if err := desktop.Spawn([]string{"xdg-open", dir}, ""); err != nil {
		say(&w.selItemError, i18n.Tf("Cannot open it in Files: %v", err))
	}
}

// remove moves the item chosen to the trash, or deletes it for good, and
// takes it off the map.
func (w *window) remove(forGood bool) {
	n := w.sel
	if n == nil || n.Parent == nil || n.Rest {
		return
	}
	path := n.Path()
	w.selTrash.SetSensitive(false)
	w.selDelete.SetSensitive(false)
	say(&w.selDeleteErr, "")
	if forGood {
		w.selDeleteNote.SetText(i18n.T("Deleting…"))
	} else {
		w.selDeleteNote.SetText(i18n.T("Moving to the trash…"))
	}
	var err error
	w.background(func() {
		if forGood {
			err = disk.Delete(path)
		} else {
			err = disk.Trash(path)
		}
	}, func() {
		if err != nil {
			if w.sel == n {
				say(&w.selDeleteErr, i18n.Tf("Cannot delete %s: %v", n.Name, err))
				w.showRoute()
			}
			return
		}
		parent := n.Parent
		n.Remove()
		delete(w.routes, n)
		delete(w.explains, n)
		w.fillSide()
		switch {
		case w.page == "kind":
			w.showKind(w.kind)
			w.selectNode(nil)
		default:
			s := w.scans[w.place]
			f := s.focus
			for a := f; a != nil; a = a.Parent {
				if a == n { // the folder in view went with it
					f = parent
				}
			}
			w.focus(f)
			w.selectNode(parent)
			w.showScan()
		}
	})
}
