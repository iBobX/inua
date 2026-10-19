package model

import (
	"slices"
	"testing"

	"github.com/lyricat/inua/internal/term/layout"
)

func TestWorkspacesAndSessions(t *testing.T) {
	var m Model
	w := m.AddWorkspace("/src/inua/", "", true)
	if w.Name != "inua" {
		t.Errorf("name %q", w.Name)
	}
	s1 := m.AddSession(w, "", 1)
	s2 := m.AddSession(w, "", 2)
	if s1.Name != "inua" || s2.Name != "Session 2" {
		t.Errorf("names %q %q", s1.Name, s2.Name)
	}
	other := m.AddWorkspace("/src/dotfiles", "dots", false)
	m.AddSession(other, "", 3)
	if m.Workspaces[0] != other {
		t.Error("a new workspace goes first")
	}
	if m.ByDir("/src/inua") != w {
		t.Error("by folder")
	}
	if err := m.Show(w.ID, s2.ID); err != nil || m.Shown != (Shown{w.ID, s2.ID}) {
		t.Errorf("show: %v %+v", err, m.Shown)
	}
	m.Show(other.ID, 0)
	m.Show(w.ID, 0) // back: the session it last showed
	if m.Shown.Session != s2.ID {
		t.Errorf("last session %+v", m.Shown)
	}
	if err := m.Show(other.ID, s1.ID); err == nil {
		t.Error("a session of another workspace")
	}
	if got := m.Panes(); !slices.Equal(got, []int{3, 1, 2}) {
		t.Errorf("panes %v", got)
	}
}

func TestSplitAndRemove(t *testing.T) {
	var m Model
	w := m.AddWorkspace("/w", "", false)
	s := m.AddSession(w, "", 1)
	m.Show(w.ID, s.ID)
	m.Split(s.ID, 2, layout.Right)
	m.Split(s.ID, 3, layout.Down) // beside 2, the focused one
	if s.Focus != 3 || !slices.Equal(s.Tree.Panes(), []int{1, 2, 3}) {
		t.Fatalf("focus %d, panes %v", s.Focus, s.Tree.Panes())
	}
	// Closing the focused pane focuses a neighbour.
	if gone, _ := m.RemovePane(3); gone || s.Focus != 2 {
		t.Errorf("after closing 3: gone %v, focus %d", gone, s.Focus)
	}
	m.RemovePane(1)
	// The last pane takes the session, and the workspace the user did
	// not add.
	sGone, wGone := m.RemovePane(2)
	if !sGone || !wGone || len(m.Workspaces) != 0 || m.Shown != (Shown{}) {
		t.Errorf("gone %v %v, %d workspaces, shown %+v", sGone, wGone, len(m.Workspaces), m.Shown)
	}
}

func TestRemoveShown(t *testing.T) {
	var m Model
	w := m.AddWorkspace("/w", "", true)
	a := m.AddSession(w, "", 1)
	b := m.AddSession(w, "", 2)
	m.Show(w.ID, a.ID)
	if gone, err := m.RemoveSession(a.ID); err != nil || gone {
		t.Fatalf("remove: %v %v", gone, err)
	}
	if m.Shown.Session != b.ID {
		t.Errorf("shown after removing a %+v", m.Shown)
	}
	m.RemoveSession(b.ID)
	if len(m.Workspaces) != 1 || m.Shown != (Shown{w.ID, 0}) {
		t.Errorf("an added workspace stays: %d, %+v", len(m.Workspaces), m.Shown)
	}
}

func TestMove(t *testing.T) {
	var m Model
	a := m.AddWorkspace("/a", "", true)
	b := m.AddWorkspace("/b", "", true)
	c := m.AddWorkspace("/c", "", true) // c b a
	m.Move(c.ID, 2)
	if m.Workspaces[0] != b || m.Workspaces[1] != a || m.Workspaces[2] != c {
		t.Errorf("order %s %s %s", m.Workspaces[0].Name, m.Workspaces[1].Name, m.Workspaces[2].Name)
	}
}
