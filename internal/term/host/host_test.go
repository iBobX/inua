package host

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/term/layout"
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
	if err := c.Resize(id, 100, 30, 0, 0); err != nil {
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

// The host answers a program's queries, with no window attached: device
// attributes, and the window's colours once it said them.
func TestAnswers(t *testing.T) {
	if err := vt.Load(); err != nil {
		t.Skip(err)
	}
	_, path := startHost(t)
	w := newWatcher()
	c := dial(t, path, w)
	if err := c.SetColors(Colors{FG: 0xe0e0e0, BG: 0x0b1012, Cursor: 0x7fb4ca}); err != nil {
		t.Fatal(err)
	}
	// The answers come as input; od shows them as text.
	script := `stty raw -echo; printf '\033[c'; head -c 12 | od -An -c; printf '\033]11;?\033\\'; head -c 24 | od -An -c; stty sane`
	id, err := c.Create([]string{"sh", "-c", script}, "", nil, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	c.Attach(id)
	clean := func() string { return strings.Join(strings.Fields(w.text(id)), "") }
	w.waitFor(t, "the answers", func() bool { return strings.Contains(clean(), "rgb:0b0b/1010/1212") })
	if got := clean(); !strings.Contains(got, `033[?62;22;52c`) {
		t.Errorf("device attributes not answered: %q", got)
	}
}

// A program's clipboard write (OSC 52) reaches the attached clients.
func TestClipboardEvent(t *testing.T) {
	if err := vt.Load(); err != nil {
		t.Skip(err)
	}
	_, path := startHost(t)
	got := make(chan Message, 4)
	c, err := Dial(path, func(int, []byte) {}, func(m Message) {
		if m.Clipboard != nil {
			got <- m
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	id, err := c.Create([]string{"sh", "-c", `read x; printf '\033]52;c;aGVsbG8=\a'; read y`}, "", nil, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Attach(id); err != nil {
		t.Fatal(err)
	}
	c.Input(id, []byte("\n")) // attached: now the write
	select {
	case m := <-got:
		if *m.Clipboard != "hello" || m.Primary || m.Pane != id {
			t.Errorf("event %+v (%q)", m, *m.Clipboard)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no clipboard event")
	}
}

// Workspaces and sessions: opening a folder makes one with a shell,
// splits start beside the focused pane, every client hears of changes,
// and closing the panes takes them away.
func TestSessions(t *testing.T) {
	h, path := startHost(t)
	h.ExitWhenEmpty = false
	states := make(chan *State, 32)
	c, err := Dial(path, nil, func(m Message) {
		if m.Type == MsgState {
			states <- m.State
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	ws, err := c.Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.State()
	if err != nil {
		t.Fatal(err)
	}
	m := st.Model
	if len(m.Workspaces) != 1 || m.Workspaces[0].ID != ws || len(m.Workspaces[0].Sessions) != 1 || m.Shown.Workspace != ws {
		t.Fatalf("after open: %+v", m)
	}
	sess := m.Workspaces[0].Sessions[0]
	first := sess.Focus
	if again, _ := c.Open(dir, ""); again != ws {
		t.Errorf("opening the folder again: %d, want %d", again, ws)
	}
	second, err := c.Split(sess.ID, "right")
	if err != nil {
		t.Fatal(err)
	}
	third, err := c.Split(sess.ID, "down")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Layout(sess.ID, OpFocus, Message{Direction: int(layout.Left)}); err != nil {
		t.Fatal(err)
	}
	st, _ = c.State()
	s := st.Model.Workspaces[0].Sessions[0]
	if got := s.Tree.Panes(); !slices.Equal(got, []int{first, second, third}) || s.Focus != first {
		t.Errorf("panes %v, focus %d", got, s.Focus)
	}
	if len(st.Panes) != 3 {
		t.Errorf("%d panes listed", len(st.Panes))
	}
	if err := c.Layout(sess.ID, OpPreset, Message{Name: "rows"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Layout(sess.ID, OpZoom, Message{}); err != nil {
		t.Fatal(err)
	}
	c.Rename(0, sess.ID, "review")
	select {
	case <-states:
	case <-time.After(5 * time.Second):
		t.Fatal("no state event")
	}
	st, _ = c.State()
	s = st.Model.Workspaces[0].Sessions[0]
	if s.Zoom != first || s.Name != "review" || s.Tree.Split != layout.Down {
		t.Errorf("zoom %d, name %q, split %q", s.Zoom, s.Name, s.Tree.Split)
	}
	// Typing marks the session used.
	c.Input(second, []byte("\n"))
	deadline := time.Now().Add(5 * time.Second)
	for st, _ = c.State(); !st.Model.Workspaces[0].Sessions[0].Used; st, _ = c.State() {
		if time.Now().After(deadline) {
			t.Fatal("not marked used")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := c.CloseSession(sess.ID); err != nil {
		t.Fatal(err)
	}
	st, _ = c.State()
	if w := st.Model.Workspaces; len(w) != 1 || len(w[0].Sessions) != 0 || len(st.Panes) != 0 {
		t.Errorf("after closing: %+v, %d panes", w, len(st.Panes))
	}
}
