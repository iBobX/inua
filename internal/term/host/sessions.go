package host

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/lyricat/inua/internal/term/layout"
	"github.com/lyricat/inua/internal/term/model"
	"github.com/lyricat/inua/internal/term/shellint"
)

// The host keeps the workspaces and sessions (internal/term/model) under
// h.mu, beside the panes; every change sends each client the whole State,
// which is small. A pane's title and folder go to every client too, for
// the columns, as events.

// ShellEnv is what a pane's shell gets on top of the host's environment.
var ShellEnv = []string{"TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=inua-term"}

// shell is the command a new pane runs: the user's shell.
func shell() []string { return []string{cmp.Or(os.Getenv("SHELL"), "/bin/sh")} }

// shellEnv is the host's environment for a pane's shell.
func shellEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "INUA_TERM_") {
			env = append(env, e)
		}
	}
	return append(env, ShellEnv...)
}

// tell sends every client a message.
func (h *Host) tell(m Message) {
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		c.send(frameControl, b)
	}
}

// state is the State now.
func (h *Host) state() *State {
	h.mu.Lock()
	// A copy through JSON: the model goes on changing under h.mu.
	b, _ := json.Marshal(&h.m)
	h.mu.Unlock()
	s := &State{Panes: h.list()}
	json.Unmarshal(b, &s.Model)
	return s
}

// publish sends every client the State, and has it kept.
func (h *Host) publish() {
	h.tell(Message{Type: MsgState, State: h.state()})
	h.changed()
}

// used marks the session of a pane typed in as used, the first time,
// and a restored pane as the user's again.
func (h *Host) used(pane int) {
	h.mu.Lock()
	s, _ := h.m.PaneSession(pane)
	changed := s != nil && !s.Used
	if changed {
		s.Used = true
	}
	h.mu.Unlock()
	if p := h.pane(pane); p != nil {
		p.mu.Lock()
		if p.restored {
			p.restored, changed = false, true
		}
		p.mu.Unlock()
	}
	if changed {
		h.publish()
	}
	h.changed() // what is typed at the prompt is kept too
}

// startIn starts the user's shell in dir, sized as the pane it sits
// beside (size: a pane ID, or 0).
func (h *Host) startIn(dir string, size int) (int, error) {
	cols, rows := 80, 24
	if p := h.pane(size); p != nil {
		p.mu.Lock()
		cols, rows = p.cols, p.rows
		p.mu.Unlock()
	}
	argv, env := shell(), shellEnv()
	if h.ShellDir != "" {
		argv, env = shellint.Wrap(argv, env, h.ShellDir)
	}
	return h.start(argv, dir, env, cols, rows, 0, 0)
}

// folderOf is where a pane's shell is, or "" for no pane.
func (h *Host) folderOf(id int) string {
	p := h.pane(id)
	if p == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.folder()
}

// handleSessions answers the workspace and session requests; false for
// another kind of message.
func (h *Host) handleSessions(c *client, m Message) bool {
	reply := Message{Type: MsgReply, ID: m.ID}
	var err error
	switch m.Type {
	case MsgState:
		reply.State = h.state()
		c.message(reply)
		return true
	case MsgOpen:
		reply.Workspace, err = h.open(m.Dir, m.Name)
	case MsgNewSession:
		reply.Session, err = h.newSession(m.Workspace, m.Name)
	case MsgSplit:
		reply.Pane, err = h.split(m.Session, layout.Split(m.Split))
	case MsgLayout:
		err = h.layout(m)
	case MsgShow:
		h.mu.Lock()
		err = h.m.Show(m.Workspace, m.Session)
		h.mu.Unlock()
	case MsgRename:
		err = h.rename(m.Workspace, m.Session, m.Name)
	case MsgCloseSession:
		err = h.closeSession(m.Session)
	case MsgRemove:
		err = h.removeWorkspace(m.Workspace)
	case MsgMove:
		h.mu.Lock()
		err = h.m.Move(m.Workspace, m.To)
		h.mu.Unlock()
	default:
		return false
	}
	if err != nil {
		reply.Error = err.Error()
		c.message(reply)
		return true
	}
	c.message(reply)
	h.publish()
	return true
}

