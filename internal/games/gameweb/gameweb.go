// Package gameweb is what the games' web pages share (fortunenet,
// checkersnet): three.js, the files with their ETags, the addresses of
// this computer the page is opened at, and a browser's WebSocket.
package gameweb

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// vendor is three.js and its OrbitControls, served at /vendor/.
//
//go:embed vendor
var vendor embed.FS

// Files serves a page's files, each with its content's hash for an ETag:
// a browser asks again each time and gets a new file only when it
// changed, as after an update. / is index.html.
func Files(page fs.FS) http.Handler {
	tags := map[string]string{}
	fs.WalkDir(page, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := fs.ReadFile(page, p)
			sum := sha256.Sum256(b)
			tags["/"+p] = `"` + hex.EncodeToString(sum[:8]) + `"`
		}
		return nil
	})
	tags["/"] = tags["/index.html"]
	files := http.FileServerFS(page)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tag, ok := tags[r.URL.Path]; ok {
			w.Header().Set("ETag", tag)
		}
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
}

// Vendor serves three.js at /vendor/three/….
func Vendor() http.Handler { return Files(vendor) }

// URLs are the addresses a page on port is opened at, one for each of
// this computer's addresses on the local network (not its containers' or
// virtual machines' bridges), with the code after #; a home network's
// first.
func URLs(port int, code string) []string {
	var urls []string
	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 || bridge(i.Name) {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			ip, ok := a.(*net.IPNet)
			if !ok || ip.IP.To4() == nil || ip.IP.IsLinkLocalUnicast() {
				continue
			}
			urls = append(urls, fmt.Sprintf("http://%s:%d/#%s", ip.IP, port, code))
		}
	}
	sort.SliceStable(urls, func(i, j int) bool { return lanFirst(urls[i]) && !lanFirst(urls[j]) })
	return urls
}

// bridge reports whether an interface is a container's or a virtual
// machine's: Docker's, Podman's, libvirt's, LXD's.
func bridge(name string) bool {
	for _, p := range []string{"docker", "br-", "veth", "virbr", "podman", "cni", "lxdbr", "vnet"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// lanFirst: a home network's address (192.168.…) before a VPN's or a
// container bridge's.
func lanFirst(url string) bool {
	return len(url) > 15 && url[:15] == "http://192.168."
}

// Client is a browser connected: what it is sent waits in out for its
// writer.
type Client struct {
	Seat int // the game's: -1 until seated, or to watch

	ws   *websocket.Conn
	out  chan []byte
	done chan struct{}
	once sync.Once
}

var upgrader = websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 16384}

const (
	pingEvery = 20 * time.Second
	deadAfter = 60 * time.Second
)

// Accept takes a browser's WebSocket (from the same origin only), and
// starts writing to it.
func Accept(w http.ResponseWriter, r *http.Request) (*Client, error) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return nil, err
	}
	c := &Client{ws: ws, out: make(chan []byte, 256), done: make(chan struct{}), Seat: -1}
	go c.write()
	ws.SetReadLimit(4096)
	ws.SetReadDeadline(time.Now().Add(deadAfter))
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(deadAfter)) })
	return c, nil
}

// Read reads the browser's next message into v.
func (c *Client) Read(v any) error { return c.ws.ReadJSON(v) }

// Send queues a message for the browser; one that cannot keep up is
// dropped.
func (c *Client) Send(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	select {
	case <-c.done:
	case c.out <- b:
	default:
		c.Close()
	}
}

// Bye sends a last message, {"t": "bye", "text": text}, why the host is
// done with the browser, and closes it once that is sent.
func (c *Client) Bye(text string) {
	c.Send(struct {
		T    string `json:"t"`
		Text string `json:"text"`
	}{"bye", text})
	select {
	case c.out <- nil:
	default:
		c.Close()
	}
}

// Close closes the connection.
func (c *Client) Close() {
	c.once.Do(func() {
		close(c.done)
		c.ws.Close()
	})
}

func (c *Client) write() {
	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case b := <-c.out:
			if b == nil { // after a bye
				c.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
				c.Close()
				return
			}
			c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if c.ws.WriteMessage(websocket.TextMessage, b) != nil {
				c.Close()
				return
			}
		case <-ping.C:
			if c.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)) != nil {
				c.Close()
				return
			}
		case <-c.done:
			return
		}
	}
}
