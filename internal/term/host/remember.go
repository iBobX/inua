package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/term/layout"
	"github.com/lyricat/inua/internal/term/model"
	"github.com/lyricat/inua/internal/term/vt"
)

// What the host remembers for when it ends (section 5, What it
// remembers): the sessions used, their layouts, and for each pane its
// folder, title and last command, which comes back at the prompt unrun.
// While the host runs nothing needs remembering; this is for after a
// reboot or a logout.

// Saved is the file's content.
type Saved struct {
	Version int               `json:"version"`
	Model   model.Model       `json:"model"`
	Panes   map[int]SavedPane `json:"panes"`
}

// SavedPane is what a pane comes back with.
type SavedPane struct {
	Dir     string `json:"dir"`
	Title   string `json:"title,omitempty"`
	Command string `json:"command,omitempty"`
}

// DefaultForget are the commands not remembered, as globs matched
// without regard to case.
var DefaultForget = []string{"*password*", "*token=*", "*secret*"}

// StatePath is the file: $XDG_STATE_HOME/inua/term.json.
func StatePath() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, _ := os.UserHomeDir()
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "inua", "term.json")
}

// Remember makes the host keep its sessions in file and restore them from
// it: what file holds now comes back at once. forget are the globs of
// commands not to keep (nil: DefaultForget); commands false keeps the
// layouts only.
func (h *Host) Remember(file string, forget []string, commands bool) error {
	if forget == nil {
		forget = DefaultForget
	}
	h.mu.Lock()
	h.saveFile, h.forget, h.keepCommands = file, forget, commands
	h.mu.Unlock()
	b, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var s Saved
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	h.restore(s)
	return nil
}

// changed notes the host has something new to keep: it is saved a
// moment later, once for many changes.
func (h *Host) changed() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.saveFile == "" || h.saveTimer != nil || h.closed {
		return
	}
	h.saveTimer = time.AfterFunc(2*time.Second, func() {
		h.mu.Lock()
		h.saveTimer = nil
		h.mu.Unlock()
		h.Save()
	})
}

// Save writes what is to be kept now: the used sessions and the
// workspaces they are in or the user added.
func (h *Host) Save() {
	h.mu.Lock()
	file := h.saveFile
	b, _ := json.Marshal(&h.m)
	waiting := make([]waitingSession, len(h.waiting))
	for i, ws := range h.waiting {
		s := *ws.s // a copy: the session changes as it starts
		waiting[i] = waitingSession{ws: ws.ws, s: &s, panes: ws.panes}
	}
	h.mu.Unlock()
	if file == "" {
		return
	}
	s := Saved{Version: 1, Panes: map[int]SavedPane{}}
	json.Unmarshal(b, &s.Model)
	var keep []*model.Workspace
	for _, w := range s.Model.Workspaces {
		var used []*model.Session
		for _, ss := range w.Sessions {
			if ss.Used {
				used = append(used, ss)
			}
		}
		w.Sessions = used
		if len(used) > 0 || w.Added {
			keep = append(keep, w)
		}
	}
	s.Model.Workspaces = keep
	if s.Model.Workspace(s.Model.Shown.Workspace) == nil {
		s.Model.Shown = model.Shown{}
	}
	for _, id := range s.Model.Panes() {
		if p := h.pane(id); p != nil {
			s.Panes[id] = h.savedPane(p)
		}
	}
	// The sessions waiting for their folders stay as they were kept.
	for _, ws := range waiting {
		w := s.Model.Workspace(ws.ws.ID)
		if w == nil {
			copied := ws.ws
			w = &copied
			s.Model.Workspaces = append(s.Model.Workspaces, w)
		}
		w.Sessions = append(w.Sessions, ws.s)
		maps.Copy(s.Panes, ws.panes)
	}
	if err := writeFile(file, &s); err != nil {
		log.Printf("host: remembering: %v", err)
	}
}

// savedPane is a pane's folder, title and last command: what is typed
// at the prompt and not run, else the command running, else the one run
// last; not a command the forget patterns name. Only the shell's own
// prompt marks what is typed as input: a password a program asks for
// (sudo, ssh) is neither marked nor drawn, so it is never kept. (The
// terminal's echo flag cannot tell: line editors turn it off to echo
// themselves.)
func (h *Host) savedPane(p *pane) SavedPane {
	h.mu.Lock()
	forget, commands := h.forget, h.keepCommands
	h.mu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	sp := SavedPane{Dir: p.folder(), Title: p.title}
	if !commands || p.exited {
		return sp
	}
	cmd := ""
	if p.term != nil {
		cmd = p.term.Input()
	}
	if cmd == "" {
		cmd = running(p)
	}
	if cmd == "" {
		cmd = p.lastRun
	}
	if cmd == "" && p.pending != "" {
		cmd = p.pending // restored, not put back at the prompt yet
	}
	if !forgotten(cmd, forget) {
		sp.Command = cmd
	}
	return sp
}

