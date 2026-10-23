// Package ipc implements the shell's control socket, used by
// `inua call <target> <function> [args...]` and `inua show`. The wire
// format is one JSON request line, one JSON response line, then the server
// closes the connection.
package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/lyricat/inua/internal/rundir"
)

type Request struct {
	Op       string   `json:"op"` // "call" or "show"
	Target   string   `json:"target,omitempty"`
	Function string   `json:"function,omitempty"`
	Args     []string `json:"args,omitempty"`
}

type Response struct {
	OK     bool   `json:"ok"`
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// HandlerFunc serves one request. It runs on its own goroutine.
type HandlerFunc func(context.Context, Request) Response

// SocketPath returns the socket for a shell instance name, in the runtime
// directory (internal/rundir).
func SocketPath(instance string) string {
	return filepath.Join(rundir.Path(), instance+".sock")
}

// Listen creates a unix listener on path. A stale socket left by a dead process
// is replaced; a live one is an error so two shells do not share a name.
func Listen(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := rundir.Check(filepath.Dir(path)); err != nil { // no one else's socket, nor a link to one
		return nil, err
	}
	if conn, err := net.Dial("unix", path); err == nil {
		conn.Close()
		return nil, fmt.Errorf("%s: another instance is running", path)
	}
	os.Remove(path)
	return net.Listen("unix", path)
}

// ServeListener accepts connections on ln until ctx ends.
func ServeListener(ctx context.Context, ln net.Listener, path string, h HandlerFunc) error {
	stop := context.AfterFunc(ctx, func() { ln.Close() })
	defer stop()
	defer os.Remove(path)

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go serveConn(ctx, conn, h)
	}
}

// Serve listens on path until ctx ends.
func Serve(ctx context.Context, path string, h HandlerFunc) error {
	ln, err := Listen(path)
	if err != nil {
		return err
	}
	return ServeListener(ctx, ln, path, h)
}

func serveConn(ctx context.Context, conn net.Conn, h HandlerFunc) {
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var req Request
	resp := Response{}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err == nil {
		err = json.Unmarshal(line, &req)
	}
	if err != nil {
		resp.Error = "bad request: " + err.Error()
	} else {
		// The request's context ends when the caller hangs up: a request
		// waiting for the user (a passphrase) is given up then.
		conn.SetReadDeadline(time.Time{})
		rctx, cancel := context.WithCancel(ctx)
		go func() {
			conn.Read(make([]byte, 1))
			cancel()
		}()
		resp = h(rctx, req)
		cancel()
	}
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	json.NewEncoder(conn).Encode(resp)
}

// Call sends one request to the shell at path.
func Call(ctx context.Context, path string, req Request) (Response, error) {
	// A socket in someone else's directory could be theirs, listening for
	// what is sent (a passphrase, from inua-pinentry).
	if err := rundir.Check(filepath.Dir(path)); err != nil {
		return Response{}, err
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return Response{}, fmt.Errorf("no shell listening on %s: %w", path, err)
	}
	defer conn.Close()
	// Called off, the connection closes: the shell gives the request up.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Response{}, err
	}
	if !resp.OK && resp.Error != "" {
		return resp, errors.New(resp.Error)
	}
	return resp, nil
}