// open shows the workspace for dir, made with a session if there is
// none.
func (h *Host) open(dir, name string) (int, error) {
	if dir == "" {
		return 0, errors.New("no folder")
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return 0, fmt.Errorf("%s is not a folder", dir)
	}
	h.mu.Lock()
	w := h.m.ByDir(dir)
	h.mu.Unlock()
	if w != nil {
		h.mu.Lock()
		defer h.mu.Unlock()
		return w.ID, h.m.Show(w.ID, 0)
	}
	pane, err := h.startIn(dir, 0)
	if err != nil {
		return 0, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	w = h.m.AddWorkspace(dir, name, true)
	s := h.m.AddSession(w, "", pane)
	return w.ID, h.m.Show(w.ID, s.ID)
}

// newSession adds a session to a workspace, its shell in the workspace's
// folder, and shows it.
func (h *Host) newSession(workspace int, name string) (int, error) {
	h.mu.Lock()
	w := h.m.Workspace(workspace)
	h.mu.Unlock()
	if w == nil {
		return 0, model.ErrNotFound
	}
	pane, err := h.startIn(w.Dir, 0)
	if err != nil {
		return 0, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.m.AddSession(w, name, pane)
	return s.ID, h.m.Show(w.ID, s.ID)
}

// split starts a shell beside the session's focused pane, in its folder.
func (h *Host) split(session int, sp layout.Split) (int, error) {
	if sp != layout.Right && sp != layout.Down {
		return 0, fmt.Errorf("split %q", sp)
	}
	h.mu.Lock()
	s, w := h.m.Session(session)
	focus, dir := 0, ""
	if s != nil {
		focus, dir = s.Focus, w.Dir
	}
	h.mu.Unlock()
	if s == nil {
		return 0, model.ErrNotFound
	}
	pane, err := h.startIn(cmp.Or(h.folderOf(focus), dir), focus)
	if err != nil {
		return 0, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return pane, h.m.Split(session, pane, sp)
}

// layout changes a session's layout or focus.
func (h *Host) layout(m Message) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, _ := h.m.Session(m.Session)
	if s == nil {
		return model.ErrNotFound
	}
	d := layout.Dir(m.Direction)
	switch m.Op {
	case OpFocus:
		target := m.Pane
		if target == 0 {
			target = layout.Neighbor(s.Tree, s.Focus, d)
		}
		if target != 0 && s.Tree.Has(target) {
			s.Focus = target
			if s.Zoom != 0 {
				s.Zoom = target
			}
		}
	case OpSwap:
		if n := layout.Neighbor(s.Tree, s.Focus, d); n != 0 {
			s.Tree = layout.Swap(s.Tree, s.Focus, n)
		}
	case OpResize:
		s.Tree = layout.Resize(s.Tree, s.Focus, d, cmp.Or(m.Step, 0.05))
	case OpEqualize:
		s.Tree = layout.Equalize(s.Tree)
	case OpZoom:
		if s.Zoom != 0 {
			s.Zoom = 0
		} else if len(s.Tree.Panes()) > 1 {
			s.Zoom = s.Focus
		}
	case OpPreset:
		name := m.Name
		if name == "" {
			// The next preset after the one the session has.
			i := slices.Index(layout.Presets, presetOf(s.Tree))
			name = layout.Presets[(i+1)%len(layout.Presets)]
		}
		if !slices.Contains(layout.Presets, name) {
			return fmt.Errorf("preset %q", name)
		}
		s.Tree = layout.Preset(s.Tree.Panes(), name)
		s.Zoom = 0
	case OpRatio:
		if !layout.SetRatioAt(s.Tree, m.Path, m.Ratio) {
			return fmt.Errorf("no split at %q", m.Path)
		}
	default:
		return fmt.Errorf("layout %q", m.Op)
	}
	return nil
}

// presetOf is the preset a tree is laid out as, "" for none.
func presetOf(t *layout.Node) string {
	b, _ := json.Marshal(t)
	for _, p := range layout.Presets {
		if q, _ := json.Marshal(layout.Preset(t.Panes(), p)); string(q) == string(b) {
			return p
		}
	}
	return ""
}

func (h *Host) rename(workspace, session int, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("no name")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if session != 0 {
		s, _ := h.m.Session(session)
		if s == nil {
			return model.ErrNotFound
		}
		s.Name = name
		return nil
	}
	w := h.m.Workspace(workspace)
	if w == nil {
		return model.ErrNotFound
	}
	w.Name = name
	return nil
}

// closeSession ends a session's panes; the session goes with the last.
func (h *Host) closeSession(session int) error {
	h.mu.Lock()
	s, _ := h.m.Session(session)
	var panes []int
	if s != nil {
		panes = s.Tree.Panes()
		h.m.RemoveSession(session)
	}
	h.mu.Unlock()
	if s == nil {
		return model.ErrNotFound
	}
	h.endPanes(panes)
	return nil
}

func (h *Host) removeWorkspace(workspace int) error {
	h.mu.Lock()
	w := h.m.Workspace(workspace)
	var panes []int
	if w != nil {
		for _, s := range w.Sessions {
			panes = append(panes, s.Tree.Panes()...)
		}
		h.m.RemoveWorkspace(workspace)
	}
	h.mu.Unlock()
	if w == nil {
		return model.ErrNotFound
	}
	h.endPanes(panes)
	return nil
}

// endPanes ends panes no longer in the model; the host ends with the
// last when it should.
func (h *Host) endPanes(ids []int) {
	last := false
	for _, id := range ids {
		if p := h.pane(id); p != nil {
			last = h.closePane(p)
		}
	}
	if last && h.ExitWhenEmpty {
		go h.Close()
	}
}

// forgetPane takes a closed pane out of the model.
func (h *Host) forgetPane(id int) {
	h.mu.Lock()
	h.m.RemovePane(id)
	h.mu.Unlock()
}
