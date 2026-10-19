package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/lyricat/inua/internal/term/model"
)

// Client is a connection to the host. Output and events arrive on the
// connection's own goroutine, through OnOutput and OnEvent, set before
// Dial returns them anything.
type Client struct {
	conn net.Conn

	wmu sync.Mutex // writes

	mu      sync.Mutex
	next    int
	pending map[int]chan Message
	err     error

	onOutput func(pane int, b []byte)
	onEvent  func(Message)
	done     chan struct{}
}

// Dial connects to the host at path; onOutput and onEvent may be nil.
func Dial(path string, onOutput func(pane int, b []byte), onEvent func(Message)) (*Client, error) {
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	if err := writeMessage(conn, Message{Type: MsgHello, Version: Version}); err != nil {
		conn.Close()
		return nil, err
	}
	kind, payload, err := readFrame(conn)
	var m Message
	if err == nil && (kind != frameControl || json.Unmarshal(payload, &m) != nil) {
		err = errors.New("host: no welcome")
	}
	if err == nil && m.Type != MsgWelcome {
		err = fmt.Errorf("host: %s", m.Error)
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	c := &Client{conn: conn, pending: map[int]chan Message{}, onOutput: onOutput, onEvent: onEvent, done: make(chan struct{})}
	go c.read()
	return c, nil
}

func (c *Client) read() {
	defer close(c.done)
	var err error
	for {
		var kind byte
		var payload []byte
		if kind, payload, err = readFrame(c.conn); err != nil {
			break
		}
		switch kind {
		case frameOutput:
			id, b, e := splitPane(payload)
			if e != nil {
				err = e
				break
			}
			if c.onOutput != nil {
				c.onOutput(id, b)
			}
			continue
		case frameControl:
			var m Message
			if e := json.Unmarshal(payload, &m); e != nil {
				err = e
				break
			}
			if m.Type == MsgReply {
				c.mu.Lock()
				ch := c.pending[m.ID]
				delete(c.pending, m.ID)
				c.mu.Unlock()
				if ch != nil {
					ch <- m
				}
			} else if c.onEvent != nil {
				c.onEvent(m)
			}
			continue
		}
		break
	}
	if err == nil {
		err = errors.New("host: unexpected frame")
	}
	c.mu.Lock()
	c.err = err
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
}

// Done is closed when the connection ends.
func (c *Client) Done() <-chan struct{} { return c.done }

// Close ends the connection; the host keeps the panes.
func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) call(m Message) (Message, error) {
	ch := make(chan Message, 1)
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return Message{}, err
	}
	c.next++
	m.ID = c.next
	c.pending[m.ID] = ch
	c.mu.Unlock()
	c.wmu.Lock()
	err := writeMessage(c.conn, m)
	c.wmu.Unlock()
	if err != nil {
		return Message{}, err
	}
	r, ok := <-ch
	if !ok {
		return Message{}, errors.New("host: connection closed")
	}
	if r.Error != "" {
		return r, fmt.Errorf("host: %s", r.Error)
	}
	return r, nil
}

// Create starts argv in dir on a new pane of cols × rows.
func (c *Client) Create(argv []string, dir string, env []string, cols, rows int) (int, error) {
	r, err := c.call(Message{Type: MsgCreate, Argv: argv, Dir: dir, Env: env, Cols: cols, Rows: rows})
	return r.Pane, err
}

// List is the host's panes.
func (c *Client) List() ([]PaneInfo, error) {
	r, err := c.call(Message{Type: MsgList})
	return r.Panes, err
}

// Attach asks for a pane's output: its replay arrives through OnOutput
// before Attach returns, then what follows.
func (c *Client) Attach(pane int) error {
	_, err := c.call(Message{Type: MsgAttach, Pane: pane})
	return err
}

// Detach stops a pane's output.
func (c *Client) Detach(pane int) error {
	_, err := c.call(Message{Type: MsgDetach, Pane: pane})
	return err
}

// Resize sets a pane's size; this client is the one typing in it.
func (c *Client) Resize(pane, cols, rows, cellW, cellH int) error {
	_, err := c.call(Message{Type: MsgResize, Pane: pane, Cols: cols, Rows: rows, CellW: cellW, CellH: cellH})
	return err
}

// SetColors tells the host the colours programs are told when they ask.
func (c *Client) SetColors(colors Colors) error {
	_, err := c.call(Message{Type: MsgColors, Colors: &colors})
	return err
}

// ClosePane ends a pane's program and forgets the pane.
func (c *Client) ClosePane(pane int) error {
	_, err := c.call(Message{Type: MsgClose, Pane: pane})
	return err
}

// Input sends what was typed to a pane.
func (c *Client) Input(pane int, b []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return writeFrame(c.conn, frameInput, paneBytes(pane, b)...)
}

// State is the workspaces, sessions and panes now; afterwards each
// change comes as an event of type MsgState.
func (c *Client) State() (*State, error) {
	r, err := c.call(Message{Type: MsgState})
	if err == nil && r.State == nil {
		err = errors.New("host: no state")
	}
	return r.State, err
}

// Open shows the workspace for dir, made with a session if new.
func (c *Client) Open(dir, name string) (int, error) {
	r, err := c.call(Message{Type: MsgOpen, Dir: dir, Name: name})
	return r.Workspace, err
}

// NewSession adds a session to a workspace and shows it.
func (c *Client) NewSession(workspace int, name string) (int, error) {
	r, err := c.call(Message{Type: MsgNewSession, Workspace: workspace, Name: name})
	return r.Session, err
}

// Split starts a shell beside the session's focused pane: split is
// "right" or "down".
func (c *Client) Split(session int, split string) (int, error) {
	r, err := c.call(Message{Type: MsgSplit, Session: session, Split: split})
	return r.Pane, err
}

// Layout does op (OpFocus…) on a session; m carries what the op needs.
func (c *Client) Layout(session int, op string, m Message) error {
	m.Type, m.Session, m.Op = MsgLayout, session, op
	_, err := c.call(m)
	return err
}

// Show shows a workspace's session (0: the last it showed).
func (c *Client) Show(workspace, session int) error {
	_, err := c.call(Message{Type: MsgShow, Workspace: workspace, Session: session})
	return err
}

// Rename names a session, or a workspace when session is 0.
func (c *Client) Rename(workspace, session int, name string) error {
	_, err := c.call(Message{Type: MsgRename, Workspace: workspace, Session: session, Name: name})
	return err
}

// CloseSession ends a session's panes and forgets it.
func (c *Client) CloseSession(session int) error {
	_, err := c.call(Message{Type: MsgCloseSession, Session: session})
	return err
}

// RemoveWorkspace forgets a workspace, ending its panes.
func (c *Client) RemoveWorkspace(workspace int) error {
	_, err := c.call(Message{Type: MsgRemove, Workspace: workspace})
	return err
}

// SetUI tells the host how the window is, to be kept.
func (c *Client) SetUI(ui model.UI) error {
	_, err := c.call(Message{Type: MsgUI, UI: &ui})
	return err
}

// MoveWorkspace puts a workspace at place to in the list.
func (c *Client) MoveWorkspace(workspace, to int) error {
	_, err := c.call(Message{Type: MsgMove, Workspace: workspace, To: to})
	return err
}
