package bootstrap

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// ShellLine is what a Hyprland config needs to start the shell.
const ShellLine = "exec-once = inua-shell"

// LuaShellLine is what a Lua config (Hyprland 0.55 and later) needs.
const LuaShellLine = `hl.on("hyprland.start", function() hl.exec_cmd("inua-shell") end)`

// ErrNoShell means a Hyprland config never starts inua-shell.
var ErrNoShell = errors.New("does not start inua-shell")

var (
	execRe   = regexp.MustCompile(`^exec(-once)?\s*=.*\binua-shell\b`)
	sourceRe = regexp.MustCompile(`^source\s*=\s*(.+)$`)
	// luaExecRe is a command in Lua that starts with inua-shell (a path
	// to it or not): hl.exec_cmd("inua-shell"), not hl.exec_cmd("pkill inua-shell").
	luaExecRe = regexp.MustCompile(`\bexec_(cmd|raw)\s*\(\s*["'](\S*/)?inua-shell\b`)
	// luaRequireRe is a module loaded with require, also through pcall,
	// in a line whose strings are blanked (blankStrings).
	luaRequireRe = regexp.MustCompile(`\brequire\s*(?:\(\s*|,\s*)?(["'])(\s*)["']`)
)

// FindShellStart reports where a Hyprland config starts inua-shell, as
// "file:line", following `source =` lines (with ~, relative paths and
// globs), or in a Lua config, require. It returns ErrNoShell if nothing
// does, and an error if the config cannot be read.
func FindShellStart(path string) (string, error) {
	where := ""
	err := walk(path, func(file string, n int, line string) bool {
		if lua := isLua(file); (!lua && execRe.MatchString(line)) || (lua && luaExecRe.MatchString(line) && !strings.Contains(line, "hl.bind")) {
			where = fmt.Sprintf("%s:%d", file, n)
			return false
		}
		return true
	})
	if err == nil && where == "" {
		err = ErrNoShell
	}
	return where, err
}

// ErrNoSource means a Hyprland config never loads the file inua writes.
var ErrNoSource = errors.New("does not load inua's file")

// overrideRe matches the settings inua's file sets; set again after the
// line that loads it, they would win over inua's.
var overrideRe = regexp.MustCompile(`^(kb_options|sensitivity|accel_profile)\s*=`)

// FindSource reports where a Hyprland config loads the file target (with
// `source =`, or require in Lua), as "file:line", and which later lines set
// what target sets (and so override it). It returns ErrNoSource if nothing
// loads target.
func FindSource(path, target string) (where string, overrides []string, err error) {
	target, err = filepath.Abs(target)
	if err != nil {
		return "", nil, err
	}
	err = walk(path, func(file string, n int, line string) bool {
		switch {
		case file == target:
		case where == "":
			var loads []string
			if m := sourceRe.FindStringSubmatch(line); m != nil && !isLua(file) {
				loads, _ = filepath.Glob(expand(strings.TrimSpace(m[1]), filepath.Dir(file)))
			} else if p := luaModule(line, path); p != "" && isLua(file) {
				loads = []string{p}
			}
			if slices.Contains(loads, target) {
				where = fmt.Sprintf("%s:%d", file, n)
			}
		case overrideRe.MatchString(line):
			overrides = append(overrides, fmt.Sprintf("%s:%d", file, n))
		}
		return true
	})
	if err == nil && where == "" {
		err = ErrNoSource
	}
	return where, overrides, err
}

func isLua(path string) bool { return strings.HasSuffix(path, ".lua") }

// luaModule is the file a require on line loads, as Hyprland finds it:
// an absolute path, or one relative to the main config's directory, with
// "." or "/" between directories. "" when line requires nothing, or uses
// a wildcard.
func luaModule(line, config string) string {
	m := luaRequireRe.FindStringSubmatchIndex(blankStrings(line))
	if m == nil {
		return ""
	}
	mod := line[m[4]:m[5]] // the string, from the line itself
	if mod == "" || strings.Contains(mod, "*") {
		return ""
	}
	if !filepath.IsAbs(mod) {
		mod = strings.TrimPrefix(mod, "./")
		if !strings.Contains(mod, "/") {
			mod = strings.ReplaceAll(mod, ".", "/")
		}
		dir := filepath.Dir(config)
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
		mod = filepath.Join(dir, mod)
	}
	if !strings.HasSuffix(mod, ".lua") {
		mod += ".lua"
	}
	return mod
}

// walk calls visit with each line of a Hyprland config, comments stripped
// and trimmed, in the order Hyprland reads them: a sourced or required
// file's lines come at the line that loads it. Files that cannot be read
// after the first are skipped, like a file sourced twice. visit returns
// false to stop.
func walk(path string, visit func(file string, n int, line string) bool) error {
	config := path // require is relative to the main config, in every file
	seen := map[string]bool{}
	var read func(path string, top bool) (bool, error)
	read = func(path string, top bool) (bool, error) {
		abs, err := filepath.Abs(path)
		if err != nil || seen[abs] {
			return true, err
		}
		seen[abs] = true
		f, err := os.Open(abs)
		if err != nil {
			if top {
				return false, err
			}
			return true, nil
		}
		defer f.Close()
		lua := isLua(abs)
		sc := bufio.NewScanner(f)
		for n := 1; sc.Scan(); n++ {
			var line string
			if lua {
				line = strings.TrimSpace(stripLuaComment(sc.Text()))
			} else {
				line = strings.TrimSpace(stripComment(sc.Text()))
			}
			if !visit(abs, n, line) {
				return false, nil
			}
			var loads []string
			if lua {
				if p := luaModule(line, config); p != "" {
					loads = []string{p}
				}
			} else if m := sourceRe.FindStringSubmatch(line); m != nil {
				loads, _ = filepath.Glob(expand(strings.TrimSpace(m[1]), filepath.Dir(abs)))
			}
			for _, src := range loads {
				if more, err := read(src, false); !more || err != nil {
					return more, err
				}
			}
		}
		return true, sc.Err()
	}
	_, err := read(path, true)
	return err
}

// stripComment drops a # comment; Hyprland writes a literal # as ##.
func stripComment(line string) string {
	for i := 0; i < len(line); i++ {
		if line[i] != '#' {
			continue
		}
		if i+1 < len(line) && line[i+1] == '#' {
			i++
			continue
		}
		return line[:i]
	}
	return line
}

// blankStrings is line with the inside of its quoted strings as spaces,
// so what is written in a string is not read as code.
func blankStrings(line string) string {
	b := []byte(line)
	var quote byte
	for i := 0; i < len(b); i++ {
		switch c := b[i]; {
		case quote != 0 && c == quote:
			quote = 0
		case quote != 0:
			if c == '\\' && i+1 < len(b) {
				b[i+1] = ' '
				b[i] = ' '
				i++
				continue
			}
			b[i] = ' '
		case c == '"' || c == '\'':
			quote = c
		}
	}
	return string(b)
}

// stripLuaComment drops a Lua comment: -- outside a quoted string.
func stripLuaComment(line string) string {
	var quote byte
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case quote != 0 && c == '\\':
			i++
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '-' && i+1 < len(line) && line[i+1] == '-':
			return line[:i]
		}
	}
	return line
}

func expand(p, dir string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[1:])
		}
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	return p
}
