package ui

import (
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
)

// settingsSearch finds pages and settings by what the window shows of
// them: the pages' titles (and other words for them), the sections' labels
// and the rows' titles and lines. While a search is typed its results
// replace the index; choosing one turns to its page, scrolls its row into
// view and lights it for a moment.
type settingsSearch struct {
	entry   gtk.SearchEntry
	results gtk.ListBox
	none    gtk.Label
	scrolls map[string]*gtk.ScrolledWindow // by page

	// What can be found, read from the pages when a search begins (the
	// devices and networks listed change) and let go when it ends.
	items   []shell.SettingsItem
	widgets []*gtk.Widget // each item's row, referenced; nil for a page
	shown   []int         // the results' items, in order

	target   *gtk.Widget // the row found, referenced while it is lit
	flashGen uintptr
}

// flashMS is how long a row found stays lit.
const flashMS = 1800

func (s *shellUI) getSettingsSearch(ui *gtkx.Builder) {
	st := &s.settings
	f := &st.search
	ui.Get("settings_search", &f.entry)
	ui.Get("settings_results", &f.results)
	ui.Get("settings_no_results", &f.none)
	f.scrolls = map[string]*gtk.ScrolledWindow{}
	for _, name := range settingsPages {
		sc := &gtk.ScrolledWindow{}
		ui.Get("scroll_"+name, sc)
		f.scrolls[name] = sc
	}
	f.items, f.widgets, f.shown, f.target = nil, nil, nil, nil
	if s.onSettingsSearch == nil {
		s.onSettingsSearch = func(gtk.SearchEntry) { s.searchSettings() }
		s.onSettingsSearchGo = func(gtk.SearchEntry) {
			f := &s.settings.search
			if row := f.results.GetSelectedRow(); row != nil {
				s.openResult(int(row.GetIndex()))
			} else if len(f.shown) > 0 {
				s.openResult(0)
			}
		}
		s.onSettingsSearchStop = func(gtk.SearchEntry) { s.settings.search.entry.SetText("") }
		s.onSettingsResult = func(_ gtk.ListBox, ptr uintptr) {
			if ptr != 0 {
				s.openResult(int(gtk.ListBoxRowNewFromInternalPtr(ptr).GetIndex()))
			}
		}
		s.onSearchScroll = func(uintptr) bool {
			s.scrollToFound()
			return false
		}
		s.onSearchFlash = func(gen uintptr) bool {
			if f := &s.settings.search; gen == f.flashGen {
				s.endFlash()
			}
			return false
		}
	}
	f.entry.ConnectSearchChanged(&s.onSettingsSearch)
	f.entry.ConnectActivate(&s.onSettingsSearchGo)
	f.entry.ConnectStopSearch(&s.onSettingsSearchStop)
	f.entry.SetKeyCaptureWidget(&st.win.Widget)
	f.results.ConnectRowActivated(&s.onSettingsResult)
}

// releaseSearch lets go of the rows the search holds; the window closes,
// or the search ended.
func (s *shellUI) releaseSearch() {
	f := &s.settings.search
	for _, w := range f.widgets {
		if w != nil {
			w.Unref()
		}
	}
	f.items, f.widgets, f.shown = nil, nil, nil
	s.endFlash()
}

func (s *shellUI) endFlash() {
	f := &s.settings.search
	if f.target != nil {
		f.target.RemoveCssClass("found")
		f.target.Unref()
		f.target = nil
	}
}

// focusSettingsSearch puts the caret in the search field (Ctrl+F).
func (s *shellUI) focusSettingsSearch() {
	f := &s.settings.search
	f.entry.GrabFocus()
	f.entry.SelectRegion(0, -1)
}

// clearSettingsSearch ends a search typed, reporting whether there was one.
func (s *shellUI) clearSettingsSearch() bool {
	f := &s.settings.search
	if f.entry.GetText() == "" {
		return false
	}
	f.entry.SetText("")
	return true
}

// searchSettings shows what the text in the search field finds, or the
// index again when it is empty.
func (s *shellUI) searchSettings() {
	st := &s.settings
	f := &st.search
	q := f.entry.GetText()
	if q == "" {
		s.releaseSearch()
		st.index.SetVisible(true)
		f.results.SetVisible(false)
		f.none.SetVisible(false)
		s.selectSettingsIndex()
		return
	}
	if f.items == nil {
		s.buildSearchIndex()
	}
	f.shown = shell.SearchSettings(f.items, q)
	st.index.SetVisible(false)
	f.results.RemoveAll()
	for _, i := range f.shown {
		f.results.Append(&s.resultRow(f.items[i]).Widget)
	}
	f.results.SetVisible(len(f.shown) > 0)
	f.none.SetVisible(len(f.shown) == 0)
	if row := f.results.GetRowAtIndex(0); row != nil {
		f.results.SelectRow(row)
	}
}

