package portal

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbustest"
)

type fakeAsker struct {
	mu      sync.Mutex
	q       AccessQuestion
	grant   bool
	path    string
	preview bool
	result  uint32
}

func (f *fakeAsker) Ask(q AccessQuestion, done <-chan struct{}) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.q = q
	return f.grant
}

func (f *fakeAsker) Notify(appID, name string, done <-chan struct{}) uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.result
}

func (f *fakeAsker) SetWallpaper(appID, path string, preview bool, done <-chan struct{}) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.path, f.preview = path, preview
	return f.grant
}

// asked is what the fake was asked, read under its lock.
type asked struct {
	q       AccessQuestion
	path    string
	preview bool
}

func (f *fakeAsker) get() asked {
	f.mu.Lock()
	defer f.mu.Unlock()
	return asked{q: f.q, path: f.path, preview: f.preview}
}

const testHandle = dbus.ObjectPath("/org/freedesktop/portal/desktop/request/1_4/x")

func TestAccess(t *testing.T) {
	addr := dbustest.Start(t)
	conn := dbustest.Conn(t, addr)
	f := &fakeAsker{grant: true}
	obj := serveShell(t, addr, conn, NewAccess(conn, f).Interface()).Object(ShellName, Path)
	opts := map[string]dbus.Variant{
		"grant_label": dbus.MakeVariant("Allow"),
		"choices": dbus.MakeVariant([]struct {
			ID, Label string
			Options   []struct{ ID, Label string }
			Default   string
		}{{"accuracy", "Accuracy", []struct{ ID, Label string }{{"city", "City"}, {"exact", "Exact"}}, "city"}}),
	}
	var resp uint32
	var res map[string]dbus.Variant
	if err := obj.Call(Prefix+"Access.AccessDialog", 0, testHandle, "org.example.Maps", "", "Use the camera?", "Maps asks", "Body", opts).Store(&resp, &res); err != nil {
		t.Fatal(err)
	}
	q := f.get().q
	if resp != Success || q.Title != "Use the camera?" || q.Grant != "Allow" || q.AppID != "org.example.Maps" {
		t.Errorf("granted: %d %+v", resp, q)
	}
	if c, _ := res["choices"].Value().([][]any); len(c) != 1 || c[0][1] != "city" {
		t.Errorf("choices %v", res["choices"])
	}
	f.mu.Lock()
	f.grant = false
	f.mu.Unlock()
	if err := obj.Call(Prefix+"Access.AccessDialog", 0, testHandle, "", "", "t", "", "", map[string]dbus.Variant{}).Store(&resp, &res); err != nil || resp != Cancelled {
		t.Errorf("denied: %d %v", resp, err)
	}
}

func TestBackground(t *testing.T) {
	addr := dbustest.Start(t)
	conn := dbustest.Conn(t, addr)
	f := &fakeAsker{result: BackgroundAllowOnce}
	auto := t.TempDir()
	b := NewBackground(conn, f, auto)
	client := serveShell(t, addr, conn, b.Interface())
	obj := client.Object(ShellName, Path)
	sigs := make(chan *dbus.Signal, 8)
	client.Signal(sigs)
	client.AddMatchSignal(dbus.WithMatchInterface(Prefix + "Background"))

	b.SetApps(map[string]uint32{"org.example.A": AppActive, "org.example.B": AppRunning})
	var apps map[string]dbus.Variant
	if err := obj.Call(Prefix+"Background.GetAppState", 0).Store(&apps); err != nil {
		t.Fatal(err)
	}
	if len(apps) != 2 || apps["org.example.A"].Value() != AppActive {
		t.Errorf("apps %v", apps)
	}
	gotChanged := false
	for !gotChanged {
		select {
		case sig := <-sigs:
			gotChanged = sig.Name == Prefix+"Background.RunningApplicationsChanged"
		case <-time.After(2 * time.Second):
			t.Fatal("no RunningApplicationsChanged")
		}
	}

	var resp uint32
	var res map[string]dbus.Variant
	if err := obj.Call(Prefix+"Background.NotifyBackground", 0, testHandle, "org.example.C", "C").Store(&resp, &res); err != nil {
		t.Fatal(err)
	}
	if resp != Success || res["result"].Value() != BackgroundAllowOnce {
		t.Errorf("NotifyBackground %d %v", resp, res)
	}

	var ok bool
	if err := obj.Call(Prefix+"Background.EnableAutostart", 0, "org.example.C", true, []string{"flatpak", "run", "org.example.C", "--x=a b"}, uint32(0)).Store(&ok); err != nil || !ok {
		t.Fatalf("EnableAutostart %v %v", ok, err)
	}
	entry, _ := os.ReadFile(filepath.Join(auto, "org.example.C.desktop"))
	if !strings.Contains(string(entry), `Exec=flatpak run org.example.C "--x=a b"`) {
		t.Errorf("entry:\n%s", entry)
	}
	obj.Call(Prefix+"Background.EnableAutostart", 0, "org.example.C", false, []string{}, uint32(0)).Store(&ok)
	if _, err := os.Stat(filepath.Join(auto, "org.example.C.desktop")); !os.IsNotExist(err) {
		t.Error("entry left after disabling")
	}
	if obj.Call(Prefix+"Background.EnableAutostart", 0, "../evil", true, []string{"x"}, uint32(0)).Store(&ok); ok {
		t.Error("an app ID with a slash written")
	}
}

func TestWallpaper(t *testing.T) {
	addr := dbustest.Start(t)
	conn := dbustest.Conn(t, addr)
	f := &fakeAsker{grant: true}
	obj := serveShell(t, addr, conn, NewWallpaper(conn, f).Interface()).Object(ShellName, Path)
	var resp uint32
	opts := map[string]dbus.Variant{"show-preview": dbus.MakeVariant(true)}
	if err := obj.Call(Prefix+"Wallpaper.SetWallpaperURI", 0, testHandle, "org.example.App", "", "file:///home/a/Sea%20view.jpg", opts).Store(&resp); err != nil {
		t.Fatal(err)
	}
	if got := f.get(); resp != Success || got.path != "/home/a/Sea view.jpg" || !got.preview {
		t.Errorf("SetWallpaperURI %d %+v", resp, got)
	}
	if err := obj.Call(Prefix+"Wallpaper.SetWallpaperURI", 0, testHandle, "", "", "https://example.com/a.jpg", map[string]dbus.Variant{}).Store(&resp); err != nil || resp != Ended {
		t.Errorf("a URL not a file: %d %v", resp, err)
	}
}
