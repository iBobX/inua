package diskui

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"codeberg.org/puregotk/puregotk/v4/glib"
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
		say(&w.selDeleteErr, "")
		say(&w.selItemError, "")
	}
	w.sel = n
	if n != nil && w.done.n != n { // another item chosen: what was done is said
		w.done.n, w.done.text = nil, ""
	}
	say(&w.selDone, w.done.text)
	w.detailsEmpty.SetVisible(n == nil)
	w.details.SetVisible(n != nil)
	if n == nil {
		return
	}
	w.selName.SetText(nodeName(n))
	w.selPath.SetText(disk.Tilde(n.Path(), w.home))
	gtkx.Clear(&w.selFacts)
	w.fact(disk.Size(n.Size), i18n.T("Size"))
	if n.Dir || n.Rest {
		w.fact(count(n.Files), i18n.N("File", "Files", int(n.Files)))
	}
	// a date from the future is a clock's mistake, not news
	if n.Mod > 0 && n.Mod <= time.Now().Unix()+86400 {
		w.fact(i18n.Time(time.Unix(n.Mod, 0), "2006-01-02"), i18n.T("Changed"))
	}
	if n.Denied {
		say(&w.selMeta, i18n.T("Some of it could not be read."))
	} else {
		say(&w.selMeta, "")
	}
	tag := ""
	if r := disk.RuleOf(n); r != nil {
		tag = "[" + r.Kind.Tag() + "]"
	}
	say(&w.selTag, tag)
	w.selMap.SetVisible(w.page == "kind")
	w.selOpen.SetVisible(!n.Rest)
	if n.Rest {
		w.showRestRoute()
		return
	}
	if r := w.routes[n]; r != nil {
		w.showRoute()
		return
	}
	gtkx.Clear(&w.selSteps)
	w.routeNode("", sub(i18n.T("Checking…")), true)
	w.showVerdict(disk.Unknown, false)
	w.selCleanBox.SetVisible(false)
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

// fact is a figure of the item chosen with its name under it.
func (w *window) fact(value, name string) {
	box := gtk.NewBox(gtk.OrientationVerticalValue, 1)
	v := gtk.NewLabel(value)
	v.AddCssClass("disk-fact")
	v.SetXalign(0)
	v.SetEllipsize(3) // end
	gtkx.Append(box, &v.Widget)
	k := gtk.NewLabel(name)
	k.AddCssClass("disk-fact-name")
	k.SetXalign(0)
	gtkx.Append(box, &k.Widget)
	gtkx.Append(&w.selFacts, &box.Widget)
}

func setVerdictClass(l *gtk.Label, v disk.Verdict) {
	for _, c := range []string{"safe", "review", "keep", "unknown"} {
		l.RemoveCssClass(c)
	}
	l.AddCssClass(verdictClass(v))
}

// showVerdict puts v in the section's head, marked when the AI gave it.
func (w *window) showVerdict(v disk.Verdict, byAI bool) {
	text := v.Tag()
	if text != "" && byAI {
		text = i18n.Tf("AI · %s", text)
	}
	w.selVerdict.SetText(text)
	w.selVerdict.SetVisible(text != "")
	setVerdictClass(&w.selVerdict, v)
}

// sub is a line of quiet text.
func sub(text string) *gtk.Widget {
	l := gtk.NewLabel(text)
	l.AddCssClass("sub")
	l.SetXalign(0)
	l.SetWrap(true)
	return &l.Widget
}

// routeNode adds a node to the route: its mark (a class: "" for a check
// passed, "on" for the one that settled it, a verdict's class for the
// verdict, "ai" for asking the AI), the line on to the next unless it is
// the last, and what it says.
func (w *window) routeNode(mark string, body *gtk.Widget, last bool) {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	row.AddCssClass("disk-node")
	row.SetVexpand(false) // the line grows within the row, not the row
	rail := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	rail.AddCssClass("disk-rail")
	m := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	m.AddCssClass("mark")
	if mark != "" {
		m.AddCssClass(mark)
	}
	m.SetHalign(gtk.AlignCenterValue)
	gtkx.Append(rail, &m.Widget)
	if !last {
		line := gtk.NewBox(gtk.OrientationVerticalValue, 0)
		line.AddCssClass("disk-line")
		line.SetHalign(gtk.AlignCenterValue)
		line.SetVexpand(true)
		gtkx.Append(rail, &line.Widget)
	}
	gtkx.Append(row, &rail.Widget)
	body.AddCssClass("disk-node-body")
	body.SetHexpand(true)
	gtkx.Append(row, body)
	gtkx.Append(&w.selSteps, &row.Widget)
}

