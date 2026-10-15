package filesui

import (
	"path/filepath"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// tab is one of a pane's places: where it is, its history and what was
// selected. The pane shows one at a time, in its one list: switching
// reads the tab's folder again, which is quick, and keeps nothing else
// alive.
type tab struct {
	key  string // a folder, or trashKey
	hist files.History
	sel  []string
}

// saveTab keeps the place shown in its tab.
func (p *pane) saveTab() {
	t := p.tabs[p.tab]
	t.key, t.hist, t.sel = p.locationKey(), p.hist, p.selectedNames()
}

// newTab opens key in a new tab after the current one, shown at once when
// show, else left behind it (a folder opened with the middle button).
func (p *pane) newTab(key string, show bool) {
	p.saveTab()
	t := &tab{key: key}
	at := p.tab + 1
	p.tabs = append(p.tabs[:at], append([]*tab{t}, p.tabs[at:]...)...)
	if show {
		p.showTab(at)
		return
	}
	p.buildTabs()
}

// showTab shows tab i.
func (p *pane) showTab(i int) {
	if i < 0 || i >= len(p.tabs) {
		return
	}
	if i != p.tab && p.tab >= 0 {
		p.saveTab()
	}
	p.tab = i
	t := p.tabs[i]
	p.hist = t.hist
	p.goKey(t.key, false, t.sel...)
	p.buildTabs()
	p.w.syncTitle()
	p.w.markSidebar()
}

// stepTab shows the next tab (d 1) or the one before (d -1), round.
func (p *pane) stepTab(d int) {
	if n := len(p.tabs); n > 1 {
		p.showTab((p.tab + d + n) % n)
	}
}

// closeTab closes tab i; the last one closes the window.
func (p *pane) closeTab(i int) {
	if len(p.tabs) == 1 {
		p.w.win.Close()
		return
	}
	shown := i == p.tab
	if !shown {
		p.saveTab()
	}
	p.tabs = append(p.tabs[:i], p.tabs[i+1:]...)
	switch {
	case shown:
		p.tab = -1 // nothing to save: it went
		p.showTab(min(i, len(p.tabs)-1))
	case i < p.tab:
		p.tab--
		p.buildTabs()
	default:
		p.buildTabs()
	}
}

// tabTitle names a place for its tab.
func tabTitle(key string) string {
	switch {
	case key == trashKey:
		return i18n.T("Trash")
	case key == files.Home():
		return i18n.T("Home")
	case key == "/":
		return "/"
	}
	return filepath.Base(key)
}

// buildTabs draws the tab strip, shown with two tabs or more: each a
// title, and a button that closes it; a middle click closes it too.
func (p *pane) buildTabs() {
	p.w.app.h.forget(p.tabPtrs)
	p.tabPtrs = p.tabPtrs[:0]
	gtkx.Clear(&p.tabsBox)
	p.tabsBox.SetVisible(len(p.tabs) > 1)
	if len(p.tabs) < 2 {
		return
	}
	for i, t := range p.tabs {
		key := t.key
		if i == p.tab {
			key = p.locationKey()
		}
		box := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		box.AddCssClass("files-tab")
		if i == p.tab {
			box.AddCssClass("current")
		}
		title := gtk.NewButton()
		title.AddCssClass("files-tab-title")
		l := gtk.NewLabel(tabTitle(key))
		l.SetEllipsize(2)
		l.SetMaxWidthChars(18)
		title.SetChild(&l.Widget)
		l.Unref()
		title.SetTooltipText(shortHome(key))
		title.SetHexpand(true)
		closer := gtk.NewButtonFromIconName("window-close-symbolic")
		closer.AddCssClass("plain")
		closer.AddCssClass("files-tab-close")
		closer.SetValign(gtk.AlignCenterValue)
		closer.SetTooltipText(i18n.T("Close the tab (Ctrl+W)"))
		i := i
		p.w.app.h.click(title, func() { p.showTab(i) })
		p.w.app.h.click(closer, func() { p.closeTab(i) })
		g := p.w.app.h.press(&title.Widget, false, func(button uint32, _ int32, _, _ float64) {
			if button == 2 {
				p.closeTab(i)
			}
		})
		p.tabPtrs = append(p.tabPtrs, title.GoPointer(), closer.GoPointer(), g)
		gtkx.Append(box, &title.Widget)
		gtkx.Append(box, &closer.Widget)
		gtkx.Append(&p.tabsBox, &box.Widget)
	}
}
