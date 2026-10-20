package mailui

import (
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/mail"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// sheet is a small window over Mail's: Move to, and the keys.
type sheet struct {
	win  *gtk.Window
	ptrs []uintptr
}

func (w *window) newSheet(title string) (*sheet, *gtk.Box) {
	s := &sheet{win: gtk.NewWindow()}
	s.win.SetTransientFor(&w.win.Window)
	s.win.SetModal(true)
	s.win.SetDestroyWithParent(true)
	s.win.SetResizable(false)
	s.win.SetTitle(title)
	for _, c := range []string{"app", "settings", "mail", "mail-dialog"} {
		s.win.AddCssClass(c)
	}
	s.ptrs = append(s.ptrs, w.h.close(s.win, func() bool { w.h.forget(s.ptrs); return false }))
	// In the capture phase: a search entry would take Escape first.
	s.ptrs = append(s.ptrs, w.h.key(&s.win.Widget, true, func(keyval uint32, _ gdk.ModifierType) bool {
		if keyval == uint32(gdk.KEY_Escape) {
			s.win.Close()
			return true
		}
		return false
	}))
	box := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	box.AddCssClass("page")
	t := gtk.NewLabel(title)
	t.AddCssClass("mail-dialog-title")
	t.SetXalign(0)
	gtkx.Append(box, &t.Widget)
	s.win.SetChild(&box.Widget)
	return s, box
}

// moveTo asks which mailbox the open conversation goes to, the list
// narrowed as the user types; Enter takes the first.
func (w *window) moveTo() {
	c := w.open
	if c == nil {
		return
	}
	x := w.acct(c.Latest().Account)
	if x == nil {
		return
	}
	here := c.Latest().Mailbox
	s, box := w.newSheet(i18n.T("Move to"))
	box.SetSizeRequest(360, -1)
	find := gtk.NewSearchEntry()
	find.SetPlaceholderText(i18n.T("Type a folder's name"))
	gtkx.Append(box, &find.Widget)
	list := gtk.NewBox(gtk.OrientationVerticalValue, 1)
	list.AddCssClass("mail-move-list")
	gtkx.Append(box, &list.Widget)
	var first func()
	var rowPtrs []uintptr
	fill := func() {
		w.h.forget(rowPtrs)
		rowPtrs = nil
		gtkx.Clear(list)
		first = nil
		typed := strings.ToLower(strings.TrimSpace(find.GetText()))
		n := 0
		for _, b := range x.boxes {
			name := boxName(b)
			if b.Name == here || (b.Role == mail.RoleDrafts) {
				continue
			}
			if typed != "" && !strings.Contains(strings.ToLower(name), typed) && !strings.Contains(strings.ToLower(b.Name), typed) {
				continue
			}
			btn := gtk.NewButton()
			btn.AddCssClass("index-item")
			btn.AddCssClass("mail-box")
			if n == 0 {
				btn.AddCssClass("selected")
			}
			l := gtk.NewLabel(name)
			l.AddCssClass("title")
			l.SetXalign(0)
			if b.Role == mail.RoleOther && name != b.Name {
				l.SetTooltipText(b.Name)
			}
			btn.SetChild(&l.Widget)
			to := b.Name
			do := func() {
				s.win.Close()
				w.moveConv(c, func(*acct) string { return to }, "", i18n.Tf("Moved to %s", name))
			}
			if first == nil {
				first = do
			}
			rowPtrs = append(rowPtrs, w.h.click(btn, do))
			gtkx.Append(list, &btn.Widget)
			n++
		}
		if n == 0 {
			l := gtk.NewLabel(i18n.T("No folder by that name"))
			l.AddCssClass("sub")
			l.SetXalign(0)
			gtkx.Append(list, &l.Widget)
		}
	}
	s.ptrs = append(s.ptrs, w.h.searched(find, fill), w.h.enter(find, func() {
		if first != nil {
			first()
		}
	}))
	fill()
	s.win.Present()
	find.GrabFocus()
}

// showKeys lists the keys.
func (w *window) showKeys() {
	s, box := w.newSheet(i18n.T("Keys"))
	grid := gtk.NewGrid()
	grid.SetColumnSpacing(18)
	grid.SetRowSpacing(6)
	grid.AddCssClass("mail-keys-sheet")
	keys := [][2]string{
		{"J  K  ↓  ↑", i18n.T("Next and previous conversation")},
		{"/  Ctrl+F", i18n.T("Search")},
		{"C", i18n.T("Write a message")},
		{"R", i18n.T("Reply")},
		{"A", i18n.T("Reply all")},
		{"F", i18n.T("Forward")},
		{"E", i18n.T("Archive")},
		{"#  Delete", i18n.T("Move to Trash")},
		{"!", i18n.T("Move to Spam")},
		{"V", i18n.T("Move to a folder")},
		{"S", i18n.T("Star or unstar")},
		{"U", i18n.T("Mark as unread")},
		{"Z", i18n.T("Undo the last archive, delete or move")},
		{"Ctrl+Enter", i18n.T("Send (in a reply or the composer)")},
		{"Esc", i18n.T("Leave the search or the reply field")},
		{"?", i18n.T("These keys")},
	}
	for i, k := range keys {
		key := gtk.NewLabel(k[0])
		key.AddCssClass("mail-key")
		key.SetXalign(0)
		grid.Attach(&key.Widget, 0, int32(i), 1, 1)
		what := gtk.NewLabel(k[1])
		what.AddCssClass("mail-key-what")
		what.SetXalign(0)
		grid.Attach(&what.Widget, 1, int32(i), 1, 1)
	}
	gtkx.Append(box, &grid.Widget)
	s.win.Present()
}
