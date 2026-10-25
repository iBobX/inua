package filesui

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/apps/files/pick"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/portal"
)

// The picker (docs/picker.md) is a window of Files' own, in a pick mode:
// one pane, the sidebar and the details, and a footer that says what the
// app asks for and answers it. It serves the file chooser portal.

// picker is a window's request, when it picks for an app.
type picker struct {
	req     pick.Request
	filter  int               // into req.Filters, -1 for none
	answer  chan<- pickResult // answered once
	checks  map[string]*gtk.CheckButton
	lists   map[string]*gtk.DropDown
	app     string // the name of the app that asked
	done    bool
	filters *gtk.StringList
}

type pickResult struct {
	a  pick.Answer
	ok bool
}

// footer is the picker's part of the window's layout.
type footer struct {
	foot, space, choices gtk.Box
	title, err           gtk.Label
	name                 gtk.Entry
	filter               gtk.DropDown
	cancel, accept       gtk.Button
}

// lingerMs is how long the process stays after its last picker, so the
// next one is quick.
const lingerMs = 10 * 60 * 1000

// Pick shows the picker for an app's request and waits for the answer
// (chooser.Picker); it runs on the request's D-Bus goroutine.
func (a *App) Pick(r pick.Request, done <-chan struct{}) (pick.Answer, bool) {
	ch := make(chan pickResult, 1)
	opened := make(chan *window, 1)
	a.whenReady(func() {
		select {
		case <-done: // closed before it opened
			ch <- pickResult{}
		default:
			opened <- a.openPicker(r, ch)
		}
	})
	select {
	case res := <-ch:
		return res.a, res.ok
	case <-done:
		// Posted after the opening, which whenReady posted or queued
		// first: the window is there, or will not be.
		a.whenReady(func() {
			select {
			case w := <-opened:
				w.pickCancel()
			default:
			}
		})
		return pick.Answer{}, false
	}
}

// openPicker opens a picker window for r.
func (a *App) openPicker(r pick.Request, ch chan<- pickResult) *window {
	if a.pickMem == nil {
		a.pickMem = pick.LoadMemory(pick.MemoryPath())
	}
	w := a.newWindow()
	w.pick = &picker{req: r, filter: r.Filter, answer: ch, checks: map[string]*gtk.CheckButton{}, lists: map[string]*gtk.DropDown{},
		app: appName(r.AppID)}
	w.setupPicker()
	a.holdPicker()

	w.win.Present()
	// Its own class, which inua's window rule floats and centres, and over
	// the app's window when the app gave it.
	gtkx.SetAppID(&w.win.Window, portal.FilesName)
	gtkx.SetForeignParent(&w.win.Window, r.Parent)

	isDir := func(p string) bool { fi, err := os.Stat(p); return err == nil && fi.IsDir() }
	start := r.StartFolder(a.pickMem, isDir)
	if r.Kind == pick.Save && r.CurrentFile != "" && filepath.Dir(r.CurrentFile) == start {
		w.reveal([]string{r.CurrentFile})
	} else {
		w.open(start)
	}
	if r.Kind == pick.Save {
		w.foot.name.GrabFocus()
		w.foot.name.SelectRegion(0, int32(pick.Stem(w.foot.name.GetText())))
	}
	return w
}

// appName is the name of the app with desktop ID id, or "".
func appName(id string) string {
	if id == "" {
		return ""
	}
	if e, err := desktop.Find(desktop.DataDirs(), id+".desktop", ""); err == nil {
		return e.Name
	}
	return ""
}

