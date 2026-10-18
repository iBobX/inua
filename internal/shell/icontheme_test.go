package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIconThemeOverlay(t *testing.T) {
	dir, overlay := t.TempDir(), t.TempDir()
	for name, svg := range map[string]string{"a-symbolic.svg": "mine-a", "extra-symbolic.svg": "mine-extra", "notes.txt": "x"} {
		if err := os.WriteFile(filepath.Join(overlay, name), []byte(svg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	theme := IconTheme{
		Name:     "inua-icons-mine",
		Inherits: "Adwaita",
		Builtin:  []string{"a-symbolic.svg", "b-symbolic.svg", "LICENSE"},
		Read:     func(name string) string { return "builtin-" + name },
		Overlay:  overlay,
	}
	if err := theme.Write(dir); err != nil {
		t.Fatal(err)
	}
	icons := filepath.Join(dir, "inua-icons-mine", "symbolic")
	want := map[string]string{
		"a-symbolic.svg":     "mine-a",
		"b-symbolic.svg":     "builtin-b-symbolic.svg",
		"extra-symbolic.svg": "mine-extra",
	}
	check := func(want map[string]string) {
		t.Helper()
		entries, err := os.ReadDir(icons)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != len(want) {
			t.Errorf("%d icons, want %d", len(entries), len(want))
		}
		for name, content := range want {
			if b, err := os.ReadFile(filepath.Join(icons, name)); err != nil || string(b) != content {
				t.Errorf("%s = %q, %v; want %q", name, b, err, content)
			}
		}
	}
	check(want)
	index, err := os.ReadFile(filepath.Join(dir, "inua-icons-mine", "index.theme"))
	if err != nil || !strings.Contains(string(index), "Inherits=Adwaita,hicolor") {
		t.Errorf("index.theme = %q, %v", index, err)
	}

	// An icon taken out of the set is gone after the next write.
	if err := os.Remove(filepath.Join(overlay, "extra-symbolic.svg")); err != nil {
		t.Fatal(err)
	}
	if err := theme.Write(dir); err != nil {
		t.Fatal(err)
	}
	delete(want, "extra-symbolic.svg")
	check(want)

	// A missing set leaves the built-in icons.
	theme.Overlay = filepath.Join(overlay, "gone")
	if err := theme.Write(dir); err != nil {
		t.Fatal(err)
	}
	check(map[string]string{"a-symbolic.svg": "builtin-a-symbolic.svg", "b-symbolic.svg": "builtin-b-symbolic.svg"})
}

func TestIconThemeWrittenOnlyWhenChanged(t *testing.T) {
	dir, overlay := t.TempDir(), t.TempDir()
	svg := filepath.Join(overlay, "a-symbolic.svg")
	if err := os.WriteFile(svg, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	theme := IconTheme{
		Name:     "inua-test",
		Inherits: "Adwaita",
		Builtin:  []string{"a-symbolic.svg", "b-symbolic.svg"},
		Read:     func(name string) string { return "builtin-" + name },
		Overlay:  overlay,
	}
	if err := theme.Write(dir); err != nil {
		t.Fatal(err)
	}
	icons := filepath.Join(dir, "inua-test", "symbolic")
	// A file of its own in the folder tells whether the next write
	// replaced it.
	marker := filepath.Join(icons, "marker")
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := theme.Write(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the same theme was written again: %v", err)
	}

	// A changed icon is written, and the folder holds only the set.
	if err := os.WriteFile(svg, []byte("mine, changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := theme.Write(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("the changed theme was not written again: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(icons, "a-symbolic.svg")); err != nil || string(b) != "mine, changed" {
		t.Errorf("a-symbolic.svg = %q, %v", b, err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "inua-test"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, " ") != ".written index.theme symbolic" {
		t.Errorf("the theme's folder holds %v", names)
	}
}
