package mailui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/mail"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// The Contacts view: the people of the accounts' address books in the
// list's place, through the search; the one chosen in the reader, with
// their addresses to write to and the conversations with them. The index
// has it only while an account has an address book.

// personFace and personBigFace are a person's face in the list and on
// their page.
const (
	personFace    = 32
	personBigFace = 56
)

// personRow is a row of the people's list.
type personRow struct {
	item       uintptr
	box        gtk.Box
	face       face
	name, addr gtk.Label
}

// setupPeople makes the people's list, a page of the list's stack.
func (w *window) setupPeople() {
	w.personRows = map[uintptr]*personRow{}
	w.peopleModel = gtk.NewStringList(nil)
	w.peopleSel = gtk.NewSingleSelection(w.peopleModel)
	w.peopleSel.SetAutoselect(false)
	w.peopleSel.SetCanUnselect(true)
	w.peopleSel.Ref()
	w.h.selection(w.peopleSel, w.personChosen)
	f := w.h.factory(w.setupPerson, w.bindPerson, w.teardownPerson)
	w.peopleList = gtk.NewListView(w.peopleSel, &f.ListItemFactory)
	w.peopleList.AddCssClass("mail-people")
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	scroll.SetChild(&w.peopleList.Widget)
	w.listStack.AddNamed(&scroll.Widget, "people")
}

// hasBook says whether an account has an address book with someone in
// it: the index's Contacts is shown then.
func (w *window) hasBook() bool {
	return slices.ContainsFunc(w.accts, func(x *acct) bool { return len(x.book) > 0 })
}

// bookChanged follows an account's address book read: Contacts comes or
// goes in the index, and the view shows it.
func (w *window) bookChanged() {
	if w.view.kind == viewPeople && !w.hasBook() {
		w.setView(view{kind: viewInboxes})
		return
	}
	w.drawSide()
	if w.view.kind == viewPeople {
		w.refresh()
	}
}

// refreshPeople lists the people again, through the search, and keeps
// the one chosen chosen.
func (w *window) refreshPeople() {
	if len(w.ids) > 0 {
		// No conversation is listed: the next one after an action is none.
		w.ids, w.convs = nil, nil
		w.model.Splice(0, w.model.GetNItems(), nil)
	}
	w.listProgress.SetVisible(false)
	var book []mail.Address
	for _, x := range w.accts {
		book = append(book, x.book...)
	}
	var ps []mail.Person
	for _, p := range mail.People(book, w.mine) {
		if p.Match(w.query) {
			ps = append(ps, p)
		}
	}
	keys := make([]string, len(ps))
	for i, p := range ps {
		keys[i] = strings.ToLower(p.Addrs[0])
	}
	w.people = ps
	if !slices.Equal(keys, w.peopleKeys) {
		w.peopleKeys = keys
		w.peopleModel.Splice(0, w.peopleModel.GetNItems(), keys)
	}
	for _, r := range w.personRows {
		w.fillPerson(r)
	}
	if i := slices.Index(keys, w.person); w.person != "" && i >= 0 {
		if w.peopleSel.GetSelected() != uint32(i) {
			w.peopleSel.SetSelected(uint32(i))
		}
	} else if w.person != "" {
		w.person = ""
		w.peopleSel.UnselectAll()
	}
	if len(ps) == 0 {
		w.listStack.SetVisibleChildName("empty")
		w.listEmpty.SetText(i18n.T("Nobody found"))
	} else {
		w.listStack.SetVisibleChildName("people")
	}
	switch {
	case w.open != nil:
		w.drawReader()
	case w.person != "":
		w.drawPerson()
	case !w.unsent:
		w.readerEmpty.SetText(i18n.T("No one chosen"))
		w.readerStack.SetVisibleChildName("empty")
	}
	w.sideCounts()
}