// setupPicker turns a new window into a picker: the footer, one pane, its
// own size and details.
func (w *window) setupPicker() {
	pk, f, r := w.pick, &w.foot, w.pick.req
	w.dualToggle.SetVisible(false)
	w.setDual(false)
	for _, p := range w.p {
		p.termButton.SetVisible(false)
	}
	f.foot.SetVisible(true)
	f.title.SetText(r.Heading(i18n.T))
	f.accept.SetLabel(r.Action(i18n.T))
	w.win.SetTitle(r.Heading(i18n.T))
	m := w.app.pickMem
	width, height, details := m.View()
	w.win.SetDefaultSize(int32(width), int32(height))
	w.detailsReveal.SetRevealChild(details)
	w.detailsToggle.SetActive(details)

	if r.Kind == pick.Save {
		f.name.SetVisible(true)
		f.space.SetVisible(false)
		f.name.SetText(r.SaveName())
		w.enter(&f.name, func() { w.pickAccept(false) })
	}
	if len(r.Filters) > 0 {
		var names []string
		for _, flt := range r.Filters {
			names = append(names, flt.Name)
		}
		pk.filters = gtk.NewStringList(names)
		f.filter.SetModel(pk.filters)
		f.filter.SetSelected(uint32(max(pk.filter, 0)))
		f.filter.SetVisible(true)
		w.notify(&f.filter, "selected", func() { w.pickFilterChanged() })
	}
	for _, c := range r.Choices {
		if len(c.Options) == 0 {
			b := gtk.NewCheckButtonWithLabel(c.Label)
			b.SetActive(c.Value == "true")
			b.SetValign(gtk.AlignCenterValue)
			pk.checks[c.ID] = b
			gtkx.Append(&f.choices, &b.Widget)
			continue
		}
		var labels []string
		sel := 0
		for i, o := range c.Options {
			labels = append(labels, o[1])
			if o[0] == c.Value {
				sel = i
			}
		}
		box := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
		label := gtk.NewLabel(c.Label)
		label.AddCssClass("sub")
		gtkx.Append(box, &label.Widget)
		d := gtk.NewDropDownFromStrings(labels)
		d.AddCssClass("dropdown")
		d.SetSelected(uint32(sel))
		d.SetValign(gtk.AlignCenterValue)
		pk.lists[c.ID] = d
		box.Append(&d.Widget) // the box owns it; pk keeps a pointer
		gtkx.Append(&f.choices, &box.Widget)
	}
	w.click(&f.cancel, func() { w.pickCancel() })
	w.click(&f.accept, func() { w.pickAccept(false) })
	w.buildSidebar()
}

// namingFile is whether the window saves under a name typed: the name
// entry keeps the keys as folders load.
func (w *window) namingFile() bool { return w.pick != nil && w.pick.req.Kind == pick.Save }

// pickShows is whether the picker lists an entry: folders always, files
// that its filter matches, and no files when it chooses a folder.
func (w *window) pickShows(e *files.Entry) bool {
	pk := w.pick
	if pk == nil || e.Dir {
		return true
	}
	if pk.req.ChoosesFolder() {
		return false
	}
	if pk.filter < 0 || pk.filter >= len(pk.req.Filters) {
		return true
	}
	return pk.req.Filters[pk.filter].Match(e.Name, w.app.kinds.of(e).mime, false, gio.ContentTypeIsA)
}

func (w *window) pickFilterChanged() {
	pk := w.pick
	sel := int(w.foot.filter.GetSelected())
	if sel == pk.filter || sel >= len(pk.req.Filters) {
		return
	}
	old := pk.filter
	pk.filter = sel
	if pk.req.Kind == pick.Save && old >= 0 {
		name := pick.SwapExtension(w.foot.name.GetText(), pk.req.Filters[old], pk.req.Filters[sel])
		w.foot.name.SetText(name)
	}
	w.cur().refill(nil, true)
}

// pickSelectionChanged puts a file clicked while saving in the name entry,
// and says whether the folder can be saved in.
func (w *window) pickSelectionChanged(p *pane) {
	pk := w.pick
	if pk == nil || p != w.cur() {
		return
	}
	if pk.req.Kind == pick.Save {
		if sel := p.selected(); len(sel) == 1 && !p.all[sel[0]].Dir && !w.focusIn(&w.foot.name.Widget) {
			w.foot.name.SetText(p.all[sel[0]].Name)
		}
	}
	w.pickError("")
	if pk.req.Kind != pick.Open && p.mode == inFolder {
		writable := pick.Writable(p.dir)
		w.foot.accept.SetSensitive(writable)
		if !writable {
			w.pickError(i18n.T("Files cannot be saved in this folder."))
		}
	}
}

func (w *window) pickError(msg string) {
	w.foot.err.SetText(msg)
	w.foot.err.SetVisible(msg != "")
}

// pickActivate is a row opened in the picker: a folder is gone into, a
// file chosen.
func (w *window) pickActivate(p *pane, i int) {
	e := &p.all[i]
	if e.Dir {
		p.goTo(p.path(i), true)
		return
	}
	switch w.pick.req.Kind {
	case pick.Save:
		w.foot.name.SetText(e.Name)
		w.pickAccept(false)
	case pick.Open:
		if !w.pick.req.ChoosesFolder() {
			w.pickAccept(false)
		}
	}
}

