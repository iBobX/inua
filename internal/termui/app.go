package termui

import (
	"cmp"
	"log"
	"os"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/i18n"
)

// App is inua-term's window: for the spike, one pane running the user's
// shell in their home folder.
type App struct {
	c    *appui.Ctx
	win  *gtk.ApplicationWindow
	pane *termPane

	onClose func(gtk.Window) bool
}

// Register makes inua-term an appui app, "term".
func Register() {
	appui.Register("term", "Term", i18n.M("Terminal"), func() appui.View { return &App{} })
}

func (a *App) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	a.c = c
	a.win = gtk.NewApplicationWindow(c.App)
	a.win.SetTitle(i18n.T("Terminal"))
	a.win.SetDefaultSize(960, 620)
	home, _ := os.UserHomeDir()
	shell := cmp.Or(os.Getenv("SHELL"), "/bin/sh")
	env := append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=inua-term")
	p, err := newTermPane([]string{shell}, home, env, c.Colors)
	if err != nil {
		log.Printf("term: %v", err)
		label := gtk.NewLabel(err.Error())
		label.SetWrap(true)
		a.win.SetChild(&label.Widget)
		return a.win
	}
	a.pane = p
	a.win.SetChild(p.widget)
	p.widget.GrabFocus()
	a.onClose = func(gtk.Window) bool {
		p.close()
		return false
	}
	a.win.ConnectCloseRequest(&a.onClose)
	return a.win
}

func (a *App) ThemeChanged() {
	if a.pane == nil {
		return
	}
	a.pane.fg, a.pane.bg, a.pane.cursor = rgba(a.c.Colors["t0"]), rgba(a.c.Colors["app"]), rgba(a.c.Colors["accent"])
	a.pane.widget.QueueDraw()
}
