package fortunenet

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	samples "github.com/lyricat/inua/config"
	"github.com/lyricat/inua/internal/games/gameweb"
)

// web is the page visitors play on.
//
//go:embed web
var web embed.FS

// DefaultAddr is where a Host listens unless told otherwise: every
// address of this computer, on a port of its own.
const DefaultAddr = ":7341"

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
	page, _ := fs.Sub(web, "web")
	mux.Handle("/", gameweb.Files(page))
	mux.Handle("/vendor/", gameweb.Vendor())
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
// addresses on the local network, with the code.
func (h *Host) URLs() []string { return gameweb.URLs(h.Port(), h.Code) }

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
		c.Close()
	}
	h.clients = map[*gameweb.Client]bool{}
	if h.srv != nil {
		go h.srv.http.Close()
	}
}

func (h *Host) serveWS(w http.ResponseWriter, r *http.Request) {
	c, err := gameweb.Accept(w, r)
	if err != nil {
		return
	}
	joined := false
	for {
		var m inMsg
		if err := c.Read(&m); err != nil {
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
				if h.clients[c] && c.Seat >= 0 {
					h.Act(c.Seat, m.Action)
				}
			})
		}
	}
	c.Close()
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
