package chooser

import (
	"slices"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/apps/files/pick"
	"github.com/lyricat/inua/internal/dbustest"
	"github.com/lyricat/inua/internal/portal"
)

// fakePicker answers as it is told, or waits for the request to close.
type fakePicker struct {
	got    chan pick.Request
	answer pick.Answer
	ok     bool
	wait   bool // until done
}

func (f *fakePicker) Pick(r pick.Request, done <-chan struct{}) (pick.Answer, bool) {
	f.got <- r
	if f.wait {
		<-done
		return pick.Answer{}, false
	}
	return f.answer, f.ok
}

type fakeApps struct {
	updated chan []string
}

func (f *fakeApps) ChooseApp(r AppRequest, updates <-chan []string, done <-chan struct{}) (string, bool) {
	select {
	case u := <-updates:
		f.updated <- u
		return u[len(u)-1], true
	case <-done:
		return "", false
	}
}

func serve(t *testing.T, files Picker, apps AppPicker) (dbus.BusObject, *dbus.Conn) {
	addr := dbustest.Start(t)
	conn := dbustest.Conn(t, addr)
	s := New(conn, files, apps)
	if _, err := portal.Export(conn, s.Interfaces()...); err != nil {
		t.Fatal(err)
	}
	if err := portal.Own(conn, portal.FilesName); err != nil {
		t.Fatal(err)
	}
	client := dbustest.Conn(t, addr)
	return client.Object(portal.FilesName, portal.Path), client
}

const handle = dbus.ObjectPath("/org/freedesktop/portal/desktop/request/1_2/r")

func TestOpenFile(t *testing.T) {
	f := &fakePicker{got: make(chan pick.Request, 1), ok: true,
		answer: pick.Answer{Paths: []string{"/home/a/b c.txt"}, Filter: 0, Choices: map[string]string{"enc": "latin1"}}}
	obj, _ := serve(t, f, nil)
	opts := map[string]dbus.Variant{
		"multiple": dbus.MakeVariant(true),
		"filters": dbus.MakeVariant([]struct {
			Name     string
			Patterns []struct {
				Kind  uint32
				Value string
			}
		}{{"Text", []struct {
			Kind  uint32
			Value string
		}{{0, "*.txt"}}}}),
		"choices": dbus.MakeVariant([]struct {
			ID, Label string
			Options   []struct{ ID, Label string }
			Default   string
		}{{"enc", "Encoding", []struct{ ID, Label string }{{"utf8", "UTF-8"}, {"latin1", "Latin-1"}}, "utf8"}}),
	}
	var resp uint32
	var results map[string]dbus.Variant
	if err := obj.Call(portal.Prefix+"FileChooser.OpenFile", 0, handle, "org.example.App", "", "Pick one", opts).Store(&resp, &results); err != nil {
		t.Fatal(err)
	}
	r := <-f.got
	if r.Kind != pick.Open || r.AppID != "org.example.App" || r.Title != "Pick one" || !r.Multiple {
		t.Errorf("request %+v", r)
	}
	if resp != portal.Success {
		t.Fatalf("response %d", resp)
	}
	if uris, _ := results["uris"].Value().([]string); !slices.Equal(uris, []string{"file:///home/a/b%20c.txt"}) {
		t.Errorf("uris %v", results["uris"])
	}
	if c, _ := results["choices"].Value().([][]any); len(c) != 1 || c[0][1] != "latin1" {
		t.Errorf("choices %v", results["choices"])
	}
	if cf, _ := results["current_filter"].Value().([]any); len(cf) != 2 || cf[0] != "Text" {
		t.Errorf("current_filter %v", results["current_filter"])
	}
}

func TestSaveCancelled(t *testing.T) {
	f := &fakePicker{got: make(chan pick.Request, 1)}
	obj, _ := serve(t, f, nil)
	var resp uint32
	var results map[string]dbus.Variant
	opts := map[string]dbus.Variant{"current_name": dbus.MakeVariant("a.txt")}
	if err := obj.Call(portal.Prefix+"FileChooser.SaveFile", 0, handle, "", "", "", opts).Store(&resp, &results); err != nil {
		t.Fatal(err)
	}
	if r := <-f.got; r.Kind != pick.Save || r.CurrentName != "a.txt" {
		t.Errorf("request %+v", r)
	}
	if resp != portal.Cancelled {
		t.Errorf("response %d, want cancelled", resp)
	}
}

func TestCloseWhileOpen(t *testing.T) {
	f := &fakePicker{got: make(chan pick.Request, 1), wait: true}
	obj, client := serve(t, f, nil)
	call := obj.Go(portal.Prefix+"FileChooser.OpenFile", 0, nil, handle, "", "", "", map[string]dbus.Variant{})
	<-f.got
	if err := client.Object(portal.FilesName, handle).Call(portal.Prefix+"Request.Close", 0).Err; err != nil {
		t.Fatal(err)
	}
	select {
	case done := <-call.Done:
		var resp uint32
		var results map[string]dbus.Variant
		if err := done.Store(&resp, &results); err != nil || resp != portal.Ended {
			t.Errorf("response %d %v, want ended", resp, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no answer after Close")
	}
}

func TestChooseApplication(t *testing.T) {
	apps := &fakeApps{updated: make(chan []string, 1)}
	obj, _ := serve(t, nil, apps)
	call := obj.Go(portal.Prefix+"AppChooser.ChooseApplication", 0, nil, handle, "org.example.App", "",
		[]string{"org.gnome.TextEditor"}, map[string]dbus.Variant{"content_type": dbus.MakeVariant("text/plain")})
	time.Sleep(100 * time.Millisecond) // the request is open
	if err := obj.Call(portal.Prefix+"AppChooser.UpdateChoices", 0, handle, []string{"org.gnome.TextEditor", "dev.zed.Zed"}).Err; err != nil {
		t.Fatal(err)
	}
	if u := <-apps.updated; len(u) != 2 {
		t.Errorf("update %v", u)
	}
	select {
	case done := <-call.Done:
		var resp uint32
		var results map[string]dbus.Variant
		if err := done.Store(&resp, &results); err != nil || resp != portal.Success || results["choice"].Value() != "dev.zed.Zed" {
			t.Errorf("answer %d %v %v", resp, results, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no answer")
	}
}
