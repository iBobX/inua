package mailui

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/mail"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// settingsWin is Mail's settings, in the look of inua's Settings: an
// index of the pages (each account, Reading, Writing, the assistant,
// Advanced) and the chosen one's page, its sections' heads held at the
// top as it scrolls. Changes are kept at once.
type settingsWin struct {
	w      *window
	win    *gtk.Window
	index  *gtk.ListBox
	status *gtk.Label // why a save failed, under the index
	scroll *gtk.ScrolledWindow
	page   *gtk.Box
	at     string // the page shown: "account:ID", "reading", "writing", "assistant", "advanced"

	sticky                  *gtk.Box // the head of the section scrolled under the top edge
	stickyLabel, stickyMeta *gtk.Label
	stickyShown             uintptr

	ptrs     []uintptr // the window's
	pptrs    []uintptr // the page's and the index's, made again with them
	building bool      // the index is being filled: its selection is not the user's
	gen      int
}

// settingsIcons are the pages' icons in the index.
var settingsIcons = map[string]string{
	"reading":   "mail-read-symbolic",
	"writing":   "document-edit-symbolic",
	"assistant": "applications-science-symbolic",
	"advanced":  "emblem-system-symbolic",
	"add":       "list-add-symbolic",
}

// showSettings opens the settings, at an account's page when one is
// named.
func (w *window) showSettings(account string) {
	at := "reading"
	if account != "" {
		at = "account:" + account
	} else if len(w.accts) > 0 {
		at = "account:" + w.accts[0].s.Acc.ID
	}
	if s := w.settingsWin; s != nil {
		s.at = at
		s.draw()
		s.win.Present()
		return
	}
	s := &settingsWin{w: w, at: at}
	w.settingsWin = s
	s.win = gtk.NewWindow()
	s.win.SetTransientFor(&w.win.Window)
	s.win.SetDestroyWithParent(true)
	s.win.SetDefaultSize(820, 600)
	s.win.SetModal(true) // Hyprland floats it over Mail, which tiles
	s.win.SetTitle(i18n.T("Mail settings"))
	for _, c := range []string{"settings", "mail", "mail-settings"} {
		s.win.AddCssClass(c)
	}
	s.ptrs = append(s.ptrs, w.h.close(s.win, func() bool {
		w.h.forget(s.ptrs)
		w.h.forget(s.pptrs)
		w.settingsWin = nil
		return false
	}))
	s.ptrs = append(s.ptrs, w.h.key(&s.win.Widget, false, func(keyval uint32, _ gdk.ModifierType) bool {
		if keyval == uint32(gdk.KEY_Escape) {
			s.win.Close()
			return true
		}
		return false
	}))

	root := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	side := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	side.AddCssClass("index")
	side.SetSizeRequest(220, -1)
	title := gtk.NewLabel(i18n.T("Mail"))
	title.AddCssClass("index-title")
	title.SetXalign(0)
	gtkx.Append(side, &title.Widget)
	iscroll := gtk.NewScrolledWindow()
	iscroll.SetVexpand(true)
	iscroll.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	s.index = gtk.NewListBox()
	s.index.SetSelectionMode(gtk.SelectionBrowseValue)
	iscroll.SetChild(&s.index.Widget)
	gtkx.Append(side, &iscroll.Widget)
	s.status = gtk.NewLabel("")
	s.status.AddCssClass("status")
	s.status.AddCssClass("error")
	s.status.SetXalign(0)
	s.status.SetWrap(true)
	s.status.SetVisible(false)
	gtkx.Append(side, &s.status.Widget)
	gtkx.Append(root, &side.Widget)
	s.ptrs = append(s.ptrs, w.h.rowSelected(s.index, func(row uintptr) {
		if row == 0 || s.building {
			return
		}
		name := gtk.ListBoxRowNewFromInternalPtr(row).GetName()
		switch {
		case name == "add":
			s.win.Close()
			w.win.Present()
			w.showWelcome("", nil)
		case name != "" && name != s.at:
			s.at = name
			s.drawPage()
		}
	}))

	over := gtk.NewOverlay()
	over.SetHexpand(true)
	s.scroll = gtk.NewScrolledWindow()
	s.scroll.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	s.page = gtk.NewBox(gtk.OrientationVerticalValue, 0)
	s.page.AddCssClass("page")
	s.scroll.SetChild(&s.page.Widget)
	over.SetChild(&s.scroll.Widget)
	s.sticky = gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	s.sticky.AddCssClass("section-head")
	s.sticky.AddCssClass("sticky")
	s.sticky.SetValign(gtk.AlignStartValue)
	s.sticky.SetVisible(false)
	mark := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	mark.AddCssClass("mark")
	mark.SetValign(gtk.AlignCenterValue)
	gtkx.Append(s.sticky, &mark.Widget)
	s.stickyLabel = gtk.NewLabel("")
	s.stickyLabel.AddCssClass("section-label")
	s.stickyLabel.SetUseMarkup(true)
	gtkx.Append(s.sticky, &s.stickyLabel.Widget)
	s.stickyMeta = gtk.NewLabel("")
	s.stickyMeta.AddCssClass("section-meta")
	s.stickyMeta.SetHexpand(true)
	s.stickyMeta.SetXalign(0)
	s.stickyMeta.SetEllipsize(3)
	gtkx.Append(s.sticky, &s.stickyMeta.Widget)
	over.AddOverlay(&s.sticky.Widget)
	s.ptrs = append(s.ptrs, w.h.notify(s.scroll.GetVadjustment(), "value", s.updateSticky))
	gtkx.Append(root, &over.Widget)
	s.win.SetChild(&root.Widget)
	s.draw()
	s.win.Present()
}

