// Package lightui has inua-apps' light apps: Clock, Weather, Stocks,
// Calculator, Calendar, Color Picker, System Monitor, Tailscale, Pass,
// GPG, Music and Video, and the workshop's apps. They run through appui.
package lightui

import (
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/i18n"
)

func init() {
	for name, a := range map[string]struct {
		id, title string
		make      func() appui.View
	}{
		"clock":          {"Clock", i18n.M("Clock"), func() appui.View { return &clockApp{} }},
		"weather":        {"Weather", i18n.M("Weather"), func() appui.View { return &weatherApp{} }},
		"stocks":         {"Stocks", i18n.M("Stocks"), func() appui.View { return &stocksApp{} }},
		"calculator":     {"Calculator", i18n.M("Calculator"), func() appui.View { return &calcApp{} }},
		"calendar":       {"Calendar", i18n.M("Calendar"), func() appui.View { return &calendarApp{} }},
		"color-picker":   {"ColorPicker", i18n.M("Color Picker"), func() appui.View { return &colorApp{} }},
		"system-monitor": {"SystemMonitor", i18n.M("System Monitor"), func() appui.View { return &sysmonApp{} }},
		"tailscale":      {"Tailscale", i18n.M("Tailscale"), func() appui.View { return &tailscaleApp{} }},
		"pass":           {"Pass", i18n.M("Pass"), func() appui.View { return &passApp{} }},
		"gpg":            {"GPG", i18n.M("GPG"), func() appui.View { return &gpgApp{} }},
	} {
		appui.Register(name, a.id, a.title, a.make)
	}
	// Started again with files, the running one plays them.
	appui.RegisterOpener("music", "Music", i18n.M("Music"), func() appui.View { return &musicApp{} })
	appui.RegisterOpener("video", "Video", i18n.M("Video"), func() appui.View { return &videoApp{} })
}