// showRestRoute is the panel of a folder's small files counted together:
// they are not one thing to delete.
func (w *window) showRestRoute() {
	gtkx.Clear(&w.selSteps)
	w.routeNode("", sub(i18n.T("Small files, each under 1 MB.")), true)
	w.showVerdict(disk.Unknown, false)
	w.selCleanBox.SetVisible(false)
	w.setDeletable(false, i18n.T("Not one item to delete."))
}

// showRoute shows how the item chosen came to its verdict: each check,
// the verdict when one settled it, and the AI, asked or to ask.
func (w *window) showRoute() {
	n := w.sel
	r := w.routes[n]
	gtkx.Clear(&w.selSteps)
	for _, s := range r.Steps {
		body := gtk.NewBox(gtk.OrientationVerticalValue, 1)
		q := gtk.NewLabel(s.Check)
		q.AddCssClass("disk-step-check")
		q.SetXalign(0)
		q.SetWrap(true)
		gtkx.Append(body, &q.Widget)
		a := gtk.NewLabel(s.Found)
		a.AddCssClass("sub")
		a.SetXalign(0)
		a.SetWrap(true)
		gtkx.Append(body, &a.Widget)
		mark := ""
		if s.Verdict != disk.Unknown {
			mark = "on"
		}
		w.routeNode(mark, &body.Widget, false)
	}
	if r.Verdict != disk.Unknown {
		l := gtk.NewLabel(r.Verdict.Name())
		l.AddCssClass("disk-verdict")
		l.SetXalign(0)
		setVerdictClass(l, r.Verdict)
		w.routeNode(verdictClass(r.Verdict), &l.Widget, false)
	}
	w.routeNode("ai", w.aiNode(n, r.Verdict == disk.Unknown), true)

	verdict, byAI := r.Verdict, false
	if a := w.answers[n]; verdict == disk.Unknown && a != nil && a.ok {
		verdict, byAI = a.Verdict, true
	}
	w.showVerdict(verdict, byAI)
	w.showCommand()

	switch {
	case n.Parent == nil:
		w.setDeletable(false, "")
	case verdict == disk.Keep, verdict == disk.Safe:
		w.setDeletable(verdict == disk.Safe, "")
	case verdict == disk.Review:
		w.setDeletable(true, i18n.T("Look inside first."))
	default:
		w.setDeletable(true, i18n.T("Look inside first, or ask the AI."))
	}
}

// aiNode is the route's end: the button to ask the AI, its answer as it
// comes (whether it can go and why, what it is), or why asking failed.
// first is whether no check settled it, the AI's answer the verdict.
func (w *window) aiNode(n *disk.Node, first bool) *gtk.Widget {
	box := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	a := w.answers[n]
	asking := w.askingAI == n
	if a != nil && a.ok {
		v := gtk.NewLabel(a.Verdict.Name())
		v.AddCssClass("disk-verdict")
		v.SetXalign(0)
		setVerdictClass(v, a.Verdict)
		gtkx.Append(box, aiField(i18n.T("Can it go"), &v.Widget, a.Why))
	}
	if a != nil && a.What != "" {
		gtkx.Append(box, aiField(i18n.T("What it is"), nil, a.What))
	}
	switch {
	case asking:
		gtkx.Append(box, sub(i18n.T("Asking…")))
	case a != nil && a.err != "":
		l := gtk.NewLabel(a.err)
		l.AddCssClass("sub")
		l.AddCssClass("disk-error")
		l.SetXalign(0)
		l.SetWrap(true)
		l.SetSelectable(true)
		gtkx.Append(box, &l.Widget)
	}
	if !asking {
		label := i18n.T("Ask AI")
		if a != nil && a.ok {
			label = i18n.T("Ask again")
		}
		b := gtk.NewButtonWithLabel(label)
		b.AddCssClass("outlined")
		if !first || (a != nil && a.ok) {
			b.AddCssClass("disk-ask-quiet")
		}
		b.SetHalign(gtk.AlignStartValue)
		b.SetTooltipText(i18n.T("Sends the path, the size and the largest names, never the files' contents."))
		b.SetSensitive(w.askingAI == nil)
		b.ConnectClicked(&w.onAsk)
		gtkx.Append(box, &b.Widget)
	}
	return &box.Widget
}