// draw fills the index and the page.
func (s *settingsWin) draw() {
	w := s.w
	s.building = true
	s.index.RemoveAll()
	heading := func(t string) {
		l := gtk.NewLabel(t)
		l.AddCssClass("kicker")
		l.AddCssClass("mail-settings-kicker")
		l.SetXalign(0)
		row := gtk.NewListBoxRow()
		row.SetChild(&l.Widget)
		row.SetSelectable(false)
		row.SetActivatable(false)
		row.AddCssClass("mail-settings-heading")
		s.index.Append(&row.Widget)
	}
	item := func(name, title string, lead *gtk.Widget) {
		box := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		if lead == nil {
			img := gtk.NewImageFromIconName(settingsIcons[name])
			img.AddCssClass("index-icon")
			lead = &img.Widget
		}
		gtkx.Append(box, lead)
		l := gtk.NewLabel(title)
		l.SetXalign(0)
		l.SetEllipsize(3)
		gtkx.Append(box, &l.Widget)
		row := gtk.NewListBoxRow()
		row.SetName(name)
		row.SetChild(&box.Widget)
		s.index.Append(&row.Widget)
		if name == s.at {
			s.index.SelectRow(row)
		}
	}
	heading(i18n.T("ACCOUNTS"))
	for _, x := range w.accts {
		item("account:"+x.s.Acc.ID, x.s.Acc.DisplayName(), w.avatarWidget(x))
	}
	item("add", i18n.T("Add account"), nil)
	heading(i18n.T("MAIL"))
	item("reading", i18n.T("Reading"), nil)
	item("writing", i18n.T("Writing"), nil)
	item("assistant", i18n.T("Assistant"), nil)
	item("advanced", i18n.T("Advanced"), nil)
	s.building = false
	s.drawPage()
}

// drawPage fills the page shown.
func (s *settingsWin) drawPage() {
	w := s.w
	w.h.forget(s.pptrs)
	s.pptrs = nil
	s.gen++
	gtkx.Clear(s.page)
	s.stickyShown = 0
	s.sticky.SetVisible(false)
	switch {
	case strings.HasPrefix(s.at, "account:"):
		if x := w.acct(strings.TrimPrefix(s.at, "account:")); x != nil {
			s.accountPage(x)
			break
		}
		s.at = "reading"
		s.readingPage()
	case s.at == "writing":
		s.writingPage()
	case s.at == "assistant":
		s.assistantPage()
	case s.at == "advanced":
		s.advancedPage()
	default:
		s.readingPage()
	}
	s.scroll.GetVadjustment().SetValue(0)
}

