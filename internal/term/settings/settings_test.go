package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "term.toml")
	s := Default()
	s.Font.Size = 13
	s.Agents.Hooks = false
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
	if s.Font.Size != 11 || s.Cursor.Style != "block" || s.Keys["zoom"] != "ctrl+shift+z" || s.Keys["copy"] != "ctrl+shift+c" {
		t.Errorf("defaults back: %+v", s)
	}
	if len(w) != 5 || !strings.Contains(strings.Join(w, "\n"), "unknown setting colors") {
		t.Errorf("warnings %q", w)
	}
	os.WriteFile(path, []byte("[font"), 0o600)
	if _, _, err := Load(path); err == nil {
		t.Error("a broken file")
	}
}
