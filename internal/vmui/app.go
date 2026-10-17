// Package vmui is inua-vm's window: the Windows guest's manager, one page for
// each stage of its life (docs/inua-vm.md). The logic, the host checks and the
// Docker backend are internal/vm; this is the view. cmd/inua-vm runs the same
// pieces without a display for --doctor, --status, --start and --stop.
package vmui

import (
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/i18n"
)

// App is the manager: one window, one guest.
type App struct{}

// Register makes the manager a light app of its own, as inua-files and
// inua-viewer are: inua-vm opens it, and a second start brings its window
// forward.
func Register() {
	appui.Register("vm", "VM", i18n.M("Windows VM"), func() appui.View { return &App{} })
}

// Build makes the window and the service behind it.
func (a *App) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	service := newService()
	w := openWindow(c, service)
	return w.win
}

// ThemeChanged is empty: everything the window draws comes from the layout
// and the style, which appui reloads when the theme changes.
func (a *App) ThemeChanged() {}

// Start brings the guest up: `inua-vm --start`, for scripts and for a session
// that wants Windows running before anyone opens its window.
func Start() error {
	s := newService()
	defer s.close()
	return s.Start()
}

// Stop shuts the guest down, keeping its disk: `inua-vm --stop`.
func Stop() error {
	s := newService()
	defer s.close()
	return s.Stop()
}
