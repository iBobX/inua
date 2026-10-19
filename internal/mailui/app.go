// Package mailui is inua-mail's window, Mail (docs/feat/inua-mail.md).
// The logic is internal/apps/mail; this is the view.
package mailui

import (
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/i18n"
)

// App is Mail's window.
type App struct {
	w       *window
	pending []string // mailto: links given before the window was built
}

// Register makes Mail a light app of its own: inua-mail opens it, and a
// second start brings its window forward, with a mailto: link given.
func Register() {
	appui.RegisterOpener("mail", "Mail", i18n.M("Mail"), func() appui.View { return &App{} })
}

// Build makes the window.
func (a *App) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	a.w = openWindow(c)
	for _, link := range a.pending {
		a.w.mailto(link)
	}
	a.pending = nil
	return a.w.win
}

// Open opens a composer for each mailto: link.
func (a *App) Open(args []string) {
	for _, arg := range args {
		if !strings.HasPrefix(strings.ToLower(arg), "mailto:") {
			continue
		}
		if a.w == nil {
			a.pending = append(a.pending, arg)
		} else {
			a.w.mailto(arg)
		}
	}
}

// ThemeChanged has nothing to redraw: the window is all CSS.
func (a *App) ThemeChanged() {}
