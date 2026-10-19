// Package diskui is inua-disk-usage's window, Disk Usage (docs/disk-usage.md).
// The logic is internal/apps/disk; this is the view.
package diskui

import (
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/i18n"
)

// App is Disk Usage's one window.
type App struct{ w *window }

// Register makes Disk Usage a light app of its own: inua-disk-usage opens it,
// and a second start brings its window forward.
func Register() {
	appui.Register("disk", "DiskUsage", i18n.M("Disk Usage"), func() appui.View { return &App{} })
}

// Build makes the window.
func (a *App) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	a.w = openWindow(c)
	return a.w.win
}

// Open scans the folder of the first start, if one was given.
func (a *App) Open(args []string) {
	if len(args) > 0 && a.w != nil {
		a.w.scanFolder(args[0])
	}
}

// ThemeChanged draws the map again in the theme's colours.
func (a *App) ThemeChanged() {
	if a.w != nil {
		a.w.drawMap()
	}
}