// updateSticky shows the head of the last section whose own head has
// scrolled under the top edge, as inua's Settings does.
func (s *settingsWin) updateSticky() {
	top := s.scroll.GetVadjustment().GetValue()
	var cur, head *gtk.Widget
	for sec := s.page.GetFirstChild(); sec != nil; sec = sec.GetNextSibling() {
		if !sec.HasCssClass("section") {
			continue
		}
		hd := sec.GetFirstChild()
		var r graphene.Rect
		if hd == nil || !hd.ComputeBounds(&s.page.Widget, &r) {
			continue
		}
		if y := float64((*(*[4]float32)(unsafe.Pointer(&r)))[1]); y < top {
			cur, head = sec, hd
		}
	}
	if cur == nil {
		s.stickyShown = 0
		s.sticky.SetVisible(false)
		return
	}
	if cur.Ptr != s.stickyShown {
		s.stickyShown = cur.Ptr
		s.stickyMeta.SetLabel("")
		for c := head.GetFirstChild(); c != nil; c = c.GetNextSibling() {
			switch {
			case c.HasCssClass("section-label"):
				s.stickyLabel.SetLabel(gtk.LabelNewFromInternalPtr(c.Ptr).GetLabel())
			case c.HasCssClass("section-meta"):
				s.stickyMeta.SetLabel(gtk.LabelNewFromInternalPtr(c.Ptr).GetLabel())
			}
		}
	}
	s.sticky.SetVisible(true)
}

// --- building blocks ---

// section adds a section to the page: its head ([LABEL] and what it is
// about) on a hairline, and the body its rows go in.
func (s *settingsWin) section(label, meta string) *gtk.Box {
	sec := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	sec.AddCssClass("section")
	head := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	head.AddCssClass("section-head")
	mark := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	mark.AddCssClass("mark")
	mark.SetValign(gtk.AlignCenterValue)
	gtkx.Append(head, &mark.Widget)
	l := gtk.NewLabel("")
	l.AddCssClass("section-label")
	l.SetMarkup("<span alpha='55%'>[</span>" + gtkEscape(label) + "<span alpha='55%'>]</span>")
	gtkx.Append(head, &l.Widget)
	m := gtk.NewLabel(meta)
	m.AddCssClass("section-meta")
	m.SetHexpand(true)
	m.SetXalign(0)
	m.SetEllipsize(3)
	gtkx.Append(head, &m.Widget)
	gtkx.Append(sec, &head.Widget)
	body := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	body.AddCssClass("section-body")
	gtkx.Append(sec, &body.Widget)
	gtkx.Append(s.page, &sec.Widget)
	return body
}

// line puts a hairline between a section's rows.
func line(body *gtk.Box) {
	if body.GetFirstChild() != nil {
		gtkx.Append(body, &gtk.NewSeparator(gtk.OrientationHorizontalValue).Widget)
	}
}

// texts is a row's title and what it does.
func texts(title, sub string) *gtk.Box {
	b := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	b.SetHexpand(true)
	b.SetValign(gtk.AlignCenterValue)
	t := gtk.NewLabel(title)
	t.AddCssClass("title")
	t.SetXalign(0)
	gtkx.Append(b, &t.Widget)
	if sub != "" {
		l := gtk.NewLabel(sub)
		l.AddCssClass("sub")
		l.SetXalign(0)
		l.SetWrap(true)
		l.SetMaxWidthChars(56)
		gtkx.Append(b, &l.Widget)
	}
	return b
}

// row is a line of a section: its title and what it does, the control at
// its end.
func (s *settingsWin) row(body *gtk.Box, title, sub string, controls ...*gtk.Widget) {
	line(body)
	r := gtk.NewBox(gtk.OrientationHorizontalValue, 16)
	r.AddCssClass("row")
	gtkx.Append(r, &texts(title, sub).Widget)
	for _, c := range controls {
		c.SetValign(gtk.AlignCenterValue)
		gtkx.Append(r, c)
	}
	gtkx.Append(body, &r.Widget)
}

// field is a labelled control under its title, the width of the page.
func (s *settingsWin) field(body *gtk.Box, title, sub string, control *gtk.Widget) {
	line(body)
	f := gtk.NewBox(gtk.OrientationVerticalValue, 8)
	f.AddCssClass("field")
	gtkx.Append(f, &texts(title, sub).Widget)
	gtkx.Append(f, control)
	gtkx.Append(body, &f.Widget)
}

// note is a paragraph at the end of a section.
func (s *settingsWin) note(body *gtk.Box, text string) {
	l := gtk.NewLabel(text)
	l.AddCssClass("sub")
	l.AddCssClass("mail-settings-note")
	l.SetXalign(0)
	l.SetWrap(true)
	l.SetMaxWidthChars(80)
	gtkx.Append(body, &l.Widget)
}

// choice is a drop-down of options; pick gets the index chosen.
func (s *settingsWin) choice(options []string, at int, pick func(int)) *gtk.Widget {
	d := gtk.NewDropDownFromStrings(options)
	d.SetSelected(uint32(at))
	s.pptrs = append(s.pptrs, s.w.h.notify(d, "selected", func() { pick(int(d.GetSelected())) }))
	return &d.Widget
}

