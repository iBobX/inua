package termui

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/vt"
)

// sheetLines is how many of the text's lines the paste sheet shows.
const sheetLines = 6

// clipboard is the pane's clipboard, or its primary selection. The caller
// unrefs it.
func (p *termPane) clipboard(primary bool) *gdk.Clipboard {
	if primary {
		return p.widget.GetPrimaryClipboard()
	}
	return p.widget.GetClipboard()
}

// pasteFrom pastes what the clipboard (or the primary selection) holds:
// files as their paths, an image alone as a file of its own, else text.
func (p *termPane) pasteFrom(primary bool) {
	if p.exited {
		return
	}
	which := 0
	if primary {
		which = 1
	}
	clip := p.clipboard(primary)
	defer clip.Unref()
	formats := clip.GetFormats()
	switch {
	case formats.ContainMimeType("text/uri-list") || formats.ContainGtype(gdk.FileListGLibType()):
		clip.ReadValueAsync(gdk.FileListGLibType(), 0, nil, &p.onFiles[which], 0)
	case imageMime(formats) != "" && !primary:
		p.pasteImage(imageMime(formats))
	default:
		clip.ReadTextAsync(nil, &p.onText[which], 0)
	}
}

// imageMime is the image type the clipboard offers, PNG first, when it
// offers no text (a browser's image comes with its HTML, which is not
// what a terminal wants either); "" for none.
func imageMime(f *gdk.ContentFormats) string {
	var n uint
	mimes := f.GetMimeTypes(&n)
	image := ""
	for _, m := range mimes {
		switch {
		case strings.HasPrefix(m, "text/plain"):
			return ""
		case m == "image/png":
			image = m
		case strings.HasPrefix(m, "image/") && image == "":
			image = m
		}
	}
	return image
}

// pasteImage saves the clipboard's image to a file and pastes its path:
// for programs that take images as files. (An agent that reads the
// clipboard itself, as Claude Code on Ctrl+V, gets that key as typed.)
// The bytes are wl-paste's, as the Files app reads the clipboard, off
// the main thread.
func (p *termPane) pasteImage(mime string) {
	go func() {
		path, err := saveImage(mime)
		gtkx.Post(func() {
			if err != nil {
				log.Printf("term: paste image: %v", err)
				return
			}
			if !p.closed && !p.exited {
				p.paste(quotePaths([]string{path}), false, false)
			}
		})
	}()
}

// saveImage writes the clipboard's image of type mime to
// $XDG_RUNTIME_DIR/inua/term-paste/, which goes with the session.
func saveImage(mime string) (string, error) {
	out, err := exec.Command("wl-paste", "--no-newline", "--type", mime).Output()
	if err != nil {
		return "", fmt.Errorf("wl-paste: %w", err)
	}
	dir := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "inua", "term-paste")
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		dir = filepath.Join(os.TempDir(), "inua-term-paste")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	ext := strings.TrimPrefix(mime, "image/")
	if ext == "jpeg" {
		ext = "jpg"
	}
	path := filepath.Join(dir, time.Now().Format("2006-01-02-150405.000")+"."+ext)
	return path, os.WriteFile(path, out, 0o600)
}

// filesRead is a file list's read end: the paths, quoted for the shell.
func (p *termPane) filesRead(primary bool, result uintptr) {
	clip := p.clipboard(primary)
	v, err := clip.ReadValueFinish(&gio.AsyncResultBase{Ptr: result})
	clip.Unref()
	if p.closed || p.exited || err != nil || v == nil {
		return
	}
	if paths := filePaths(v); len(paths) > 0 {
		p.paste(quotePaths(paths), primary, false)
	}
}

