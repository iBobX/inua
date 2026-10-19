// Command inua-term is inua's terminal (docs/feat/inua-term.md): a window
// that is a client of the terminal host. inua-term --host runs the host,
// which the window starts by itself when none runs.
package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/term/host"
	"github.com/lyricat/inua/internal/termui"
)

func main() {
	uiDir := flag.String("ui-dir", "", "read layouts and styles from this directory instead of the embedded copy")
	runHost := flag.Bool("host", false, "run the terminal host (the window starts it when needed)")
	flag.Parse()
	if *runHost {
		os.Exit(serveHost())
	}
	termui.Register()
	os.Exit(appui.Run("term", flag.Args(), *uiDir))
}

// serveHost runs the host until its last pane is closed, logging to
// $XDG_STATE_HOME/inua/term-host.log.
func serveHost() int {
	state, err := os.UserHomeDir()
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		state = dir
	} else if err == nil {
		state = filepath.Join(state, ".local", "state")
	}
	if err := os.MkdirAll(filepath.Join(state, "inua"), 0o700); err == nil {
		if f, err := os.OpenFile(filepath.Join(state, "inua", "term-host.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			log.SetOutput(f)
		}
	}
	h, err := host.Listen(host.SocketPath())
	if err != nil {
		log.Printf("inua-term: %v", err)
		return 1
	}
	h.ExitWhenEmpty = true
	log.Printf("inua-term: host on %s", host.SocketPath())
	if err := h.Serve(); err != nil {
		log.Printf("inua-term: %v", err)
		return 1
	}
	return 0
}
