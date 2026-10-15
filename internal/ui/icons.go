package ui

import (
	"cmp"
	"log"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/rundir"
	"github.com/lyricat/inua/internal/shell"
	layouts "github.com/lyricat/inua/ui"
)

// The shell's icon set. style.icons = "phosphor" puts the icons in
// ui/icons in an icon theme of inua's own, which inherits the system's,
// so app icons (and any icon the set lacks) still come from it. A user's
// set goes over the Phosphor icons in a theme named after it, so changing
// sets changes the theme's name and GTK loads it afresh. The icon theme
// in GtkSettings belongs to this process: other apps keep theirs.

// iconThemeName names inua's icon theme.
const iconThemeName = "inua-phosphor"

type icons struct {
	system string // the system's icon theme, read before inua set its own
	dir    string // the directory holding inua's themes, once on the search path
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
	overlay := s.cfg.IconSetDir()
	if s.cfg.Style.Icons != "system" {
		name := iconThemeName
		if overlay != "" {
			name = "inua-icons-" + s.cfg.Style.Icons
		}
		if err := s.writeIconTheme(name, overlay); err != nil {
			log.Printf("icons: %v; using %s", err, want)
		} else {
			want = name
		}
	}
	current := stringProperty(settings, "gtk-icon-theme-name")
	if current == want && overlay != "" {
		// The set's files may have changed: make GTK read them again.
		setStringProperty(settings, "gtk-icon-theme-name", ic.system)
		current = ic.system
	}
	if current != want {
		setStringProperty(settings, "gtk-icon-theme-name", want)
		log.Printf("icons: %s", want)
	}
}

// writeIconTheme writes an icon theme of the built-in icons, with the
// overlay's over them, to the runtime directory and puts it on the icon
// search path.
func (s *shellUI) writeIconTheme(name, overlay string) error {
	ic := &s.icons
	dir := ic.dir
	if dir == "" {
		var err error
		if dir, err = rundir.Sub("icons"); err != nil {
			return err
		}
	}
	entries, err := layouts.FS.ReadDir("icons")
	if err != nil {
		return err
	}
	t := shell.IconTheme{
		Name:     name,
		Comment:  "Icons for inua-shell (written by inua-shell)",
		Inherits: ic.system,
		Read:     func(file string) string { return s.readUI("icons/" + file) },
		Overlay:  overlay,
	}
	for _, e := range entries {
		t.Builtin = append(t.Builtin, e.Name())
	}
	if err := t.Write(dir); err != nil {
		return err
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
