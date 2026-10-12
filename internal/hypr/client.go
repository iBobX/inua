// Package hypr talks to Hyprland over its two unix sockets and keeps a
// snapshot of monitors, workspaces and toplevels in sync with the compositor.
//
// Hyprland is the only source of truth: the store never guesses state after
// issuing a dispatch, it waits for the resulting events and re-queries.
package hypr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Paths holds the request (.socket.sock) and event (.socket2.sock) sockets of
// one Hyprland instance.
type Paths struct {
	Request string
	Event   string
}

// PathsFromEnv resolves the sockets of the instance this process runs under.
func PathsFromEnv() (Paths, error) {
	sig := os.Getenv("HYPRLAND_INSTANCE_SIGNATURE")
	if sig == "" {
		return Paths{}, errors.New("HYPRLAND_INSTANCE_SIGNATURE is not set; not running under Hyprland")
	}
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		runtime = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	dir := filepath.Join(runtime, "hypr", sig)
	return Paths{
		Request: filepath.Join(dir, ".socket.sock"),
		Event:   filepath.Join(dir, ".socket2.sock"),
	}, nil
}

// Client issues one-shot requests on the request socket. Each request opens a
// new connection; Hyprland closes it after writing the reply.
type Client struct {
	Paths   Paths
	Timeout time.Duration
}

func NewClient(p Paths) *Client {
	return &Client{Paths: p, Timeout: 2 * time.Second}
}

// Request sends a raw command such as "j/clients" or "dispatch workspace 2"
// and returns the full reply.
func (c *Client) Request(ctx context.Context, cmd string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.Paths.Request)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}
	if _, err := io.WriteString(conn, cmd); err != nil {
		return nil, err
	}
	return io.ReadAll(conn)
}

// Dispatch runs a dispatcher, e.g. Dispatch(ctx, "focuswindow address:0x1").
// Hyprland answers "ok" on success and an error message otherwise.
func (c *Client) Dispatch(ctx context.Context, args string) error {
	out, err := c.Request(ctx, "dispatch "+args)
	if err != nil {
		return err
	}
	if reply := strings.TrimSpace(string(out)); reply != "ok" {
		return fmt.Errorf("dispatch %q: %s", args, reply)
	}
	return nil
}

// LuaDispatch issues a dispatcher expression for Hyprland 0.55+, whose IPC
// dispatch interface accepts Lua expressions instead of the legacy text form.
func (c *Client) LuaDispatch(ctx context.Context, expression string) error {
	out, err := c.Request(ctx, "dispatch "+expression)
	if err != nil {
		return err
	}
	if reply := strings.TrimSpace(string(out)); reply != "ok" {
		return fmt.Errorf("lua dispatch %q: %s", expression, reply)
	}
	return nil
}
