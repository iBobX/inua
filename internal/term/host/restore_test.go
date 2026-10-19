package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/term/layout"
	"github.com/lyricat/inua/internal/term/model"
)

// savedFile writes a kept state of two workspaces: home's, one pane, and
// the drive's, a session of two panes in dir.
func savedFile(t *testing.T, home, dir string) string {
	t.Helper()
	s := Saved{Version: 1, Model: model.Model{Next: 4, Workspaces: []*model.Workspace{
		{ID: 1, Name: "home", Dir: home, Sessions: []*model.Session{{ID: 2, Name: "home", Tree: layout.Leaf(1), Focus: 1, Used: true}}},
		{ID: 3, Name: "drive", Dir: dir, Sessions: []*model.Session{{ID: 4, Name: "build", Tree: layout.SplitPane(layout.Leaf(2), 2, 3, layout.Right), Focus: 3, Used: true}}},
	}}, Panes: map[int]SavedPane{1: {Dir: home}, 2: {Dir: dir, Command: "make"}, 3: {Dir: filepath.Join(dir, "sub")}}}
	file := filepath.Join(t.TempDir(), "term.json")
	b, _ := json.Marshal(&s)
	os.WriteFile(file, b, 0o600)
	return file
}

// A session on a drive not mounted yet waits for it, kept in the file,
// and comes back once its folder is there.
func TestRestoreWaits(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	defer func(d time.Duration) { waitCheck = d }(waitCheck)
	waitCheck = 50 * time.Millisecond
	home := t.TempDir()
	drive := filepath.Join(t.TempDir(), "mnt", "project")
	file := savedFile(t, home, drive)
	h, path := startHost(t)
	h.ExitWhenEmpty = false
	if err := h.Remember(file, nil, true); err != nil {
		t.Fatal(err)
	}
	c := dial(t, path, newWatcher())
	st, _ := c.State()
	if len(st.Model.Workspaces) != 1 || st.Model.Workspaces[0].Name != "home" || len(st.Panes) != 1 {
		t.Fatalf("before the drive: %+v, %d panes", st.Model.Workspaces, len(st.Panes))
	}
	h.Save()
	b, _ := os.ReadFile(file)
	var kept Saved
	json.Unmarshal(b, &kept)
	if w := kept.Model.Workspace(3); w == nil || len(w.Sessions) != 1 || len(w.Sessions[0].Tree.Panes()) != 2 {
		t.Fatalf("the waiting session not kept: %s", b)
	} else if p := kept.Panes[w.Sessions[0].Tree.Panes()[0]]; p.Command != "make" {
		t.Errorf("its pane kept as %+v", p)
	}
	os.MkdirAll(drive, 0o755)
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, _ = c.State()
		if w := st.Model.Workspace(3); w != nil && len(w.Sessions) == 1 {
			s := w.Sessions[0]
			if panes := s.Tree.Panes(); len(panes) != 2 || panes[0] <= 0 || !s.Tree.Has(s.Focus) || len(st.Panes) != 3 {
				t.Errorf("back as %+v, %d panes", s, len(st.Panes))
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("not back once the drive is there")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A pane closes with its program: the layout closes up, a session
// without panes goes, and the host ends with its last pane.
func TestExitedPaneCloses(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	h, path := startHost(t)
	h.ExitWhenEmpty = true
	c := dial(t, path, newWatcher())
	c.Open(t.TempDir(), "")
	st, _ := c.State()
	s := st.Model.Workspaces[0].Sessions[0]
	first := s.Focus
	second, _ := c.Split(s.ID, "right")
	c.Input(second, []byte("exit\n"))
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, _ = c.State()
		if len(st.Model.Workspaces) == 1 && len(st.Panes) == 1 {
			if got := st.Model.Workspaces[0].Sessions[0]; len(got.Tree.Panes()) != 1 || got.Focus != first {
				t.Errorf("the session after %+v", got)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the exited pane stays: %+v", st.Panes)
		}
		time.Sleep(20 * time.Millisecond)
	}
	c.Input(first, []byte("exit\n"))
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the host stays after its last pane exited")
	}
}

// A session one of whose panes cannot start is dropped whole: the panes
// already started for it are closed, not left running unseen.
func TestRestoreFailsWhole(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root enters any folder")
	}
	t.Setenv("SHELL", "/bin/sh")
	home := t.TempDir()
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	os.MkdirAll(sub, 0o755)
	os.Chmod(sub, 0) // there, but not to be entered
	t.Cleanup(func() { os.Chmod(sub, 0o755) })
	file := savedFile(t, home, dir)
	h, path := startHost(t)
	h.ExitWhenEmpty = false
	if err := h.Remember(file, nil, true); err != nil {
		t.Fatal(err)
	}
	c := dial(t, path, newWatcher())
	st, _ := c.State()
	if w := st.Model.Workspace(3); w != nil && len(w.Sessions) > 0 {
		t.Errorf("the failed session shows: %+v", w.Sessions)
	}
	if len(st.Panes) != 1 || len(h.allPanes()) != 1 {
		t.Errorf("panes left: %+v", st.Panes)
	}
}
