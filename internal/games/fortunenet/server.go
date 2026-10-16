package fortunenet

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	samples "github.com/lyricat/inua/config"
)

// web is the page visitors play on.
//
//go:embed web
var web embed.FS

// DefaultAddr is where a Host listens unless told otherwise: every
// address of this computer, on a port of its own.
const DefaultAddr = ":7341"

// pageHandler serves the page's files, each with its content's hash for
// an ETag: a browser asks again each time and gets a new file only when
// it changed, as after an update.
func pageHandler() http.Handler {
	page, _ := fs.Sub(web, "web")
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

type server struct {
	http *http.Server
	port int
}

// Serve listens on addr (DefaultAddr; another port when it is taken)
// for browsers: the page at /, the game at /ws. Called where the Host
// lives, once.
func (h *Host) Serve(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil && addr == DefaultAddr {
		ln, err = net.Listen("tcp", ":0")
	}
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/", pageHandler())
	mux.HandleFunc("/ws", h.serveWS)
	mux.HandleFunc("/board.jpg", h.servePrint)
	mux.HandleFunc("/texts.json", serveTexts)
	mux.HandleFunc("/art/", h.serveArt)
	mux.HandleFunc("/icons/", serveIcon)
	h.srv = &server{http: &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}, port: ln.Addr().(*net.TCPAddr).Port}
	go h.srv.http.Serve(ln)
	return nil
}

// Port is the port the Host listens on; 0 before Serve.
func (h *Host) Port() int {
	if h.srv == nil {
		return 0
	}
	return h.srv.port
}

// URLs are the addresses visitors open, one for each of this computer's
// addresses on the local network (not its containers' or virtual
// machines' bridges), with the code.
func (h *Host) URLs() []string {
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
			urls = append(urls, fmt.Sprintf("http://%s:%d/#%s", ip.IP, h.Port(), h.Code))
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

// Close stops the game: the timers, every browser, the server. Called
// where the Host lives.
func (h *Host) Close() {
	h.closed = true
	if h.opt.AI != nil {
		h.opt.AI.Stop()
	}
	if h.cpu != nil {
		h.cpu.Stop()
	}
	for _, s := range h.Seats {
		if s.gone != nil {
			s.gone.Stop()
		}
	}
	for c := range h.clients {
		c.close()
	}
	h.clients = map[*client]bool{}
	if h.srv != nil {
		go h.srv.http.Close()
	}
}

// client is a browser connected: what it is sent waits in out for its
// writer; its reader hands what it sends to the Host.
type client struct {
	ws   *websocket.Conn
	out  chan []byte
	done chan struct{}
	once sync.Once
	seat int // -1 until seated, or to watch
}

var upgrader = websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 16384}

const (
	pingEvery = 20 * time.Second
	deadAfter = 60 * time.Second
)

func (h *Host) serveWS(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil) // same origin only
	if err != nil {
		return
	}
	c := &client{ws: ws, out: make(chan []byte, 256), done: make(chan struct{}), seat: -1}
	go c.write()
	ws.SetReadLimit(4096)
	ws.SetReadDeadline(time.Now().Add(deadAfter))
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(deadAfter)) })
	joined := false
	for {
		var m inMsg
		if err := ws.ReadJSON(&m); err != nil {
			break
		}
		switch {
		case m.T == "join" && !joined:
			joined = true
			h.opt.Run(func() {
				if !h.closed {
					h.join(c, m)
				}
			})
		case m.T == "act" && joined:
			h.opt.Run(func() {
				if h.clients[c] && c.seat >= 0 {
					h.Act(c.seat, m.Action)
				}
			})
		}
	}
	c.close()
	h.opt.Run(func() { h.leave(c) })
}

// servePrint serves the board's print, drawn by the host's screen the
// first time it is asked for.
func (h *Host) servePrint(w http.ResponseWriter, r *http.Request) {
	ch := make(chan []byte, 1)
	h.opt.Run(func() {
		if h.print == nil && h.Game != nil && h.opt.Print != nil && !h.closed {
			h.print = h.opt.Print(h.Game.City)
		}
		ch <- h.print
	})
	var b []byte
	select {
	case b = <-ch:
	case <-time.After(30 * time.Second):
	case <-r.Context().Done():
		return
	}
	if b == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(b)
}

// artName is a picture the page may ask for (Options.Art).
var artName = regexp.MustCompile(`^(card-\d{1,2}(-price)?|card-back|sign-\d{1,3}|(house|hotel)-[01]-[0-3]|piece-\d{1,3})$`)

// serveArt serves a picture of the game's, drawn by the host's screen the
// first time it is asked for: /art/NAME.png.
func (h *Host) serveArt(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/art/"), ".png")
	if !artName.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	ch := make(chan *Picture, 1)
	h.opt.Run(func() {
		pic, ok := h.art[name]
		if !ok && h.Game != nil && h.opt.Art != nil && !h.closed {
			pic = h.opt.Art(h.Game.City, name)
			if h.art == nil {
				h.art = map[string]*Picture{}
			}
			h.art[name] = pic
		}
		ch <- pic
	})
	var pic *Picture
	select {
	case pic = <-ch:
	case <-time.After(30 * time.Second):
	case <-r.Context().Done():
		return
	}
	if pic == nil || pic.PNG == nil {
		http.NotFound(w, r)
		return
	}
	b := pic.PNG
	if pic.W > 0 { // a piece standing on the board: its size and foot
		w.Header().Set("X-Piece", fmt.Sprintf("%.2f %.2f %.2f %.2f", pic.W, pic.H, pic.FootX, pic.Dv))
	}
	sum := sha256.Sum256(b)
	w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:8])+`"`)
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, name+".png", time.Time{}, bytes.NewReader(b))
}

var iconName = regexp.MustCompile(`^(fortune-[a-z]+|audio-volume-(high|muted))\.svg$`)

// serveIcon serves one of the game's icons, the shell's own in Phosphor's
// bold weight (inua's sample icon set): /icons/fortune-house.svg. The
// regular weight's hairlines all but vanish in a browser at their size.
func serveIcon(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/icons/")
	if !iconName.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	b, err := fs.ReadFile(samples.FS, samples.IconSet+"/"+strings.TrimSuffix(name, ".svg")+"-symbolic.svg")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "max-age=86400")
	w.Write(b)
}

// send queues a message for the browser; one that cannot keep up is
// dropped.
func (c *client) send(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	select {
	case <-c.done:
	case c.out <- b:
	default:
		c.close()
	}
}

// bye sends a last message, why the host is done with the browser, and
// closes it once that is sent.
func (c *client) bye(text string) {
	c.send(byeMsg{T: "bye", Text: text})
	select {
	case c.out <- nil:
	default:
		c.close()
	}
}

func (c *client) close() {
	c.once.Do(func() {
		close(c.done)
		c.ws.Close()
	})
}

func (c *client) write() {
	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case b := <-c.out:
			if b == nil { // after a bye
				c.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
				c.close()
				return
			}
			c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if c.ws.WriteMessage(websocket.TextMessage, b) != nil {
				c.close()
				return
			}
		case <-ping.C:
			if c.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)) != nil {
				c.close()
				return
			}
		case <-c.done:
			return
		}
	}
}
