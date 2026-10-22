package config

import (
	"bufio"
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// The icon themes installed on the system (freedesktop's icon theme
// specification): a folder with an index.theme in one of the icon
// folders, as GTK finds them. style.icons may name one for the shell's own
// icons, style.app_icons one for the icons of apps and files.

// IconTheme is an installed icon theme: its folder's name, which GTK
// knows it by, and the name it gives itself.
type IconTheme struct {
	Name, Title string
}

// iconThemeDirs are the folders GTK looks for icon themes in, first first.
func iconThemeDirs() []string {
	home, _ := os.UserHomeDir()
	data := cmp.Or(os.Getenv("XDG_DATA_HOME"), filepath.Join(home, ".local", "share"))
	dirs := []string{filepath.Join(data, "icons"), filepath.Join(home, ".icons")}
	for _, d := range filepath.SplitList(cmp.Or(os.Getenv("XDG_DATA_DIRS"), "/usr/local/share:/usr/share")) {
		dirs = append(dirs, filepath.Join(d, "icons"))
	}
	return dirs
}

// The list read last, kept while none of the folders changed.
var iconThemes struct {
	sync.Mutex
	stamp string
	list  []IconTheme
}

// IconThemes are the installed icon themes, by title: not hicolor (every
// theme's last resort), cursor themes or hidden ones but Adwaita, nor the
// themes inua writes for itself. Read again only when one of the folders
// changed.
func IconThemes() []IconTheme {
	dirs := iconThemeDirs()
	var stamp strings.Builder
	for _, d := range dirs {
		if info, err := os.Stat(d); err == nil {
			stamp.WriteString(d + "@" + info.ModTime().String() + "\n")
		}
	}
	t := &iconThemes
	t.Lock()
	defer t.Unlock()
	if t.list != nil && t.stamp == stamp.String() {
		return t.list
	}
	list := []IconTheme{}
	seen := map[string]bool{}
	for _, d := range dirs {
		entries, _ := os.ReadDir(d)
		for _, e := range entries {
			name := e.Name()
			if seen[name] || name == "hicolor" || name == "default" || strings.HasPrefix(name, "inua-") {
				continue
			}
			title, ok := readIconTheme(filepath.Join(d, name, "index.theme"))
			if !ok && (name != "Adwaita" || title == "") { // hidden, yet GTK's own
				continue
			}
			seen[name] = true
			list = append(list, IconTheme{Name: name, Title: cmp.Or(title, name)})
		}
	}
	slices.SortFunc(list, func(a, b IconTheme) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)), cmp.Compare(a.Name, b.Name))
	})
	t.stamp, t.list = stamp.String(), list
	return list
}

// IsIconTheme reports whether name is an installed icon theme.
func IsIconTheme(name string) bool {
	return slices.ContainsFunc(IconThemes(), func(t IconTheme) bool { return t.Name == name })
}

// readIconTheme reads an index.theme's name, and whether it is a theme of
// icons to offer: it lists folders of icons (a cursor theme lists none)
// and is not hidden.
func readIconTheme(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	var name string
	in, dirs, hidden := false, false, false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			if in {
				break // only the [Icon Theme] group
			}
			in = line == "[Icon Theme]"
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !in || !ok {
			continue
		}
		switch value = strings.TrimSpace(value); strings.TrimSpace(key) {
		case "Name":
			name = value
		case "Directories", "ScaledDirectories":
			dirs = dirs || value != ""
		case "Hidden":
			hidden = strings.EqualFold(value, "true")
		}
	}
	return name, dirs && !hidden
}
