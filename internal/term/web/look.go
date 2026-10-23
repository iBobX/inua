package web

import (
	"cmp"
	"fmt"
	"image/color"
	"log"
	"slices"
	"strings"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/scheme"
	"github.com/lyricat/inua/internal/term/settings"
	"github.com/lyricat/inua/ui"
)

// look is what the page draws with: inua-term's theme, scheme and font on
// this computer, and every theme and scheme there is, for a phone that
// picks its own.
type look struct {
	Theme   string       `json:"theme"`
	Scheme  string       `json:"scheme"` // "" for the theme's colours
	Font    string       `json:"font"`
	Themes  []theme      `json:"themes"`
	Schemes []termScheme `json:"schemes"`
}

type theme struct {
	Name   string            `json:"name"`
	Label  string            `json:"label"`
	Brief  string            `json:"brief"`
	Tokens map[string]string `json:"tokens"`
}

type termScheme struct {
	Name    string   `json:"name"`
	Label   string   `json:"label"`
	Brief   string   `json:"brief"`
	Dark    bool     `json:"dark"`
	FG      string   `json:"fg"`
	BG      string   `json:"bg"`
	Cursor  string   `json:"cursor"`
	Palette []string `json:"palette"`
}

// themeKanji names the built-in themes as the settings' swatches do.
var themeKanji = map[string]string{
	"yoru": "夜", "washi": "和紙", "aojashin": "青写真", "suimen": "水面",
	"rinko": "燐光", "bunko": "分光", "denko": "電光",
}

func readUI(name string) string {
	b, _ := ui.FS.ReadFile(name)
	return string(b)
}

// settingsNow are inua's and inua-term's settings as they are now: the
// page follows a change at its next request.
func settingsNow() (config.Config, settings.Settings) {
	cfg, _, err := config.Load(config.DefaultPath())
	if err != nil {
		log.Printf("inua-term serve: %v", err)
	}
	set, _, err := settings.Load(settings.Path())
	if err != nil {
		log.Printf("inua-term serve: %v", err)
	}
	return cfg, set
}

// windowTheme is inua-term's window theme: its own, else inua's.
func windowTheme(cfg config.Config, set settings.Settings) string {
	return cmp.Or(set.Look.Theme, cfg.Style.Theme, config.Default().Style.Theme)
}

func readLook() look {
	cfg, set := settingsNow()
	l := look{Theme: windowTheme(cfg, set), Scheme: set.Look.Scheme, Font: set.Font.Family}
	for _, name := range cfg.ThemeNames() {
		t := theme{Name: name, Label: name, Tokens: config.ThemeColors(cfg.ThemeCSS(name, readUI))}
		if k, ok := themeKanji[name]; ok {
			t.Label = k + " " + strings.ToUpper(name[:1]) + name[1:]
		}
		if !slices.Contains(config.Themes, name) {
			t.Brief = i18n.T("your own")
		}
		l.Themes = append(l.Themes, t)
	}
	for _, s := range scheme.All {
		ts := termScheme{Name: s.Name, Label: s.Label, Brief: i18n.T(s.Brief), Dark: s.Dark,
			FG: colorHex(s.FG), BG: colorHex(s.BG), Cursor: colorHex(s.Cursor)}
		for _, c := range s.Palette {
			ts.Palette = append(ts.Palette, colorHex(c))
		}
		l.Schemes = append(l.Schemes, ts)
	}
	return l
}

// themeColors are the window theme's ground and accent, as 0xRRGGBB.
func themeColors() (app, accent uint32) {
	cfg, set := settingsNow()
	c := config.ThemeColors(cfg.ThemeCSS(windowTheme(cfg, set), readUI))
	return parseHex(c["app"], 0x0b1012), parseHex(c["accent"], 0x9fc3c9)
}

func colorHex(c uint32) string { return fmt.Sprintf("#%06x", c&0xffffff) }

func parseHex(s string, def uint32) uint32 {
	var c uint32
	if _, err := fmt.Sscanf(strings.TrimPrefix(s, "#"), "%06x", &c); err != nil {
		return def
	}
	return c
}

func rgba(c uint32) color.RGBA { return color.RGBA{uint8(c >> 16), uint8(c >> 8), uint8(c), 255} }
