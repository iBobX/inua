// Package desktop reads XDG desktop entries and launches them; it backs the
// DesktopEntries / DesktopEntry QML types.
package desktop

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Entry is an application from a .desktop file.
type Entry struct {
	ID            string // "org.gnome.Nautilus" for org.gnome.Nautilus.desktop
	Name          string
	GenericName   string
	Comment       string
	Icon          string
	ExecString    string // raw Exec value
	Command       []string
	WorkingDir    string
	RunInTerminal bool
	Categories    []string
	Keywords      []string
	NoDisplay     bool
	WMClass       string // StartupWMClass: the class of the app's windows
	Path          string // file the entry came from
}

// Parse reads the [Desktop Entry] group. locale is like "zh_CN.UTF-8"; ""
// selects untranslated values. Entries that are not applications, or are
// Hidden, return an error.
func Parse(r io.Reader, id, locale string) (*Entry, error) {
	vals := map[string]string{}
	inMain := false
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			inMain = line == "[Desktop Entry]"
			continue
		}
		if !inMain {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		vals[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	if vals["Type"] != "Application" {
		return nil, fmt.Errorf("%s: not an application", id)
	}
	if vals["Hidden"] == "true" {
		return nil, fmt.Errorf("%s: hidden", id)
	}
	get := func(key string) string {
		for _, l := range localeKeys(locale) {
			if v, ok := vals[key+"["+l+"]"]; ok {
				return unescape(v)
			}
		}
		return unescape(vals[key])
	}
	e := &Entry{
		ID:            id,
		Name:          get("Name"),
		GenericName:   get("GenericName"),
		Comment:       get("Comment"),
		Icon:          unescape(vals["Icon"]),
		ExecString:    unescape(vals["Exec"]),
		WorkingDir:    unescape(vals["Path"]),
		RunInTerminal: vals["Terminal"] == "true",
		Categories:    splitList(vals["Categories"]),
		Keywords:      splitList(get("Keywords")),
		NoDisplay:     vals["NoDisplay"] == "true",
		WMClass:       unescape(vals["StartupWMClass"]),
	}
	if e.Name == "" {
		return nil, fmt.Errorf("%s: no Name", id)
	}
	if e.ExecString != "" {
		cmd, err := ParseExec(e.ExecString, e)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
		e.Command = cmd
	}
	return e, nil
}

// localeKeys returns lookup keys for "lang_COUNTRY.ENCODING@MODIFIER" in the
// order the spec prescribes.
func localeKeys(locale string) []string {
	if locale == "" || locale == "C" || locale == "POSIX" {
		return nil
	}
	locale, _, _ = strings.Cut(locale, ".")
	base, mod, hasMod := strings.Cut(locale, "@")
	lang, country, hasCountry := strings.Cut(base, "_")
	var keys []string
	if hasCountry && hasMod {
		keys = append(keys, lang+"_"+country+"@"+mod)
	}
	if hasCountry {
		keys = append(keys, lang+"_"+country)
	}
	if hasMod {
		keys = append(keys, lang+"@"+mod)
	}
	return append(keys, lang)
}

func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 's':
			b.WriteByte(' ')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '\\':
			b.WriteByte('\\')
		default:
			// Keep unknown escapes (e.g. "\;" in lists, Exec quoting).
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ";") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ParseExec splits an Exec value into argv and expands field codes. File and
// URL codes (%f %F %u %U) are dropped since the launcher passes no files.
func ParseExec(exec string, e *Entry) ([]string, error) {
	var args []string
	var cur strings.Builder
	inArg, quoted := false, false
	for i := 0; i < len(exec); i++ {
		c := exec[i]
		switch {
		case quoted && c == '\\' && i+1 < len(exec):
			i++
			cur.WriteByte(exec[i])
		case c == '"':
			quoted = !quoted
			inArg = true
		case !quoted && (c == ' ' || c == '\t'):
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteByte(c)
			inArg = true
		}
	}
	if quoted {
		return nil, errors.New("unterminated quote in Exec")
	}
	if inArg {
		args = append(args, cur.String())
	}

	var out []string
	for _, a := range args {
		switch a {
		case "%f", "%F", "%u", "%U", "%d", "%D", "%n", "%N", "%v", "%m":
			continue
		case "%i":
			if e != nil && e.Icon != "" {
				out = append(out, "--icon", e.Icon)
			}
			continue
		}
		var b strings.Builder
		for i := 0; i < len(a); i++ {
			if a[i] != '%' || i+1 == len(a) {
				b.WriteByte(a[i])
				continue
			}
			i++
			switch a[i] {
			case '%':
				b.WriteByte('%')
			case 'c':
				if e != nil {
					b.WriteString(e.Name)
				}
			case 'k':
				if e != nil {
					b.WriteString(e.Path)
				}
			}
			// Other codes inside a word are removed.
		}
		out = append(out, b.String())
	}
	if len(out) == 0 {
		return nil, errors.New("empty Exec")
	}
	return out, nil
}

// DataDirs returns application directories in precedence order.
func DataDirs() []string {
	home := os.Getenv("XDG_DATA_HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".local", "share")
		}
	}
	dirs := os.Getenv("XDG_DATA_DIRS")
	if dirs == "" {
		dirs = "/usr/local/share:/usr/share"
	}
	var out []string
	if home != "" {
		out = append(out, filepath.Join(home, "applications"))
	}
	for _, d := range filepath.SplitList(dirs) {
		if d != "" {
			out = append(out, filepath.Join(d, "applications"))
		}
	}
	return out
}

// Scan loads all applications from dirs. An ID found in an earlier dir hides
// later ones, including when the earlier file is Hidden. Unparseable files are
// skipped. The result is sorted by ID.
func Scan(dirs []string, locale string) []*Entry {
	seen := map[string]bool{}
	var out []*Entry
	for _, dir := range dirs {
		filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".desktop") {
				return nil
			}
			rel, _ := filepath.Rel(dir, path)
			id := strings.TrimSuffix(strings.ReplaceAll(rel, string(filepath.Separator), "-"), ".desktop")
			if seen[id] {
				return nil
			}
			seen[id] = true
			f, err := os.Open(path)
			if err != nil {
				return nil
			}
			defer f.Close()
			if e, err := Parse(f, id, locale); err == nil {
				e.Path = path
				if e.ExecString != "" {
					e.Command, _ = ParseExec(e.ExecString, e)
				}
				out = append(out, e)
			}
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
