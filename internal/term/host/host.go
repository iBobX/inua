package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/lyricat/inua/internal/term/pty"
	"github.com/lyricat/inua/internal/term/vt"
)

// ReplayLimit is how much of a pane's recent output the host keeps for a
// client attaching later.
const ReplayLimit = 4 << 20

// queueLen is how many frames wait for a slow client before the host
// gives up on it; it can attach again and get the replay.
const queueLen = 1024

// Host owns the panes and serves clients on a socket.
type Host struct {
	ln          net.Listener
	replayLimit int
	// ExitWhenEmpty ends Serve once the last pane has been closed.
	ExitWhenEmpty bool

	mu      sync.Mutex
	panes   map[int]*pane
	next    int
	clients map[*client]bool
	closed  bool
	done    chan struct{}
}

// pane is a program on a PTY, with the host's own engine for its state.
type pane struct {
	id   int
	argv []string
	dir  string

	pty  *pty.PTY
	term *vt.Terminal // nil without libinua-vt: no title or folder then

	mu       sync.Mutex
	replay   []byte
	title    string
	pwd      string
	cols     int
	rows     int
	exited   bool
	code     int
	attached map[*client]bool
}

// client is a connection to the host.
type client struct {
	conn net.Conn
	out  chan [][]byte // frames: kind byte first, then payload parts
	once sync.Once
}

// Listen makes a host on the socket at path: its folder is made private
// to the user, and a socket left by a host that is gone is replaced.
func Listen(path string) (*Host, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if c, err := net.Dial("unix", path); err == nil {
		c.Close()
		return nil, fmt.Errorf("host: %s: a host is running", path)
	}
	os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return &Host{
		ln: ln, replayLimit: ReplayLimit,
		panes: map[int]*pane{}, clients: map[*client]bool{}, done: make(chan struct{}),
	}, nil
}

// Serve accepts clients until Close, or until the last pane is closed
// with ExitWhenEmpty set.
func (h *Host) Serve() error {
	go func() {
		<-h.done
		h.ln.Close()
	}()
	for {
		c, err := h.ln.Accept()
		if err != nil {
			select {
			case <-h.done:
				return nil
			default:
				return err
			}
		}
		go h.serveConn(c)
	}
}

// Close stops the host: every program gets SIGHUP.
func (h *Host) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	panes := h.panes
	h.panes = map[int]*pane{}
	clients := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()
	for _, p := range panes {
		p.pty.Close()
	}
	for _, c := range clients {
		c.finish() // what is queued (the reply to the last close) still goes out
	}
	close(h.done)
}

func (h *Host) serveConn(conn net.Conn) {
	c := &client{conn: conn, out: make(chan [][]byte, queueLen)}
	defer h.drop(c)
	// The first message is hello, with our version.
	kind, payload, err := readFrame(conn)
	var hello Message
	if err != nil || kind != frameControl || json.Unmarshal(payload, &hello) != nil || hello.Type != MsgHello {
		return
	}
	if hello.Version != Version {
		writeMessage(conn, Message{Type: MsgReply, Error: fmt.Sprintf("protocol %d, the host speaks %d", hello.Version, Version)})
		return
	}
	if err := writeMessage(conn, Message{Type: MsgWelcome, Version: Version}); err != nil {
		return
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.clients[c] = true
	h.mu.Unlock()
	go c.write()
	for {
		kind, payload, err := readFrame(conn)
		if err != nil {
			return
		}
		switch kind {
		case frameControl:
			var m Message
			if err := json.Unmarshal(payload, &m); err != nil {
				return
			}
			h.handle(c, m)
		case frameInput:
			id, b, err := splitPane(payload)
			if err != nil {
				return
			}
			if p := h.pane(id); p != nil {
				p.pty.Write(b)
			}
		default:
			return
		}
	}
}

// write sends the client's frames in order, and closes the connection
// when the queue is closed and empty.
func (c *client) write() {
	defer c.conn.Close()
	for f := range c.out {
		if err := writeFrame(c.conn, f[0][0], f[1:]...); err != nil {
			c.close()
			return
		}
	}
}

// send queues a frame; a client too far behind is let go.
func (c *client) send(kind byte, payload ...[]byte) {
	f := append([][]byte{{kind}}, payload...)
	defer func() { recover() }() // the queue closed under us: the client is gone
	select {
	case c.out <- f:
	default:
		c.close()
	}
}

func (c *client) message(m Message) {
	b, err := json.Marshal(m)
	if err == nil {
		c.send(frameControl, b)
	}
}

// close drops the client now.
func (c *client) close() {
	c.once.Do(func() {
		c.conn.Close()
		close(c.out)
	})
}

// finish lets the client's writer send what is queued, then close.
func (c *client) finish() {
	c.once.Do(func() { close(c.out) })
}

// drop forgets a client that went away.
func (h *Host) drop(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	panes := make([]*pane, 0, len(h.panes))
	for _, p := range h.panes {
		panes = append(panes, p)
	}
	h.mu.Unlock()
	for _, p := range panes {
		p.mu.Lock()
		delete(p.attached, c)
		p.mu.Unlock()
	}
	c.close()
}

func (h *Host) pane(id int) *pane {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.panes[id]
}

func (h *Host) handle(c *client, m Message) {
	reply := Message{Type: MsgReply, ID: m.ID}
	fail := func(err error) {
		reply.Error = err.Error()
		c.message(reply)
	}
	switch m.Type {
	case MsgList:
		reply.Panes = h.list()
	case MsgCreate:
		id, err := h.create(m)
		if err != nil {
			fail(err)
			return
		}
		reply.Pane = id
	case MsgAttach, MsgDetach, MsgResize, MsgClose:
		p := h.pane(m.Pane)
		if p == nil {
			fail(fmt.Errorf("no pane %d", m.Pane))
			return
		}
		switch m.Type {
		case MsgAttach:
			p.attach(c) // the replay goes out before this reply
		case MsgDetach:
			p.mu.Lock()
			delete(p.attached, c)
			p.mu.Unlock()
		case MsgResize:
			if err := p.resize(m.Cols, m.Rows); err != nil {
				fail(err)
				return
			}
		case MsgClose:
			if h.closePane(p) && h.ExitWhenEmpty {
				// The last pane: the reply goes out, then the host ends.
				reply.Pane = m.Pane
				c.message(reply)
				go h.Close()
				return
			}
		}
		reply.Pane = m.Pane
	default:
		fail(fmt.Errorf("unknown request %q", m.Type))
		return
	}
	c.message(reply)
}

func (h *Host) list() []PaneInfo {
	h.mu.Lock()
	panes := make([]*pane, 0, len(h.panes))
	for _, p := range h.panes {
		panes = append(panes, p)
	}
	h.mu.Unlock()
	infos := make([]PaneInfo, 0, len(panes))
	for _, p := range panes {
		p.mu.Lock()
		infos = append(infos, PaneInfo{
			ID: p.id, Argv: p.argv, Title: p.title, Dir: p.folder(),
			Cols: p.cols, Rows: p.rows, Exited: p.exited, Code: p.code,
		})
		p.mu.Unlock()
	}
	return infos
}

func (h *Host) create(m Message) (int, error) {
	if len(m.Argv) == 0 {
		return 0, errors.New("no command")
	}
	cols, rows := m.Cols, m.Rows
	if cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}
	pt, err := pty.Start(m.Argv, m.Dir, m.Env, cols, rows)
	if err != nil {
		return 0, err
	}
	p := &pane{argv: m.Argv, dir: m.Dir, pty: pt, cols: cols, rows: rows, attached: map[*client]bool{}}
	if t, err := vt.New(cols, rows); err == nil {
		p.term = t
	} else {
		log.Printf("host: pane state off: %v", err)
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		pt.Close()
		return 0, errors.New("the host is closing")
	}
	h.next++
	p.id = h.next
	h.panes[p.id] = p
	h.mu.Unlock()
	go h.read(p)
	return p.id, nil
}

