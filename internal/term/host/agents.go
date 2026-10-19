package host

import (
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/agent"
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
	case m == nil:
		// The agent ended: the pane is a shell again.
		a.m, a.t = nil, agent.Tracker{}
		return true
	case a.m == nil || a.m.Type != m.Type:
		a.m = m
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
	if a.m == nil || a.timer != nil || a.t.Reported {
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
		return true, &note{pane: p.id, agent: a.m.Name, state: a.t.State, title: p.title, ask: ask}
	}
	return true, nil
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

// procArgs is a process's argument list.
func procArgs(pid int) []string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil || len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
}

// procEnv is one variable of a process's environment.
func procEnv(pid int, key string) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	if err != nil {
		return ""
	}
	for _, e := range strings.Split(string(b), "\x00") {
		if v, ok := strings.CutPrefix(e, key+"="); ok {
			return v
		}
	}
	return ""
}

// note is a notification about an agent.
type note struct {
	pane  int
	agent string
	state agent.State
	title string
	ask   string
}

// notifier posts notifications through any notification server; Show
// brings the window to the pane.
type notifier struct {
	once sync.Once
	conn *dbus.Conn
	mu   sync.Mutex
	live map[uint32]int // notification to pane
}

func (h *Host) notify(n note) {
	if !h.Notify {
		return
	}
	nf := &h.notes
	nf.once.Do(func() {
		conn, err := dbus.ConnectSessionBus()
		if err != nil {
			log.Printf("host: notifications: %v", err)
			return
		}
		nf.conn, nf.live = conn, map[uint32]int{}
		conn.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.Notifications"), dbus.WithMatchObjectPath("/org/freedesktop/Notifications"))
		sigs := make(chan *dbus.Signal, 16)
		conn.Signal(sigs)
		go func() {
			for s := range sigs {
				if s.Name != "org.freedesktop.Notifications.ActionInvoked" || len(s.Body) < 1 {
					continue
				}
				id, _ := s.Body[0].(uint32)
				nf.mu.Lock()
				pane, ok := nf.live[id]
				nf.mu.Unlock()
				if ok {
					h.showPane(pane)
				}
			}
		}()
	})
	if nf.conn == nil {
		return
	}
	summary := i18n.Tf("%s needs you", n.agent)
	body := n.ask
	if n.state == agent.Done {
		summary, body = i18n.Tf("%s is done", n.agent), ""
	}
	if t, _ := agent.Title(n.title); body == "" {
		body = t
	}
	var id uint32
	err := nf.conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications").Call(
		"org.freedesktop.Notifications.Notify", 0,
		"inua-term", uint32(0), "utilities-terminal", summary, body,
		[]string{"default", i18n.T("Show")}, map[string]dbus.Variant{"desktop-entry": dbus.MakeVariant("io.github.lyricat.Inua.Term")}, int32(-1),
	).Store(&id)
	if err != nil {
		log.Printf("host: notify: %v", err)
		return
	}
	nf.mu.Lock()
	nf.live[id] = n.pane
	nf.mu.Unlock()
}

// showPane shows a pane's session with the pane focused, and brings a
// window up: inua-term again, which raises the one open or opens one.
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
	if h.Raise != nil {
		h.Raise()
		return
	}
	if exe, err := os.Executable(); err == nil {
		cmd := exec.Command(exe)
		if err := cmd.Start(); err == nil {
			go cmd.Wait()
		}
	}
}