// toggle is a switch.
func (s *settingsWin) toggle(on bool, set func(bool)) *gtk.Widget {
	sw := gtk.NewSwitch()
	sw.SetActive(on)
	s.pptrs = append(s.pptrs, s.w.h.notify(sw, "active", func() { set(sw.GetActive()) }))
	return &sw.Widget
}

// button is an outlined button; "danger" makes it the red one.
func (s *settingsWin) button(label string, f func(), classes ...string) *gtk.Widget {
	b := gtk.NewButtonWithLabel(label)
	b.AddCssClass("outlined")
	for _, c := range classes {
		b.AddCssClass(c)
	}
	s.pptrs = append(s.pptrs, s.w.h.click(b, f))
	return &b.Widget
}

// save keeps the settings in mail.toml; a failure shows under the index.
func (s *settingsWin) save() {
	s.said(s.w.settings.Save(mail.SettingsPath()))
}

func (s *settingsWin) said(err error) {
	if err != nil {
		s.status.SetText(i18n.Tf("Not saved: %v", err))
	}
	s.status.SetVisible(err != nil)
}

// --- the pages ---

func (s *settingsWin) accountPage(x *acct) {
	w := s.w
	acc := x.s.Acc
	body := s.section(strings.ToUpper(acc.DisplayName()), acc.Address)
	name := gtk.NewEntry()
	name.SetText(acc.Name)
	name.SetPlaceholderText(acc.DisplayName())
	name.SetSizeRequest(240, -1)
	s.pptrs = append(s.pptrs, w.h.edit(name, func() {
		s.setAccount(acc.ID, func(a *mail.Account) { a.Name = strings.TrimSpace(name.GetText()) })
	}))
	s.row(body, i18n.T("Your name"), i18n.T("On the messages you send"), &name.Widget)

	sig := gtk.NewTextView()
	sig.AddCssClass("mail-settings-signature")
	sig.SetWrapMode(gtk.WrapWordCharValue)
	sig.SetTopMargin(8)
	sig.SetBottomMargin(8)
	sig.SetLeftMargin(10)
	sig.SetRightMargin(10)
	buf := sig.GetBuffer()
	buf.SetText(acc.Signature, -1)
	s.pptrs = append(s.pptrs, w.h.edit(buf, func() {
		var a, b gtk.TextIter
		buf.GetBounds(&a, &b)
		text := strings.TrimRight(buf.GetText(&a, &b, true), "\n ")
		s.setAccount(acc.ID, func(ac *mail.Account) { ac.Signature = text })
	}))
	s.field(body, i18n.T("Signature"), i18n.T("Under what you write, after a line with two dashes; kept as you type."), &sig.Widget)

	how := i18n.T("With a password, at its IMAP and SMTP servers")
	if acc.OAuth() {
		how = i18n.Tf("On %s's page, in the browser", acc.Provider.Name())
	}
	sign := s.section(i18n.T("SIGN-IN"), acc.Provider.Name())
	s.row(sign, i18n.T("Sign in again"), how, s.button(i18n.T("Sign in…"), func() {
		a := x.s.Acc
		s.win.Close()
		w.win.Present()
		w.showWelcome(a.Provider, &a)
	}))
	if !acc.OAuth() {
		i, m := acc.Servers()
		s.row(sign, i18n.T("Servers"), "IMAP "+i+" · SMTP "+m)
	}
	s.row(sign, i18n.T("Remove the account"), i18n.T("Forgets it on this computer: its sign-in and the mail kept. The mail on the server stays."),
		s.button(i18n.T("Remove…"), func() { s.win.Close(); w.askRemove(x) }, "danger"))
}

// setAccount changes an account in the settings and its session, keeps
// it, and redraws the window's index.
func (s *settingsWin) setAccount(id string, f func(*mail.Account)) {
	w := s.w
	a := w.settings.Account(id)
	if a == nil {
		return
	}
	f(a)
	if x := w.acct(id); x != nil {
		f(&x.s.Acc)
	}
	s.save()
	w.drawSide()
}

