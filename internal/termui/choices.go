package termui

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/scheme"
	"github.com/lyricat/inua/internal/term/settings"
)

// The choices the settings and the guide offer for the look and the
// shell: each a value as term.toml writes it, "" first for following
// inua or the system.

type choice struct{ value, label, brief string }

// themeChoices are inua's themes, after following inua's own; a built-in
// one is named and told by its file's first line ("夜 Yoru — dark:
// night").
func themeChoices(c *appui.Ctx) []choice {
	cfg, _, err := config.Load(config.DefaultPath())
	if err != nil {
		cfg = config.Default()
	}
	out := []choice{{"", i18n.Tf("As inua (%s)", cmp.Or(cfg.Style.Theme, "yoru")), i18n.T("The window changes with inua's theme")}}
	for _, name := range cfg.ThemeNames() {
		ch := choice{name, name, i18n.T("A theme of your own")}
		if slices.Contains(config.Themes, name) {
			head := c.ReadUI("themes/" + name + ".css")
			head, _, _ = strings.Cut(strings.TrimPrefix(head, "/*"), "\n")
			if l, b, ok := strings.Cut(head, "—"); ok {
				ch.label, ch.brief = strings.TrimSpace(l), strings.TrimSuffix(strings.TrimSpace(b), ".")
			}
		}
		out = append(out, ch)
	}
	return out
}

// themeColors are a theme's colour tokens, "" for inua's own.
func themeColors(c *appui.Ctx, name string) map[string]string {
	cfg, _, err := config.Load(config.DefaultPath())
	if err != nil {
		cfg = config.Default()
	}
	name = cmp.Or(name, cfg.Style.Theme, "yoru")
	return config.ThemeColors(cfg.ThemeCSS(name, c.ReadUI))
}

// schemeChoices are the terminals' colour schemes, after the theme's.
func schemeChoices() []choice {
	out := []choice{{"", i18n.T("The window's theme"), i18n.T("The terminals take the window's colours")}}
	for _, sc := range scheme.All {
		out = append(out, choice{sc.Name, sc.Label, i18n.T(sc.Brief)})
	}
	return out
}

// shellChoices are the shells installed, after $SHELL; current is kept
// when it is not one of them.
func shellChoices(current string) []choice {
	etc, _ := os.ReadFile("/etc/shells")
	login := cmp.Or(os.Getenv("SHELL"), "/bin/sh")
	out := []choice{{"", i18n.Tf("Your login shell (%s)", filepath.Base(login)), login}}
	paths := settings.Shells(string(etc))
	if current != "" && !slices.Contains(paths, current) {
		paths = append(paths, current)
	}
	for _, p := range paths {
		out = append(out, choice{p, filepath.Base(p), p})
	}
	return out
}

// choiceIndex is the place of value among choices, 0 when not there.
func choiceIndex(cs []choice, value string) int {
	return max(0, slices.IndexFunc(cs, func(c choice) bool { return c.value == value }))
}

func choiceLabels(cs []choice) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.label
	}
	return out
}
