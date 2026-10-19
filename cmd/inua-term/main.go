// Command inua-term is inua's terminal (docs/feat/inua-term.md): a window
// that is a client of the terminal host. inua-term --host runs the host,
// which the window starts by itself when none runs.
package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/agent"
	"github.com/lyricat/inua/internal/term/hooks"
	"github.com/lyricat/inua/internal/term/host"
	"github.com/lyricat/inua/internal/term/settings"
	"github.com/lyricat/inua/internal/term/shellint"
	"github.com/lyricat/inua/internal/termui"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "report":
			os.Exit(report(os.Args[2:]))
		case "agents":
			os.Exit(agents(os.Args[2:]))
		case "integration":
			os.Exit(integration(os.Args[2:]))
		}
	}
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
	set, warnings, err := settings.Load(settings.Path())
	if err != nil {
		log.Printf("inua-term: %v", err)
	}
	for _, w := range warnings {
		log.Printf("inua-term: %s: %s", settings.Path(), w)
	}
	h.SetReplayLimit(set.Terminal.Replay << 20)
	if set.Shell.Integration {
		if err := shellint.Install(shellint.Dir()); err != nil {
			log.Printf("inua-term: no shell integration: %v", err)
		} else {
			h.ShellDir = shellint.Dir()
		}
	}
	if set.Agents.Hooks {
		h.Hook = func(t string) { addHooks(h, t) }
	}
	known, errs := agent.Load(agent.Dir())
	for _, err := range errs {
		log.Printf("inua-term: agents: %v", err)
	}
	h.Agents, h.Notify = known, set.Agents.Notifications
	if err := h.Remember(host.StatePath(), set.Remember.Forget, set.Remember.Commands); err != nil {
		log.Printf("inua-term: remembering: %v", err)
	}
	// Ending the session (a logout, a shutdown) keeps the sessions first.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	go func() {
		<-sig
		h.Close()
	}()
	log.Printf("inua-term: host on %s", host.SocketPath())
	if err := h.Serve(); err != nil {
		log.Printf("inua-term: %v", err)
		return 1
	}
	return 0
}

// addHooks adds inua-term's hooks to an agent's settings, the first time
// the host sees it, and says so.
func addHooks(h *host.Host, t string) {
	exe := "inua-term"
	if p, err := exec.LookPath("inua-term"); err != nil || !sameFile(p) {
		if self, err := os.Executable(); err == nil {
			exe = self
		}
	}
	var path string
	var changed bool
	var err error
	switch t {
	case "claude":
		path = hooks.ClaudePath()
		changed, err = hooks.InstallClaude(path, exe)
	case "codex":
		path = hooks.CodexPath()
		changed, err = hooks.InstallCodex(path, exe)
	default:
		return
	}
	if err != nil {
		log.Printf("inua-term: hooks: %v", err)
		return
	}
	if changed {
		log.Printf("inua-term: added its hooks to %s", path)
		h.Announce(i18n.T("inua-term added its hooks"),
			i18n.Tf("%s now reports its state to inua-term. The file before is %s.bak; `inua-term integration remove %s` takes them out.", path, path, t))
	}
}

// sameFile says the inua-term on the PATH is this one.
func sameFile(p string) bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	a, err1 := os.Stat(p)
	b, err2 := os.Stat(self)
	return err1 == nil && err2 == nil && os.SameFile(a, b)
}
