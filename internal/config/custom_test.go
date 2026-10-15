package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// userFiles writes a config.toml and the user's themes and icon sets next
// to it.
func userFiles(t *testing.T, config string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{
		"config.toml":                     config,
		"themes/mine.css":                 "@define-color accent #112233;\n",
		"themes/washi.css":                "@define-color accent #445566;\n",
		"themes/Bad Name.css":             "",
		"themes/notes.txt":                "",
		"icons/line/go-home-symbolic.svg": "<svg/>",
		"icons/phosphor/x.svg":            "<svg/>",
		"icons/.hidden/x.svg":             "<svg/>",
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "config.toml")
}

func TestCustomNames(t *testing.T) {
	c, w, err := Load(userFiles(t, ""))
	if err != nil || len(w) != 0 {
		t.Fatalf("err=%v warnings=%v", err, w)
	}
	if got, want := c.ThemeNames(), append(slices.Clone(Themes), "mine"); !slices.Equal(got, want) {
		t.Errorf("themes = %v, want %v", got, want)
	}
	if got, want := c.IconSetNames(), append(slices.Clone(IconSets), "line"); !slices.Equal(got, want) {
		t.Errorf("icon sets = %v, want %v", got, want)
	}
}

func TestCustomChoices(t *testing.T) {
	c, w, err := Load(userFiles(t, "[style]\ntheme = \"mine\"\nicons = \"line\"\n"))
	if err != nil || len(w) != 0 {
		t.Fatalf("err=%v warnings=%v", err, w)
	}
	if c.Style.Theme != "mine" || c.Style.Icons != "line" {
		t.Errorf("style = %+v", c.Style)
	}
	if got, want := c.IconSetDir(), filepath.Join(c.Dir, "icons", "line"); got != want {
		t.Errorf("IconSetDir = %q, want %q", got, want)
	}

	for _, style := range []string{`theme = "gone"`, `theme = "../mine"`, `icons = "gone"`, `icons = ".hidden"`} {
		c, w, _ := Load(userFiles(t, "[style]\n"+style+"\n"))
		if len(w) != 1 || c.Style.Theme != "yoru" || c.Style.Icons != "phosphor" {
			t.Errorf("%s: style = %+v, warnings %v", style, c.Style, w)
		}
		if c.IconSetDir() != "" {
			t.Errorf("%s: IconSetDir = %q", style, c.IconSetDir())
		}
	}
}

func TestThemeCSS(t *testing.T) {
	c, _, err := Load(userFiles(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	builtin := func(name string) string { return "/* " + name + " */\n" }
	for _, tt := range []struct{ name, want string }{
		{"mine", "/* themes/yoru.css */\n\n@define-color accent #112233;\n"},   // over the default
		{"washi", "/* themes/washi.css */\n\n@define-color accent #445566;\n"}, // over its built-in self
		{"rinko", "/* themes/rinko.css */\n"},
		{"../mine", "/* themes/yoru.css */\n"},
	} {
		if got := c.ThemeCSS(tt.name, builtin); got != tt.want {
			t.Errorf("ThemeCSS(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
	// The user's tokens come last, so they win.
	css := c.ThemeCSS("mine", func(string) string { return "@define-color accent #000000;\n@define-color t0 #ffffff;\n" })
	if i, j := strings.Index(css, "#000000"), strings.Index(css, "#112233"); i > j {
		t.Errorf("user tokens before built-in ones: %q", css)
	}
}