// aiField is a field of the AI's answer: its name, a headline when there
// is one, and the text.
func aiField(name string, headline *gtk.Widget, text string) *gtk.Widget {
	box := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	k := gtk.NewLabel(name)
	k.AddCssClass("disk-fact-name")
	k.SetXalign(0)
	gtkx.Append(box, &k.Widget)
	if headline != nil {
		gtkx.Append(box, headline)
	}
	if text != "" {
		t := gtk.NewLabel(text)
		t.AddCssClass("disk-ai-text")
		t.SetXalign(0)
		t.SetWrap(true)
		t.SetSelectable(true)
		gtkx.Append(box, &t.Widget)
	}
	return &box.Widget
}

func (w *window) setDeletable(on bool, note string) {
	w.selTrash.SetSensitive(on)
	w.selDelete.SetSensitive(on)
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

// aiAnswer is what the AI said of an item: the fields as they came, and
// why asking failed.
type aiAnswer struct {
	disk.Answer
	ok  bool // the verdict has come
	err string
}

// askAI asks the AI whether the item chosen can go and what it is, the
// answer showing as it comes; one item at a time.
func (w *window) askAI() {
	n := w.sel
	if n == nil || n.Rest || w.askingAI != nil {
		return
	}
	r := w.routes[n]
	if r == nil {
		r = &disk.Route{}
	}
	w.askingAI = n
	w.answers[n] = &aiAnswer{}
	w.showRoute()
	prompt := disk.AskPrompt(disk.Facts(n, *r, w.home), i18n.Lang())
	var err error
	var text string
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
		text, err = as.Ask(ctx, prompt, assistant.Events{Answer: func(text string) {
			gtkx.Post(func() {
				if w.closed || w.askingAI != n {
					return
				}
				a, ok := disk.ParseAnswer(text)
				w.answers[n] = &aiAnswer{Answer: a, ok: ok}
				if w.sel == n {
					w.showRoute()
				}
			})
		}})
	}, func() {
		w.askingAI = nil
		a, ok := disk.ParseAnswer(text)
		ans := &aiAnswer{Answer: a, ok: ok}
		switch {
		case err != nil:
			ans.err = i18n.Tf("The AI could not answer: %v", err)
		case !ok:
			ans.err = i18n.Tf("The AI answered %q", strings.TrimSpace(text))
		}
		w.answers[n] = ans
		if w.sel != nil && w.routes[w.sel] != nil && !w.sel.Rest {
			w.showRoute() // the button is free again, whichever is chosen
		}
	})
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

// remove moves n to the trash, or deletes it for good, takes it off the
// map, and says so on the folder it was in.
func (w *window) remove(n *disk.Node, forGood bool) {
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
		text := i18n.Tf("Moved %s to the trash.", n.Name)
		if forGood {
			text = i18n.Tf("Deleted %s: %s freed.", n.Name, disk.Size(n.Size))
		}
		w.gone(n, n.Parent, text)
	})
}

// gone shows the tree after n left it (n.Remove has not run yet) or changed:
// the folder in view, the lists and totals, and text said on then, the item
// chosen now.
func (w *window) gone(n, then *disk.Node, text string) {
	if n != then {
		n.Remove()
		delete(w.routes, n)
		delete(w.answers, n)
		delete(w.runs, n)
	}
	w.done.n, w.done.text = then, text
	w.fillSide()
	if w.page == "kind" {
		w.showKind(w.kind)
		w.selectNode(then)
		return
	}
	s := w.scans[w.place]
	if s == nil || s.focus == nil {
		return
	}
	f := s.focus
	for a := f; a != nil; a = a.Parent {
		if a == n && n != then { // the folder in view went with it
			f = then
		}
	}
	w.focus(f)
	w.selectNode(then)
	w.showScan()
}

