package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path"
	"path/filepath"
	"strconv"
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
	b, err := os.ReadFile("/proc/" + strconv.Itoa(fg) + "/cmdline")
	if err != nil || len(b) == 0 {
		return ""
	}
	args := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
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

// restore starts the kept sessions again: a shell in each pane's folder,
// in the same layouts, each pane's command waiting to be put back at its
// prompt.
func (h *Host) restore(s Saved) {
	ids := map[int]int{} // old pane IDs to new
	for _, w := range s.Model.Workspaces {
		var kept []*model.Session
		for _, ss := range w.Sessions {
			ok := true
			for _, old := range ss.Tree.Panes() {
				sp := s.Panes[old]
				dir := sp.Dir
				if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
					dir = w.Dir
				}
				id, err := h.startIn(dir, 0)
				if err != nil {
					log.Printf("host: restoring %s: %v", ss.Name, err)
					ok = false
					break
				}
				ids[old] = id
				if p := h.pane(id); p != nil {
					p.mu.Lock()
					p.title, p.pending, p.restored = sp.Title, sp.Command, true
					p.mu.Unlock()
				}
			}
			if ok {
				ss.Tree = renumber(ss.Tree, ids)
				ss.Focus, ss.Zoom = ids[ss.Focus], ids[ss.Zoom]
				if !ss.Tree.Has(ss.Focus) {
					ss.Focus = ss.Tree.Panes()[0]
				}
				kept = append(kept, ss)
			}
		}
		w.Sessions = kept
	}
	h.mu.Lock()
	next := h.m.Next
	h.m = s.Model
	h.m.Next = max(h.m.Next, next)
	h.mu.Unlock()
	h.publish()
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
