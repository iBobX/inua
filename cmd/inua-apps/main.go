// Command inua-apps runs one of inua's light apps: inua-apps clock |
// weather | stocks | calculator | calendar | color-picker |
// system-monitor | tailscale | pass | gpg | music | video; for Clock
// (world, alarms, stopwatch, timer) and System Monitor (overview,
// processes) a tab, for Music files and folders to play, for Video a
// file. inua-apps workshop ID|DIR runs a workshop app. Viewer is a
// program of its own, inua-viewer.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/lightui"
)

func main() {
	uiDir := flag.String("ui-dir", "", "read layouts and styles from this directory instead of the embedded copy")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: inua-apps [-ui-dir DIR] clock|weather|stocks|calculator|calendar|color-picker|system-monitor|tailscale|pass|gpg [page]")
		fmt.Fprintln(os.Stderr, "       inua-apps [-ui-dir DIR] music [file|folder|playlist]...")
		fmt.Fprintln(os.Stderr, "       inua-apps [-ui-dir DIR] video [file]")
		fmt.Fprintln(os.Stderr, "       inua-apps [-ui-dir DIR] workshop [--dev] ID|DIR")
	}
	flag.Parse()
	if flag.NArg() < 1 || (flag.Arg(0) != "workshop" && flag.Arg(0) != "music" && flag.NArg() > 2) {
		flag.Usage()
		os.Exit(2)
	}
	if flag.Arg(0) == "workshop" {
		os.Exit(lightui.RunWorkshop(flag.Args()[1:], *uiDir))
	}
	os.Exit(appui.Run(flag.Arg(0), flag.Args()[1:], *uiDir))
}
