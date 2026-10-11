// Command inua-apps runs one of inua's light apps: inua-apps clock |
// weather | stocks | calculator | calendar | color-picker |
// system-monitor | viewer; for Clock (world, alarms, stopwatch,
// timer) and System Monitor (overview, processes) a tab, and for Image
// Viewer the images (a file to step through its folder, a folder, or
// several files).
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/lyricat/inua/internal/appui"
)

func main() {
	uiDir := flag.String("ui-dir", "", "read layouts and styles from this directory instead of the embedded copy")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: inua-apps [-ui-dir DIR] clock|weather|stocks|calculator|calendar|color-picker|system-monitor [page]")
		fmt.Fprintln(os.Stderr, "       inua-apps [-ui-dir DIR] viewer [file|folder]...")
		fmt.Fprintln(os.Stderr, "       inua-apps [-ui-dir DIR] games [last-tile|full-fridge|clock-out|harbor|dice-shop|odd-groups|minesweeper|space-pinball|small-fortune]")
		fmt.Fprintln(os.Stderr, "       Each game can also be opened directly: inua-apps minesweeper")
	}
	flag.Parse()
	if flag.NArg() < 1 || (flag.Arg(0) != "viewer" && flag.NArg() > 2) {
		flag.Usage()
		os.Exit(2)
	}
	os.Exit(appui.Run(flag.Arg(0), flag.Args()[1:], *uiDir))
}
