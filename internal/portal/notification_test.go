package portal

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbustest"
	"github.com/lyricat/inua/internal/notify"
)

type fakeCenter struct {
	mu     sync.Mutex
	notes  []notify.Notification
	closed []uint32
	got    chan struct{}
}

func (c *fakeCenter) Notify(n notify.Notification) {
	c.mu.Lock()
	c.notes = append(c.notes, n)
	c.mu.Unlock()
	c.got <- struct{}{}
}

func (c *fakeCenter) Close(id uint32) {
	c.mu.Lock()
	c.closed = append(c.closed, id)
	c.mu.Unlock()
	c.got <- struct{}{}
}

func (c *fakeCenter) wait(t *testing.T) {
	t.Helper()
	select {
	case <-c.got:
	case <-time.After(2 * time.Second):
		t.Fatal("the center heard nothing")
	}
}

func TestNotifications(t *testing.T) {
	addr := dbustest.Start(t)
	conn := dbustest.Conn(t, addr)
	center := &fakeCenter{got: make(chan struct{}, 8)}
	next := uint32(40)
	dir := t.TempDir()
	n := NewNotifications(conn, center, func() uint32 { next++; return next }, dir)
	client := serveShell(t, addr, conn, n.Interface())
	obj := client.Object(ShellName, Path)
	add := func(id string, opts map[string]dbus.Variant) {
		t.Helper()
		if err := obj.Call(Prefix+"Notification.AddNotification", 0, "org.example.App", id, opts).Err; err != nil {
			t.Fatal(err)
		}
	}

	add("new-mail", map[string]dbus.Variant{
		"title":    dbus.MakeVariant("New mail"),
		"body":     dbus.MakeVariant("From <Ann> & co"),
		"priority": dbus.MakeVariant("urgent"),
		"icon": dbus.MakeVariant(struct {
			Kind string
			V    dbus.Variant
		}{"themed", dbus.MakeVariant([]string{"mail-unread", "mail"})}),
		"default-action":        dbus.MakeVariant("app.open"),
		"default-action-target": dbus.MakeVariant("inbox"),
		"buttons": dbus.MakeVariant([]map[string]dbus.Variant{
			{"label": dbus.MakeVariant("Archive"), "action": dbus.MakeVariant("app.archive"), "target": dbus.MakeVariant(uint32(7))},
		}),
	})
	center.wait(t)
	got := center.notes[0]
	if got.ID != 41 || got.Entry != "org.example.App" || got.Summary != "New mail" || got.Body != "From &lt;Ann&gt; &amp; co" ||
		got.Urgency != notify.Critical || got.Icon != "mail-unread" || len(got.Actions) != 1 || got.Actions[0].Label != "Archive" {
		t.Errorf("notification %+v", got)
	}
	if !n.Owns(41) || n.Owns(40) {
		t.Error("Owns")
	}

	sigs := make(chan *dbus.Signal, 4)
	client.Signal(sigs)
	client.AddMatchSignal(dbus.WithMatchInterface(Prefix + "Notification"))
	if !n.Invoke(41, got.Actions[0].Key) {
		t.Fatal("button not invoked")
	}
	var sig *dbus.Signal
	for sig == nil || sig.Name != Prefix+"Notification.ActionInvoked" {
		select {
		case sig = <-sigs:
		case <-time.After(2 * time.Second):
			t.Fatal("no ActionInvoked")
		}
	}
	param, _ := sig.Body[3].([]dbus.Variant)
	if sig.Body[0] != "org.example.App" || sig.Body[1] != "new-mail" || sig.Body[2] != "app.archive" || len(param) != 1 || param[0].Value() != uint32(7) {
		t.Errorf("ActionInvoked %v", sig.Body)
	}

	// The same ID replaces it; one without a default action leaves the
	// click to the shell.
	add("new-mail", map[string]dbus.Variant{"title": dbus.MakeVariant("2 new mails"), "markup-body": dbus.MakeVariant("<b>Ann</b>")})
	center.wait(t)
	if r := center.notes[1]; r.ID != 41 || r.Body != "<b>Ann</b>" {
		t.Errorf("replacement %+v", r)
	}
	if n.Invoke(41, "default") {
		t.Error("default invoked without a default action")
	}

	// An icon sent as bytes is written to a file.
	add("pic", map[string]dbus.Variant{"title": dbus.MakeVariant("Pic"), "icon": dbus.MakeVariant(struct {
		Kind string
		V    dbus.Variant
	}{"bytes", dbus.MakeVariant([]byte("PNG…"))})})
	center.wait(t)
	if icon := center.notes[2].Icon; filepath.Dir(icon) != dir {
		t.Errorf("icon %q, want a file in %s", icon, dir)
	} else if b, _ := os.ReadFile(icon); string(b) != "PNG…" {
		t.Errorf("icon file holds %q", b)
	}

	if err := obj.Call(Prefix+"Notification.RemoveNotification", 0, "org.example.App", "new-mail").Err; err != nil {
		t.Fatal(err)
	}
	center.wait(t)
	if len(center.closed) != 1 || center.closed[0] != 41 || n.Owns(41) {
		t.Errorf("closed %v", center.closed)
	}
}

func TestNotificationsOff(t *testing.T) {
	addr := dbustest.Start(t)
	conn := dbustest.Conn(t, addr)
	center := &fakeCenter{got: make(chan struct{}, 8)}
	n := NewNotifications(conn, center, func() uint32 { return 0 }, "")
	client := serveShell(t, addr, conn, n.Interface())
	err := client.Object(ShellName, Path).Call(Prefix+"Notification.AddNotification", 0, "a", "b", map[string]dbus.Variant{}).Err
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-center.got:
		t.Error("shown with notifications off")
	case <-time.After(100 * time.Millisecond):
	}
}
