// Command inua-term is inua's terminal (docs/terminal.md): a window that
// is a client of the terminal host, inua-termd, which the window starts by
// itself when none runs.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/logfile"
	"github.com/lyricat/inua/internal/term/host"
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
	workspace := flag.String("workspace", "", "open or show the workspace for this folder")
	quick := flag.Bool("quick", false, "open a terminal for a moment: one window, one shell, no workspace or session, nothing kept")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: inua-term [--workspace DIR] [-e COMMAND ARGS…]")
		fmt.Fprintln(os.Stderr, "       inua-term --quick [-e COMMAND ARGS…]")
		fmt.Fprintln(os.Stderr, "       inua-term report|agents|integration …")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *quick {
		if _, err := unix.IoctlGetTermios(2, unix.TCGETS); err != nil {
			logfile.To("term.log")
		}
		termui.QuickCommand = command
		termui.RegisterQuick()
		os.Exit(appui.Run("term-quick", flag.Args(), *uiDir))
	}
	if len(command) > 0 || *workspace != "" {
		// The host shows it; the window, opened or raised, follows.
		if err := show(command, *workspace); err != nil {
			fmt.Fprintf(os.Stderr, "inua-term: %v\n", err)
			os.Exit(1)
		}
	}
	if _, err := unix.IoctlGetTermios(2, unix.TCGETS); err != nil {
		logfile.To("term.log") // no terminal to tell: the dock's, the drawer's
	}
	termui.Register()
	code := appui.Run("term", flag.Args(), *uiDir)
	log.Printf("inua-term %d: ended (%d)", os.Getpid(), code)
	os.Exit(code)
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