func (w *window) setupPerson(item uintptr) {
	li := gtk.ListItemNewFromInternalPtr(item)
	r := &personRow{item: item}
	r.box = *gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	r.box.AddCssClass("mail-person-row")
	r.face = *newFace(personFace)
	r.face.box.SetValign(gtk.AlignCenterValue)
	gtkx.Append(&r.box, &r.face.box.Widget)
	col := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	col.SetHexpand(true)
	col.SetValign(gtk.AlignCenterValue)
	r.name = *label("mail-who", true)
	r.name.AddCssClass("mail-person-name")
	gtkx.Append(col, &r.name.Widget)
	r.addr = *label("mail-to", true)
	gtkx.Append(col, &r.addr.Widget)
	gtkx.Append(&r.box, &col.Widget)
	li.SetChild(&r.box.Widget)
	r.box.Unref()
	w.personRows[item] = r
}

func (w *window) bindPerson(item uintptr) {
	if r := w.personRows[item]; r != nil {
		w.fillPerson(r)
	}
}

func (w *window) teardownPerson(item uintptr) { delete(w.personRows, item) }

// fillPerson shows a row's person; its position is read each time, as
// for a conversation's row.
func (w *window) fillPerson(r *personRow) {
	pos := gtk.ListItemNewFromInternalPtr(r.item).GetPosition()
	if int(pos) >= len(w.people) {
		return
	}
	p := w.people[pos]
	r.name.SetText(p.Title())
	addr := p.Addrs[0]
	if n := len(p.Addrs) - 1; n > 0 {
		addr = i18n.Nf("%s and %d more", "%s and %d more", n, p.Addrs[0], n)
	}
	r.addr.SetText(addr)
	r.addr.SetVisible(p.Name != "")
	w.setFace(&r.face, mail.Address{Name: p.Name, Addr: p.Addrs[0]}, false)
}

// personChosen shows the person the list chose, in the reader.
func (w *window) personChosen() {
	i := int(w.peopleSel.GetSelected())
	if i >= len(w.people) {
		return
	}
	key := strings.ToLower(w.people[i].Addrs[0])
	if key == w.person && w.open == nil {
		return
	}
	w.person = key
	w.open, w.openID = nil, ""
	w.drawPerson()
}

// chosenPerson is the person chosen, nil for none.
func (w *window) chosenPerson() *mail.Person {
	i := slices.Index(w.peopleKeys, w.person)
	if w.person == "" || i < 0 || i >= len(w.people) {
		return nil
	}
	return &w.people[i]
}

// withPerson are the conversations kept with any of a person's
// addresses, the newest first: in the inboxes, the archives and Sent.
func (w *window) withPerson(p *mail.Person) []*mail.Conversation {
	var items []mail.Item
	for _, x := range w.accts {
		for _, r := range []mail.Role{mail.RoleInbox, mail.RoleArchive, mail.RoleSent} {
			if name := x.s.Role(r); name != "" {
				items = append(items, x.s.Items(name)...)
			}
		}
	}
	theirs := func(a mail.Address) bool {
		return slices.ContainsFunc(p.Addrs, func(s string) bool { return strings.EqualFold(s, a.Addr) })
	}
	items = slices.DeleteFunc(items, func(it mail.Item) bool {
		return !theirs(it.From) && !slices.ContainsFunc(it.To, theirs) && !slices.ContainsFunc(it.Cc, theirs)
	})
	return mail.Group(items, nil)
}

