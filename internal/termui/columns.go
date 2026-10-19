package termui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/agent"
	"github.com/lyricat/inua/internal/term/host"
	"github.com/lyricat/inua/internal/term/model"
)

// Column 1 lists the workspaces (name, folder, the state of what runs in
// them); column 2 the shown workspace's sessions, each a section head
// over its panes. Both are made anew from each State: there are few.

type columns struct {
	col1, col2 *gtk.Box
	list       *gtk.ListBox
	sessions   *gtk.Box
	find       *gtk.SearchEntry
	// rowIDs is the workspace of each row of the list, in order.
	rowIDs []int
	// filling is set while the list is made, so selecting a row then
	// does not show it.
	filling bool

	onRow          func(gtk.ListBox, uintptr)
	onFind         func(uintptr, uintptr)
	onNewWorkspace func(gtk.Button)
	onNewSession   func(gtk.Button)
	// clicks are the session heads' and pane rows' callbacks, kept for
	// as long as their buttons.
	clicks []*func(gtk.Button)
	// times are the labels of the time in state, by pane.
	times []timed
}

type timed struct {
	label *gtk.Label
	pane  int
}

// updateTimes counts the times in state on.
func (c *columns) updateTimes(a *App) {
	now := time.Now()
	for _, t := range c.times {
		t.label.SetText(since(a.info[t.pane].Since, now))
	}
}

func (a *App) buildColumns(ui *gtkx.Builder) {
	c := &a.cols
	c.col1, c.col2 = &gtk.Box{}, &gtk.Box{}
	c.list, c.sessions, c.find = &gtk.ListBox{}, &gtk.Box{}, &gtk.SearchEntry{}
	ui.Get("workspaces_col", c.col1)
	ui.Get("sessions_col", c.col2)
	ui.Get("workspaces", c.list)
	ui.Get("sessions", c.sessions)
	ui.Get("find", c.find)
	var newWorkspace, newSession gtk.Button
	ui.Get("new_workspace", &newWorkspace)
	ui.Get("new_session", &newSession)
	c.onRow = func(_ gtk.ListBox, row uintptr) {
		i := int(gtk.ListBoxRowNewFromInternalPtr(row).GetIndex())
		if c.filling || i < 0 || i >= len(c.rowIDs) {
			return
		}
		a.showWorkspace(c.rowIDs[i])
	}
	c.list.ConnectRowActivated(&c.onRow)
	c.onFind = func(uintptr, uintptr) { c.update(a) }
	gtkx.ConnectChanged(c.find, &c.onFind)
	c.onNewWorkspace = func(gtk.Button) { a.newWorkspace() }
	newWorkspace.ConnectClicked(&c.onNewWorkspace)
	c.onNewSession = func(gtk.Button) { a.newSession() }
	newSession.ConnectClicked(&c.onNewSession)
}

// update makes both columns from the State.
func (c *columns) update(a *App) {
	if a.state == nil {
		return
	}
	c.fillWorkspaces(a)
	c.fillSessions(a)
}

func (c *columns) fillWorkspaces(a *App) {
	m := &a.state.Model
	query := strings.ToLower(strings.TrimSpace(c.find.GetText()))
	c.filling = true
	defer func() { c.filling = false }()
	c.list.RemoveAll()
	c.rowIDs = c.rowIDs[:0]
	for _, w := range m.Workspaces {
		if query != "" && !a.matches(w, query) {
			continue
		}
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		row.AddCssClass("term-ws")
		text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		text.SetHexpand(true)
		name := gtk.NewLabel(w.Name)
		name.AddCssClass("term-ws-name")
		name.SetXalign(0)
		name.SetEllipsize(3)
		path := gtk.NewLabel(shortDir(w.Dir))
		path.AddCssClass("term-ws-dir")
		path.SetXalign(0)
		path.SetEllipsize(1)
		text.Append(&name.Widget)
		text.Append(&path.Widget)
		row.Append(&text.Widget)
		blocked, l := a.rollup(workspacePanes(w))
		if b := badge(blocked); b != nil {
			row.Append(&b.Widget)
		}
		mark := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		mark.AddCssClass("mark")
		mark.SetValign(gtk.AlignCenterValue)
		paint(mark, nil, l)
		row.Append(&mark.Widget)
		c.list.Append(&row.Widget)
		c.rowIDs = append(c.rowIDs, w.ID)
		if w.ID == m.Shown.Workspace {
			c.list.SelectRow(c.list.GetRowAtIndex(int32(len(c.rowIDs) - 1)))
		}
	}
}

// matches says a workspace is what the search asks for: its name, its
// folder, or a pane title in it.
func (a *App) matches(w *model.Workspace, query string) bool {
	if strings.Contains(strings.ToLower(w.Name), query) || strings.Contains(strings.ToLower(w.Dir), query) {
		return true
	}
	for _, s := range w.Sessions {
		for _, id := range s.Tree.Panes() {
			if strings.Contains(strings.ToLower(paneTitle(a.info[id])), query) {
				return true
			}
		}
	}
	return false
}

