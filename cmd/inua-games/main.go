// Command inua-games runs one of inua's light games, each its own app
// with its own window: inua-games minesweeper | space-pinball |
// small-fortune | chinese-checkers.
package main

import (
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gamesui"
)

func main() {
	uiDir := flag.String("ui-dir", "", "read layouts and styles from this directory instead of the embedded copy")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: inua-games [-ui-dir DIR] GAME")
		fmt.Fprintln(os.Stderr, "games: "+strings.Join(gamesui.Names(), ", "))
	}
	flag.Parse()
	if flag.NArg() != 1 || !slices.Contains(gamesui.Names(), flag.Arg(0)) {
		flag.Usage()
		os.Exit(2)
	}
	gamesui.Register()
	os.Exit(appui.Run(flag.Arg(0), nil, *uiDir))
}
