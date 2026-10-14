package workshop

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// JSON-RPC 2.0 error codes; NotAllowed is the workshop's own.
const (
	CodeParse      = -32700
	CodeNoMethod   = -32601
	CodeBadParams  = -32602
	CodeFailed     = -32000
	CodeNotAllowed = -32001
)

// Error is a JSON-RPC error, as a request's answer.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("%s (%d)", e.Message, e.Code) }

// message is any JSON-RPC message: a request (method and id), a
// notification (method, no id) or an answer (id, result or error).
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Handler answers what the other side sends: a request's result, or an
// error (an *Error to choose its code). A request's runs on a goroutine
// of its own, so it may make calls; a notification's runs on the reading
// one, in order, and must not.
type Handler func(method string, params json.RawMessage) (any, error)

// Conn is one side of JSON-RPC over a pair of streams, one message a
// line, requests going both ways at once.
type Conn struct {
	w       io.Writer
	handle  Handler
	onWrite func(line []byte) // sees each line sent, for the dev log
	onRead  func(line []byte)

	mu      sync.Mutex // guards w, next and waiting
	next    int64
	waiting map[int64]chan message
	done    chan struct{}
	err     error
}

// NewConn speaks on w and reads r until it ends; handle answers what
// comes in.
func NewConn(r io.Reader, w io.Writer, handle Handler) *Conn {
	c := &Conn{w: w, handle: handle, waiting: map[int64]chan message{}, done: make(chan struct{})}
	go c.read(r)
	return c
}

// Trace sets functions that see every line sent and read.
func (c *Conn) Trace(sent, read func(line []byte)) {
	c.mu.Lock()
	c.onWrite, c.onRead = sent, read
	c.mu.Unlock()
}

// Done is closed when the other side stopped; Err says why.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err is why the connection ended; nil while it runs, io.EOF for a plain
// end.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *Conn) send(m message) error {
	m.JSONRPC = "2.0"
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.onWrite != nil {
		c.onWrite(b)
	}
	_, err = c.w.Write(append(b, '\n'))
	return err
}

// Notify sends a notification.
func (c *Conn) Notify(method string, params any) error {
	p, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.send(message{Method: method, Params: p})
}

// Call sends a request and waits for its answer, stored in result (when
// not nil). An error answer is an *Error.
func (c *Conn) Call(ctx context.Context, method string, params, result any) error {
	p, err := json.Marshal(params)
	if err != nil {
		return err
	}
	ch := make(chan message, 1)
	c.mu.Lock()
	c.next++
	id := c.next
	c.waiting[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.waiting, id)
		c.mu.Unlock()
	}()
	if err := c.send(message{ID: &id, Method: method, Params: p}); err != nil {
		return err
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return m.Error
		}
		if result != nil && len(m.Result) > 0 {
			return json.Unmarshal(m.Result, result)
		}
		return nil
	case <-c.done:
		return fmt.Errorf("%s: the app stopped: %w", method, c.Err())
	case <-ctx.Done():
		return ctx.Err()
	}
}

// read takes the lines that come in until the stream ends.
func (c *Conn) read(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16<<20) // a render can be large
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		c.mu.Lock()
		if c.onRead != nil {
			c.onRead(line)
		}
		c.mu.Unlock()
		var m message
		if err := json.Unmarshal(line, &m); err != nil {
			c.send(message{ID: new(int64), Error: &Error{CodeParse, "not JSON: " + err.Error()}})
			continue
		}
		switch {
		case m.Method == "" && m.ID != nil: // an answer
			c.mu.Lock()
			ch := c.waiting[*m.ID]
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		case m.Method != "" && m.ID == nil:
			c.handle(m.Method, m.Params) // in order: renders must not overtake each other
		case m.Method != "":
			go c.answer(m)
		}
	}
	err := sc.Err()
	if err == nil {
		err = io.EOF
	}
	c.mu.Lock()
	c.err = err
	c.mu.Unlock()
	close(c.done)
}

// answer runs the handler for a request or notification.
func (c *Conn) answer(m message) {
	result, err := c.handle(m.Method, m.Params)
	if err != nil {
		var e *Error
		if !errors.As(err, &e) {
			e = &Error{CodeFailed, err.Error()}
		}
		c.send(message{ID: m.ID, Error: e})
		return
	}
	b, merr := json.Marshal(result)
	if merr != nil {
		c.send(message{ID: m.ID, Error: &Error{CodeFailed, merr.Error()}})
		return
	}
	c.send(message{ID: m.ID, Result: b})
}
