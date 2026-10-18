// Package remoteui is inua-remote's windows: Remote Desktop, sharing this
// desktop and connecting to other machines' (docs/remote-desktop.md). The
// logic is internal/apps/remotedesk and the protocol internal/gamestream;
// this is the view.
package remoteui

import (
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/i18n"
)

// App is Remote Desktop: one window, and a window for each stream.
type App struct{}

// Register makes Remote Desktop a light app of its own: inua-remote opens
// it, and a second start brings its window forward.
func Register() {
	appui.Register("remote", "Remote", i18n.M("Remote Desktop"), func() appui.View { return &App{} })
}

// Build makes the window.
func (a *App) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	return openWindow(c).win
}

// ThemeChanged is empty: the window's look is its style's, which appui
// reloads.
func (a *App) ThemeChanged() {}
