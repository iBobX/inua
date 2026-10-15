package config

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Themes and icon sets of the user's own live next to the settings file:
// a theme is themes/<name>.css, colour tokens like the built-in ones; an
// icon set is a folder icons/<name>/ of NAME-symbolic.svg files, which
// replace the built-in icons of the same name.

// customName is what a theme's or icon set's name may be: it becomes a
// CSS class and an icon theme name.
var customName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// ThemeNames are the themes to choose from: the built-in ones, then the
// user's, sorted. A user's theme named like a built-in one changes it.
func (c Config) ThemeNames() []string {
	names := slices.Clone(Themes)
	for _, name := range c.custom("themes", func(e os.DirEntry) (string, bool) {
		name, ok := strings.CutSuffix(e.Name(), ".css")
		return name, ok && !e.IsDir()
	}) {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

// IconSetNames are the icon sets to choose from: IconSets, then the
// user's, sorted.
func (c Config) IconSetNames() []string {
	names := slices.Clone(IconSets)
	for _, name := range c.custom("icons", func(e os.DirEntry) (string, bool) {
		return e.Name(), e.IsDir() || e.Type()&os.ModeSymlink != 0
	}) {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

// custom lists the valid names in a folder of the config directory.
func (c Config) custom(folder string, name func(os.DirEntry) (string, bool)) []string {
	if c.Dir == "" {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(c.Dir, folder))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if n, ok := name(e); ok && customName.MatchString(n) {
			names = append(names, n)
		}
	}
	return names // os.ReadDir sorts
}

// ThemeCSS is a theme's colour tokens: the built-in theme of that name, or
// the default theme for one of the user's own, then the user's file over
// it, so a theme of the user's needs only the tokens it changes. builtin
// reads a file of the built-in ui directory, such as "themes/yoru.css".
func (c Config) ThemeCSS(name string, builtin func(name string) string) string {
	base := name
	if !slices.Contains(Themes, base) {
		base = Default().Style.Theme
	}
	css := builtin("themes/" + base + ".css")
	if c.Dir == "" || !customName.MatchString(name) {
		return css
	}
	user, err := os.ReadFile(filepath.Join(c.Dir, "themes", name+".css"))
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("theme: %v", err)
		}
		return css
	}
	return css + "\n" + string(user)
}

// IconSetDir is the folder of style.icons when it is a set of the user's,
// "" for a built-in set.
func (c Config) IconSetDir() string {
	name := c.Style.Icons
	if c.Dir == "" || slices.Contains(IconSets, name) || !customName.MatchString(name) {
		return ""
	}
	return filepath.Join(c.Dir, "icons", name)
}
