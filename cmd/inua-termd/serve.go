package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/host"
	"github.com/lyricat/inua/internal/term/web"
	"github.com/lyricat/inua/internal/term/webterm"
)

// serve shows the host's terminals in a browser (internal/term/web): on
// this computer only, unless told an address; with --tailscale, on this
// computer's Tailscale address, for a phone on the tailnet.
func serve(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:7681", "where to listen")
	tailnet := fs.Bool("tailscale", false, "listen on this computer's Tailscale address (port from --addr)")
	renew := fs.Bool("new-token", false, "make a new token: links with the old one stop working")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: inua-termd serve [--addr HOST:PORT] [--tailscale] [--new-token]")
		fs.PrintDefaults()
	}
	fs.Parse(args)
	if cfg, _, err := config.Load(config.DefaultPath()); err == nil {
		i18n.Init(cfg.Language)
	}
	listen := *addr
	if *tailnet {
		ip, err := webterm.TailscaleIP()
		if err != nil {
			fmt.Fprintf(os.Stderr, "inua-termd serve: %v\n", err)
			return 1
		}
		_, port, _ := net.SplitHostPort(*addr)
		listen = net.JoinHostPort(ip, port)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	token, err := webterm.Token(ctx, *renew)
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "inua-termd serve: %v\n", err)
		return 1
	}
	// The host, started if it is not running: the page finds it there.
	c, err := host.Connect(nil, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "inua-termd serve: the terminal host: %v\n", err)
		return 1
	}
	c.Close()
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "inua-termd serve: %v\n", err)
		return 1
	}
	s := &web.Server{Socket: host.SocketPath(), Token: token}
	fmt.Printf("http://%s/?t=%s\n", ln.Addr(), url.QueryEscape(token))
	log.Printf("inua-termd serve: on %s", ln.Addr())
	if err := http.Serve(ln, s.Handler()); err != nil && !errors.Is(err, net.ErrClosed) {
		fmt.Fprintf(os.Stderr, "inua-termd serve: %v\n", err)
		return 1
	}
	return 0
}
