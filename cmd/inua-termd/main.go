// Command inua-termd is inua-term's host (docs/terminal.md): it keeps the
// terminals, their workspaces and sessions, and the windows and pages are
// its clients over its socket. inua-term starts it when none runs; it
// ends with its last pane. inua-termd serve shows the terminals in a
// browser. It needs no GTK and no desktop: it runs on a computer without
// one.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/logfile"
	"github.com/lyricat/inua/internal/term/agent"
	"github.com/lyricat/inua/internal/term/desktop"
	"github.com/lyricat/inua/internal/term/host"
	"github.com/lyricat/inua/internal/term/settings"
	"github.com/lyricat/inua/internal/term/shellint"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "serve":
			os.Exit(serve(os.Args[2:]))
		default:
			fmt.Fprintln(os.Stderr, "usage: inua-termd          run the terminal host (inua-term starts it when needed)")
			fmt.Fprintln(os.Stderr, "       inua-termd serve …  the terminals in a browser")
			os.Exit(2)
		}
	}
	os.Exit(serveHost())
}

// serveHost runs the host until its last pane is closed, logging to
// $XDG_STATE_HOME/inua/term-host.log.
func serveHost() int {
	logfile.To("term-host.log")
	h, err := host.Listen(host.SocketPath())
	if err != nil {
		log.Printf("inua-termd: %v", err)
		return 1
	}
	h.ExitWhenEmpty = true
	if cfg, _, err := config.Load(config.DefaultPath()); err == nil {
		i18n.Init(cfg.Language) // the notifications', the Commands workspace's
	}
	set, warnings, err := settings.Load(settings.Path())
	if err != nil {
		log.Printf("inua-termd: %v", err)
	}
	for _, w := range warnings {
		log.Printf("inua-termd: %s: %s", settings.Path(), w)
	}
	h.SetReplayLimit(set.Terminal.Replay << 20)
	if set.Shell.Integration {
		if err := shellint.Install(shellint.Dir()); err != nil {
			log.Printf("inua-termd: no shell integration: %v", err)
		} else {
			h.ShellDir = shellint.Dir()
		}
	}
	h.SetShell(set.Shell.Program)
	known, errs := agent.Load(agent.Dir())
	for _, err := range errs {
		log.Printf("inua-termd: agents: %v", err)
	}
	h.Agents, h.Notify, h.Desktop = known, set.Agents.Notifications, desktop.New()
	if err := h.Remember(host.StatePath(), set.Remember.Forget, set.Remember.Commands); err != nil {
		log.Printf("inua-termd: remembering: %v", err)
	}
	// Ending the session (a logout, a shutdown) keeps the sessions first.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	go func() {
		<-sig
		h.Close()
	}()
	log.Printf("inua-termd: host on %s", host.SocketPath())
	if err := h.Serve(); err != nil {
		log.Printf("inua-termd: %v", err)
		return 1
	}
	return 0
}
