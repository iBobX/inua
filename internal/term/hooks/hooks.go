// Package hooks adds inua-term's hooks to the agents that have them, so
// they report their state to inua-term themselves (docs/feat/inua-term.md,
// section 9): Claude Code's UserPromptSubmit, Notification and Stop in
// its settings.json, Codex's notify in its config.toml. What is there is
// kept: the user's own hooks stay, a notify Codex has already is left
// alone, and the file changed is kept first as FILE.bak. Remove takes
// inua-term's out again.
package hooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Marker is what every hook command of inua-term's has in it.
const Marker = "inua-term report"

// claudeEvents are the hooks Claude Code runs that tell its state.
var claudeEvents = []string{"UserPromptSubmit", "Notification", "Stop"}

// ClaudePath is Claude Code's settings: $CLAUDE_CONFIG_DIR/settings.json,
// else ~/.claude/settings.json.
func ClaudePath() string {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".claude")
	}
	return filepath.Join(dir, "settings.json")
}

// CodexPath is Codex's config: $CODEX_HOME/config.toml, else
// ~/.codex/config.toml.
func CodexPath() string {
	dir := os.Getenv("CODEX_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".codex")
	}
	return filepath.Join(dir, "config.toml")
}

// InstallClaude adds `exe report --hook claude` to Claude Code's hooks
// in path; it reports whether it changed the file.
func InstallClaude(path, exe string) (bool, error) {
	root, orig, err := readJSON(path)
	if err != nil {
		return false, err
	}
	command := exe + " report --hook claude"
	hooks := root.object("hooks")
	changed := false
	for _, ev := range claudeEvents {
		groups, _ := hooks.get(ev).([]any)
		if hasOurs(groups) {
			continue
		}
		group := &object{}
		group.set("hooks", []any{(&object{}).set("type", "command").set("command", command)})
		hooks.set(ev, append(groups, group))
		changed = true
	}
	if !changed {
		return false, nil
	}
	root.set("hooks", hooks)
	return true, writeJSON(path, root, orig)
}

// RemoveClaude takes inua-term's hooks out of Claude Code's settings.
func RemoveClaude(path string) (bool, error) {
	root, orig, err := readJSON(path)
	if err != nil || orig == nil {
		return false, err
	}
	hooks, ok := root.get("hooks").(*object)
	if !ok {
		return false, nil
	}
	changed := false
	for _, ev := range claudeEvents {
		groups, ok := hooks.get(ev).([]any)
		if !ok {
			continue
		}
		var kept []any
		for _, g := range groups {
			if gg, ok := g.(*object); ok {
				hs, _ := gg.get("hooks").([]any)
				var keep []any
				for _, h := range hs {
					if !ours(h) {
						keep = append(keep, h)
					}
				}
				if len(keep) != len(hs) {
					changed = true
					if len(keep) == 0 {
						continue // the group was inua-term's alone
					}
					gg.set("hooks", keep)
				}
			}
			kept = append(kept, g)
		}
		if len(kept) == 0 {
			hooks.del(ev)
		} else {
			hooks.set(ev, kept)
		}
	}
	if !changed {
		return false, nil
	}
	if len(hooks.keys) == 0 {
		root.del("hooks")
	}
	return true, writeJSON(path, root, orig)
}

func ours(h any) bool {
	o, ok := h.(*object)
	if !ok {
		return false
	}
	c, _ := o.get("command").(string)
	return strings.Contains(c, Marker)
}

func hasOurs(groups []any) bool {
	for _, g := range groups {
		if gg, ok := g.(*object); ok {
			hs, _ := gg.get("hooks").([]any)
			for _, h := range hs {
				if ours(h) {
					return true
				}
			}
		}
	}
	return false
}

// readJSON is a settings file as an object keeping its keys' order, and
// its bytes (nil when there is no file).
func readJSON(path string) (*object, []byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &object{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return &object{}, b, nil
	}
	v, err := decode(json.NewDecoder(bytes.NewReader(b)))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	o, ok := v.(*object)
	if !ok {
		return nil, nil, fmt.Errorf("%s: not a JSON object", path)
	}
	return o, b, nil
}

func writeJSON(path string, root *object, orig []byte) error {
	b, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return write(path, append(b, '\n'), orig)
}

// write keeps the file's old content as FILE.bak, then writes the new.
func write(path string, b, orig []byte) error {
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if orig != nil {
		if err := os.WriteFile(path+".bak", orig, mode); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".inua-term"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// InstallCodex sets Codex's notify to `exe report --hook codex`, unless
// it has a notify already; it reports whether it changed the file.
func InstallCodex(path, exe string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	lines := strings.Split(string(b), "\n")
	first := len(lines) // the first table: notify goes above it, at the top level
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") {
			first = i
			break
		}
		if k, _, ok := strings.Cut(t, "="); ok && strings.TrimSpace(k) == "notify" {
			return false, nil // the user's own, or ours already
		}
	}
	line := fmt.Sprintf("notify = [%q, \"report\", \"--hook\", \"codex\"] # %s: added by inua-term", exe, Marker)
	if strings.TrimSpace(string(b)) == "" {
		lines, first = []string{""}, 0 // a new file: the line and its newline
	} else if first == len(lines) && lines[first-1] == "" {
		first-- // no table: last, before the final newline
	}
	out := append([]string{}, lines[:first]...)
	out = append(out, line)
	out = append(out, lines[first:]...)
	var orig []byte
	if err == nil {
		orig = b
	}
	return true, write(path, []byte(strings.Join(out, "\n")), orig)
}

// RemoveCodex takes inua-term's notify out of Codex's config.
func RemoveCodex(path string) (bool, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	lines := strings.Split(string(b), "\n")
	var out []string
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "notify") && strings.Contains(t, Marker) {
			continue
		}
		out = append(out, l)
	}
	if len(out) == len(lines) {
		return false, nil
	}
	return true, write(path, []byte(strings.Join(out, "\n")), b)
}

// object is a JSON object that keeps its keys' order.
type object struct {
	keys []string
	vals map[string]any
}

func (o *object) get(k string) any {
	if o.vals == nil {
		return nil
	}
	return o.vals[k]
}

func (o *object) set(k string, v any) *object {
	if o.vals == nil {
		o.vals = map[string]any{}
	}
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
	return o
}

func (o *object) del(k string) {
	if _, ok := o.vals[k]; !ok {
		return
	}
	delete(o.vals, k)
	for i, kk := range o.keys {
		if kk == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

// object is the object at k, made when there is none.
func (o *object) object(k string) *object {
	if c, ok := o.get(k).(*object); ok {
		return c
	}
	return &object{}
}

func (o *object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		vb, err := json.Marshal(o.vals[k])
		if err != nil {
			return nil, err
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// decode reads a JSON value, its objects as objects; numbers stay as
// written.
func decode(d *json.Decoder) (any, error) {
	d.UseNumber()
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch t := t.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := &object{}
			for d.More() {
				kt, err := d.Token()
				if err != nil {
					return nil, err
				}
				k, _ := kt.(string)
				v, err := decode(d)
				if err != nil {
					return nil, err
				}
				o.set(k, v)
			}
			_, err := d.Token()
			return o, err
		case '[':
			a := []any{}
			for d.More() {
				v, err := decode(d)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
			_, err := d.Token()
			return a, err
		}
	}
	return t, nil
}
