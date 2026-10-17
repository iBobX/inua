package fm1

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/dbustest"
)

type recorder struct {
	mu   sync.Mutex
	got  []string
	done chan struct{}
}

func (r *recorder) add(kind string, paths []string) {
	r.mu.Lock()
	for _, p := range paths {
		r.got = append(r.got, kind+" "+p)
	}
	r.mu.Unlock()
	r.done <- struct{}{}
}

func (r *recorder) ShowFolders(p []string)        { r.add("folder", p) }
func (r *recorder) ShowItems(p []string)          { r.add("item", p) }
func (r *recorder) ShowItemProperties(p []string) { r.add("props", p) }

func TestClaimAndHand(t *testing.T) {
	addr := dbustest.Start(t)
	first := &recorder{done: make(chan struct{}, 4)}
	ok, err := Claim(dbustest.Conn(t, addr), first)
	if err != nil || !ok {
		t.Fatalf("first claim: %v %v", ok, err)
	}
	second := dbustest.Conn(t, addr)
	if ok, err := Claim(second, &recorder{}); err != nil || ok {
		t.Fatalf("second claim should lose: %v %v", ok, err)
	}

	dir := t.TempDir()
	file := filepath.Join(dir, "a b.txt")
	os.WriteFile(file, nil, 0o644)
	if err := Hand(second, []string{dir, file, "sftp://box/srv"}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case <-first.done:
		case <-time.After(2 * time.Second):
			t.Fatal("not handed over")
		}
	}
	first.mu.Lock()
	defer first.mu.Unlock()
	want := []string{"folder " + dir, "folder sftp://box/srv", "item " + file}
	if !slices.Equal(first.got, want) {
		t.Fatalf("got %v, want %v", first.got, want)
	}
}

func TestStandardNameQueued(t *testing.T) {
	addr := dbustest.Start(t)
	other := dbustest.Conn(t, addr)
	other.RequestName(Standard, 0) // another file manager has it
	conn := dbustest.Conn(t, addr)
	if ok, err := Claim(conn, &recorder{done: make(chan struct{}, 1)}); !ok || err != nil {
		t.Fatalf("claim: %v %v", ok, err)
	}
	var owner string
	conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, Standard).Store(&owner)
	if owner != other.Names()[0] {
		t.Fatalf("the other file manager keeps the name; owner %s", owner)
	}
	other.ReleaseName(Standard)
	time.Sleep(100 * time.Millisecond)
	conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, Standard).Store(&owner)
	if owner != conn.Names()[0] {
		t.Fatalf("taken over when it left; owner %s", owner)
	}
}

// TestSplitResolvesRelativePaths: `open .` and `open Downloads` give
// inua-files paths relative to where they were typed, which are made
// absolute before a running instance, elsewhere, is handed them.
func TestSplitResolvesRelativePaths(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "Downloads"), 0o755)
	os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0o644)
	t.Chdir(dir)
	folders, items := Split([]string{".", "Downloads", "notes.txt", "sftp://box/srv"})
	if want := []string{dir, filepath.Join(dir, "Downloads"), "sftp://box/srv"}; !slices.Equal(folders, want) {
		t.Errorf("folders = %q, want %q", folders, want)
	}
	if want := []string{filepath.Join(dir, "notes.txt")}; !slices.Equal(items, want) {
		t.Errorf("items = %q, want %q", items, want)
	}
}