// filePaths are the local files in a GdkFileList value. (The GSList of
// its files is ours, a few bytes not freed, as tailscale.go has it.)
func filePaths(v *gobject.Value) []string {
	boxed := v.GetBoxed()
	if boxed == 0 {
		return nil
	}
	var paths []string
	files := (*gdk.FileList)(unsafe.Add(unsafe.Pointer(nil), boxed))
	for l := files.GetFiles(); l != nil; l = l.Next {
		if path := (&gio.FileBase{Ptr: l.Data}).GetPath(); path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

// quotePaths is paths for a shell's command line: space-separated, each
// in single quotes when it has more than letters, digits and ./_-+:@%,=.
func quotePaths(paths []string) string {
	quoted := make([]string, len(paths))
	for i, path := range paths {
		plain := path != ""
		for _, r := range path {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("./_-+:@%,=", r)) {
				plain = false
				break
			}
		}
		if plain {
			quoted[i] = path
		} else {
			quoted[i] = "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
		}
	}
	return strings.Join(quoted, " ")
}

// clipRead is a clipboard read's end.
func (p *termPane) clipRead(primary bool, result uintptr) {
	clip := p.clipboard(primary)
	text, err := clip.ReadTextFinish(&gio.AsyncResultBase{Ptr: result})
	clip.Unref()
	if p.closed || p.exited || err != nil || text == "" {
		return
	}
	p.paste(text, primary, false)
}

// paste sends text as the program wants pasted text; text that could run
// commands waits for the sheet, unless force.
func (p *termPane) paste(text string, primary, force bool) {
	b, err := p.term.Paste(text, primary, force)
	if errors.Is(err, vt.ErrUnsafe) {
		p.showSheet(text, primary)
		return
	}
	if err != nil {
		log.Printf("term: paste: %v", err)
		return
	}
	p.input(b)
}

// lines splits text at its line breaks (\r\n, \n, \r), a final break
// ending the last line rather than starting one.
func lines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// oneLine is text with its line breaks made spaces.
func oneLine(text string) string {
	return strings.TrimSpace(strings.Join(lines(text), " "))
}

// showSheet asks before pasting text that could run commands: Paste, Paste
// as one line, or Cancel (Escape).
func (p *termPane) showSheet(text string, primary bool) {
	p.hideSheet()
	p.sheetCbs = nil
	ls := lines(text)
	sheet := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	sheet.AddCssClass("term-sheet")
	sheet.SetHalign(gtk.AlignCenterValue)
	sheet.SetValign(gtk.AlignEndValue)
	sheet.SetMarginBottom(24)
	sheet.SetMarginStart(16)
	sheet.SetMarginEnd(16)
	sheet.SetSizeRequest(440, -1)

	title := gtk.NewLabel(i18n.Nf("Paste %d line?", "Paste %d lines?", len(ls), len(ls)))
	title.AddCssClass("term-sheet-title")
	title.SetXalign(0)
	sheet.Append(&title.Widget)
	why := i18n.T("The program here takes a line break as Enter, so each line would run as soon as it is pasted.")
	if strings.Contains(text, "\x1b[201~") {
		why = i18n.T("The text holds the mark that ends a paste, so what follows it would run as if typed.")
	}
	reason := gtk.NewLabel(why)
	reason.AddCssClass("term-sheet-why")
	reason.SetXalign(0)
	reason.SetWrap(true)
	reason.SetMaxWidthChars(56)
	sheet.Append(&reason.Widget)

	shown := ls[:min(len(ls), sheetLines)]
	preview := gtk.NewLabel(strings.Join(shown, "\n"))
	preview.AddCssClass("term-sheet-text")
	preview.SetXalign(0)
	preview.SetEllipsize(pango.EllipsizeEndValue)
	preview.SetMaxWidthChars(56)
	sheet.Append(&preview.Widget)
	if more := len(ls) - len(shown); more > 0 {
		rest := gtk.NewLabel(i18n.Nf("and %d more line", "and %d more lines", more, more))
		rest.AddCssClass("term-sheet-why")
		rest.SetXalign(0)
		sheet.Append(&rest.Widget)
	}

	buttons := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	buttons.SetHalign(gtk.AlignEndValue)
	buttons.SetMarginTop(6)
	button := func(label, class string, do func()) *gtk.Button {
		b := gtk.NewButtonWithLabel(label)
		b.AddCssClass(class)
		cb := func(gtk.Button) { do() }
		p.sheetCbs = append(p.sheetCbs, &cb)
		b.ConnectClicked(p.sheetCbs[len(p.sheetCbs)-1])
		buttons.Append(&b.Widget)
		return b
	}
	button(i18n.T("Cancel"), "outlined", p.hideSheet)
	button(i18n.T("Paste as one line"), "outlined", func() {
		p.hideSheet()
		p.paste(oneLine(text), primary, true)
	})
	ok := button(i18n.T("Paste"), "suggested-action", func() {
		p.hideSheet()
		p.paste(text, primary, true)
	})
	sheet.Append(&buttons.Widget)

	keys := gtk.NewEventControllerKey()
	p.onSheetKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if int32(keyval) == gdk.KEY_Escape {
			p.hideSheet()
			return true
		}
		return false
	}
	keys.ConnectKeyPressed(&p.onSheetKey)
	sheet.AddController(&keys.EventController)

	p.sheet = sheet
	p.root.AddOverlay(&sheet.Widget)
	ok.GrabFocus()
}

// hideSheet closes the paste sheet, if open, and gives the pane the keys
// again.
func (p *termPane) hideSheet() {
	if p.sheet == nil {
		return
	}
	p.root.RemoveOverlay(&p.sheet.Widget)
	p.sheet = nil
	p.widget.GrabFocus()
}
