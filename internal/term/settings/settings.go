// Package settings is inua-term's own settings, ~/.config/inua/term.toml
// (docs/feat/inua-term.md, section 10): the font, the cursor, copying,
// scrollback, the host's replay, shell integration, agents' hooks and
// notifications, remembering commands, and the keys. inua's look (theme,
// icons, text size) still follows ~/.config/inua/config.toml.
package settings

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/lyricat/inua/internal/i18n"
)

// Settings is the file's content.
type Settings struct {
	Font     Font     `toml:"font"`
	Cursor   Cursor   `toml:"cursor"`
	Terminal Terminal `toml:"terminal"`
	Shell    Shell    `toml:"shell"`
	Agents   Agents   `toml:"agents"`
	Remember Remember `toml:"remember"`
	// Keys are inua-term's chords by action ("ctrl+shift+t"); "" turns
	// one off.
	Keys map[string]string `toml:"keys"`
}

type Font struct {
	Family string  `toml:"family"`
	Size   float64 `toml:"size"`
}

type Cursor struct {
	Style string `toml:"style"` // block, bar or underline
	Blink bool   `toml:"blink"`
}

type Terminal struct {
	CopyOnSelect bool `toml:"copy_on_select"`
	Scrollback   int  `toml:"scrollback"` // lines kept per pane
	Replay       int  `toml:"replay"`     // MiB of a pane's output the host keeps
}

type Shell struct {
	Integration bool `toml:"integration"`
}

type Agents struct {
	Notifications bool `toml:"notifications"`
	Hooks         bool `toml:"hooks"`
}

type Remember struct {
	Commands bool     `toml:"commands"`
	Forget   []string `toml:"forget"`
}

// Actions are the keys' actions, in the order the settings show them,
// with their chords by default.
var Actions = []struct{ Name, Keys, Label string }{
	{"new_session", "ctrl+shift+t", i18n.M("New session in this workspace")},
	{"new_workspace", "ctrl+shift+n", i18n.M("New workspace")},
	{"split_right", "ctrl+shift+d", i18n.M("Split right")},
	{"split_down", "ctrl+shift+e", i18n.M("Split down")},
	{"close_pane", "ctrl+shift+w", i18n.M("Close pane")},
	{"swap", "ctrl+shift+x", i18n.M("Swap with the pane in a direction (then an arrow)")},
	{"zoom", "ctrl+shift+z", i18n.M("Zoom pane / back")},
	{"equalize", "ctrl+shift+equal", i18n.M("Equalize")},
	{"next_layout", "ctrl+shift+l", i18n.M("Next layout")},
	{"previous_session", "ctrl+shift+bracketleft", i18n.M("Previous session")},
	{"next_session", "ctrl+shift+bracketright", i18n.M("Next session")},
	{"next_waiting", "ctrl+shift+j", i18n.M("Next agent that needs you")},
	{"agents", "ctrl+shift+a", i18n.M("Every agent")},
	{"fold_workspaces", "ctrl+shift+b", i18n.M("Fold the workspaces")},
	{"fold_sessions", "ctrl+shift+s", i18n.M("Fold the sessions")},
	{"copy", "ctrl+shift+c", i18n.M("Copy")},
	{"paste", "ctrl+shift+v", i18n.M("Paste")},
	{"find", "ctrl+shift+f", i18n.M("Search the scrollback")},
	{"copy_mode", "ctrl+shift+space", i18n.M("Copy mode")},
	{"rename", "ctrl+shift+r", i18n.M("Rename the session")},
	{"palette", "ctrl+shift+p", i18n.M("Every action by name")},
	{"keys", "ctrl+shift+slash", i18n.M("Show the keys")},
	{"settings", "ctrl+shift+comma", i18n.M("Settings")},
	{"font_larger", "ctrl+equal", i18n.M("Font larger")},
	{"font_smaller", "ctrl+minus", i18n.M("Font smaller")},
	{"font_reset", "ctrl+0", i18n.M("Font size as set")},
}

// Default is the settings without a file.
func Default() Settings {
	keys := map[string]string{}
	for _, a := range Actions {
		keys[a.Name] = a.Keys
	}
	return Settings{
		Font:     Font{Family: "Geist Mono", Size: 11},
		Cursor:   Cursor{Style: "block"},
		Terminal: Terminal{CopyOnSelect: true, Scrollback: 10000, Replay: 4},
		Shell:    Shell{Integration: true},
		Agents:   Agents{Notifications: true, Hooks: true},
		Remember: Remember{Commands: true, Forget: []string{"*password*", "*token=*", "*secret*"}},
		Keys:     keys,
	}
}

// Path is $XDG_CONFIG_HOME/inua/term.toml.
func Path() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "inua", "term.toml")
}

// Load reads path over the defaults; a missing file is the defaults. The
// error is for a file that cannot be read or parsed (the defaults come
// back then); warnings name unknown keys and invalid values, replaced by
// their defaults.
func Load(path string) (Settings, []string, error) {
	s := Default()
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil, nil
	}
	if err != nil {
		return s, nil, err
	}
	md, err := toml.Decode(string(b), &s)
	if err != nil {
		return Default(), nil, fmt.Errorf("%s: %w", path, err)
	}
	var w []string
	unknown := md.Undecoded()
	for _, k := range unknown {
		// An unknown table once, not each key in it.
		if slices.ContainsFunc(unknown, func(o toml.Key) bool { return strings.HasPrefix(k.String(), o.String()+".") }) {
			continue
		}
		w = append(w, fmt.Sprintf("unknown setting %s", k))
	}
	return s, append(w, s.validate()...), nil
}

