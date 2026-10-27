// Package web is inua-term serve: the host's terminals in a browser, a
// phone's over Tailscale. It serves a page (static/index.html, with
// xterm.js and the fonts it needs in static/vendor, from
// scripts/term-web.sh) and bridges each of its WebSockets to the host's
// socket, frame for frame: the page speaks the host's protocol as the
// window does. Every request but the page's files carries the token.
package web

import (
	"bytes"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"image/png"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/host"
	"github.com/lyricat/inua/ui"
)

//go:embed static
var static embed.FS

// Server serves the page and bridges it to the host.
type Server struct {
	// Socket is the host's.
	Socket string
	// Token is what every request carries (t=…); without it, nothing.
	Token string
}

// Handler is the server's routes.
func (s *Server) Handler() http.Handler {
	vendor, _ := fs.Sub(static, "static/vendor")
	files := http.FileServer(http.FS(vendor))
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.page)
	mux.Handle("/vendor/", http.StripPrefix("/vendor/", cached(files)))
	mux.HandleFunc("/icons/", s.icon)
	mux.HandleFunc("/icon-180.png", s.appIcon(180))
	mux.HandleFunc("/icon-192.png", s.appIcon(192))
	mux.HandleFunc("/icon-512.png", s.appIcon(512))
	mux.HandleFunc("/look.json", s.withToken(s.look))
	mux.HandleFunc("/manifest.webmanifest", s.withToken(s.manifest))
	mux.HandleFunc("/upload", s.withToken(s.upload))
	mux.HandleFunc("/files", s.withToken(s.files))
	mux.HandleFunc("/download", s.withToken(s.download))
	mux.HandleFunc("/ws", s.withToken(s.bridge))
	return mux
}

// cached lets a browser keep the page's fixed files a day.
func cached(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=86400")
		h.ServeHTTP(w, r)
	})
}

func (s *Server) tokenOK(r *http.Request) bool {
	return s.Token != "" && subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("t")), []byte(s.Token)) == 1
}

func (s *Server) withToken(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.tokenOK(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

// page is index.html with what the page needs from here put in: its
// text in the user's language, and the home folder (shown as ~).
func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if !s.tokenOK(r) {
		askToken(w, r.URL.Query().Has("t"))
		return
	}
	b, err := static.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	text, one, many := pageStrings()
	home, _ := os.UserHomeDir()
	data, _ := json.Marshal(map[string]any{"strings": text, "one": one, "many": many, "home": home, "lang": i18n.Lang()})
	b = bytes.Replace(b, []byte("/*INUA*/"), append(append([]byte("window.INUA = "), data...), ';'), 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b)
}

var tokenPage = template.Must(template.ParseFS(static, "static/token.html"))

// cssToken is a theme token as the token page can put it in its style.
var cssToken = regexp.MustCompile(`^[a-z0-9-]+$`)
var cssColor = regexp.MustCompile(`^(#[0-9a-fA-F]{3,8}|rgba?\([0-9., %]+\))$`)

// askToken is the page without its token, or with a wrong one (an old
// link's): a card in the window's theme asks for it, the token or the
// whole link.
func askToken(w http.ResponseWriter, wrong bool) {
	cfg, set := settingsNow()
	var tokens strings.Builder
	for k, v := range config.ThemeColors(cfg.ThemeCSS(windowTheme(cfg, set), readUI)) {
		if cssToken.MatchString(k) && cssColor.MatchString(v) {
			fmt.Fprintf(&tokens, "--%s:%s; ", k, v)
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusForbidden)
	tokenPage.Execute(w, map[string]any{
		"Lang":      i18n.Lang(),
		"Tokens":    template.CSS(tokens.String()),
		"Question":  i18n.T("The Web Terminal's token"),
		"Where":     i18n.T("It is in the link the computer gives: in inua-term's Terminal Settings, Web Terminal, Link. Paste the whole link or the part after t=."),
		"Wrong":     wrong,
		"WrongText": i18n.T("That token is not right. The link may be an old one, from before New link."),
		"Open":      i18n.T("Open"),
	})
}

var iconName = regexp.MustCompile(`^[a-z0-9-]+\.svg$`)

// icon is one of the shell's icons (ui/icons), by its file's name.
func (s *Server) icon(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path[len("/icons/"):]
	if !iconName.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	b, err := ui.FS.ReadFile("icons/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "max-age=86400")
	w.Write(b)
}

// appIcon is the web app's icon, n pixels square, in the theme's colours.
func (s *Server) appIcon(n int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		app, accent := themeColors()
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "max-age=3600")
		png.Encode(w, drawIcon(n, app, accent))
	}
}

// manifest makes the page a web app: its name, its icons, its colours,
// and where it starts (with the token).
func (s *Server) manifest(w http.ResponseWriter, r *http.Request) {
	app, _ := themeColors()
	w.Header().Set("Content-Type", "application/manifest+json")
	json.NewEncoder(w).Encode(map[string]any{
		"name": "inua-term", "short_name": "inua-term", "id": "/",
		"start_url": "/?t=" + url.QueryEscape(s.Token), "scope": "/", "display": "standalone",
		"background_color": colorHex(app), "theme_color": colorHex(app),
		"icons": []map[string]string{
			{"src": "/icon-192.png", "sizes": "192x192", "type": "image/png", "purpose": "any maskable"},
			{"src": "/icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "any maskable"},
		},
	})
}

func (s *Server) look(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(readLook())
}

var upgrader = websocket.Upgrader{
	EnableCompression: true, // the output compresses some sevenfold
	// The page's own origin only: a page elsewhere has no business here,
	// token or not.
	CheckOrigin: func(r *http.Request) bool {
		o := r.Header.Get("Origin")
		if o == "" {
			return true
		}
		u, err := url.Parse(o)
		return err == nil && u.Host == r.Host
	},
}

// A page gone without a word (a phone's browser in the background, its
// network lost) answers no ping: after pongWait its connection, and the
// host's, end.
const (
	pingEvery = 20 * time.Second
	pongWait  = 50 * time.Second
)

// bridge relays a WebSocket and a connection to the host: the bridge says
// hello, then each binary message is a frame (its kind, then its
// payload) one way, and each frame a message the other. The page sends
// control messages and input only.
func (s *Server) bridge(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	conn, err := net.Dial("unix", s.Socket)
	if err != nil {
		ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "the terminal host is not running"))
		return
	}
	defer conn.Close()
	hello, _ := json.Marshal(host.Message{Type: host.MsgHello, Version: host.Version, Web: true})
	if host.WriteFrame(conn, host.FrameControl, hello) != nil {
		return
	}
	log.Printf("inua-termd serve: %s connected", r.RemoteAddr)
	defer log.Printf("inua-termd serve: %s gone", r.RemoteAddr)
	ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(pongWait)) })
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		t := time.NewTicker(pingEvery)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
					return
				}
			}
		}
	}()
	go func() {
		defer ws.Close()
		for {
			kind, p, err := host.ReadFrame(conn)
			if err != nil {
				return
			}
			if ws.WriteMessage(websocket.BinaryMessage, append([]byte{kind}, p...)) != nil {
				return
			}
		}
	}()
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil || len(msg) < 1 {
			return
		}
		ws.SetReadDeadline(time.Now().Add(pongWait))
		if msg[0] != host.FrameControl && msg[0] != host.FrameInput {
			return
		}
		if host.WriteFrame(conn, msg[0], msg[1:]) != nil {
			return
		}
	}
}
