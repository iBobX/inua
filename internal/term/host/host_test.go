package host

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/term/vt"
)

func init() {
	// The engine scripts/libghostty.sh built, for titles and folders.
	if os.Getenv("INUA_VT_LIB") == "" {
		if p, err := filepath.Abs(filepath.Join("..", "..", "..", "bin", vt.Library)); err == nil {
			os.Setenv("INUA_VT_LIB", p)
		}
	}
}

func startHost(t *testing.T) (*Host, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "inua", "term.sock")
	h, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	go h.Serve()
	t.Cleanup(h.Close)
	return h, path
}

// watcher collects a client's output and events.
type watcher struct {
	mu     sync.Mutex
	out    map[int]*strings.Builder
	events []Message
}

func newWatcher() *watcher { return &watcher{out: map[int]*strings.Builder{}} }

func (w *watcher) output(pane int, b []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.out[pane] == nil {
		w.out[pane] = &strings.Builder{}
	}
	w.out[pane].Write(b)
}

func (w *watcher) event(m Message) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = append(w.events, m)
}

func (w *watcher) text(pane int) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.out[pane] == nil {
		return ""
	}
	return w.out[pane].String()
}

func (w *watcher) waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func dial(t *testing.T, path string, w *watcher) *Client {
	t.Helper()
	c, err := Dial(path, w.output, w.event)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestPane(t *testing.T) {
	_, path := startHost(t)
	w := newWatcher()
	c := dial(t, path, w)
	dir := t.TempDir()
	id, err := c.Create([]string{"sh", "-c", "echo hello; stty size; read x; echo got $x; read y"}, dir, nil, 90, 20)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Attach(id); err != nil {
		t.Fatal(err)
	}
	w.waitFor(t, "hello", func() bool { return strings.Contains(w.text(id), "20 90") })
	if err := c.Input(id, []byte("there\n")); err != nil {
		t.Fatal(err)
	}
	w.waitFor(t, "the echo of input", func() bool { return strings.Contains(w.text(id), "got there") })

	panes, err := c.List()
	if err != nil || len(panes) != 1 || panes[0].ID != id || panes[0].Dir != dir || panes[0].Cols != 90 || panes[0].Exited {
		t.Fatalf("list %+v, %v", panes, err)
	}
	if err := c.Resize(id, 100, 30); err != nil {
		t.Fatal(err)
	}
	if panes, _ := c.List(); panes[0].Cols != 100 || panes[0].Rows != 30 {
		t.Errorf("after resize %+v", panes[0])
	}
}

// A client attaching later gets what came before; two clients both get
// what comes after.
func TestReplayAndTwoClients(t *testing.T) {
	_, path := startHost(t)
	w1 := newWatcher()
	c1 := dial(t, path, w1)
	id, err := c1.Create([]string{"sh", "-c", "echo before; read x; echo after $x; read y"}, "", nil, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	c1.Attach(id)
	w1.waitFor(t, "before", func() bool { return strings.Contains(w1.text(id), "before") })
	c1.Close() // the window goes; the pane stays

	w2 := newWatcher()
	c2 := dial(t, path, w2)
	if err := c2.Attach(id); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w2.text(id), "before") {
		t.Errorf("the replay was not there when Attach returned: %q", w2.text(id))
	}
	w3 := newWatcher()
	c3 := dial(t, path, w3)
	c3.Attach(id)
	c2.Input(id, []byte("both\n"))
	for _, w := range []*watcher{w2, w3} {
		w.waitFor(t, "after", func() bool { return strings.Contains(w.text(id), "after both") })
	}
	if n := strings.Count(w2.text(id), "before"); n != 1 {
		t.Errorf("the replay came %d times", n)
	}
}

// A program that ends leaves its pane, with its code, until closed; the
// host ends with its last pane.
func TestExitAndClose(t *testing.T) {
	h, path := startHost(t)
	h.ExitWhenEmpty = true
	w := newWatcher()
	c := dial(t, path, w)
	id, _ := c.Create([]string{"sh", "-c", "exit 7"}, "", nil, 80, 24)
	c.Attach(id)
	w.waitFor(t, "the exit", func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		for _, m := range w.events {
			if m.Pane == id && m.Code != nil && *m.Code == 7 {
				return true
			}
		}
		return false
	})
	if panes, _ := c.List(); len(panes) != 1 || !panes[0].Exited || panes[0].Code != 7 {
		t.Errorf("after exit %+v", panes)
	}
	if err := c.ClosePane(id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.done:
	case <-time.After(3 * time.Second):
		t.Fatal("the host stays after its last pane")
	}
}

// The title and folder the shell sends come as events.
func TestTitleAndFolder(t *testing.T) {
	if err := vt.Load(); err != nil {
		t.Skip(err)
	}
	_, path := startHost(t)
	w := newWatcher()
	c := dial(t, path, w)
	id, _ := c.Create([]string{"sh", "-c", `printf '\033]2;✳ Refactor\007\033]7;file://host/tmp/work\033\\'; read x`}, "", nil, 80, 24)
	c.Attach(id)
	w.waitFor(t, "the title and folder", func() bool {
		panes, _ := c.List()
		return len(panes) == 1 && panes[0].Title == "✳ Refactor" && panes[0].Dir == "/tmp/work"
	})
}

func TestVersion(t *testing.T) {
	_, path := startHost(t)
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	writeMessage(conn, Message{Type: MsgHello, Version: Version + 1})
	_, payload, err := readFrame(conn)
	var m Message
	if err != nil || json.Unmarshal(payload, &m) != nil || m.Type == MsgWelcome || m.Error == "" {
		t.Errorf("another version was welcomed: %+v, %v", m, err)
	}
}

// A second host on the same socket is refused; a stale socket is not in
// the way.
func TestListen(t *testing.T) {
	_, path := startHost(t)
	if _, err := Listen(path); err == nil {
		t.Error("a second host on a live socket")
	}
	stale := filepath.Join(t.TempDir(), "term.sock")
	os.WriteFile(stale, nil, 0o600)
	h, err := Listen(stale)
	if err != nil {
		t.Fatal(err)
	}
	h.Close()
	if fi, err := os.Stat(filepath.Dir(path)); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("socket folder %v %v", fi.Mode(), err)
	}
}
