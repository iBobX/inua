package portalconf

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeDirs is a system with the Hyprland and GTK backends, and the user's
// and /etc's config directories, empty.
func fakeDirs(t *testing.T) Dirs {
	root := t.TempDir()
	d := Dirs{
		Own:      filepath.Join(root, "data", "inua", "portals"),
		System:   filepath.Join(root, "usr"),
		Configs:  []string{filepath.Join(root, "config"), filepath.Join(root, "etc")},
		Services: filepath.Join(root, "data", "dbus-1", "services"),
		Exec:     func(program string) (string, error) { return "/opt/inua/bin/" + program, nil },
		Xdph:     filepath.Join(root, "config", "hypr", "xdph.conf"),
	}
	write(t, filepath.Join(d.System, "portals", "hyprland.portal"), "[portal]\n")
	write(t, filepath.Join(d.System, "portals", "gtk.portal"), "[portal]\n")
	write(t, filepath.Join(d.System, "hyprland-portals.conf"), "[preferred]\ndefault=hyprland;gtk\n")
	write(t, filepath.Join(d.System, "gnome-portals.conf"), "[preferred]\ndefault=gnome;gtk\n")
	return d
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestEnableWritesDirectory(t *testing.T) {
	d := fakeDirs(t)
	if on, err := d.Set(true, "settings"); err != nil || !on {
		t.Fatalf("Set = %v, %v", on, err)
	}
	conf := read(t, filepath.Join(d.Own, confName))
	if !strings.Contains(conf, "default=hyprland;gtk\n") || !strings.Contains(conf, "org.freedesktop.impl.portal.Settings=inua;hyprland;gtk\n") {
		t.Errorf("config:\n%s", conf)
	}
	if p := read(t, filepath.Join(d.Own, "inua.portal")); !strings.Contains(p, "DBusName=org.freedesktop.impl.portal.desktop.inua\n") ||
		!strings.Contains(p, "Interfaces=org.freedesktop.impl.portal.Settings;") {
		t.Errorf("inua.portal:\n%s", p)
	}
	for _, name := range []string{"hyprland.portal", "gtk.portal", "gnome-portals.conf"} {
		target, err := os.Readlink(filepath.Join(d.Own, name))
		if err != nil || !strings.HasPrefix(target, d.System) {
			t.Errorf("%s: link to %q, %v", name, target, err)
		}
	}
	if got := d.Enabled(); !slices.Equal(got, []string{"Settings"}) {
		t.Errorf("Enabled = %v", got)
	}
}

func TestUserConfigKept(t *testing.T) {
	d := fakeDirs(t)
	write(t, filepath.Join(d.Configs[0], confName), "[preferred]\ndefault=hyprland;gtk\norg.freedesktop.impl.portal.Settings=darkman;gtk\norg.freedesktop.impl.portal.Print=gtk\n")
	write(t, filepath.Join(d.Configs[1], "gnome-portals.conf"), "[preferred]\ndefault=etc\n")
	if _, err := d.Set(true, "Settings"); err != nil {
		t.Fatal(err)
	}
	conf := read(t, filepath.Join(d.Own, confName))
	for _, want := range []string{"org.freedesktop.impl.portal.Settings=inua;darkman;gtk\n", "org.freedesktop.impl.portal.Print=gtk\n"} {
		if !strings.Contains(conf, want) {
			t.Errorf("config has no %q:\n%s", want, conf)
		}
	}
	if target, _ := os.Readlink(filepath.Join(d.Own, "gnome-portals.conf")); target != filepath.Join(d.Configs[1], "gnome-portals.conf") {
		t.Errorf("gnome-portals.conf links to %q, want /etc's over the system's", target)
	}
}

func TestEnableTwiceOnce(t *testing.T) {
	d := fakeDirs(t)
	d.Set(true, "Settings")
	d.Set(true, "Settings")
	conf := read(t, filepath.Join(d.Own, confName))
	if n := strings.Count(conf, "Settings="); n != 1 {
		t.Errorf("%d Settings lines:\n%s", n, conf)
	}
	if strings.Contains(conf, "inua;inua") {
		t.Errorf("inua twice:\n%s", conf)
	}
}

func TestDisableLastRemovesDirectory(t *testing.T) {
	d := fakeDirs(t)
	d.Set(true, "Settings")
	if on, err := d.Set(false, "Settings"); err != nil || on {
		t.Fatalf("Set = %v, %v", on, err)
	}
	if _, err := os.Stat(d.Own); !os.IsNotExist(err) {
		t.Errorf("directory left: %v", err)
	}
	if on, err := d.Refresh(); on || err != nil {
		t.Errorf("Refresh = %v, %v; want nothing written", on, err)
	}
}

func TestRefreshFollowsSystem(t *testing.T) {
	d := fakeDirs(t)
	d.Set(true, "Settings")
	os.Remove(filepath.Join(d.System, "portals", "gtk.portal"))
	write(t, filepath.Join(d.System, "portals", "kde.portal"), "[portal]\n")
	if _, err := d.Refresh(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(d.Own, "gtk.portal")); !os.IsNotExist(err) {
		t.Errorf("gtk.portal still linked")
	}
	if _, err := os.Readlink(filepath.Join(d.Own, "kde.portal")); err != nil {
		t.Errorf("kde.portal not linked: %v", err)
	}
}

func TestUnknownInterface(t *testing.T) {
	d := fakeDirs(t)
	if _, err := d.Set(true, "Print"); err == nil {
		t.Error("Print enabled; inua does not serve it")
	}
	if _, err := os.Stat(d.Own); !os.IsNotExist(err) {
		t.Errorf("directory written after an error")
	}
}

func TestServiceFiles(t *testing.T) {
	d := fakeDirs(t)
	keyring := filepath.Join(d.Services, "org.freedesktop.impl.portal.desktop.inua.keyring.service")
	d.Set(true, "Settings")
	if _, err := os.Stat(keyring); !os.IsNotExist(err) {
		t.Error("keyring's service file written with Secret off")
	}
	d.Set(true, "Secret")
	if s := read(t, keyring); !strings.Contains(s, "Name=org.freedesktop.impl.portal.desktop.inua.keyring\n") || !strings.Contains(s, "Exec=/opt/inua/bin/inua-keyring\n") {
		t.Errorf("service file:\n%s", s)
	}
	if conf := read(t, filepath.Join(d.Own, confName)); !strings.Contains(conf, "org.freedesktop.impl.portal.Secret=inua-keyring;") {
		t.Errorf("config:\n%s", conf)
	}
	d.Set(false, "Secret")
	if _, err := os.Stat(keyring); !os.IsNotExist(err) {
		t.Error("service file left after Secret went off")
	}
	// A file of the same name that inua did not write stays.
	write(t, keyring, "[D-BUS Service]\nName=mine\n")
	d.Set(false, "Settings")
	if _, err := os.Stat(keyring); err != nil {
		t.Errorf("someone else's service file removed: %v", err)
	}
}

func TestSharePicker(t *testing.T) {
	d := fakeDirs(t)
	// No config: one of inua's own, removed again.
	if on, err := d.Set(true, "sharepicker"); err != nil || !on {
		t.Fatalf("Set = %v, %v", on, err)
	}
	if s := read(t, d.Xdph); !strings.Contains(s, "screencopy {\n    custom_picker_binary = /opt/inua/bin/inua-share-picker\n}") {
		t.Errorf("xdph.conf:\n%s", s)
	}
	if !slices.Contains(d.Enabled(), SharePicker) {
		t.Errorf("Enabled = %v", d.Enabled())
	}
	if _, err := os.Stat(d.Own); !os.IsNotExist(err) {
		t.Error("the portal directory written for the share picker alone")
	}
	d.Set(false, SharePicker)
	if _, err := os.Stat(d.Xdph); !os.IsNotExist(err) {
		t.Error("inua's own xdph.conf left")
	}

	// The user's config: the rest kept, the picker put in its section.
	user := "screencopy {\n    max_fps = 30\n    custom_picker_binary = hyprland-share-picker\n}\n"
	write(t, d.Xdph, user)
	d.Set(true, SharePicker)
	if s := read(t, d.Xdph); !strings.Contains(s, "max_fps = 30\n") || !strings.Contains(s, "custom_picker_binary = /opt/inua/bin/inua-share-picker\n") || strings.Contains(s, "hyprland-share-picker") {
		t.Errorf("xdph.conf:\n%s", s)
	}
	d.Set(false, SharePicker)
	if s := read(t, d.Xdph); !strings.Contains(s, "max_fps = 30") || strings.Contains(s, "custom_picker_binary") {
		t.Errorf("after disabling:\n%s", s)
	}
	write(t, d.Xdph, "screencopy {\n    max_fps = 30\n}\n")
	d.Set(true, SharePicker)
	if s := read(t, d.Xdph); s != "screencopy {\n    custom_picker_binary = /opt/inua/bin/inua-share-picker\n    max_fps = 30\n}\n" {
		t.Errorf("into the section:\n%s", s)
	}
}