func (s *settingsWin) readingPage() {
	w := s.w
	r := &w.settings.Reading
	redraw := func() { s.save(); w.lastDrawn = ""; w.drawReader() }
	body := s.section(i18n.T("MESSAGES"), i18n.T("How a conversation shows"))
	at := 0
	if r.Text {
		at = 1
	}
	s.row(body, i18n.T("HTML mail"), i18n.T("Formatted keeps its headings, emphasis, links and pictures; plain text keeps only the words."),
		s.choice([]string{i18n.T("Formatted"), i18n.T("Plain text")}, at, func(i int) { r.Text = i == 1; redraw() }))
	s.row(body, i18n.T("Mark as read when opened"), i18n.T("Off: a conversation stays unread until you mark it (U)."),
		s.toggle(!r.KeepUnread, func(on bool) { r.KeepUnread = !on; s.save() }))

	pics := s.section(i18n.T("PICTURES FROM THE WEB"), i18n.T("They tell the sender you opened the message"))
	img := map[string]int{"": 0, "ask": 0, "always": 1, "never": 2}[r.Images]
	s.row(pics, i18n.T("Load them"), i18n.T("Asking shows a line over the message: load them for it, or always for its sender. A suspicious message never loads them by itself."),
		s.choice([]string{i18n.T("Ask"), i18n.T("Always"), i18n.T("Never")}, img, func(i int) {
			r.Images = []string{"ask", "always", "never"}[i]
			redraw()
		}))
	for _, addr := range r.ImagesFrom {
		s.row(pics, addr, i18n.T("Its pictures always load"), s.button(i18n.T("Forget"), func() {
			r.ImagesFrom = slicesDelete(r.ImagesFrom, addr)
			redraw()
			s.drawPage()
		}))
	}
}

func slicesDelete(list []string, v string) []string {
	out := list[:0:0]
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

func (s *settingsWin) writingPage() {
	w := s.w
	body := s.section(i18n.T("SENDING"), "")
	opts := []int{-1, 5, 10, 20, 30}
	at := 1
	for i, o := range opts {
		if o == w.settings.UndoSeconds() || (o == -1 && w.settings.UndoSeconds() == 0) {
			at = i
		}
	}
	names := []string{i18n.T("Off")}
	for _, o := range opts[1:] {
		names = append(names, i18n.Nf("%d second", "%d seconds", o, o))
	}
	s.row(body, i18n.T("Time to take a send back"), i18n.T("A message waits this long before it goes; Undo stops it."),
		s.choice(names, at, func(i int) {
			w.settings.Writing.UndoSend = opts[i]
			if opts[i] == 5 {
				w.settings.Writing.UndoSend = 0
			}
			s.save()
		}))
}

func (s *settingsWin) assistantPage() {
	w := s.w
	on := gtk.NewSwitch()
	on.SetActive(w.settings.AI())
	body := s.section(i18n.T("AI"), i18n.T("In Mail only; the assistant itself is set in Settings → AI"))
	s.row(body, i18n.T("Use AI in Mail"), i18n.T("Off: no assistant's panel, no Ask AI, no search from a sentence, no drafting or polishing in the composer, no summaries of attachments, no sorting. Nothing of your mail goes to a model."), &on.Widget)
	sort := s.toggle(w.settings.Assistant.Sort, func(v bool) { w.settings.Assistant.Sort = v; s.save() })
	sort.SetSensitive(w.settings.AI())
	sorting := s.section(i18n.T("SORTING"), i18n.T("The only thing it does unasked"))
	s.row(sorting, i18n.T("Sort new mail"), i18n.T("Tags new mail the rules leave (needs a reply, newsletter, receipt …) with the assistant's quick choices. It sends each message's sender, subject and first 400 characters. Whether a message needs your reply is only ever decided here."), sort)
	s.pptrs = append(s.pptrs, w.h.notify(on, "active", func() {
		w.settings.Assistant.Off = !on.GetActive()
		s.save()
		sort.SetSensitive(w.settings.AI())
		w.aiChanged()
	}))
}

func (s *settingsWin) advancedPage() {
	w := s.w
	o := w.settings.OAuth
	body := s.section(i18n.T("YOUR OWN OAUTH CLIENT"), i18n.T("Empty fields use inua's"))
	s.note(body, i18n.T("For a Google Cloud project or an Azure app of your own. Empty fields use inua's. The secret is kept in your keyring, not in mail.toml. Accounts already signed in with a provider sign in again after its client changes."))
	entry := func(value, placeholder string) *gtk.Entry {
		e := gtk.NewEntry()
		e.AddCssClass("mono-entry")
		e.SetText(value)
		e.SetPlaceholderText(placeholder)
		e.SetHexpand(true)
		return e
	}
	gID := entry(o.Google.ClientID, "…apps.googleusercontent.com")
	gSecret := entry("", "GOCSPX-…")
	gSecret.SetVisibility(false)
	if o.Google.SecretInKeyring || o.Google.ClientSecret != "" {
		gSecret.SetPlaceholderText(i18n.T("Kept in your keyring; type to replace it"))
	}
	mID := entry(o.Microsoft.ClientID, "00000000-0000-0000-0000-000000000000")
	mTenant := entry(o.Microsoft.Tenant, "common")
	s.field(body, i18n.T("Google client ID"), "", &gID.Widget)
	s.field(body, i18n.T("Google client secret"), "", &gSecret.Widget)
	s.field(body, i18n.T("Microsoft client ID"), "", &mID.Widget)
	s.field(body, i18n.T("Microsoft tenant"), i18n.T("common, or your organization's"), &mTenant.Widget)
	done := gtk.NewLabel("")
	done.AddCssClass("sub")
	gen := s.gen
	var saveBtn *gtk.Widget
	saveBtn = s.button(i18n.T("Save"), func() {
		saveBtn.SetSensitive(false)
		w.saveClients(strings.TrimSpace(gID.GetText()), strings.TrimSpace(gSecret.GetText()),
			strings.TrimSpace(mID.GetText()), strings.TrimSpace(mTenant.GetText()), func(err error) {
				if w.settingsWin != s || s.gen != gen {
					return
				}
				saveBtn.SetSensitive(true)
				s.said(err)
				if err == nil {
					done.SetText(i18n.T("Saved."))
				}
			})
	})
	foot := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	foot.AddCssClass("row")
	saveBtn.SetValign(gtk.AlignCenterValue)
	gtkx.Append(foot, saveBtn)
	gtkx.Append(foot, &done.Widget)
	line(body)
	gtkx.Append(body, &foot.Widget)

	cache := s.section(i18n.T("CACHE"), tilde(filepath.Dir(w.store.AvatarPath("x"))))
	size := gtk.NewLabel(i18n.T("Counting…"))
	size.AddCssClass("readout")
	dir := filepath.Dir(w.store.AvatarPath("x"))
	go func() {
		var n int64
		filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if info, err := d.Info(); err == nil {
					n += info.Size()
				}
			}
			return nil
		})
		gtkx.Post(func() {
			if w.settingsWin == s && s.gen == gen {
				size.SetText(sizeText(n))
			}
		})
	}()
	s.row(cache, i18n.T("Mail kept on this computer"), i18n.T("The indexes and the messages opened: a copy of the servers', so clearing it loses nothing. Mail fetches them again at its next start."),
		&size.Widget, s.button(i18n.T("Clear"), func() {
			for _, x := range w.accts {
				w.store.Forget(x.s.Acc.ID)
			}
			s.drawPage()
		}))
}

