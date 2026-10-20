// Command inua-term is inua's terminal (docs/feat/inua-term.md): a window
// that is a client of the terminal host. inua-term --host runs the host,
// which the window starts by itself when none runs.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/agent"
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
	// -e runs the rest as a command, as terminals do (xdg-terminal-exec).
	var command []string
	for i, a := range os.Args[1:] {
		if a == "-e" || a == "--" {
			command = os.Args[i+2:]
			os.Args = os.Args[:i+1]
			break
		}
	}
	uiDir := flag.String("ui-dir", "", "read layouts and styles from this directory instead of the embedded copy")
	runHost := flag.Bool("host", false, "run the terminal host (the window starts it when needed)")
	workspace := flag.String("workspace", "", "open or show the workspace for this folder")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: inua-term [--workspace DIR] [-e COMMAND ARGS…]")
		fmt.Fprintln(os.Stderr, "       inua-term report|agents|integration …")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *runHost {
		os.Exit(serveHost())
	}
	if len(command) > 0 || *workspace != "" {
		// The host shows it; the window, opened or raised, follows.
		if err := show(command, *workspace); err != nil {
			fmt.Fprintf(os.Stderr, "inua-term: %v\n", err)
			os.Exit(1)
		}
	}
	if _, err := unix.IoctlGetTermios(2, unix.TCGETS); err != nil {
		logTo("term.log") // no terminal to tell: the dock's, the drawer's
	}
	termui.Register()
	code := appui.Run("term", flag.Args(), *uiDir)
	log.Printf("inua-term %d: ended (%d)", os.Getpid(), code)
	os.Exit(code)
}

// serveHost runs the host until its last pane is closed, logging to
// $XDG_STATE_HOME/inua/term-host.log.
func serveHost() int {
	logTo("term-host.log")
	h, err := host.Listen(host.SocketPath())
	if err != nil {
		log.Printf("inua-term: %v", err)
		return 1
	}
	h.ExitWhenEmpty = true
	if cfg, _, err := config.Load(config.DefaultPath()); err == nil {
		i18n.Init(cfg.Language) // the notifications', the Commands workspace's
	}
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
	h.SetShell(set.Shell.Program)
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

// show has the host show a command run in a session of its own, or the
// workspace for a folder.
func show(command []string, workspace string) error {
	c, err := host.Connect(nil, nil)
	if err != nil {
		return err
	}
	defer c.Close()
	if workspace != "" {
		dir, err := filepath.Abs(workspace)
		if err != nil {
			return err
		}
		if _, err := c.Open(dir, ""); err != nil {
			return err
		}
	}
	if len(command) > 0 {
		dir, _ := os.Getwd()
		_, err = c.Run(command, dir)
	}
	return err
}

// maxLog is how large a log grows before it is kept as NAME.1 and begun
// again.
const maxLog = 1 << 20

// logTo has the log, and everything written to the standard error (a Go
// panic, GTK's warnings), go to $XDG_STATE_HOME/inua/NAME: started from
// the dock or by the window, the host and the window have no terminal
// for them.
func logTo(name string) {
	state, err := os.UserHomeDir()
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		state = dir
	} else if err == nil {
		state = filepath.Join(state, ".local", "state")
	}
	dir := filepath.Join(state, "inua")
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	path := filepath.Join(dir, name)
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLog {
		os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	log.SetOutput(f)
	if err := syscall.Dup3(int(f.Fd()), 2, 0); err != nil {
		log.Printf("inua-term: the standard error stays: %v", err)
	}
	log.Printf("inua-term %d: started", os.Getpid())
}
