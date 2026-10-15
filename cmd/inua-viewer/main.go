// Command inua-viewer is inua's viewer (docs/apps.md#viewer): inua-viewer
// [FILE|FOLDER ...] shows images, PDFs, Markdown and plain text. A single
// file steps through the files of its kind in its folder, a folder through
// its images, several files through those given. Each start is a window
// of its own.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/viewerui"
)

func main() {
	uiDir := flag.String("ui-dir", "", "read layouts and styles from this directory instead of the embedded copy")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: inua-viewer [-ui-dir DIR] [FILE|FOLDER ...]")
		flag.PrintDefaults()
	}
	flag.Parse()
	viewerui.Register()
	os.Exit(appui.Run("viewer", flag.Args(), *uiDir))
}
