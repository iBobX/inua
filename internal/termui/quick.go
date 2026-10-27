package termui

import (
	"log"
	"os"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/agent"
	"github.com/lyricat/inua/internal/term/host"
	"github.com/lyricat/inua/internal/term/pty"
	"github.com/lyricat/inua/internal/term/shellint"
)

// The quick mode (inua-term --quick) is a terminal for a moment: one
// window, one shell, nothing else. No host, no workspace or session,
// nothing kept for later; the window closes with its shell, and its shell
// with the window. Each start is a window of its own.

// QuickCommand is what a quick window runs instead of the shell (-e).
var QuickCommand []string

// RegisterQuick makes the quick mode an appui app, "term-quick".
func RegisterQuick() {
	appui.RegisterMany("term-quick", "Term.Quick", i18n.M("Terminal"), func() appui.View { return &App{quick: true} })
}

// quickTerm is a quick window's pane and its program.
type quickTerm struct {
	pane    *termPane
	pty     *pty.PTY
	title   string
	closing bool // the window is closing: its shell ends with it

	onClose func(gtk.Window) bool
}

// buildQuick is the quick window: the pane, and only it.
func (a *App) buildQuick(c *appui.Ctx) *gtk.ApplicationWindow {
	q := &a.quickTerm
	a.c = c
	a.win = gtk.NewApplicationWindow(c.App)
	a.win.SetTitle(i18n.T("Terminal"))
	a.win.SetDefaultSize(900, 560)
	a.win.AddCssClass("term")
	a.win.AddCssClass("term-quick")
	p, err := newTermPane(quickBackend{q}, 80, 24, a.scheme(), c.Colors["warn"], a.look())
	if err != nil {
		log.Printf("term: %v", err)
		a.win.SetChild(&gtk.NewLabel(err.Error()).Widget)
		return a.win
	}
	q.pane = p
	p.term.Answer() // no host answers its program's questions
	p.chord = a.quickChord
	p.focused = func() {}
	p.wrote = func() { a.quickWrote() }
	a.win.SetChild(&p.root.Widget)
	q.onClose = func(gtk.Window) bool {
		q.closing = true
		if q.pty != nil {
			q.pty.Close() // the shell's terminal hangs up
		}
		return false
	}
	a.win.ConnectCloseRequest(&q.onClose)
	p.widget.GrabFocus()
	go a.startQuick()
	return a.win
}

// startQuick starts the shell (or QuickCommand) off the main thread,
// where inua-term was started, and reads it until it ends; the window
// closes then.
func (a *App) startQuick() {
	q := &a.quickTerm
	sh := a.set.Shell.Program
	if sh == "" {
		sh = os.Getenv("SHELL")
	}
	if sh == "" {
		sh = "/bin/sh"
	}
	argv, env := QuickCommand, host.ShellEnviron(sh)
	if len(argv) == 0 {
		argv = []string{sh}
		if err := shellint.Install(shellint.Dir()); err == nil {
			argv, env = shellint.Wrap(argv, env, shellint.Dir())
		}
	}
	dir, err := os.Getwd()
	if err != nil {
		dir, _ = os.UserHomeDir()
	}
	// 80 × 24 to start: the window's size follows as soon as it is known.
	pt, err := pty.Start(argv, dir, env, 80, 24)
	if err != nil {
		log.Printf("term: %v", err)
		q.pane.feed([]byte(err.Error() + "\r\n"))
		return
	}
	gtkx.Post(func() {
		q.pty = pt
		if q.closing {
			pt.Close()
			return
		}
		pt.Resize(q.pane.cols, q.pane.rows)
	})
	buf := make([]byte, 32*1024)
	for {
		n, err := pt.Read(buf)
		if n > 0 {
			q.pane.feed(append([]byte(nil), buf[:n]...))
		}
		if err != nil {
			break
		}
	}
	pt.Wait()
	gtkx.Post(func() {
		if !q.closing {
			a.win.Close()
		}
	})
}

// quickWrote does what the host does for a pane: its program's
// questions answered, its clipboard writes, its title on the window.
func (a *App) quickWrote() {
	q := &a.quickTerm
	if b := q.pane.term.Answers(); b != nil && q.pty != nil {
		q.pty.Write(b)
	}
	if text, primary, ok := q.pane.term.Clipboard(); ok {
		q.pane.setClipboard(text, primary)
	}
	if t := q.pane.term.Title(); t != q.title {
		q.title = t
		if t, _ = agent.Title(t); t == "" {
			t = i18n.T("Terminal")
		}
		a.win.SetTitle(t)
	}
}

// quickChord does a quick window's keys: the pane's own (copy, paste,
// search, copy mode) and the font's size; any other goes to the program.
func (a *App) quickChord(p *termPane, base, _ uint32, state gdk.ModifierType) bool {
	held := state & (gdk.ControlMaskValue | gdk.ShiftMaskValue | gdk.AltMaskValue | gdk.SuperMaskValue)
	switch action := a.keymap[chordName(base, held)]; action {
	case "copy", "paste", "find", "copy_mode":
		return a.do(action, p)
	case "font_larger":
		a.zoomFont(1)
	case "font_smaller":
		a.zoomFont(-1)
	case "font_reset":
		a.zoomFont(0)
	default:
		return false
	}
	return true
}

// quickBackend is a quick pane's program, on its own terminal.
type quickBackend struct{ q *quickTerm }

func (b quickBackend) Input(p []byte) error {
	if b.q.pty == nil {
		return errNoHost
	}
	_, err := b.q.pty.Write(p)
	return err
}

// Resize sizes the terminal; the pane's engine takes the size at once
// (errNoSizes), there being no host to tell it later.
func (b quickBackend) Resize(cols, rows, _, _ int) error {
	if b.q.pty != nil {
		b.q.pty.Resize(cols, rows)
	}
	return errNoSizes
}
