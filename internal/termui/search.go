package termui

import (
	"log"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// The find bar (Ctrl+Shift+F) searches the pane's screen and scrollback
// as you type, matches marked in the warn colour, the selected one
// filled. Enter goes to the next match up (older), Shift+Enter down;
// Escape closes it.

func (p *termPane) openFind() {
	if p.find != nil {
		p.findEntry.GrabFocus()
		p.findEntry.SelectRegion(0, -1)
		return
	}
	bar := gtk.NewBox(gtk.OrientationHorizontalValue, 4)
	bar.AddCssClass("term-find")
	bar.SetHalign(gtk.AlignEndValue)
	bar.SetValign(gtk.AlignStartValue)
	bar.SetMarginTop(10)
	bar.SetMarginEnd(16)

	entry := gtk.NewEntry()
	entry.SetPlaceholderText(i18n.T("Find"))
	entry.SetWidthChars(22)
	p.onFindChanged = func(uintptr, uintptr) { p.search(entry.GetText()) }
	gtkx.ConnectChanged(entry, &p.onFindChanged)
	bar.Append(&entry.Widget)

	count := gtk.NewLabel("")
	count.AddCssClass("term-find-count")
	count.SetWidthChars(7)
	count.SetXalign(1)
	bar.Append(&count.Widget)

	button := func(icon, tip string, do *func(gtk.Button)) {
		b := gtk.NewButtonFromIconName(icon)
		b.AddCssClass("plain")
		b.SetTooltipText(tip)
		b.ConnectClicked(do)
		bar.Append(&b.Widget)
	}
	p.onFindUp = func(gtk.Button) { p.findMove(1) }
	p.onFindDown = func(gtk.Button) { p.findMove(-1) }
	p.onFindClose = func(gtk.Button) { p.closeFind() }
	button("pan-up-symbolic", i18n.T("Previous match (Enter)"), &p.onFindUp)
	button("pan-down-symbolic", i18n.T("Next match (Shift+Enter)"), &p.onFindDown)
	button("window-close-symbolic", i18n.T("Close (Escape)"), &p.onFindClose)

	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue)
	p.onFindKey = func(_ gtk.EventControllerKey, keyval, _ uint32, state gdk.ModifierType) bool {
		switch int32(keyval) {
		case gdk.KEY_Escape:
			p.closeFind()
		case gdk.KEY_Return, gdk.KEY_KP_Enter:
			if state&gdk.ShiftMaskValue != 0 {
				p.findMove(-1)
			} else {
				p.findMove(1)
			}
		default:
			return false
		}
		return true
	}
	keys.ConnectKeyPressed(&p.onFindKey)
	bar.AddController(&keys.EventController)

	p.find, p.findEntry, p.findCount = bar, entry, count
	p.root.AddOverlay(&bar.Widget)
	entry.GrabFocus()
}

// search looks for text as it is typed.
func (p *termPane) search(text string) {
	if err := p.term.Search(text); err != nil {
		log.Printf("term: search: %v", err)
	}
	if text != "" {
		p.term.SearchMove(1) // the newest match, scrolled to
	}
	p.update(false)
}

func (p *termPane) findMove(dir int) {
	p.term.SearchMove(dir)
	p.update(false)
}

// showCount writes "k / n" in the find bar, after each frame.
func (p *termPane) showCount() {
	if p.find == nil {
		return
	}
	total, cur := p.term.SearchCount()
	switch {
	case p.findEntry.GetText() == "":
		p.findCount.SetText("")
	case total == 0:
		p.findCount.SetText(i18n.T("none"))
	default:
		p.findCount.SetText(itoa(cur) + " / " + itoa(total))
	}
}

// closeFind ends the search; the view stays where it went.
func (p *termPane) closeFind() {
	if p.find == nil {
		return
	}
	p.root.RemoveOverlay(&p.find.Widget)
	p.find, p.findEntry, p.findCount = nil, nil, nil
	p.term.Search("")
	p.update(false)
	p.widget.GrabFocus()
}