// saveClients keeps the user's own OAuth clients: the IDs in mail.toml,
// the Google secret in the keyring (off the main thread: it may ask for a
// passphrase). An empty secret keeps the one kept; no Google client ID
// drops it.
func (w *window) saveClients(id, secret, mID, tenant string, done func(error)) {
	keep := ""
	switch {
	case id == "":
		keep = "-"
	case secret != "":
		keep = secret
	}
	go func() {
		var err error
		switch keep {
		case "":
		case "-":
			err = w.secrets.Set(mail.SecretKey, mail.ClientSecretKind(mail.Google), "")
		default:
			err = w.secrets.Set(mail.SecretKey, mail.ClientSecretKind(mail.Google), keep)
		}
		gtkx.Post(func() {
			if err != nil {
				done(fmt.Errorf("%s", i18n.Tf("Could not keep the secret in the keyring: %v", err)))
				return
			}
			g := &w.settings.OAuth.Google
			g.ClientID = id
			switch keep {
			case "-":
				g.ClientSecret, g.SecretInKeyring = "", false
			case "":
			default:
				g.ClientSecret, g.SecretInKeyring = "", true
			}
			w.settings.OAuth.Microsoft.ClientID, w.settings.OAuth.Microsoft.Tenant = mID, tenant
			done(w.settings.Save(mail.SettingsPath()))
		})
	}()
}

func sizeText(n int64) string {
	switch {
	case n < 1000*1000:
		return fmt.Sprintf("%.0f KB", float64(n)/1000)
	case n < 1000*1000*1000:
		return fmt.Sprintf("%.1f MB", float64(n)/1e6)
	}
	return fmt.Sprintf("%.1f GB", float64(n)/1e9)
}

func tilde(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