// drawPerson fills the reader with the person chosen: their face and
// name, each address with Write, then the conversations with them, each
// opening in the reader.
func (w *window) drawPerson() {
	p := w.chosenPerson()
	if p == nil {
		return
	}
	convs := w.withPerson(p)
	sig := fmt.Sprint("person ", p.Name, p.Addrs, len(convs))
	for _, c := range convs {
		sig += " " + c.ID + fmt.Sprint(len(c.Messages))
	}
	if sig == w.lastDrawn && w.readerStack.GetVisibleChildName() == "read" {
		return
	}
	w.h.forget(w.readerBtns)
	w.readerBtns = nil
	w.lastDrawn = sig
	gtkx.Clear(&w.readerBar)
	gtkx.Clear(&w.reader)
	w.readerStack.SetVisibleChildName("read")
	w.readerScroll.GetVadjustment().SetValue(0)

	head := gtk.NewBox(gtk.OrientationHorizontalValue, 16)
	head.AddCssClass("mail-person-head")
	gtkx.Append(head, w.senderMark(mail.Address{Name: p.Name, Addr: p.Addrs[0]}, personBigFace, false))
	name := gtk.NewLabel(p.Title())
	name.AddCssClass("mail-read-subject")
	name.SetXalign(0)
	name.SetValign(gtk.AlignCenterValue)
	name.SetHexpand(true)
	name.SetWrap(true)
	name.SetWrapMode(2) // word_char
	name.SetSelectable(true)
	gtkx.Append(head, &name.Widget)
	gtkx.Append(&w.reader, &head.Widget)

	for _, a := range p.Addrs {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		row.AddCssClass("mail-person-addr")
		l := gtk.NewLabel(a)
		l.AddCssClass("mail-from-addr")
		l.SetXalign(0)
		l.SetHexpand(true)
		l.SetEllipsize(3)
		l.SetSelectable(true)
		gtkx.Append(row, &l.Widget)
		b := gtk.NewButtonWithLabel(i18n.T("Write"))
		b.AddCssClass("outlined")
		b.SetValign(gtk.AlignCenterValue)
		b.SetTooltipText(i18n.Tf("Write a message to %s", a))
		to := mail.Address{Name: p.Name, Addr: a}
		w.readerBtns = append(w.readerBtns, w.h.click(b, func() { w.newComposer(mail.Draft{To: to.String()}, nil) }))
		gtkx.Append(row, &b.Widget)
		gtkx.Append(&w.reader, &row.Widget)
	}

	t := gtk.NewLabel(i18n.T("CONVERSATIONS"))
	t.AddCssClass("mail-person-section")
	t.SetXalign(0)
	gtkx.Append(&w.reader, &t.Widget)
	if len(convs) == 0 {
		l := gtk.NewLabel(i18n.T("No mail with them kept on this computer."))
		l.AddCssClass("sub")
		l.SetXalign(0)
		gtkx.Append(&w.reader, &l.Widget)
	}
	now := time.Now()
	for _, c := range convs {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		row.AddCssClass("mail-person-conv")
		open := gtk.NewButton()
		open.AddCssClass("mail-person-open")
		open.SetHexpand(true)
		open.SetTooltipText(i18n.T("Open"))
		texts := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		subject := c.Subject()
		if subject == "" {
			subject = i18n.T("(no subject)")
		}
		s := label("mail-subject-line", true)
		s.SetText(subject)
		gtkx.Append(texts, &s.Widget)
		latest := c.Latest()
		meta := label("mail-to", true)
		meta.SetText(latest.Snippet)
		meta.SetVisible(latest.Snippet != "")
		gtkx.Append(texts, &meta.Widget)
		open.SetChild(&texts.Widget)
		cc := c
		w.readerBtns = append(w.readerBtns, w.h.click(open, func() { w.openConversation(cc) }))
		gtkx.Append(row, &open.Widget)
		at := label("mail-when", false)
		at.SetValign(gtk.AlignStartValue)
		at.SetText(when(latest.Date, now))
		gtkx.Append(row, &at.Widget)
		gtkx.Append(&w.reader, &row.Widget)
	}
}

// movePerson moves the choice in the people's list, as move does in the
// conversations'.
func (w *window) movePerson(d int) {
	if len(w.people) == 0 {
		return
	}
	i := int(w.peopleSel.GetSelected())
	if i >= len(w.people) {
		i = -1
		if d < 0 {
			i = len(w.people)
		}
	}
	i = max(0, min(len(w.people)-1, i+d))
	w.peopleSel.SetSelected(uint32(i))
	w.peopleList.ScrollTo(uint32(i), gtk.ListScrollFocusValue, nil)
}

// leavePeople ends the Contacts view: nobody chosen, and the reader empty
// until a conversation opens.
func (w *window) leavePeople() {
	w.person = ""
	w.peopleSel.UnselectAll()
	w.readerEmpty.SetText(i18n.T("No conversation chosen"))
	if w.open == nil && !w.unsent {
		w.lastDrawn = ""
		w.readerStack.SetVisibleChildName("empty")
	}
}
