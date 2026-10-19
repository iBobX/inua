// Command inua-term is inua's terminal (docs/feat/inua-term.md). This is
// the first spike: one window, one pane, the user's shell.
package main

import (
	"flag"
	"os"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/termui"
)

func main() {
	uiDir := flag.String("ui-dir", "", "read layouts and styles from this directory instead of the embedded copy")
	flag.Parse()
	termui.Register()
	os.Exit(appui.Run("term", flag.Args(), *uiDir))
}