// running is the command the pane's shell runs now, from the PTY's
// foreground process: "" while the shell itself waits.
func running(p *pane) string {
	fg, err := p.pty.Foreground()
	if err != nil || fg == p.pty.Pid() || fg <= 0 {
		return ""
	}
	args := procArgs(fg)
	if len(args) == 0 {
		return ""
	}
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\n'\"\\$`*?[]{}()<>|&;#~") {
			args[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(args, " ")
}

// forgotten says cmd matches a forget pattern.
func forgotten(cmd string, forget []string) bool {
	low := strings.ToLower(cmd)
	for _, f := range forget {
		if ok, _ := path.Match(strings.ToLower(f), low); ok {
			return true
		}
	}
	return false
}

// writeFile writes s whole, mode 0600, through a file renamed over the
// old one; unchanged content is not written again.
func writeFile(file string, s *Saved) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if old, err := os.ReadFile(file); err == nil && bytes.Equal(old, b) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// waitingSession is a kept session whose folders are not there yet, as on
// a drive mounted after the host started (a network share, a disk): it
// stays out of the model, in the file all the same, and starts once its
// folders are there. Its panes are numbered below zero, apart from the
// host's.
type waitingSession struct {
	ws    model.Workspace // its workspace, without sessions
	s     *model.Session
	panes map[int]SavedPane
}

// waitCheck is how often the waiting sessions' folders are looked for
// (a test's shorter).
var waitCheck = 3 * time.Second

// restore starts the kept sessions again: a shell in each pane's folder,
// in the same layouts, each pane's command waiting to be put back at its
// prompt. A session whose folders are not there waits for them; one
// that fails to start is dropped whole, its panes started closed again.
func (h *Host) restore(s Saved) {
	var waiting []waitingSession
	var keep []*model.Workspace
	for _, w := range s.Model.Workspaces {
		var kept []*model.Session
		waits := false
		for _, ss := range w.Sessions {
			if !reachable(w, ss, s.Panes) {
				waiting = append(waiting, h.toWait(w, ss, s.Panes))
				waits = true
				continue
			}
			if h.startSession(w, ss, s.Panes) {
				kept = append(kept, ss)
			}
		}
		w.Sessions = kept
		// A workspace whose sessions all wait comes back with them.
		if len(kept) > 0 || !waits {
			keep = append(keep, w)
		}
	}
	s.Model.Workspaces = keep
	if ws := s.Model.Workspace(s.Model.Shown.Workspace); ws == nil || (s.Model.Shown.Session != 0 && !slices.ContainsFunc(ws.Sessions, func(ss *model.Session) bool { return ss.ID == s.Model.Shown.Session })) {
		s.Model.Shown = model.Shown{}
	}
	h.mu.Lock()
	next := h.m.Next
	h.m = s.Model
	h.m.Next = max(h.m.Next, next)
	h.waiting = append(h.waiting, waiting...)
	h.mu.Unlock()
	h.publish()
	if len(waiting) > 0 {
		go h.wait()
	}
}

// reachable says every pane of a session has a folder to start in: its
// own, else its workspace's.
func reachable(w *model.Workspace, ss *model.Session, panes map[int]SavedPane) bool {
	for _, old := range ss.Tree.Panes() {
		if !isDir(panes[old].Dir) && !isDir(w.Dir) {
			return false
		}
	}
	return true
}

func isDir(dir string) bool {
	fi, err := os.Stat(dir)
	return dir != "" && err == nil && fi.IsDir()
}

// toWait is a session set aside until its folders are there, its panes
// numbered below zero.
func (h *Host) toWait(w *model.Workspace, ss *model.Session, panes map[int]SavedPane) waitingSession {
	ws := *w
	ws.Sessions = nil
	ids := map[int]int{}
	kept := map[int]SavedPane{}
	h.mu.Lock()
	for _, old := range ss.Tree.Panes() {
		h.waitIDs--
		ids[old] = h.waitIDs
		kept[h.waitIDs] = panes[old]
	}
	h.mu.Unlock()
	ss.Tree = renumber(ss.Tree, ids)
	ss.Focus, ss.Zoom = ids[ss.Focus], ids[ss.Zoom]
	log.Printf("host: %s / %s waits for %s", w.Name, ss.Name, w.Dir)
	return waitingSession{ws: ws, s: ss, panes: kept}
}

// startSession starts a kept session's panes, each in its folder (else
// its workspace's), and numbers the session's layout with them; when one
// fails, those started are closed and it reports false.
func (h *Host) startSession(w *model.Workspace, ss *model.Session, panes map[int]SavedPane) bool {
	ids := map[int]int{} // old pane IDs to new
	for _, old := range ss.Tree.Panes() {
		sp := panes[old]
		dir := sp.Dir
		if !isDir(dir) {
			dir = w.Dir
		}
		id, err := h.startIn(dir, 0)
		if err != nil {
			log.Printf("host: restoring %s / %s: %v", w.Name, ss.Name, err)
			for _, started := range ids {
				if p := h.pane(started); p != nil {
					h.closePane(p)
				}
			}
			return false
		}
		ids[old] = id
		if p := h.pane(id); p != nil {
			p.mu.Lock()
			p.title, p.pending, p.restored = sp.Title, h.resume(sp.Command), true
			p.mu.Unlock()
		}
	}
	ss.Tree = renumber(ss.Tree, ids)
	ss.Focus, ss.Zoom = ids[ss.Focus], ids[ss.Zoom]
	if !ss.Tree.Has(ss.Focus) {
		ss.Focus = ss.Tree.Panes()[0]
	}
	return true
}

// wait starts the waiting sessions as their folders appear, until none
// waits or the host ends.
func (h *Host) wait() {
	tick := time.NewTicker(waitCheck)
	defer tick.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-tick.C:
		}
		h.mu.Lock()
		waiting := h.waiting
		h.waiting = nil
		h.mu.Unlock()
		var still []waitingSession
		started := false
		for _, ws := range waiting {
			if !reachable(&ws.ws, ws.s, ws.panes) {
				still = append(still, ws)
				continue
			}
			if !h.startSession(&ws.ws, ws.s, ws.panes) {
				continue // dropped, as at the start
			}
			started = true
			log.Printf("host: %s / %s is back", ws.ws.Name, ws.s.Name)
			h.mu.Lock()
			w := h.m.Workspace(ws.ws.ID)
			if w == nil {
				copied := ws.ws
				w = &copied
				h.m.Workspaces = append(h.m.Workspaces, w)
			}
			w.Sessions = append(w.Sessions, ws.s)
			h.mu.Unlock()
		}
		h.mu.Lock()
		h.waiting = append(still, h.waiting...)
		left := len(h.waiting)
		h.mu.Unlock()
		if started {
			h.publish()
		}
		if left == 0 {
			return
		}
	}
}