var chordRe = regexp.MustCompile(`^((ctrl|shift|alt|super)\+)*[a-z0-9_]+$`)

// ValidKeys says s is a chord as the file writes them, or "" for none.
func ValidKeys(s string) bool { return s == "" || chordRe.MatchString(s) }

func (s *Settings) validate() []string {
	d := Default()
	var w []string
	if strings.TrimSpace(s.Font.Family) == "" {
		w = append(w, fmt.Sprintf("font.family is empty; using %q", d.Font.Family))
		s.Font.Family = d.Font.Family
	}
	if s.Font.Size < 6 || s.Font.Size > 48 {
		w = append(w, fmt.Sprintf("font.size = %v: want 6..48; using %v", s.Font.Size, d.Font.Size))
		s.Font.Size = d.Font.Size
	}
	if !slices.Contains([]string{"block", "bar", "underline"}, s.Cursor.Style) {
		w = append(w, fmt.Sprintf("cursor.style = %q: want block, bar or underline; using %q", s.Cursor.Style, d.Cursor.Style))
		s.Cursor.Style = d.Cursor.Style
	}
	if s.Terminal.Scrollback < 0 || s.Terminal.Scrollback > 1000000 {
		w = append(w, fmt.Sprintf("terminal.scrollback = %d: want 0..1000000 lines; using %d", s.Terminal.Scrollback, d.Terminal.Scrollback))
		s.Terminal.Scrollback = d.Terminal.Scrollback
	}
	if s.Terminal.Replay < 1 || s.Terminal.Replay > 64 {
		w = append(w, fmt.Sprintf("terminal.replay = %d: want 1..64 MiB; using %d", s.Terminal.Replay, d.Terminal.Replay))
		s.Terminal.Replay = d.Terminal.Replay
	}
	for name, k := range s.Keys {
		if !slices.ContainsFunc(Actions, func(a struct{ Name, Keys, Label string }) bool { return a.Name == name }) {
			w = append(w, fmt.Sprintf("keys.%s: no such action", name))
			delete(s.Keys, name)
			continue
		}
		if !ValidKeys(k) {
			w = append(w, fmt.Sprintf("keys.%s = %q: want modifiers and a key, as \"ctrl+shift+t\"; using %q", name, k, d.Keys[name]))
			s.Keys[name] = d.Keys[name]
		}
	}
	for name, k := range d.Keys {
		if _, ok := s.Keys[name]; !ok {
			s.Keys[name] = k
		}
	}
	return w
}

// Format is the settings as the file writes them, with what each means.
func Format(s Settings) string {
	var b strings.Builder
	b.WriteString(`# inua-term's settings. inua's look (theme, icons, text size) follows
# ~/.config/inua/config.toml; this file is inua-term's own, written by its
# settings (Ctrl+Shift+,) and read when it starts.

[font]
family = ` + q(s.Font.Family) + ` # a monospace family; Pango falls back for CJK
size = ` + fmt.Sprint(s.Font.Size) + `

[cursor]
style = ` + q(s.Cursor.Style) + ` # block, bar or underline
blink = ` + fmt.Sprint(s.Cursor.Blink) + `

[terminal]
copy_on_select = ` + fmt.Sprint(s.Terminal.CopyOnSelect) + ` # selecting copies to the clipboard
scrollback = ` + fmt.Sprint(s.Terminal.Scrollback) + ` # lines kept per pane
replay = ` + fmt.Sprint(s.Terminal.Replay) + ` # MiB of a pane's recent output the host keeps for a window opened later

[shell]
integration = ` + fmt.Sprint(s.Shell.Integration) + ` # the prompt marks for bash, zsh and fish, for remembering commands

[agents]
notifications = ` + fmt.Sprint(s.Agents.Notifications) + ` # when an agent needs you or is done in a pane you are not looking at
hooks = ` + fmt.Sprint(s.Agents.Hooks) + ` # add inua-term's hooks to Claude Code's and Codex's settings when they first run

[remember]
commands = ` + fmt.Sprint(s.Remember.Commands) + ` # put each pane's last command back at its prompt, not run
forget = ` + list(s.Remember.Forget) + ` # commands never kept

[keys]
# inua-term's chords; "" turns one off.
`)
	for _, a := range Actions {
		fmt.Fprintf(&b, "%s = %s # %s\n", a.Name, q(s.Keys[a.Name]), a.Label)
	}
	return b.String()
}

func q(s string) string { return fmt.Sprintf("%q", s) }

func list(items []string) string {
	qs := make([]string, len(items))
	for i, s := range items {
		qs[i] = q(s)
	}
	return "[" + strings.Join(qs, ", ") + "]"
}

// Save writes the settings to path, mode 0600, through a file renamed
// over the old one.
func Save(path string, s Settings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(Format(s)), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
