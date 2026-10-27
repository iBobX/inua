package mailui

import (
	"slices"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/mail"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// A message's people and its details: a click on who wrote (the face or
// the name) or on anyone in the details opens their card; "to …" opens
// the details under the head, as the header has them; Source shows the
// message as it came.

// personButton is a person's name (child) as a button opening their card;
// verified as for their face.
func (w *window) personButton(a mail.Address, child *gtk.Widget, verified bool) *gtk.MenuButton {
	b := gtk.NewMenuButton()
	b.AddCssClass("mail-plain")
	b.AddCssClass("mail-person-btn")
	b.SetChild(child)
	b.SetTooltipText(a.String())
	b.SetPopover(w.personCard(a, verified))
	return b
}

// personCard is a person's card: their face, name and address, and what
// is done with them at once.
func (w *window) personCard(a mail.Address, verified bool) *gtk.Popover {
	pop := gtk.NewPopover()
	pop.AddCssClass("mail-menu")
	pop.AddCssClass("mail-card")
	pop.SetHasArrow(false)
	box := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	head := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	head.AddCssClass("mail-card-head")
	gtkx.Append(head, w.senderMark(a, 40, verified))
	who := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	who.SetValign(gtk.AlignCenterValue)
	if a.Name != "" {
		name := gtk.NewLabel(a.Name)
		name.AddCssClass("mail-from")
		name.SetXalign(0)
		name.SetMaxWidthChars(32)
		name.SetEllipsize(3)
		gtkx.Append(who, &name.Widget)
	}
	addr := gtk.NewLabel(a.Addr)
	addr.AddCssClass("mail-from-addr")
	addr.SetXalign(0)
	addr.SetSelectable(true)
	addr.SetWrap(true)
	addr.SetWrapMode(2) // word_char
	addr.SetMaxWidthChars(32)
	gtkx.Append(who, &addr.Widget)
	gtkx.Append(head, &who.Widget)
	gtkx.Append(box, &head.Widget)

	item := func(icon, label string, f func()) {
		b := gtk.NewButton()
		b.AddCssClass("mail-menu-item")
		in := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		img := gtk.NewImageFromIconName(icon)
		img.SetPixelSize(16)
		gtkx.Append(in, &img.Widget)
		l := gtk.NewLabel(label)
		l.SetXalign(0)
		gtkx.Append(in, &l.Widget)
		b.SetChild(&in.Widget)
		w.readerBtns = append(w.readerBtns, w.h.click(b, func() { pop.Popdown(); f() }))
		gtkx.Append(box, &b.Widget)
	}
	item("mail-drafts-symbolic", i18n.T("Write"), func() { w.newComposer(mail.Draft{To: a.String()}, nil) })
	item("edit-copy-symbolic", i18n.T("Copy address"), func() { w.win.GetClipboard().SetText(a.Addr) })
	item("edit-find-symbolic", i18n.T("Their conversations"), func() {
		w.search.SetText(a.Addr)
		w.searchEntered()
	})
	addr2 := strings.ToLower(a.Addr)
	if r := &w.settings.Reading; r.Images != "never" && r.Images != "always" && !w.mine(a.Addr) {
		on := slices.Contains(r.ImagesFrom, addr2)
		label := i18n.T("Always load pictures")
		if on {
			label = i18n.T("Ask before loading pictures")
		}
		item("image-x-generic-symbolic", label, func() {
			if on {
				r.ImagesFrom = slices.DeleteFunc(r.ImagesFrom, func(s string) bool { return strings.EqualFold(s, addr2) })
			} else {
				r.ImagesFrom = append(r.ImagesFrom, addr2)
			}
			if err := w.settings.Save(mail.SettingsPath()); err != nil {
				w.actErr = i18n.Tf("Not saved: %v", err)
			}
			w.lastDrawn = ""
			w.drawReader()
		})
	}
	pop.SetChild(&box.Widget)
	return pop
}

// drawDetails shows a message's details under its head: each line's
// name, then its people (each opening their card) or its text; Source
// at the end.
func (w *window) drawDetails(box *gtk.Box, it mail.Item, m *mail.Message) {
	grid := gtk.NewGrid()
	grid.AddCssClass("mail-details")
	grid.SetColumnSpacing(14)
	grid.SetRowSpacing(4)
	row := 0
	for _, d := range mail.Details(m) {
		name := gtk.NewLabel(d.Name)
		name.AddCssClass("mail-details-name")
		name.SetXalign(1)
		name.SetValign(gtk.AlignStartValue)
		grid.Attach(&name.Widget, 0, int32(row), 1, 1)
		if len(d.People) > 0 {
			flow := gtk.NewFlowBox()
			flow.SetSelectionMode(gtk.SelectionNoneValue)
			flow.SetMaxChildrenPerLine(20)
			flow.SetColumnSpacing(10)
			flow.SetRowSpacing(0)
			flow.SetHexpand(true)
			for _, a := range d.People {
				l := gtk.NewLabel(a.String())
				l.AddCssClass("mail-details-value")
				l.SetEllipsize(3)
				l.SetMaxWidthChars(48)
				b := w.personButton(a, &l.Widget, strings.EqualFold(a.Addr, it.From.Addr) && w.verified(it))
				flow.Append(&b.Widget)
			}
			grid.Attach(&flow.Widget, 1, int32(row), 1, 1)
		} else {
			v := gtk.NewLabel(d.Text)
			v.AddCssClass("mail-details-value")
			if d.Mono {
				v.AddCssClass("readout")
			}
			v.SetXalign(0)
			v.SetHexpand(true)
			v.SetWrap(true)
			v.SetWrapMode(2) // word_char
			v.SetSelectable(true)
			grid.Attach(&v.Widget, 1, int32(row), 1, 1)
		}
		row++
	}
	gtkx.Append(box, &grid.Widget)
	links := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	links.AddCssClass("mail-details-links")
	w.chip(links, "mail-source-symbolic", i18n.T("Source"), func() { w.showSource(it) })
	gtkx.Append(box, &links.Widget)
}

// sourceWin is a message as it came, in a window of its own.
type sourceWin struct {
	win  *gtk.Window
	ptrs []uintptr
}

// sourceMax is how much of a message the source window shows: a large
// one's files are base64 text no one reads; Save keeps all of it.
const sourceMax = 2 << 20

// showSource opens a message's source: its header and its parts, as the
// server has them, with Copy and Save.
func (w *window) showSource(it mail.Item) {
	x := w.acct(it.Account)
	if x == nil {
		return
	}
	raw, ok := x.s.Raw(it.Mailbox, it.UID)
	if !ok {
		w.actErr = i18n.T("The message's source is not kept on this computer yet: open it first.")
		w.drawReader()
		return
	}
	s := &sourceWin{win: gtk.NewWindow()}
	s.win.SetTransientFor(&w.win.Window)
	s.win.SetDestroyWithParent(true)
	s.win.SetDefaultSize(820, 680)
	subject := it.Subject
	if subject == "" {
		subject = i18n.T("(no subject)")
	}
	s.win.SetTitle(i18n.Tf("Source of “%s”", subject))
	for _, c := range []string{"app", "settings", "mail", "mail-source"} {
		s.win.AddCssClass(c)
	}
	done := func() {
		w.h.forget(s.ptrs)
		s.win.Destroy()
	}
	s.ptrs = append(s.ptrs, w.h.close(s.win, func() bool { w.h.forget(s.ptrs); return false }))
	s.ptrs = append(s.ptrs, w.h.key(&s.win.Widget, false, func(keyval uint32, _ gdk.ModifierType) bool {
		if keyval == uint32(gdk.KEY_Escape) {
			done()
			return true
		}
		return false
	}))

	page := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	bar := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	bar.AddCssClass("mail-source-bar")
	t := gtk.NewLabel(subject)
	t.AddCssClass("mail-from")
	t.SetXalign(0)
	t.SetHexpand(true)
	t.SetEllipsize(3)
	gtkx.Append(bar, &t.Widget)
	size := gtk.NewLabel(sizeText(int64(len(raw))))
	size.AddCssClass("mail-date")
	gtkx.Append(bar, &size.Widget)
	text := strings.ToValidUTF8(string(raw), "�")
	cp := gtk.NewButtonWithLabel(i18n.T("Copy"))
	cp.AddCssClass("outlined")
	s.ptrs = append(s.ptrs, w.h.click(cp, func() { s.win.GetClipboard().SetText(text) }))
	gtkx.Append(bar, &cp.Widget)
	save := gtk.NewButtonWithLabel(i18n.T("Save…"))
	save.AddCssClass("outlined")
	name := mail.SafeName(subject) + ".eml"
	s.ptrs = append(s.ptrs, w.h.click(save, func() { w.saveData(i18n.T("Save the message"), name, raw) }))
	gtkx.Append(bar, &save.Widget)
	gtkx.Append(page, &bar.Widget)

	shown := text
	if len(shown) > sourceMax {
		shown = strings.ToValidUTF8(shown[:sourceMax], "")
		note := gtk.NewLabel(i18n.Tf("The first %s are shown; Save keeps all of it.", sizeText(sourceMax)))
		note.AddCssClass("mail-source-note")
		note.SetXalign(0)
		gtkx.Append(page, &note.Widget)
	}
	view := gtk.NewTextView()
	view.AddCssClass("mail-source-text")
	view.SetEditable(false)
	view.SetMonospace(true)
	view.SetWrapMode(gtk.WrapCharValue)
	view.SetTopMargin(12)
	view.SetBottomMargin(12)
	view.SetLeftMargin(16)
	view.SetRightMargin(16)
	view.GetBuffer().SetText(shown, -1)
	scroll := gtk.NewScrolledWindow()
	scroll.SetVexpand(true)
	scroll.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	scroll.SetChild(&view.Widget)
	gtkx.Append(page, &scroll.Widget)
	s.win.SetChild(&page.Widget)
	s.win.Present()
}