// pickAccept chooses: what is selected, the name typed, or the folder
// shown (choosing a folder, saving several: the one selected, else that).
// folder is Ctrl+Enter's: saving, a folder selected is gone into rather
// than the name typed saved.
func (w *window) pickAccept(folder bool) {
	pk, p := w.pick, w.cur()
	if pk == nil || pk.done {
		return
	}
	if p.mode == inTrash {
		return
	}
	sel := p.selected()
	switch r := pk.req; {
	case r.Kind == pick.Open && !r.Directory:
		var paths []string
		for _, i := range sel {
			if !p.all[i].Dir {
				paths = append(paths, p.path(i))
			}
		}
		if len(paths) == 0 {
			if len(sel) == 1 && p.all[sel[0]].Dir {
				p.goTo(p.path(sel[0]), true)
				return
			}
			w.pickError(i18n.T("Choose a file."))
			return
		}
		if !r.Multiple {
			paths = paths[:1]
		}
		w.pickDone(paths, filepath.Dir(paths[0]))
	case r.Kind == pick.Open: // a folder: the one selected, else the one shown
		dir := p.dir
		if len(sel) == 1 && p.all[sel[0]].Dir {
			dir = p.path(sel[0])
		}
		w.pickDone([]string{dir}, filepath.Dir(dir))
	case r.Kind == pick.SaveFiles:
		dir := p.dir
		if len(sel) == 1 && p.all[sel[0]].Dir {
			dir = p.path(sel[0])
		}
		paths := r.SaveFilesPaths(dir)
		if taken := r.Taken(dir); len(taken) > 0 {
			w.askReplace(taken, func() { w.pickDone(paths, dir) })
			return
		}
		w.pickDone(paths, dir)
	default: // Save
		if folder && len(sel) == 1 && p.all[sel[0]].Dir {
			p.goTo(p.path(sel[0]), true)
			return
		}
		path, err := pick.Target(p.dir, w.foot.name.GetText())
		if err != nil {
			w.pickError(saveError(err))
			return
		}
		if !pick.Writable(filepath.Dir(path)) {
			w.pickError(i18n.T("Files cannot be saved in this folder."))
			return
		}
		if pick.Exists(path) {
			w.askReplace([]string{filepath.Base(path)}, func() { w.pickDone([]string{path}, filepath.Dir(path)) })
			return
		}
		w.pickDone([]string{path}, filepath.Dir(path))
	}
}

func saveError(err error) string {
	switch {
	case errors.Is(err, pick.ErrNoName):
		return i18n.T("Type a name to save under.")
	case errors.Is(err, pick.ErrNoFolder):
		return i18n.T("There is no such folder.")
	case errors.Is(err, pick.ErrIsFolder):
		return i18n.T("A folder has this name.")
	}
	return i18n.T("A name cannot hold that.")
}

// askReplace asks before choosing names that are taken; the app writes
// the files, the picker only answers.
func (w *window) askReplace(names []string, yes func()) {
	title := i18n.Tf("Replace “%s”?", names[0])
	if len(names) > 1 {
		title = i18n.Nf("Replace %d file?", "Replace %d files?", len(names), len(names))
	}
	w.openCard(&card{def: 0, buttons: []cardButton{
		{label: i18n.T("Cancel")},
		{label: i18n.T("Replace"), class: "destructive-action", action: yes},
	}}, cardParts{title: title, text: i18n.T("A file of the same name is in this folder; the app will write over it.")})
}

// pickDone answers with paths, remembering the folder for the app.
func (w *window) pickDone(paths []string, dir string) {
	pk := w.pick
	a := pick.Answer{Paths: paths, Filter: pk.filter, Choices: map[string]string{}}
	for id, b := range pk.checks {
		a.Choices[id] = map[bool]string{true: "true", false: "false"}[b.GetActive()]
	}
	for _, c := range pk.req.Choices {
		if d := pk.lists[c.ID]; d != nil {
			if i := int(d.GetSelected()); i < len(c.Options) {
				a.Choices[c.ID] = c.Options[i][0]
			}
		}
	}
	appID, mem := pk.req.AppID, w.app.pickMem
	go mem.Chose(appID, dir)
	w.app.settings.Jumps.Visit(dir, time.Now())
	w.app.settings.Touch()
	w.finishPick(a, true)
	w.win.Close()
}

func (w *window) pickCancel() {
	w.finishPick(pick.Answer{}, false)
	w.win.Close()
}

// finishPick sends the answer, once.
func (w *window) finishPick(a pick.Answer, ok bool) {
	pk := w.pick
	if pk == nil || pk.done {
		return
	}
	pk.done = true
	pk.answer <- pickResult{a, ok}
}

// pickClosed is the picker window going: unanswered, it cancels.
func (w *window) pickClosed() {
	w.finishPick(pick.Answer{}, false)
	width, height := w.win.GetWidth(), w.win.GetHeight()
	if width > 0 && height > 0 && !w.win.IsMaximized() {
		go w.app.pickMem.SetSize(int(width), int(height))
	}
	w.app.releasePicker()
}

