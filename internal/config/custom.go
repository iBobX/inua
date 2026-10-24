package config

import (
	"errors"
	"fmt"
	"io"
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

var defineColor = regexp.MustCompile(`@define-color\s+([\w-]+)\s+(#[0-9a-fA-F]{6})\s*;`)

// ThemeColors reads the colour tokens of a theme file (ui/themes/*.css):
// name → "#rrggbb".
func ThemeColors(css string) map[string]string {
	colors := map[string]string{}
	for _, m := range defineColor.FindAllStringSubmatch(css, -1) {
		colors[m[1]] = m[2]
	}
	return colors
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
	path := c.ThemePath(name)
	if path == "" {
		return css
	}
	user, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("theme: %v", err)
		}
		return css
	}
	return css + "\n" + string(user)
}

// IconSetDir is the folder of style.icons when it is a set of the user's,
// "" for a built-in set or an installed icon theme.
func (c Config) IconSetDir() string {
	path := c.IconSetPath(c.Style.Icons)
	if path == "" {
		return ""
	}
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// IconThemeName is the installed icon theme style.icons names, "" for a
// set of inua's or of the user's.
func (c Config) IconThemeName() string {
	if slices.Contains(IconSets, c.Style.Icons) || c.IconSetDir() != "" {
		return ""
	}
	return c.Style.Icons
}

// Installing themes and icon sets: the settings window copies a file or a
// folder the user picks into the config directory.

var (
	// ErrExists is a theme or icon set whose name is taken, by one of the
	// user's or a built-in one.
	ErrExists = errors.New("the name is taken")
	// ErrNotTheme is a file without colour tokens.
	ErrNotTheme = errors.New("no @define-color lines with #rrggbb colours")
	// ErrNoIcons is a folder without SVG files.
	ErrNoIcons = errors.New("no SVG files")
)

var (
	themeToken  = regexp.MustCompile(`@define-color\s+[\w-]+\s+#[0-9a-fA-F]{6}\s*;`)
	nameInvalid = regexp.MustCompile(`[^a-z0-9_-]+`)
)

// maxThemeSize bounds a theme file: tokens take a few hundred bytes.
const maxThemeSize = 1 << 20

// CustomName makes a theme's or icon set's name from a file or folder
// name: lower case, other characters as "-". "" if nothing is left.
func CustomName(base string) string {
	name := strings.Trim(nameInvalid.ReplaceAllString(strings.ToLower(base), "-"), "-_")
	if !customName.MatchString(name) {
		return ""
	}
	return name
}

// ThemePath is the file of the user's theme name, "" for a name that
// cannot be one.
func (c Config) ThemePath(name string) string {
	if c.Dir == "" || !customName.MatchString(name) {
		return ""
	}
	return filepath.Join(c.Dir, "themes", name+".css")
}

// IconSetPath is the folder of the user's icon set name, "" for a name
// that cannot be one.
func (c Config) IconSetPath(name string) string {
	if c.Dir == "" || !customName.MatchString(name) || slices.Contains(IconSets, name) {
		return ""
	}
	return filepath.Join(c.Dir, "icons", name)
}

// IsCustomTheme reports whether name is a theme of the user's own, rather
// than built in (or a built-in one the user changed).
func (c Config) IsCustomTheme(name string) bool {
	return !slices.Contains(Themes, name) && slices.Contains(c.ThemeNames(), name)
}

// InstallTheme copies a theme file into the themes folder, named after
// the file, and returns the name. It neither replaces nor changes a theme.
func (c Config) InstallTheme(src string) (string, error) {
	name := CustomName(strings.TrimSuffix(filepath.Base(src), ".css"))
	dst := c.ThemePath(name)
	if dst == "" {
		return "", fmt.Errorf("%s: not a usable name", filepath.Base(src))
	}
	if slices.Contains(c.ThemeNames(), name) {
		return name, ErrExists
	}
	f, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer f.Close()
	css, err := io.ReadAll(io.LimitReader(f, maxThemeSize+1))
	if err != nil {
		return "", err
	}
	if len(css) > maxThemeSize || !themeToken.Match(css) {
		return "", ErrNotTheme
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return name, ErrExists
		}
		return "", err
	}
	if _, err := out.Write(css); err != nil {
		out.Close()
		os.Remove(dst)
		return "", err
	}
	return name, out.Close()
}

// InstallIconSet copies the SVG files of a folder (not its subfolders)
// into the icons folder, named after the folder, and returns the name and
// how many icons it holds. The set appears whole or not at all.
func (c Config) InstallIconSet(src string) (string, int, error) {
	name := CustomName(filepath.Base(filepath.Clean(src)))
	dst := c.IconSetPath(name)
	if dst == "" {
		return "", 0, fmt.Errorf("%s: not a usable name", filepath.Base(src))
	}
	if slices.Contains(c.IconSetNames(), name) {
		return name, 0, ErrExists
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return "", 0, err
	}
	icons := filepath.Dir(dst)
	if err := os.MkdirAll(icons, 0o755); err != nil {
		return "", 0, err
	}
	// A hidden folder is no set: it is not listed while being filled.
	tmp, err := os.MkdirTemp(icons, ".install-")
	if err != nil {
		return "", 0, err
	}
	defer os.RemoveAll(tmp)
	n := 0
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".svg") {
			continue
		}
		svg, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return "", 0, err
		}
		if err := os.WriteFile(filepath.Join(tmp, e.Name()), svg, 0o644); err != nil {
			return "", 0, err
		}
		n++
	}
	if n == 0 {
		return "", 0, ErrNoIcons
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return "", 0, err
	}
	if _, err := os.Stat(dst); err == nil {
		return name, 0, ErrExists
	}
	if err := os.Rename(tmp, dst); err != nil {
		return "", 0, err
	}
	return name, n, nil
}
