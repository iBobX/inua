package portal

import (
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbustest"
)

// serveShell serves interfaces on conn under the shell's name, and
// returns a connection to call them from.
func serveShell(t *testing.T, addr string, conn *dbus.Conn, ifaces ...Interface) *dbus.Conn {
	t.Helper()
	if _, err := Export(conn, ifaces...); err != nil {
		t.Fatal(err)
	}
	if err := Own(conn, ShellName); err != nil {
		t.Fatal(err)
	}
	return dbustest.Conn(t, addr)
}

func TestRequestClose(t *testing.T) {
	addr := dbustest.Start(t)
	conn := dbustest.Conn(t, addr)
	if err := Own(conn, ShellName); err != nil {
		t.Fatal(err)
	}
	handle := dbus.ObjectPath("/org/freedesktop/portal/desktop/request/1_5/t")
	r, err := NewRequest(conn, handle)
	if err != nil {
		t.Fatal(err)
	}
	client := dbustest.Conn(t, addr)
	obj := client.Object(ShellName, handle)
	for range 2 { // closing twice is not an error
		if err := obj.Call(Prefix+"Request.Close", 0).Err; err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-r.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done not closed")
	}
	r.End()
	if err := obj.Call(Prefix+"Request.Close", 0).Err; err == nil {
		t.Error("Close answered after End")
	}
}

func TestSettings(t *testing.T) {
	addr := dbustest.Start(t)
	conn := dbustest.Conn(t, addr)
	s := NewSettings(conn)
	s.Set(Appearance{Dark: true, Accent: color.NRGBA{R: 255, G: 0, B: 51}, TextScale: 1.25}.Values())
	if _, err := Export(conn, s.Interface()); err != nil {
		t.Fatal(err)
	}
	if err := Own(conn, ShellName); err != nil {
		t.Fatal(err)
	}
	client := dbustest.Conn(t, addr)
	obj := client.Object(ShellName, Path)

	var all map[string]map[string]dbus.Variant
	if err := obj.Call(Prefix+"Settings.ReadAll", 0, []string{"org.freedesktop.*"}).Store(&all); err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all["org.freedesktop.appearance"]["color-scheme"].Value() != uint32(1) {
		t.Errorf("ReadAll(org.freedesktop.*) = %v", all)
	}
	var v dbus.Variant
	if err := obj.Call(Prefix+"Settings.Read", 0, "org.freedesktop.appearance", "accent-color").Store(&v); err != nil {
		t.Fatal(err)
	}
	if rgb, ok := v.Value().([]any); !ok || len(rgb) != 3 || rgb[0] != 1.0 || rgb[1] != 0.0 || rgb[2] != 0.2 {
		t.Errorf("accent-color = %v", v)
	}
	err := obj.Call(Prefix+"Settings.Read", 0, "org.gnome.desktop.interface", "icon-theme").Store(&v)
	if e, ok := err.(dbus.Error); !ok || e.Name != "org.freedesktop.portal.Error.NotFound" {
		t.Errorf("icon-theme without one: %v, want NotFound", err)
	}

	// A change is signalled; what did not change is not.
	sigs := make(chan *dbus.Signal, 8)
	client.Signal(sigs)
	if err := client.AddMatchSignal(dbus.WithMatchInterface(Prefix + "Settings")); err != nil {
		t.Fatal(err)
	}
	s.Set(Appearance{Dark: false, Accent: color.NRGBA{R: 255, G: 0, B: 51}, TextScale: 1.25}.Values())
	select {
	case sig := <-sigs:
		if sig.Body[1] != "color-scheme" || sig.Body[2].(dbus.Variant).Value() != uint32(2) {
			t.Errorf("SettingChanged %v", sig.Body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no SettingChanged")
	}
	select {
	case sig := <-sigs:
		t.Errorf("another SettingChanged: %v", sig.Body)
	case <-time.After(200 * time.Millisecond):
	}
}

// fakeGTK serves Settings as another backend would, under its own name.
func fakeGTK(t *testing.T, addr string) {
	conn := dbustest.Conn(t, addr)
	s := NewSettings(conn)
	s.Set(map[string]map[string]dbus.Variant{
		"org.freedesktop.appearance":  {"color-scheme": dbus.MakeVariant(uint32(2))},
		"org.gnome.desktop.interface": {"icon-theme": dbus.MakeVariant("Adwaita"), "font-name": dbus.MakeVariant("Cantarell 11")},
	})
	if _, err := Export(conn, s.Interface()); err != nil {
		t.Fatal(err)
	}
	if err := Own(conn, Prefix+"desktop.fgtk"); err != nil {
		t.Fatal(err)
	}
}

// TestThroughFrontend asks the real xdg-desktop-portal, given a directory
// as inua writes it: inua's keys win, the other backend's fill in.
func TestThroughFrontend(t *testing.T) {
	frontend := ""
	for _, p := range []string{"/usr/libexec/xdg-desktop-portal", "/usr/lib/xdg-desktop-portal"} {
		if _, err := os.Stat(p); err == nil {
			frontend = p
		}
	}
	if frontend == "" {
		t.Skip("xdg-desktop-portal not installed")
	}
	addr := dbustest.Start(t)
	dir := t.TempDir()
	for name, body := range map[string]string{
		"inua.portal":           "[portal]\nDBusName=" + ShellName + "\nInterfaces=" + Prefix + "Settings;\n",
		"fgtk.portal":           "[portal]\nDBusName=" + Prefix + "desktop.fgtk\nInterfaces=" + Prefix + "Settings;\n",
		"hyprland-portals.conf": "[preferred]\ndefault=fgtk\n" + Prefix + "Settings=inua;fgtk\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fakeGTK(t, addr)
	conn := dbustest.Conn(t, addr)
	s := NewSettings(conn)
	s.Set(Appearance{Dark: true, IconTheme: "Papirus", TextScale: 1}.Values())
	if _, err := Export(conn, s.Interface()); err != nil {
		t.Fatal(err)
	}
	if err := Own(conn, ShellName); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(frontend)
	cmd.Env = append(os.Environ(), "DBUS_SESSION_BUS_ADDRESS="+addr, "XDG_DESKTOP_PORTAL_DIR="+dir, "XDG_CURRENT_DESKTOP=Hyprland")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })

	client := dbustest.Conn(t, addr)
	obj := client.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
	read := func(ns, key string) any {
		var v dbus.Variant
		var err error
		for range 50 { // until the frontend is up
			if err = obj.Call("org.freedesktop.portal.Settings.ReadOne", 0, ns, key).Store(&v); err == nil {
				return v.Value()
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("ReadOne(%s, %s): %v", ns, key, err)
		return nil
	}
	if got := read("org.freedesktop.appearance", "color-scheme"); got != uint32(1) {
		t.Errorf("color-scheme = %v, want inua's 1", got)
	}
	if got := read("org.gnome.desktop.interface", "icon-theme"); got != "Papirus" {
		t.Errorf("icon-theme = %v, want inua's", got)
	}
	if got := read("org.gnome.desktop.interface", "font-name"); got != "Cantarell 11" {
		t.Errorf("font-name = %v, want the other backend's", got)
	}
}
