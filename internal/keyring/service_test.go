package keyring

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/dbustest"
)

// serve runs the service with a fake pass on a private bus.
func serve(t *testing.T) (addr string, s *Store, back *memory) {
	t.Helper()
	addr = dbustest.Start(t)
	s, back, _ = newStore(t)
	svc := NewService(dbustest.Conn(t, addr), s)
	done := make(chan error, 1)
	go func() { done <- svc.Serve(t.Context()) }()
	probe := dbustest.Conn(t, addr)
	for range 100 {
		var has bool
		if probe.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, OwnName).Store(&has) == nil && has {
			return addr, s, back
		}
		select {
		case err := <-done:
			t.Fatal(err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatal("the service did not start")
	return
}

func TestClient(t *testing.T) {
	addr, s, back := serve(t)
	for _, alg := range []string{AlgPlain, AlgDH} {
		t.Run(alg, func(t *testing.T) {
			c, err := Connect(dbustest.Conn(t, addr), alg)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if !c.Ours() {
				t.Error("not ours")
			}
			attrs := map[string]string{"xdg:schema": "test", "alg": alg}
			if err := c.Store(t.Context(), "Test "+alg, attrs, []byte("secret of "+alg)); err != nil {
				t.Fatal(err)
			}
			// Again: replaced, not added.
			if err := c.Store(t.Context(), "Test "+alg, attrs, []byte("new secret of "+alg)); err != nil {
				t.Fatal(err)
			}
			got, ok, err := c.Lookup(t.Context(), attrs)
			if err != nil || !ok || string(got) != "new secret of "+alg {
				t.Errorf("lookup %q %v %v", got, ok, err)
			}
			if items := s.Search(Login, attrs); len(items) != 1 {
				t.Errorf("stored %d items", len(items))
			}
			cols, err := c.Collections()
			if err != nil || len(cols) != 2 || !cols[0].Default || cols[0].Label != "Login" {
				t.Errorf("collections %+v %v", cols, err)
			}
			items, err := c.Items(t.Context(), cols[0].Path)
			if err != nil || len(items) == 0 {
				t.Fatalf("items %+v %v", items, err)
			}
			if err := c.Delete(attrs); err != nil {
				t.Fatal(err)
			}
			if _, ok, _ := c.Lookup(t.Context(), attrs); ok {
				t.Error("still there")
			}
		})
	}
	if e := back.entries(); len(e) != 0 {
		t.Errorf("entries left: %v", e)
	}
}

// TestLibsecret checks the service with libsecret itself, through the
// system Python's GObject bindings, where they are installed.
func TestLibsecret(t *testing.T) {
	py := "/usr/bin/python3"
	if exec.Command(py, "-c", "import gi; gi.require_version('Secret', '1')").Run() != nil {
		t.Skip("no libsecret for Python")
	}
	addr, s, back := serve(t)
	script := filepath.Join(t.TempDir(), "secret.py")
	os.WriteFile(script, []byte(`
import sys, gi
gi.require_version('Secret', '1')
from gi.repository import Secret
schema = Secret.Schema.new("io.github.lyricat.Inua.Test", Secret.SchemaFlags.NONE, {"profile": Secret.SchemaAttributeType.STRING})
op = sys.argv[1]
if op == "store":
    print(Secret.password_store_sync(schema, {"profile": "work"}, Secret.COLLECTION_DEFAULT, "inua test: work", "sk-λ-123", None))
elif op == "lookup":
    print(Secret.password_lookup_sync(schema, {"profile": "work"}, None))
elif op == "search":
    service = Secret.Service.get_sync(Secret.ServiceFlags.OPEN_SESSION | Secret.ServiceFlags.LOAD_COLLECTIONS, None)
    for c in service.get_collections():
        print("collection", c.get_label())
    items = service.search_sync(schema, {"profile": "work"}, Secret.SearchFlags.LOAD_SECRETS | Secret.SearchFlags.UNLOCK, None)
    for i in items:
        print("item", i.get_label(), i.get_secret().get_text())
elif op == "clear":
    print(Secret.password_clear_sync(schema, {"profile": "work"}, None))
`), 0o600)
	run := func(op string) string {
		cmd := exec.Command(py, "-I", script, op)
		cmd.Env = append(os.Environ(), "DBUS_SESSION_BUS_ADDRESS="+addr)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", op, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if out := run("store"); out != "True" {
		t.Fatalf("store: %s", out)
	}
	if items := s.Search(Login, map[string]string{"profile": "work", "xdg:schema": "io.github.lyricat.Inua.Test"}); len(items) != 1 {
		t.Errorf("stored %+v", items)
	}
	if e := back.entries(); e["login/inua-test-work"] != "sk-λ-123" {
		t.Errorf("entry %v", e)
	}
	if out := run("lookup"); out != "sk-λ-123" {
		t.Errorf("lookup: %s", out)
	}
	if out := run("search"); !strings.Contains(out, "collection Login") || !strings.Contains(out, "item inua test: work sk-λ-123") {
		t.Errorf("search: %s", out)
	}
	if out := run("clear"); out != "True" {
		t.Errorf("clear: %s", out)
	}
	if out := run("lookup"); out != "None" {
		t.Errorf("after clear: %s", out)
	}
}

func TestNameTaken(t *testing.T) {
	addr := dbustest.Start(t)
	other := dbustest.Conn(t, addr)
	other.RequestName(Name, 0)
	s, _, _ := newStore(t)
	err := NewService(dbustest.Conn(t, addr), s).Serve(t.Context())
	if err == nil || !strings.Contains(err.Error(), "already served") {
		t.Errorf("%v", err)
	}
}

// TestImport copies from another Secret Service (standing in for
// gnome-keyring) into the store.
func TestImport(t *testing.T) {
	addr := dbustest.Start(t)
	src, _, _ := newStore(t)
	other := NewService(dbustest.Conn(t, addr), src)
	other.foreign = true
	go other.Serve(t.Context())
	src.CreateItem(Login, "Chrome Safe Storage", map[string]string{"application": "chrome"}, []byte("k1"), "text/plain", false)
	work, _ := src.CreateCollection("Work", "")
	src.CreateItem(work.ID, "VPN", map[string]string{"vpn": "corp"}, []byte{0, 1, 2}, "application/octet-stream", false)
	src.CreateItem(Session, "temp", nil, []byte("gone"), "", false)

	var c *Client
	var err error
	for range 100 { // until the service is up
		if c, err = Connect(dbustest.Conn(t, addr), AlgDH); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	dst, back, _ := newStore(t)
	rep, err := Import(t.Context(), c, dst)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Added != 2 || string(back.m["login/chrome-safe-storage"]) != "k1" || len(back.m) != 2 {
		t.Errorf("%+v %v", rep, back.m)
	}
	vpn := dst.Search("", map[string]string{"vpn": "corp"})
	if len(vpn) != 1 || vpn[0].Collection != "work" {
		t.Fatalf("vpn %+v", vpn)
	}
	if got, ct, _ := dst.Secret(vpn[0].Collection, vpn[0].ID); string(got) != "\x00\x01\x02" || ct != "application/octet-stream" {
		t.Errorf("vpn secret %q %q", got, ct)
	}
	// Again: nothing new; a changed secret is updated.
	items, _ := src.Items(Login)
	src.SetSecret(Login, items[0].ID, []byte("k2"), "text/plain")
	rep, err = Import(t.Context(), c, dst)
	if err != nil || rep.Added != 0 || rep.Same != 1 || rep.Updated != 1 || string(back.m["login/chrome-safe-storage"]) != "k2" {
		t.Errorf("again: %+v %v %v", rep, err, back.m)
	}
}