// resume is the command to put back for one that ran: an agent that can
// continue where it was comes back as its resume command (claude
// --continue), the rest as they were.
func (h *Host) resume(cmd string) string {
	if h.Agents == nil || cmd == "" {
		return cmd
	}
	if m := h.Agents.Detect(strings.Fields(cmd), ""); m != nil && m.Resume != "" {
		return m.Resume
	}
	return cmd
}

// renumber is a tree with its panes' new IDs.
func renumber(n *layout.Node, ids map[int]int) *layout.Node {
	if n == nil {
		return nil
	}
	c := *n
	if n.A == nil {
		c.Pane = ids[n.Pane]
		return &c
	}
	c.A, c.B = renumber(n.A, ids), renumber(n.B, ids)
	return &c
}

// prompts follows a pane's prompt marks: the command that ran last, and
// a restored command put back once the shell takes input. p.mu is held.
func (p *pane) prompts(marks []vt.Prompt) {
	for _, m := range marks {
		switch m.Kind {
		case vt.CommandStart:
			if m.Command != "" {
				p.lastRun = m.Command
			}
			p.host.changed()
		case vt.InputStart:
			if p.pending == "" || p.term == nil {
				continue
			}
			// Typed in for the user, never run: a multi-line command goes
			// in as one bracketed paste when the shell takes those, else
			// as one line.
			b, err := p.term.Paste(p.pending, false, false)
			if errors.Is(err, vt.ErrUnsafe) {
				b, err = p.term.Paste(strings.Join(strings.Fields(p.pending), " "), false, false)
			}
			p.pending = ""
			if err == nil && len(b) > 0 {
				select {
				case p.answers <- b:
				default:
				}
			}
		}
	}
}
