// Package tailscale is the Tailscale app's logic: tailscaled's local API
// (its status, settings, pings, Taildrop files, logging in and out) over
// the daemon's socket, and what the app shows of it.
package tailscale

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Socket is where tailscaled listens on Linux.
const Socket = "/var/run/tailscale/tailscaled.sock"

// ErrPermission is a change refused because the user is not tailscaled's
// operator; `tailscale set --operator=USER`, as root, makes them one.
var ErrPermission = errors.New("tailscale: not allowed to change Tailscale")

// ErrNotRunning is a daemon that does not answer.
var ErrNotRunning = errors.New("tailscale: tailscaled is not running")

// Client talks to tailscaled's local API.
type Client struct {
	Socket string
	http   *http.Client
}

// NewClient talks to the daemon at socket, Socket if "".
func NewClient(socket string) *Client {
	if socket == "" {
		socket = Socket
	}
	c := &Client{Socket: socket}
	c.http = &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", c.Socket)
		},
	}}
	return c
}

// do sends a request to the local API and decodes its JSON answer into
// out, if out is not nil.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader, size int64, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, "http://local-tailscaled.sock/localapi/v0/"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Sec-Tailscale", "localapi") // tailscaled asks it of changes
	if size >= 0 && body != nil {
		req.ContentLength = size
	}
	resp, err := c.http.Do(req)
	if err != nil {
		var op *net.OpError
		if errors.As(err, &op) {
			return ErrNotRunning
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return ErrPermission
	}
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("tailscale: %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	if out == nil {
		_, err = io.Copy(io.Discard, resp.Body)
		return err
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Status is the daemon's state, this device's and its peers'.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var st Status
	err := c.do(ctx, http.MethodGet, "status", nil, -1, &st)
	return st, err
}

// Prefs are this device's settings.
func (c *Client) Prefs(ctx context.Context) (Prefs, error) {
	var p Prefs
	err := c.do(ctx, http.MethodGet, "prefs", nil, -1, &p)
	return p, err
}

// EditPrefs changes the settings edit names, and returns them all after.
func (c *Client) EditPrefs(ctx context.Context, edit Edit) (Prefs, error) {
	body, err := json.Marshal(edit)
	if err != nil {
		return Prefs{}, err
	}
	var p Prefs
	err = c.do(ctx, http.MethodPatch, "prefs", bytes.NewReader(body), int64(len(body)), &p)
	return p, err
}

// LoginInteractive starts logging in; the address to visit shows in the
// status as AuthURL.
func (c *Client) LoginInteractive(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "login-interactive", nil, -1, nil)
}

// Logout logs this device out of its tailnet.
func (c *Client) Logout(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "logout", nil, -1, nil)
}

// PingResult is how a ping went.
type PingResult struct {
	NodeName       string
	Err            string
	LatencySeconds float64
	Endpoint       string // the address answered from, when direct
	DERPRegionCode string // the relay, when not
}

// Ping pings a peer through Tailscale, as `tailscale ping` does.
func (c *Client) Ping(ctx context.Context, ip string) (PingResult, error) {
	var r PingResult
	err := c.do(ctx, http.MethodPost, "ping?"+url.Values{"ip": {ip}, "type": {"disco"}}.Encode(), nil, -1, &r)
	return r, err
}

// SendFile sends a file to a peer with Taildrop.
func (c *Client) SendFile(ctx context.Context, peerID, name string, r io.Reader, size int64) error {
	return c.do(ctx, http.MethodPut, "file-put/"+url.PathEscape(peerID)+"/"+url.PathEscape(name), r, size, nil)
}

// WaitingFile is a file Taildrop received, waiting to be saved.
type WaitingFile struct {
	Name string
	Size int64
}

// WaitingFiles are the files received and not saved yet.
func (c *Client) WaitingFiles(ctx context.Context) ([]WaitingFile, error) {
	var fs []WaitingFile
	err := c.do(ctx, http.MethodGet, "files/", nil, -1, &fs)
	return fs, err
}

// SaveFile writes a waiting file to w, then lets the daemon forget it.
func (c *Client) SaveFile(ctx context.Context, name string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://local-tailscaled.sock/localapi/v0/files/"+url.PathEscape(name), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Sec-Tailscale", "localapi")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return ErrPermission
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("tailscale: %s", resp.Status)
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, "files/"+url.PathEscape(name), nil, -1, nil)
}

// Timeout bounds a call that should be quick.
func Timeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}