// resultRow is a result in the index: the page's icon, what was found,
// and where it is.
func (s *shellUI) resultRow(it shell.SettingsItem) *gtk.ListBoxRow {
	box := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	img := gtk.NewImageFromIconName(settingsIcons[it.Page])
	img.AddCssClass("index-icon")
	img.SetValign(gtk.AlignCenterValue)
	gtkx.Append(box, &img.Widget)
	text := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	text.SetValign(gtk.AlignCenterValue)
	title, where := it.Title, i18n.T("Page")
	if title == "" {
		title = it.PageTitle
	} else if it.Section != "" {
		where = it.PageTitle + " › " + it.Section
	} else {
		where = it.PageTitle
	}
	t := gtk.NewLabel(title)
	t.SetXalign(0)
	t.SetEllipsize(3)
	gtkx.Append(text, &t.Widget)
	w := gtk.NewLabel(where)
	w.AddCssClass("result-where")
	w.SetXalign(0)
	w.SetEllipsize(3)
	gtkx.Append(text, &w.Widget)
	gtkx.Append(box, &text.Widget)
	row := gtk.NewListBoxRow()
	row.SetChild(&box.Widget)
	return row
}

// buildSearchIndex reads what each page shows: its title, then each row
// shown on it with the label of the section it is in.
func (s *shellUI) buildSearchIndex() {
	st := &s.settings
	f := &st.search
	f.items, f.widgets = []shell.SettingsItem{}, []*gtk.Widget{}
	for _, name := range settingsPages {
		child := st.pages.GetChildByName(name)
		sc := f.scrolls[name]
		if child == nil || sc == nil {
			continue
		}
		pageTitle := st.pages.GetPage(child).GetTitle()
		f.items = append(f.items, shell.SettingsItem{Page: name, PageTitle: pageTitle})
		f.widgets = append(f.widgets, nil)
		vp := sc.GetChild()
		if vp == nil {
			continue
		}
		section := ""
		var walk func(w *gtk.Widget)
		walk = func(w *gtk.Widget) {
			for c := w.GetFirstChild(); c != nil; c = c.GetNextSibling() {
				if !c.GetVisible() {
					continue
				}
				switch {
				case c.HasCssClass("section-label") && c.GetCssName() == "label":
					section = shell.SectionName(gtk.LabelNewFromInternalPtr(c.Ptr).GetText())
				case c.HasCssClass("row") || c.HasCssClass("field"):
					title := findLabel(c, "title")
					if title == "" {
						title = findLabel(c, "label")
					}
					if title == "" {
						continue
					}
					c.Ref()
					f.items = append(f.items, shell.SettingsItem{
						Page: name, PageTitle: pageTitle, Section: section, Title: title, Sub: findLabel(c, "sub"),
					})
					f.widgets = append(f.widgets, c)
				default:
					walk(c)
				}
			}
		}
		walk(vp)
	}
}

// findLabel is the text of the first label with the class under w.
func findLabel(w *gtk.Widget, class string) string {
	for c := w.GetFirstChild(); c != nil; c = c.GetNextSibling() {
		if c.GetCssName() == "label" && c.HasCssClass(class) {
			if t := gtk.LabelNewFromInternalPtr(c.Ptr).GetText(); t != "" {
				return t
			}
		}
		if t := findLabel(c, class); t != "" {
			return t
		}
	}
	return ""
}

// openResult turns to a result's page and, for a row, scrolls it into
// view once the page is laid out, and lights it.
func (s *shellUI) openResult(i int) {
	f := &s.settings.search
	if i < 0 || i >= len(f.shown) {
		return
	}
	it, w := f.items[f.shown[i]], f.widgets[f.shown[i]]
	s.showSettingsPage(it.Page)
	s.endFlash()
	if w == nil {
		if sc := f.scrolls[it.Page]; sc != nil {
			sc.GetVadjustment().SetValue(0)
		}
		return
	}
	w.Ref()
	f.target = w
	w.AddCssClass("found")
	f.flashGen++
	glib.TimeoutAdd(60, &s.onSearchScroll, 0)
	glib.TimeoutAdd(flashMS, &s.onSearchFlash, f.flashGen)
}

// scrollToFound scrolls the page so the row found is under the sticky
// head, a little below the top.
func (s *shellUI) scrollToFound() {
	st := &s.settings
	f := &st.search
	sc := f.scrolls[st.pages.GetVisibleChildName()]
	if f.target == nil || sc == nil || sc.GetChild() == nil {
		return
	}
	page := sc.GetChild().GetFirstChild() // viewport > page box
	var r graphene.Rect
	if page == nil || !f.target.ComputeBounds(page, &r) {
		return
	}
	top := float64((*(*[4]float32)(unsafe.Pointer(&r)))[1])
	adj := sc.GetVadjustment()
	adj.SetValue(max(0, min(top-64, adj.GetUpper()-adj.GetPageSize())))
}
