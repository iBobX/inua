package keyring

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbustest"
	"github.com/lyricat/inua/internal/portal"
)

// retrieve calls RetrieveSecret from conn and returns what was written.
func retrieve(t *testing.T, conn *dbus.Conn, appID string) ([]byte, uint32, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var resp uint32
	var results map[string]dbus.Variant
	call := conn.Object(portal.KeyringName, portal.Path).Call(portal.Prefix+"Secret.RetrieveSecret", 0,
		dbus.ObjectPath("/org/freedesktop/portal/desktop/request/1_1/t"), appID, dbus.UnixFD(w.Fd()), map[string]dbus.Variant{})
	w.Close()
	if call.Err != nil {
		return nil, 0, call.Err
	}
	if err := call.Store(&resp, &results); err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	return got, resp, nil
}

func TestPortalSecret(t *testing.T) {
	addr, s, _ := serve(t)
	frontend := dbustest.Conn(t, addr)
	if _, err := frontend.RequestName(frontendName, 0); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		var has bool
		if frontend.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, portal.KeyringName).Store(&has); has {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	first, resp, err := retrieve(t, frontend, "org.example.App")
	if err != nil || resp != portal.Success || len(first) != portalSize {
		t.Fatalf("first: %d bytes, %d, %v", len(first), resp, err)
	}
	again, _, _ := retrieve(t, frontend, "org.example.App")
	if !bytes.Equal(first, again) {
		t.Error("a second call made another secret")
	}
	other, _, _ := retrieve(t, frontend, "org.example.Other")
	if len(other) != portalSize || bytes.Equal(first, other) {
		t.Error("two apps share a secret")
	}
	items := s.Search(Login, map[string]string{"xdg:schema": portalSchema, "app_id": "org.example.App"})
	if len(items) != 1 {
		t.Fatalf("%d items for the app", len(items))
	}
	// As gnome-keyring's are kept, so imported ones are found.
	if secret, _, err := s.Secret(Login, items[0].ID, ""); err != nil || !bytes.Equal(secret, first) {
		t.Errorf("stored secret differs: %v", err)
	}

	if got, resp, _ := retrieve(t, frontend, ""); resp != portal.Ended || len(got) != 0 {
		t.Errorf("no app ID: %d, %d bytes", resp, len(got))
	}
	// Another process naming an app is refused.
	if got, _, err := retrieve(t, dbustest.Conn(t, addr), "org.example.App"); err == nil || len(got) != 0 {
		t.Errorf("answered a caller other than the frontend: %d bytes, %v", len(got), err)
	}
}
