package termui

import (
	"errors"
	"log"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"

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

// pasteFrom reads the clipboard (or the primary selection) and pastes
// what text it holds.
func (p *termPane) pasteFrom(primary bool) {
	if p.exited {
		return
	}
	cb := &p.onClipText
	if primary {
		cb = &p.onPrimaryText
	}
	clip := p.clipboard(primary)
	clip.ReadTextAsync(nil, cb, 0)
	clip.Unref()
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