func (c *columns) fillSessions(a *App) {
	for w := c.sessions.GetFirstChild(); w != nil; w = c.sessions.GetFirstChild() {
		c.sessions.Remove(w)
	}
	c.clicks = c.clicks[:0]
	c.times = c.times[:0]
	w := a.shownWorkspace()
	if w == nil {
		return
	}
	click := func(b *gtk.Button, do func()) {
		f := func(gtk.Button) { do() }
		c.clicks = append(c.clicks, &f)
		b.ConnectClicked(c.clicks[len(c.clicks)-1])
	}
	shown := a.state.Model.Shown.Session
	for _, s := range w.Sessions {
		section := gtk.NewBox(gtk.OrientationVerticalValue, 0)
		section.AddCssClass("term-session")
		head := gtk.NewButton()
		head.AddCssClass("term-session-head")
		if s.ID == shown {
			head.AddCssClass("current")
		}
		hb := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		mark := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		mark.AddCssClass("mark")
		mark.SetValign(gtk.AlignCenterValue)
		blocked, l := a.rollup(s.Tree.Panes())
		paint(mark, nil, l)
		hb.Append(&mark.Widget)
		label := gtk.NewLabel("")
		label.SetUseMarkup(true)
		label.SetMarkup("<span alpha='55%'>[</span>" + escape(strings.ToUpper(s.Name)) + "<span alpha='55%'>]</span>")
		label.AddCssClass("term-session-label")
		label.SetEllipsize(3)
		label.SetXalign(0)
		label.SetHexpand(true)
		hb.Append(&label.Widget)
		n := len(s.Tree.Panes())
		count := gtk.NewLabel(i18n.Nf("%d pane", "%d panes", n, n))
		count.AddCssClass("term-session-count")
		hb.Append(&count.Widget)
		if b := badge(blocked); b != nil {
			hb.Append(&b.Widget)
		}
		head.SetChild(&hb.Widget)
		ws, sid := w.ID, s.ID
		click(head, func() { a.showSession(ws, sid) })
		section.Append(&head.Widget)
		for _, id := range s.Tree.Panes() {
			section.Append(&c.paneRow(a, ws, s, id, click).Widget)
		}
		c.sessions.Append(&section.Widget)
	}
}

// paneRow is a pane in its session's section: its kind, its title, its
// state and folder.
func (c *columns) paneRow(a *App, ws int, s *model.Session, id int, click func(*gtk.Button, func())) *gtk.Button {
	info := a.info[id]
	b := gtk.NewButton()
	b.AddCssClass("term-pane-row")
	if s.ID == a.state.Model.Shown.Session && s.Focus == id {
		b.AddCssClass("current")
	}
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	kind := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	kind.AddCssClass("term-kind-box")
	icon := gtk.NewImageFromIconName(iconOf(info))
	icon.SetPixelSize(15)
	icon.SetHexpand(true)
	icon.SetHalign(gtk.AlignCenterValue)
	kind.Append(&icon.Widget)
	kind.SetValign(gtk.AlignCenterValue)
	kind.SetHexpand(false)
	kind.SetSizeRequest(28, 28)
	row.Append(&kind.Widget)
	text := gtk.NewBox(gtk.OrientationVerticalValue, 3)
	text.SetHexpand(true)
	title := gtk.NewLabel(paneTitle(info))
	title.AddCssClass("term-pane-title")
	title.SetXalign(0)
	title.SetEllipsize(3)
	text.Append(&title.Widget)
	line := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
	mark := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	mark.AddCssClass("mark")
	mark.SetValign(gtk.AlignCenterValue)
	line.Append(&mark.Widget)
	l := lookOf(info)
	state := gtk.NewLabel("")
	state.AddCssClass("term-state")
	paint(mark, state, l)
	if agent.State(info.State) == agent.Blocked && info.Ask != "" {
		// What it asks, in the label's place.
		b.AddCssClass("blocked")
		state.SetText(info.Ask)
		state.AddCssClass("term-ask")
		state.SetEllipsize(3)
		state.SetHexpand(true)
		state.SetXalign(0)
	}
	line.Append(&state.Widget)
	right := gtk.NewLabel(shortDir(info.Dir))
	right.AddCssClass("term-pane-dir")
	right.SetHexpand(true)
	right.SetXalign(1)
	right.SetEllipsize(1)
	if info.Agent != "" {
		right.SetText(since(info.Since, time.Now()))
		c.times = append(c.times, timed{right, id})
	}
	line.Append(&right.Widget)
	text.Append(&line.Widget)
	row.Append(&text.Widget)
	b.SetChild(&row.Widget)
	sid := s.ID
	click(b, func() { a.showPane(ws, sid, id) })
	return b
}

// showWorkspace shows a workspace, at the session it last showed.
func (a *App) showWorkspace(id int) {
	a.ask("show", func(c *host.Client) error { return c.Show(id, 0) })
}

func (a *App) showSession(ws, session int) {
	a.ask("show", func(c *host.Client) error { return c.Show(ws, session) })
}

// showPane shows a pane's session and focuses it.
func (a *App) showPane(ws, session, pane int) {
	a.ask("show", func(c *host.Client) error {
		if err := c.Show(ws, session); err != nil {
			return err
		}
		return c.Layout(session, host.OpFocus, host.Message{Pane: pane})
	})
}

// shortDir is a folder with the home folder as ~.
func shortDir(dir string) string {
	if dir == "" {
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil {
		if dir == home {
			return "~"
		}
		if rel, ok := strings.CutPrefix(dir, home+string(filepath.Separator)); ok {
			return "~/" + rel
		}
	}
	return dir
}

func baseName(p string) string { return filepath.Base(p) }

// escape is text for Pango markup.
func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "'", "&apos;", `"`, "&quot;").Replace(s)
}
