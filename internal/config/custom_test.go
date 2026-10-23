package config

import (
	"errors"
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

func TestCustomName(t *testing.T) {
	for in, want := range map[string]string{
		"Nord":         "nord",
		"My Theme (2)": "my-theme-2",
		"_x_":          "x",
		"..":           "",
		"日本":           "",
		"a.b":          "a-b",
	} {
		if got := CustomName(in); got != want {
			t.Errorf("CustomName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInstallTheme(t *testing.T) {
	c, _, err := Load(userFiles(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	put := func(name, content string) string {
		p := filepath.Join(src, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	name, err := c.InstallTheme(put("Nord Dark.css", "@define-color accent #88c0d0;\n"))
	if err != nil || name != "nord-dark" {
		t.Fatalf("InstallTheme = %q, %v", name, err)
	}
	if !slices.Contains(c.ThemeNames(), "nord-dark") || !c.IsCustomTheme("nord-dark") {
		t.Errorf("themes = %v", c.ThemeNames())
	}
	if b, _ := os.ReadFile(c.ThemePath("nord-dark")); string(b) != "@define-color accent #88c0d0;\n" {
		t.Errorf("installed %q", b)
	}

	// Taken names, built-in or the user's, are left alone.
	for _, taken := range []string{"Nord Dark.css", "mine.css", "yoru.css"} {
		if _, err := c.InstallTheme(put(taken, "@define-color accent #000000;\n")); !errors.Is(err, ErrExists) {
			t.Errorf("%s: err = %v, want ErrExists", taken, err)
		}
	}
	if b, _ := os.ReadFile(c.ThemePath("mine")); string(b) != "@define-color accent #112233;\n" {
		t.Errorf("mine changed to %q", b)
	}
	if c.IsCustomTheme("washi") || c.IsCustomTheme("yoru") {
		t.Error("built-in themes count as the user's")
	}

	if _, err := c.InstallTheme(put("style.css", "window { color: red; }\n")); !errors.Is(err, ErrNotTheme) {
		t.Errorf("not a theme: err = %v", err)
	}
	if _, err := os.Stat(c.ThemePath("style")); !os.IsNotExist(err) {
		t.Errorf("not a theme was installed: %v", err)
	}
}

func TestInstallIconSet(t *testing.T) {
	c, _, err := Load(userFiles(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "Lucide Icons")
	for name, content := range map[string]string{
		"go-home-symbolic.svg": "<svg>home</svg>",
		"edit-symbolic.svg":    "<svg>edit</svg>",
		"README.md":            "",
		"sub/x-symbolic.svg":   "<svg/>",
	} {
		p := filepath.Join(src, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	name, n, err := c.InstallIconSet(src)
	if err != nil || name != "lucide-icons" || n != 2 {
		t.Fatalf("InstallIconSet = %q, %d, %v", name, n, err)
	}
	if got := c.IconSetNames(); !slices.Equal(got, []string{"phosphor", "system", "line", "lucide-icons"}) {
		t.Errorf("icon sets = %v", got)
	}
	entries, _ := os.ReadDir(c.IconSetPath("lucide-icons"))
	if len(entries) != 2 {
		t.Errorf("installed %v", entries)
	}
	all, _ := os.ReadDir(filepath.Join(c.Dir, "icons"))
	for _, e := range all {
		if strings.HasPrefix(e.Name(), ".install-") {
			t.Errorf("left %s behind", e.Name())
		}
	}

	if _, _, err := c.InstallIconSet(src); !errors.Is(err, ErrExists) {
		t.Errorf("again: err = %v, want ErrExists", err)
	}
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.InstallIconSet(empty); !errors.Is(err, ErrNoIcons) {
		t.Errorf("empty: err = %v, want ErrNoIcons", err)
	}
	if slices.Contains(c.IconSetNames(), "empty") {
		t.Error("an empty set was installed")
	}
	phosphor := filepath.Join(t.TempDir(), "Phosphor")
	if err := os.Mkdir(phosphor, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.InstallIconSet(phosphor); err == nil || errors.Is(err, ErrNoIcons) {
		t.Errorf("phosphor: err = %v, want an unusable name", err)
	}
}

func TestThemeColors(t *testing.T) {
	css, err := os.ReadFile("../../ui/themes/yoru.css")
	if err != nil {
		t.Fatal(err)
	}
	c := ThemeColors(string(css))
	if c["app"] != "#0B1012" || c["field"] != "#44504E" || c["accent"] != "#9FC3C9" {
		t.Errorf("yoru: %v", c)
	}
}
