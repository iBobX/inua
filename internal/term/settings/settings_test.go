package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "term.toml")
	s := Default()
	s.Font.Size = 13
	s.Agents.Notifications = false
	s.Look = Look{Theme: "washi", Scheme: "nord"}
	s.Shell.Program = "/usr/bin/fish"
	s.Keys["zoom"] = ""
	s.Keys["split_right"] = "ctrl+shift+backslash"
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	got, warnings, err := Load(path)
	if err != nil || len(warnings) > 0 {
		t.Fatalf("load: %v %v", err, warnings)
	}
	if !reflect.DeepEqual(got, s) {
		t.Errorf("round trip:\n%+v\n%+v", got, s)
	}
}

func TestMissingAndInvalid(t *testing.T) {
	if s, w, err := Load(filepath.Join(t.TempDir(), "none.toml")); err != nil || len(w) > 0 || !reflect.DeepEqual(s, Default()) {
		t.Errorf("missing: %v %v", w, err)
	}
	path := filepath.Join(t.TempDir(), "term.toml")
	os.WriteFile(path, []byte(`
version = 2
[look]
scheme = "nope"
[font]
size = 200
[cursor]
style = "beam"
[keys]
zoom = "Ctrl Shift Z"
nothing = "ctrl+q"
[colors]
x = 1
`), 0o600)
	s, w, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Font.Size != 11 || s.Cursor.Style != "block" || s.Keys["zoom"] != "ctrl+shift+return" || s.Keys["copy"] != "ctrl+shift+c" || s.Look.Scheme != "" {
		t.Errorf("defaults back: %+v", s)
	}
	if len(w) != 6 || !strings.Contains(strings.Join(w, "\n"), "unknown setting colors") {
		t.Errorf("warnings %q", w)
	}
	os.WriteFile(path, []byte("[font"), 0o600)
	if _, _, err := Load(path); err == nil {
		t.Error("a broken file")
	}
}

// A file of the first version: the keys it kept at that version's
// defaults become Ghostty's, the ones the user chose stay, and its hooks
// setting is let go without a warning.
func TestFirstVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "term.toml")
	os.WriteFile(path, []byte(`
[agents]
notifications = true
hooks = true
[keys]
split_right = "ctrl+shift+d"
zoom = "ctrl+shift+m"
settings = "ctrl+shift+comma"
`), 0o600)
	s, w, err := Load(path)
	if err != nil || len(w) > 0 {
		t.Fatalf("load: %v %q", err, w)
	}
	if s.Version != Version || s.Keys["split_right"] != "ctrl+shift+o" || s.Keys["settings"] != "ctrl+comma" || s.Keys["zoom"] != "ctrl+shift+m" {
		t.Errorf("keys %+v", s.Keys)
	}
}

// A file of the second version: a new session moves to Ctrl+Shift+N and a
// new workspace loses its chord, when both kept their defaults; a file
// that changed either keeps both.
func TestSecondVersion(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		keys               string
		session, workspace string
	}{
		{"new_session = \"ctrl+shift+t\"\nnew_workspace = \"ctrl+shift+n\"", "ctrl+shift+n", ""},
		{"", "ctrl+shift+n", ""}, // no keys: the defaults
		{"new_session = \"ctrl+shift+y\"\nnew_workspace = \"ctrl+shift+n\"", "ctrl+shift+y", "ctrl+shift+n"},
		{"new_session = \"\"\nnew_workspace = \"ctrl+shift+n\"", "", "ctrl+shift+n"},
	} {
		path := filepath.Join(dir, "term.toml")
		os.WriteFile(path, []byte("version = 2\n[keys]\n"+c.keys+"\n"), 0o600)
		s, w, err := Load(path)
		if err != nil || len(w) > 0 {
			t.Fatalf("load: %v %q", err, w)
		}
		if s.Version != Version || s.Keys["new_session"] != c.session || s.Keys["new_workspace"] != c.workspace {
			t.Errorf("%q: session %q, workspace %q", c.keys, s.Keys["new_session"], s.Keys["new_workspace"])
		}
	}
}

func TestShells(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "usr", "bin")
	os.MkdirAll(bin, 0o755)
	os.Symlink(bin, filepath.Join(dir, "bin"))
	for _, name := range []string{"bash", "zsh", "nologin"} {
		os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/true\n"), 0o755)
	}
	os.Symlink("bash", filepath.Join(bin, "sh"))
	os.WriteFile(filepath.Join(bin, "notrun"), nil, 0o644)
	t.Setenv("SHELL", filepath.Join(bin, "zsh"))
	etc := "# /etc/shells\n" + strings.Join([]string{
		filepath.Join(dir, "bin", "sh"),
		filepath.Join(dir, "bin", "bash"),
		filepath.Join(bin, "bash"),
		filepath.Join(bin, "zsh"),
		filepath.Join(bin, "nologin"),
		filepath.Join(bin, "notrun"),
		filepath.Join(bin, "fish"),
	}, "\n")
	want := []string{filepath.Join(bin, "zsh"), filepath.Join(dir, "bin", "sh"), filepath.Join(dir, "bin", "bash")}
	if got := Shells(etc); !slices.Equal(got, want) {
		t.Errorf("shells %q, want %q", got, want)
	}
}
