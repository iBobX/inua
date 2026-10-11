package ui

import (
	"cmp"
	"fmt"
	"github.com/lyricat/inua/internal/rundir"
	"log"
	"os"
	"path/filepath"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	layouts "github.com/lyricat/inua/ui"
)

// The shell's icon set. style.icons = "phosphor" puts the icons in
// ui/icons in an icon theme of inua's own, which inherits the system's,
// so app icons (and any icon the set lacks) still come from it. The icon
// theme in GtkSettings belongs to this process: other apps keep theirs.

// iconThemeName names inua's icon theme.
const iconThemeName = "inua-phosphor"

type icons struct {
	system string // the system's icon theme, read before inua set its own
	dir    string // the directory holding inua's theme, once on the search path
}

// loadIcons switches the icon theme to follow style.icons. GTK redraws
// every icon on its own when the theme changes.
func (s *shellUI) loadIcons() {
	settings := gtk.SettingsGetDefault()
	ic := &s.icons
	if ic.system == "" {
		ic.system = cmp.Or(stringProperty(settings, "gtk-icon-theme-name"), "Adwaita")
	}
	want := ic.system
	if s.cfg.Style.Icons == "phosphor" {
		if err := s.writeIconTheme(); err != nil {
			log.Printf("icons: %v; using %s", err, want)
		} else {
			want = iconThemeName
		}
	}
	if stringProperty(settings, "gtk-icon-theme-name") != want {
		setStringProperty(settings, "gtk-icon-theme-name", want)
		log.Printf("icons: %s", want)
	}
}

// writeIconTheme writes inua's icon theme to the runtime directory and
// puts it on the icon search path.
func (s *shellUI) writeIconTheme() error {
	ic := &s.icons
	dir := ic.dir
	if dir == "" {
		var err error
		if dir, err = rundir.Sub("icons"); err != nil {
			return err
		}
	}
	theme := filepath.Join(dir, iconThemeName)
	if err := os.MkdirAll(filepath.Join(theme, "symbolic"), 0o700); err != nil {
		return err
	}
	index := fmt.Sprintf(`[Icon Theme]
Name=%s
Comment=Phosphor icons for inua-shell (written by inua-shell)
Inherits=%s,hicolor
Directories=symbolic

[symbolic]
Size=16
MinSize=8
MaxSize=512
Type=Scalable
Context=Status
`, iconThemeName, ic.system)
	if err := os.WriteFile(filepath.Join(theme, "index.theme"), []byte(index), 0o644); err != nil {
		return err
	}
	entries, err := layouts.FS.ReadDir("icons")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".svg") {
			continue
		}
		svg := s.readUI("icons/" + e.Name())
		if err := os.WriteFile(filepath.Join(theme, "symbolic", e.Name()), []byte(svg), 0o644); err != nil {
			return err
		}
	}
	if ic.dir == "" {
		gtk.IconThemeGetForDisplay(gdk.DisplayGetDefault()).AddSearchPath(dir)
		ic.dir = dir
	}
	return nil
}

func stringProperty(o *gtk.Settings, name string) string {
	var v gobject.Value
	v.Init(gobject.TypeStringVal)
	defer v.Unset()
	o.GetProperty(name, &v)
	return v.GetString()
}

func setStringProperty(o *gtk.Settings, name, value string) {
	var v gobject.Value
	v.Init(gobject.TypeStringVal)
	defer v.Unset()
	v.SetString(value)
	o.SetProperty(name, &v)
}
