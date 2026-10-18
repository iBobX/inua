// Package connectui is inua-connect's windows: Mobile Link, the devices paired
// with this computer (docs/mobile-link.md), and the answer to a device's
// notification. The logic is internal/apps/connect, which runs as a
// service of its own; this is the view, which asks it over D-Bus.
package connectui

import (
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/i18n"
)

// App is Mobile Link's window.
type App struct{}

// Register makes Mobile Link a light app of its own: inua-connect opens it,
// and a second start brings its window forward.
func Register() {
	appui.Register("connect", "MobileLink", i18n.M("Mobile Link"), func() appui.View { return &App{} })
}

// Build makes the window.
func (a *App) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	return openWindow(c).win
}

// ThemeChanged is empty: the window's look is its style's, which appui
// reloads.
func (a *App) ThemeChanged() {}

// replyApp is the window answering a notification, one for each.
type replyApp struct{ device, id, title string }

// RegisterReply makes the answer to the device's notification with reply
// ID id an app: each runs on its own.
func RegisterReply(device, id, title string) {
	appui.RegisterMany("connect-reply", "MobileLink.Reply", i18n.M("Reply"), func() appui.View {
		return &replyApp{device: device, id: id, title: title}
	})
}

func (r *replyApp) Build(c *appui.Ctx) *gtk.ApplicationWindow { return openReply(c, r) }

func (r *replyApp) ThemeChanged() {}