// pickKey handles the keys that differ in the picker; handled is false for
// the keys it leaves to the window's.
func (w *window) pickKey(keyval uint32, state gdk.ModifierType) (result, handled bool) {
	ctrl := state&gdk.ControlMaskValue != 0
	shift := state&gdk.ShiftMaskValue != 0
	alt := state&gdk.AltMaskValue != 0
	k := gdk.KeyvalToLower(keyval)
	p := w.cur()
	enter := keyval == uint32(gdk.KEY_Return) || keyval == uint32(gdk.KEY_KP_Enter)

	if w.focusIn(&w.foot.name.Widget) {
		switch {
		case enter:
			w.pickAccept(ctrl)
		case keyval == uint32(gdk.KEY_Escape):
			w.pickCancel()
		case keyval == uint32(gdk.KEY_Tab) && !ctrl:
			p.focus()
		case alt && k == 'f':
			w.foot.filter.Activate()
		default:
			return false, true // typing the name
		}
		return true, true
	}
	switch {
	case alt && k == 'f':
		if w.foot.filter.GetVisible() {
			w.foot.filter.Activate()
		}
		return true, true
	case ctrl && enter:
		w.pickAccept(true)
		return true, true
	case enter && !alt && p.pathStack.GetVisibleChildName() == "crumbs":
		if sel := p.selected(); len(sel) > 1 {
			w.pickAccept(false)
			return true, true
		}
		if len(p.selected()) == 0 {
			w.pickAccept(false)
			return true, true
		}
		return false, false // the view activates the row
	case keyval == uint32(gdk.KEY_Escape) && p.filter == "" && p.mode != inSearch && p.pathStack.GetVisibleChildName() == "crumbs":
		w.pickCancel()
		return true, true
	case keyval == uint32(gdk.KEY_Tab) && w.pick.req.Kind == pick.Save && !ctrl && p.pathStack.GetVisibleChildName() == "crumbs":
		w.foot.name.GrabFocus()
		return true, true
	}
	// What the picker leaves out: operations, tabs, panes, windows.
	switch {
	case ctrl && shift && slices.Contains([]uint32{'c', 'z'}, k),
		ctrl && !shift && !alt && slices.Contains([]uint32{'c', 'x', 'v', 'z', 'd', 'n', 't', 'w'}, k),
		ctrl && alt,
		!ctrl && !alt && slices.Contains([]uint32{uint32(gdk.KEY_F3), uint32(gdk.KEY_F5), uint32(gdk.KEY_F6), uint32(gdk.KEY_Delete), uint32(gdk.KEY_KP_Delete)}, keyval),
		shift && enter:
		return true, true
	}
	return false, false
}

// pickMenuItems are the list's menu in the picker: what its keys do.
func (w *window) pickMenuItems(p *pane) []menuItem {
	sel := p.selected()
	items := []menuItem{}
	if len(sel) == 1 && p.all[sel[0]].Dir {
		items = append(items, w.item("open", i18n.T("Open"), "Enter"))
	}
	if len(sel) > 0 {
		items = append(items, menuItem{label: w.pick.req.Action(i18n.T), key: "Ctrl+Enter", run: func() { w.pickAccept(true) }})
	}
	if len(sel) == 1 {
		items = append(items, w.item("rename", i18n.T("Rename…"), "F2"))
	}
	if len(items) > 0 {
		items = append(items, menuItem{})
	}
	return append(items,
		w.item("new-folder", i18n.T("New folder…"), "Ctrl+Shift+N"),
		w.item("hidden", i18n.T("Hidden files"), "Ctrl+H"),
		w.item("reload", i18n.T("Reload"), "Ctrl+R"),
	)
}

// holdPicker keeps the process while a picker is open, and lingerMs
// after the last.
func (a *App) holdPicker() {
	a.pickers++
	a.lingerGen++
	if !a.holding {
		a.c.App.Hold()
		a.holding = true
	}
}

func (a *App) releasePicker() {
	a.pickers--
	a.linger()
}

// linger lets the process go lingerMs after its last picker: the spare
// window D-Bus's start left hidden goes with the hold.
func (a *App) linger() {
	if a.pickers > 0 || !a.holding {
		return
	}
	a.lingerGen++
	gen := a.lingerGen
	a.h.later(lingerMs, func() {
		if gen != a.lingerGen || a.pickers > 0 {
			return
		}
		if w := a.spare; w != nil {
			a.spare = nil
			w.closeRequested()
			w.win.Destroy()
		}
		a.holding = false
		a.c.App.Release()
	})
}

// takeSpare is the hidden window of a start by D-Bus, for the first
// folder the user opens, or a new window.
func (a *App) takeSpare() *window {
	if w := a.spare; w != nil {
		a.spare = nil
		return w
	}
	return a.newWindow()
}