// askDelete asks before deleting the item chosen for good.
func (w *window) askDelete() {
	n := w.sel
	if n == nil {
		return
	}
	text := i18n.Nf("%s in %s file. This cannot be undone.",
		"%s in %s files. This cannot be undone.", int(n.Files), disk.Size(n.Size), count(n.Files))
	if !n.Dir {
		text = i18n.Tf("%s. This cannot be undone.", disk.Size(n.Size))
	}
	w.confirm(i18n.Tf("Delete %s for good?", nodeName(n)), disk.Tilde(n.Path(), w.home), text, i18n.T("Delete"), true,
		func() { w.remove(n, true) })
}

// --- the clean command ---

// runResult is how a clean command ended: what it printed, and the
// sentence on it.
type runResult struct{ out, done, err string }

// showCommand shows the item chosen's clean command, whether inua can run
// it, and how its last run ended.
func (w *window) showCommand() {
	n := w.sel
	r := w.routes[n]
	if n == nil || r == nil || r.Command.Text == "" {
		w.selCleanBox.SetVisible(false)
		return
	}
	c := r.Command
	w.selCleanBox.SetVisible(true)
	w.selClean.SetText(c.Text)
	w.selRun.SetSensitive(c.Not == "" && w.running == nil)
	if w.running == n {
		w.selRun.SetLabel(i18n.T("Running…"))
	} else {
		w.selRun.SetLabel(i18n.T("Run"))
	}
	note := c.Not
	switch {
	case note != "":
	case c.Root:
		note = i18n.T("Runs as root, after your password.")
	default:
		note = i18n.Tf("Runs in %s.", disk.Tilde(c.Dir, w.home))
	}
	w.selRunNote.SetText(note)
	res := w.runs[n]
	say(&w.selRunOut, res.out)
	say(&w.selRunDone, res.done)
	say(&w.selRunError, res.err)
}

// copyCommand puts the clean command on the clipboard.
func (w *window) copyCommand() {
	r := w.routes[w.sel]
	if r == nil || r.Command.Text == "" {
		return
	}
	w.win.GetClipboard().SetText(r.Command.Text)
	w.selCopy.SetLabel(i18n.T("Copied"))
	w.onCopied = func(uintptr) bool {
		if !w.closed {
			w.selCopy.SetLabel(i18n.T("Copy"))
		}
		return false
	}
	glib.TimeoutAdd(1500, &w.onCopied, 0)
}

// askRun asks before running the clean command of the item chosen.
func (w *window) askRun() {
	n := w.sel
	r := w.routes[n]
	if n == nil || r == nil || r.Command.Not != "" || w.running != nil {
		return
	}
	c := r.Command
	text := i18n.Tf("Cleans %s, %s now.", nodeName(n), disk.Size(n.Size))
	if c.Root {
		text += " " + i18n.T("Runs as root, after your password.")
	}
	w.confirm(i18n.T("Run the clean command?"), c.Text, text, i18n.T("Run"), false, func() { w.run(n, c) })
}

// run runs c, the clean command of n, then measures n again.
func (w *window) run(n *disk.Node, c disk.Command) {
	w.running = n
	w.runs[n] = runResult{}
	if w.sel == n {
		w.showCommand()
	}
	var out string
	var err, rerr error
	var fresh *disk.Node
	w.background(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		out, err = disk.Run(ctx, c)
		fresh, rerr = disk.Rescan(ctx, n, w.home)
	}, func() {
		w.running = nil
		before := n.Size
		res := runResult{out: out}
		if err != nil {
			res.err = i18n.Tf("%s ended with an error: %v", c.Args[0], err)
		}
		if rerr != nil {
			res.err = strings.TrimSpace(res.err + " " + i18n.Tf("Cannot measure it again: %v", rerr))
			w.runs[n] = res
			if w.sel == n {
				w.showCommand()
			}
			return
		}
		after := int64(0)
		if fresh != nil {
			after = fresh.Size
		}
		if freed := before - after; freed > 0 {
			res.done = i18n.Tf("Done: %s freed.", disk.Size(freed))
		} else if err == nil {
			res.done = i18n.T("Done: it freed no room here.")
		}
		if fresh == nil { // the command took the folder away
			w.gone(n, n.Parent, i18n.Tf("%s removed %s: %s freed.", c.Text, n.Name, disk.Size(before)))
			return
		}
		n.Replace(fresh)
		w.runs[n] = res
		w.gone(n, n, "")
	})
}