// read follows a pane's program until it ends.
func (h *Host) read(p *pane) {
	buf := make([]byte, 64*1024)
	for {
		n, err := p.pty.Read(buf)
		if n > 0 {
			// Its own copy: the clients' queues hold it while buf is read into again.
			p.output(append([]byte(nil), buf[:n]...), h.replayLimit)
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("host: pane %d: %v", p.id, err)
			}
			break
		}
	}
	code, _ := p.pty.Wait()
	p.mu.Lock()
	p.exited, p.code = true, code
	p.broadcast(Message{Type: MsgEvent, Pane: p.id, Code: &code})
	p.mu.Unlock()
}

// output keeps, feeds and sends what the program wrote, and tells the
// clients when its title or folder changed.
func (p *pane) output(b []byte, limit int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.replay = append(p.replay, b...)
	if over := len(p.replay) - limit; over > 0 {
		p.replay = append(p.replay[:0], p.replay[over:]...)
	}
	for c := range p.attached {
		c.send(frameOutput, paneBytes(p.id, b)...)
	}
	if p.term == nil {
		return
	}
	p.term.Write(b)
	if t := p.term.Title(); t != p.title {
		p.title = t
		p.broadcast(Message{Type: MsgEvent, Pane: p.id, Title: t})
	}
	if d := p.term.Pwd(); d != p.pwd {
		p.pwd = d
		p.broadcast(Message{Type: MsgEvent, Pane: p.id, Dir: p.folder()})
	}
}

// broadcast tells the attached clients; p.mu is held.
func (p *pane) broadcast(m Message) {
	for c := range p.attached {
		c.message(m)
	}
}

// folder is where the pane's shell is: what it reported (OSC 7), else
// where it started. p.mu is held.
func (p *pane) folder() string {
	if p.pwd != "" {
		if u, err := url.Parse(p.pwd); err == nil && u.Scheme == "file" && u.Path != "" {
			return u.Path
		}
	}
	return p.dir
}

// attach sends the replay and then everything after it, with nothing in
// between lost or sent twice.
func (p *pane) attach(c *client) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.replay) > 0 {
		c.send(frameOutput, paneBytes(p.id, append([]byte(nil), p.replay...))...)
	}
	p.attached[c] = true
	c.message(Message{Type: MsgEvent, Pane: p.id, Title: p.title, Dir: p.folder()})
	if p.exited {
		code := p.code
		c.message(Message{Type: MsgEvent, Pane: p.id, Code: &code})
	}
}

func (p *pane) resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("size %dx%d", cols, rows)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cols, p.rows = cols, rows
	if p.term != nil {
		p.term.Resize(cols, rows)
	}
	if p.exited {
		return nil
	}
	return p.pty.Resize(cols, rows)
}

// closePane ends a pane's program and forgets the pane; it reports
// whether that was the last one.
func (h *Host) closePane(p *pane) bool {
	h.mu.Lock()
	delete(h.panes, p.id)
	empty := len(h.panes) == 0
	h.mu.Unlock()
	p.pty.Close()
	p.mu.Lock()
	if p.term != nil {
		p.term.Close()
		p.term = nil
	}
	p.mu.Unlock()
	return empty
}
