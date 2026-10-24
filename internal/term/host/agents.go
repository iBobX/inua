package host

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/agent"
	"github.com/lyricat/inua/internal/term/vt"
)

// The host follows each pane's agent (section 9): which one runs, from
// the PTY's foreground process (checked every second), and its state,
// from the screen after output (at most a few times a second). A blocked
// or finished agent in a pane nobody looks at posts a notification.

// checkEvery is how often the panes' foreground processes are looked at.
const checkEvery = time.Second

// screenDelay is how long after output a pane's screen is read.
const screenDelay = 300 * time.Millisecond

// paneAgent is a pane's agent and what the host knows of its program.
type paneAgent struct {
	title   string // the title the agent reported
	fg      int    // the foreground process group last seen
	command string // the command running, "" while the shell waits
	m       *agent.Manifest
	t       agent.Tracker
	timer   *time.Timer // a screen read waiting
}

// follow starts watching the panes' programs; it ends with the host.
func (h *Host) follow() {
	tick := time.NewTicker(checkEvery)
	defer tick.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-tick.C:
			changed := false
			for _, p := range h.allPanes() {
				changed = h.checkProcess(p) || changed
			}
			if changed {
				h.publish()
			}
		}
	}
}

func (h *Host) allPanes() []*pane {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*pane, 0, len(h.panes))
	for _, p := range h.panes {
		out = append(out, p)
	}
	return out
}

// checkProcess looks at a pane's foreground process: a new one may be an
// agent, or end one. It reports whether what the columns show changed.
func (h *Host) checkProcess(p *pane) bool {
	if h.Agents == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	a := &p.agent
	if p.exited {
		if a.m != nil && a.t.State != agent.Exited {
			return a.t.Set(agent.Exited, "", time.Now())
		}
		return false
	}
	fg, err := p.pty.Foreground()
	if err != nil || fg == a.fg {
		return false
	}
	a.fg = fg
	var argv []string
	override := ""
	if fg != p.pty.Pid() {
		argv = procArgs(fg)
		override = procEnv(fg, "INUA_TERM_AGENT")
	}
	a.command = strings.Join(argv, " ")
	m := h.Agents.Detect(argv, override)
	switch {
	case m == nil && a.m == nil:
		return true // the command running changed
	case m == nil && a.t.Reported && fg != p.pty.Pid():
		return true // one that reports itself, run by something not known
	case m == nil:
		// The agent ended: the pane is a shell again.
		a.m, a.t, a.title = nil, agent.Tracker{}, ""
		return true
	case a.m == nil || a.m.Type != m.Type:
		a.m, a.title = m, ""
		a.t = agent.Tracker{Type: m.Type, State: agent.Idle, Since: time.Now()}
		h.readScreenLater(p)
		return true
	}
	return true
}

// readScreenLater reads a pane's screen for its agent's state a moment
// from now, once for any output until then. p.mu is held.
func (h *Host) readScreenLater(p *pane) {
	a := &p.agent
	if a.m == nil || a.timer != nil || (a.t.Reported && len(a.m.States) == 0) {
		return
	}
	a.timer = time.AfterFunc(screenDelay, func() {
		p.mu.Lock()
		p.agent.timer = nil
		changed, note := h.readScreen(p)
		p.mu.Unlock()
		if changed {
			h.publish()
		}
		if note != nil {
			h.notify(*note)
		}
	})
}

// readScreen sets a pane's agent's state from its screen and title.
// p.mu is held; a notification to post comes back.
func (h *Host) readScreen(p *pane) (bool, *note) {
	a := &p.agent
	if a.m == nil || p.term == nil || p.exited {
		return false, nil
	}
	state, ask := a.m.State(p.term.Tail(p.rows))
	if a.t.Reported && state == "" {
		// An agent that reports says what it does; its screen adds what
		// its reports leave out (Claude Code's hooks say nothing of a
		// question it asks), and a question gone was answered.
		if a.t.State != agent.Blocked {
			return false, nil
		}
		state = agent.Working
	}
	if state == "" {
		state = agent.Idle
		if _, spinning := agent.Title(p.title); spinning && strings.ContainsFunc(p.title, func(r rune) bool { return r >= 0x2800 && r <= 0x28ff }) {
			state = agent.Working // a braille spinner in the title
		}
	}
	before := a.t.State
	if !a.t.Set(state, ask, time.Now()) {
		return false, nil
	}
	if (a.t.State == agent.Blocked || a.t.State == agent.Done) && before != a.t.State && !h.looking(p.id) {
		return true, agentNote(p.id, a.m.Name, a.t.State, p.title, ask)
	}
	return true, nil
}

// agentNote is the notification for an agent that needs you or is done.
func agentNote(pane int, name string, s agent.State, title, ask string) *note {
	n := &note{pane: pane, summary: i18n.Tf("%s needs you", name), body: ask}
	if s == agent.Done {
		n.summary, n.body = i18n.Tf("%s is done", name), ""
	}
	if n.body == "" {
		n.body, _ = agent.Title(title)
	}
	return n
}

