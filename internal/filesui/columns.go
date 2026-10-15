package filesui

import (
	"os/user"
	"path/filepath"
	"strconv"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// column is one of the list's columns after the name. Each has a fixed
// width, the head's and the rows' alike, so they line up whatever the
// text.
type column struct {
	by    files.SortBy
	title string // in small capitals
	width int32
	right bool // numbers end at the right
}

var columns = [...]column{
	{files.BySize, i18n.M("SIZE"), 86, true},
	{files.ByModified, i18n.M("MODIFIED"), 128, false},
	{files.ByCreated, i18n.M("CREATED"), 128, false},
	{files.ByOpened, i18n.M("OPENED"), 128, false},
	{files.ByKind, i18n.M("KIND"), 150, false},
	{files.ByPerms, i18n.M("PERMISSIONS"), 92, false},
	{files.ByOwner, i18n.M("OWNER"), 84, false},
	{files.ByGroup, i18n.M("GROUP"), 84, false},
}

// trailing is the width at the end of the head that holds the columns
// button; rows leave it empty, so both line up.
const trailing = 24

// columnTitle is a column's head in what the pane shows: the trash's
// dates are when items were trashed and its kinds where they came from;
// a search's kinds are the folders found in.
func (p *pane) columnTitle(c column) string {
	switch {
	case p.mode == inTrash && c.by == files.ByModified:
		return i18n.T("TRASHED")
	case p.mode == inTrash && c.by == files.ByKind:
		return i18n.T("FROM")
	case p.mode == inSearch && c.by == files.ByKind:
		return i18n.T("FOLDER")
	}
	return i18n.T(c.title)
}

// columnText is what the row of the entry at i shows in column c.
func (p *pane) columnText(c column, i int, path string) string {
	e := &p.all[i]
	switch c.by {
	case files.BySize:
		if e.Dir {
			return p.w.app.counts.text(path, p)
		}
		return sizeText(e.Size)
	case files.ByModified:
		return dateText(e.ModTime)
	case files.ByCreated:
		return dateText(e.Created)
	case files.ByOpened:
		return dateText(e.Opened)
	case files.ByKind:
		switch p.mode {
		case inTrash:
			return shortHome(filepath.Dir(p.trash[i].Orig))
		case inSearch:
			return shortHome(p.dirs[i])
		}
		return p.w.app.kinds.of(e).desc
	case files.ByPerms:
		return e.Mode.String()
	case files.ByOwner:
		return p.w.app.userName(e.UID)
	case files.ByGroup:
		return p.w.app.groupName(e.GID)
	}
	return ""
}

// fixedLabel is a label of exactly width: its text is cut, never let
// widen it.
func fixedLabel(width int32, right bool) *gtk.Label {
	l := gtk.NewLabel("")
	l.SetSizeRequest(width, -1)
	l.SetMaxWidthChars(1)
	l.SetEllipsize(3)
	l.SetXalign(map[bool]float32{true: 1, false: 0}[right])
	return l
}

// buildColumns makes the column heads: the name's, one for each column,
// and the button that chooses them.
func (p *pane) buildColumns() {
	w := p.w
	name := gtk.NewButton()
	name.AddCssClass("files-col")
	name.SetHexpand(true)
	p.nameHead = *gtk.NewLabel("")
	p.nameHead.AddCssClass("kicker")
	p.nameHead.SetXalign(0)
	name.SetChild(&p.nameHead.Widget)
	w.click(name, func() { p.sortBy(files.ByName) })
	gtkx.Append(&p.columns, &name.Widget)
	for k, c := range columns {
		b := gtk.NewButton()
		b.AddCssClass("files-col")
		l := fixedLabel(c.width, c.right)
		l.AddCssClass("kicker")
		b.SetChild(&l.Widget)
		p.colHeads[k], p.colLabels[k] = b, l
		by := c.by
		w.click(b, func() { p.sortBy(by) })
		gtkx.Append(&p.columns, &b.Widget)
	}
	more := gtk.NewButtonFromIconName("view-more-symbolic")
	more.AddCssClass("plain")
	more.AddCssClass("files-col-more")
	more.SetValign(gtk.AlignCenterValue)
	more.SetSizeRequest(trailing, -1)
	more.SetTooltipText(i18n.T("Columns"))
	w.click(more, func() {
		var x, y float64
		more.TranslateCoordinates(&p.box.Widget, 0, float64(more.GetHeight()), &x, &y)
		p.columnsMenu(int32(x), int32(y))
	})
	gtkx.Append(&p.columns, &more.Widget)
	w.press(&p.columns.Widget, false, func(button uint32, _ int32, x, y float64) {
		if button == 3 {
			var px, py float64
			p.columns.TranslateCoordinates(&p.box.Widget, x, y, &px, &py)
			p.columnsMenu(int32(px), int32(py))
		}
	})
}

// columnsMenu turns the columns on and off.
func (p *pane) columnsMenu(x, y int32) {
	p.menu.show(func() []menuItem {
		var items []menuItem
		for _, c := range columns {
			by := c.by
			items = append(items, menuItem{label: p.columnTitle(c), check: true, on: p.w.app.settings.ShowsColumn(by), run: func() {
				p.w.app.settings.ToggleColumn(by)
				p.w.app.columnsChanged()
			}})
		}
		return items
	}, x, y)
}

// syncColumns shows the heads of the columns chosen, the order marked on
// the one sorted by, and the name's head over the names.
func (p *pane) syncColumns() {
	s := p.w.app.settings
	by := p.fv.Order.By
	if by == "" {
		by = files.ByName
	}
	arrow := func(on bool) string {
		switch {
		case !on || p.mode != inFolder:
			return ""
		case p.fv.Order.Reverse:
			return " ↓"
		}
		return " ↑"
	}
	p.nameHead.SetText(i18n.T("NAME") + arrow(by == files.ByName))
	p.nameHead.SetMarginStart(listIcon(s.Zoom) + 10) // past the icons
	for k, c := range columns {
		p.colHeads[k].SetVisible(s.ShowsColumn(c.by))
		p.colLabels[k].SetText(p.columnTitle(c) + arrow(by == c.by))
	}
}

// userName and groupName are an owner's names, looked up once.
func (a *App) userName(uid uint32) string {
	if n, ok := a.users[uid]; ok {
		return n
	}
	n := strconv.Itoa(int(uid))
	if u, err := user.LookupId(n); err == nil {
		n = u.Username
	}
	a.users[uid] = n
	return n
}

func (a *App) groupName(gid uint32) string {
	if n, ok := a.groups[gid]; ok {
		return n
	}
	n := strconv.Itoa(int(gid))
	if g, err := user.LookupGroupId(n); err == nil {
		n = g.Name
	}
	a.groups[gid] = n
	return n
}

// columnsChanged shows the columns chosen in every pane.
func (a *App) columnsChanged() {
	for _, w := range a.wins {
		for _, p := range w.p {
			p.syncColumns()
			p.rebind()
		}
	}
}
