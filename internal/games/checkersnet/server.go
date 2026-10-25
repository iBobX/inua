package checkersnet

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"time"

	"github.com/lyricat/inua/internal/games/gameweb"
)

// web is the page visitors play on.
//
//go:embed web
var web embed.FS

// DefaultAddr is where a Host listens unless told otherwise: every
// address of this computer, on a port of its own (Small Fortune's, plus
// one).
const DefaultAddr = ":7342"

type server struct {
	http *http.Server
	port int
}

// Serve listens on addr (DefaultAddr; another port when it is taken) for
// browsers: the page at /, the game at /ws. Called where the Host lives,
// once.
func (h *Host) Serve(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil && addr == DefaultAddr {
		ln, err = net.Listen("tcp", ":0")
	}
	if err != nil {
		return err
	}
	page, _ := fs.Sub(web, "web")
	mux := http.NewServeMux()
	mux.Handle("/", gameweb.Files(page))
	mux.Handle("/vendor/", gameweb.Vendor())
	mux.HandleFunc("/ws", h.serveWS)
	mux.HandleFunc("/texts.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(webTexts())
	})
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
	h.stop()
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
		case m.T == "move" && joined:
			h.opt.Run(func() {
				if h.clients[c] && c.Seat >= 0 {
					h.Act(c.Seat, m.From, m.To)
				}
			})
		case joined:
			h.opt.Run(func() {
				if h.clients[c] {
					h.fromLead(c, m)
				}
			})
		}
	}
	c.Close()
	h.opt.Run(func() { h.leave(c) })
}
