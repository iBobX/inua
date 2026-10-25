package portal

import (
	"image/color"
	"sync"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbustest"
)

type fakeShots struct {
	mu          sync.Mutex
	interactive bool
	path        string
	c           color.NRGBA
	ok          bool
}

func (f *fakeShots) Screenshot(appID string, interactive bool, done <-chan struct{}) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.interactive = interactive
	return f.path, f.ok
}

func (f *fakeShots) PickColor(appID string, done <-chan struct{}) (color.NRGBA, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.c, f.ok
}

func TestScreenshot(t *testing.T) {
	addr := dbustest.Start(t)
	conn := dbustest.Conn(t, addr)
	f := &fakeShots{path: "/run/user/1000/inua/portal/screenshot 1.png", c: color.NRGBA{R: 255, G: 51}, ok: true}
	client := serveShell(t, addr, conn, NewScreenshots(conn, f).Interface())
	obj := client.Object(ShellName, Path)
	handle := dbus.ObjectPath("/org/freedesktop/portal/desktop/request/1_3/s")

	var resp uint32
	var res map[string]dbus.Variant
	opts := map[string]dbus.Variant{"interactive": dbus.MakeVariant(true)}
	if err := obj.Call(Prefix+"Screenshot.Screenshot", 0, handle, "org.example.App", "", opts).Store(&resp, &res); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	interactive := f.interactive
	f.mu.Unlock()
	if resp != Success || !interactive || res["uri"].Value() != "file:///run/user/1000/inua/portal/screenshot%201.png" {
		t.Errorf("Screenshot: %d %v interactive=%v", resp, res, interactive)
	}
	if err := obj.Call(Prefix+"Screenshot.PickColor", 0, handle, "org.example.App", "", map[string]dbus.Variant{}).Store(&resp, &res); err != nil {
		t.Fatal(err)
	}
	if rgb, _ := res["color"].Value().([]any); resp != Success || len(rgb) != 3 || rgb[0] != 1.0 || rgb[1] != 0.2 || rgb[2] != 0.0 {
		t.Errorf("PickColor: %d %v", resp, res)
	}
	f.mu.Lock()
	f.ok = false
	f.mu.Unlock()
	if err := obj.Call(Prefix+"Screenshot.Screenshot", 0, handle, "", "", map[string]dbus.Variant{}).Store(&resp, &res); err != nil || resp != Cancelled {
		t.Errorf("cancelled: %d %v", resp, err)
	}
}
