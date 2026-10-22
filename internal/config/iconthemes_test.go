package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fakeIconThemes points the icon folders at a temporary data directory
// with these index.theme files, by theme folder.
func fakeIconThemes(t *testing.T, themes map[string]string) {
	t.Helper()
	data := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", filepath.Join(data, "home"))
	t.Setenv("XDG_DATA_DIRS", filepath.Join(data, "system"))
	for name, index := range themes {
		dir := filepath.Join(data, "system", "icons", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if index != "" {
			if err := os.WriteFile(filepath.Join(dir, "index.theme"), []byte(index), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestIconThemes(t *testing.T) {
	icons := "[Icon Theme]\nName=%s\nDirectories=16x16/apps\n"
	fakeIconThemes(t, map[string]string{
		"Papirus":     strings.Replace(icons, "%s", "Papirus", 1),
		"numix":       strings.Replace(icons, "%s", "Numix Circle", 1),
		"Adwaita":     "[Icon Theme]\nName=Adwaita\nHidden=true\nDirectories=scalable/apps\n",
		"Hidden":      "[Icon Theme]\nName=Hidden\nHidden=true\nDirectories=scalable/apps\n",
		"DMZ-White":   "[Icon Theme]\nName=DMZ (White)\nInherits=DMZ\n", // cursors only
		"hicolor":     strings.Replace(icons, "%s", "Hicolor", 1),
		"inua-icons":  strings.Replace(icons, "%s", "inua", 1),
		"no-index":    "",
		"other-group": "[Desktop Entry]\nName=Nope\nDirectories=16x16\n",
	})
	want := []IconTheme{{"Adwaita", "Adwaita"}, {"numix", "Numix Circle"}, {"Papirus", "Papirus"}}
	if got := IconThemes(); !reflect.DeepEqual(got, want) {
		t.Errorf("IconThemes() = %v, want %v", got, want)
	}
	if !IsIconTheme("Papirus") || IsIconTheme("hicolor") || IsIconTheme("Hidden") {
		t.Error("IsIconTheme")
	}
}

func TestIconThemeSettings(t *testing.T) {
	fakeIconThemes(t, map[string]string{
		"Papirus": "[Icon Theme]\nName=Papirus\nDirectories=16x16/apps\n",
		"line":    "[Icon Theme]\nName=Line\nDirectories=16x16/apps\n",
	})
	c, w, err := Load(userFiles(t, "[style]\nicons = \"Papirus\"\napp_icons = \"Papirus\"\n"))
	if err != nil || len(w) != 0 {
		t.Fatalf("err=%v warnings=%v", err, w)
	}
	if c.Style.Icons != "Papirus" || c.Style.AppIcons != "Papirus" || c.IconThemeName() != "Papirus" || c.IconSetDir() != "" {
		t.Errorf("icons %q, app icons %q, theme %q, set %q", c.Style.Icons, c.Style.AppIcons, c.IconThemeName(), c.IconSetDir())
	}

	// A set of the user's wins over an installed theme of its name.
	c, _, err = Load(userFiles(t, "[style]\nicons = \"line\"\n"))
	if err != nil || c.IconThemeName() != "" || c.IconSetDir() == "" {
		t.Errorf("line: err=%v theme %q set %q", err, c.IconThemeName(), c.IconSetDir())
	}

	c, w, err = Load(userFiles(t, "[style]\nicons = \"Gone\"\napp_icons = \"Gone\"\n"))
	if err != nil || len(w) != 2 || c.Style.Icons != "phosphor" || c.Style.AppIcons != "system" {
		t.Errorf("err=%v warnings=%v icons %q app icons %q", err, w, c.Style.Icons, c.Style.AppIcons)
	}
}