// programNotes takes the notifications a pane's program sent (OSC 9,
// 777): they go on to the desktop when nobody looks at the pane, and an
// agent's may say its state (Claude Code's "needs your permission", "is
// waiting for your input"). p.mu is held.
func (h *Host) programNotes(p *pane, ns []vt.Notification) {
	a := &p.agent
	changed := false
	for _, n := range ns {
		low := strings.ToLower(n.Title + " " + n.Body)
		if a.m != nil && !a.t.Reported {
			// A state the agent's own notification will tell of.
			var s agent.State
			switch {
			case strings.Contains(low, "permission") || strings.Contains(low, "needs your"):
				s = agent.Blocked
			case strings.Contains(low, "waiting for your input"):
				s = agent.Idle
			}
			if s != "" {
				before := a.t.State
				if a.t.Set(s, n.Body, time.Now()) {
					changed = true
					if a.t.State != before && (a.t.State == agent.Blocked || a.t.State == agent.Done) {
						nn, id := agentNote(p.id, a.m.Name, a.t.State, p.title, a.t.Ask), p.id
						go func() {
							if !h.looking(id) {
								h.notify(*nn)
							}
						}()
					}
				}
				continue
			}
		}
		summary := n.Title
		if summary == "" {
			summary = p.title
			if a.m != nil {
				summary = a.m.Name
			}
		}
		id := p.id
		go func() {
			if !h.looking(id) {
				h.notify(note{pane: id, summary: summary, body: n.Body})
			}
		}()
	}
	if changed {
		go h.publish()
	}
}

// report is an agent saying what it does, through its hooks or
// `inua-term report`: its screen is not read for state after that. Only
// a process running in the pane (from, the reporter, whose controlling
// terminal is the pane's) may: Codex runs its notify from a daemon that
// left the pane it started in and keeps that pane's INUA_TERM_PANE, so
// the turns of every Codex anywhere came to that pane.
func (h *Host) report(m Message, from int) error {
	p := h.pane(m.Pane)
	if p == nil {
		return fmt.Errorf("no pane %d", m.Pane)
	}
	if from <= 0 || !p.pty.Controls(from) {
		return fmt.Errorf("pane %d: the reporter does not run in it", m.Pane)
	}
	s := agent.State(m.AgentState)
	switch s {
	case agent.Working, agent.Blocked, agent.Done, agent.Idle, "":
	default:
		return fmt.Errorf("state %q: want working, blocked, done or idle", s)
	}
	p.mu.Lock()
	a := &p.agent
	if m.Agent != "" && (a.m == nil || a.m.Type != m.Agent) {
		mf := &agent.Manifest{Type: m.Agent, Name: m.Agent}
		if h.Agents != nil {
			if known := h.Agents.ByType(m.Agent); known != nil {
				mf = known
			}
		}
		a.m, a.t = mf, agent.Tracker{Type: mf.Type, State: agent.Idle, Since: time.Now()}
	}
	if a.m == nil {
		p.mu.Unlock()
		return errors.New("no agent in the pane: name it with --agent")
	}
	a.t.Reported = true
	if m.Title != "" {
		a.title = m.Title
	}
	var n *note
	if s != "" {
		before := a.t.State
		if s == agent.Done {
			a.t.Set(agent.Working, "", time.Now()) // done is the end of a turn
		}
		a.t.Set(s, m.Summary, time.Now())
		if a.t.State != before && (a.t.State == agent.Blocked || a.t.State == agent.Done) && !h.looking(p.id) {
			n = agentNote(p.id, a.m.Name, a.t.State, p.title, m.Summary)
			if a.title != "" {
				n.body = a.title
				if a.t.State == agent.Blocked && m.Summary != "" {
					n.body = m.Summary
				}
			}
		}
	}
	p.mu.Unlock()
	h.publish()
	if n != nil {
		h.notify(*n)
	}
	return nil
}

// seen is the user looking at a pane: a done agent there is idle again.
func (h *Host) seen(id int) {
	p := h.pane(id)
	if p == nil {
		return
	}
	p.mu.Lock()
	changed := p.agent.t.Seen(time.Now())
	p.mu.Unlock()
	if changed {
		h.publish()
	}
}

// looking says a window is active and shows the pane with the focus.
func (h *Host) looking(id int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	active := false
	for c := range h.clients {
		active = active || c.active
	}
	s, _ := h.m.Session(h.m.Shown.Session)
	return active && s != nil && s.Focus == id
}

// note is a notification about a pane.
type note struct {
	pane          int
	summary, body string
}

// Desktop is what the host asks of the desktop it runs on
// (internal/term/desktop).
type Desktop interface {
	// Notify posts a notification; show runs when its Show is taken.
	Notify(summary, body string, show func()) error
	// Raise brings inua-term's window up: the one open, else a new one.
	Raise()
}

func (h *Host) notify(n note) {
	if !h.Notify || h.Desktop == nil {
		return
	}
	if err := h.Desktop.Notify(n.summary, n.body, func() { h.showPane(n.pane) }); err != nil {
		log.Printf("host: notify: %v", err)
	}
}

// showPane shows a pane's session with the pane focused, and brings a
// window up.
func (h *Host) showPane(id int) {
	h.mu.Lock()
	s, w := h.m.PaneSession(id)
	if s != nil {
		h.m.Show(w.ID, s.ID)
		s.Focus = id
	}
	h.mu.Unlock()
	if s == nil {
		return
	}
	h.publish()
	h.seen(id)
	if h.Desktop != nil {
		h.Desktop.Raise()
	}
}
