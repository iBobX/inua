package mailui

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/mail"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// settingsWin is Mail's settings: each account (its name, signature,
// sign-in), reading, writing, the assistant, and the advanced (an OAuth
// client of the user's own, the cache). In the settings look: the index,
// then the page chosen.
type settingsWin struct {
	w     *window
	win   *gtk.Window
	index *gtk.Box
	page  *gtk.Box
	at    string    // the page shown: "account:ID", "reading", "writing", "assistant", "advanced"
	ptrs  []uintptr // the window's
	pptrs []uintptr // the page's and the index's, made again with them
	saved string    // a line said under the page after a save
	gen   int
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
	s.win.SetDefaultSize(860, 600)
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
	side.SetSizeRequest(230, -1)
	title := gtk.NewLabel(i18n.T("Settings"))
	title.AddCssClass("index-title")
	title.SetXalign(0)
	gtkx.Append(side, &title.Widget)
	s.index = gtk.NewBox(gtk.OrientationVerticalValue, 2)
	s.index.AddCssClass("mail-settings-index")
	gtkx.Append(side, &s.index.Widget)
	gtkx.Append(root, &side.Widget)
	scroll := gtk.NewScrolledWindow()
	scroll.SetHexpand(true)
	scroll.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	s.page = gtk.NewBox(gtk.OrientationVerticalValue, 0)
	s.page.AddCssClass("page")
	s.page.AddCssClass("mail-settings-page")
	scroll.SetChild(&s.page.Widget)
	gtkx.Append(root, &scroll.Widget)
	s.win.SetChild(&root.Widget)
	s.draw()
	s.win.Present()
}

func (s *settingsWin) draw() {
	w := s.w
	w.h.forget(s.pptrs)
	s.pptrs = nil
	s.gen++
	gtkx.Clear(s.index)
	gtkx.Clear(s.page)
	kicker := func(t string) {
		l := gtk.NewLabel(t)
		l.AddCssClass("kicker")
		l.AddCssClass("mail-settings-kicker")
		l.SetXalign(0)
		gtkx.Append(s.index, &l.Widget)
	}
	item := func(at, title string) {
		b := gtk.NewButton()
		b.AddCssClass("index-item")
		if at == s.at {
			b.AddCssClass("selected")
		}
		l := gtk.NewLabel(title)
		l.AddCssClass("title")
		l.SetXalign(0)
		l.SetEllipsize(3)
		b.SetChild(&l.Widget)
		s.pptrs = append(s.pptrs, w.h.click(b, func() { s.at, s.saved = at, ""; s.draw() }))
		gtkx.Append(s.index, &b.Widget)
	}
	kicker(i18n.T("ACCOUNTS"))
	for _, x := range w.accts {
		item("account:"+x.s.Acc.ID, x.s.Acc.DisplayName())
	}
	add := gtk.NewButtonWithLabel(i18n.T("Add account"))
	add.AddCssClass("mail-link")
	add.SetHalign(gtk.AlignStartValue)
	add.AddCssClass("mail-settings-add")
	s.pptrs = append(s.pptrs, w.h.click(add, func() { s.win.Close(); w.win.Present(); w.showWelcome("", nil) }))
	gtkx.Append(s.index, &add.Widget)
	kicker(i18n.T("MAIL"))
	item("reading", i18n.T("Reading"))
	item("writing", i18n.T("Writing"))
	item("assistant", i18n.T("Assistant"))
	item("advanced", i18n.T("Advanced"))

	switch {
	case strings.HasPrefix(s.at, "account:"):
		if x := w.acct(strings.TrimPrefix(s.at, "account:")); x != nil {
			s.accountPage(x)
		} else {
			s.at = "reading"
			s.readingPage()
		}
	case s.at == "writing":
		s.writingPage()
	case s.at == "assistant":
		s.assistantPage()
	case s.at == "advanced":
		s.advancedPage()
	default:
		s.readingPage()
	}
	if s.saved != "" {
		l := gtk.NewLabel(s.saved)
		l.AddCssClass("sub")
		l.AddCssClass("mail-settings-saved")
		l.SetXalign(0)
		gtkx.Append(s.page, &l.Widget)
	}
}

// --- building blocks: a section, a row ---

func (s *settingsWin) section(label, meta string) *gtk.Box {
	sec := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	sec.AddCssClass("section")
	head := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	head.AddCssClass("section-head")
	mark := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	mark.AddCssClass("mark")
	mark.SetValign(gtk.AlignCenterValue)
	gtkx.Append(head, &mark.Widget)
	l := gtk.NewLabel(label)
	l.AddCssClass("section-label")
	gtkx.Append(head, &l.Widget)
	if meta != "" {
		m := gtk.NewLabel(meta)
		m.AddCssClass("section-meta")
		m.SetHexpand(true)
		m.SetXalign(1)
		m.SetEllipsize(3)
		gtkx.Append(head, &m.Widget)
	}
	gtkx.Append(sec, &head.Widget)
	body := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	body.AddCssClass("section-body")
	gtkx.Append(sec, &body.Widget)
	gtkx.Append(s.page, &sec.Widget)
	return body
}

// row is a line of a section: its title and what it does, the control at
// its end.
func (s *settingsWin) row(body *gtk.Box, title, sub string, control *gtk.Widget) {
	r := gtk.NewBox(gtk.OrientationHorizontalValue, 16)
	r.AddCssClass("row")
	texts := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	texts.SetHexpand(true)
	texts.SetValign(gtk.AlignCenterValue)
	t := gtk.NewLabel(title)
	t.AddCssClass("title")
	t.SetXalign(0)
	gtkx.Append(texts, &t.Widget)
	if sub != "" {
		l := gtk.NewLabel(sub)
		l.AddCssClass("sub")
		l.SetXalign(0)
		l.SetWrap(true)
		l.SetMaxWidthChars(60)
		gtkx.Append(texts, &l.Widget)
	}
	gtkx.Append(r, &texts.Widget)
	if control != nil {
		control.SetValign(gtk.AlignCenterValue)
		gtkx.Append(r, control)
	}
	gtkx.Append(body, &r.Widget)
}

// choice is a drop-down of options; pick gets the index chosen.
func (s *settingsWin) choice(options []string, at int, pick func(int)) *gtk.Widget {
	d := gtk.NewDropDownFromStrings(options)
	d.SetSelected(uint32(at))
	d.AddCssClass("mail-settings-choice")
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

// save keeps the settings in mail.toml and says so under the page.
func (s *settingsWin) save() {
	if err := s.w.settings.Save(mail.SettingsPath()); err != nil {
		s.saved = i18n.Tf("Not saved: %v", err)
	} else {
		s.saved = ""
	}
}

// --- the pages ---

func (s *settingsWin) accountPage(x *acct) {
	w := s.w
	acc := x.s.Acc
	body := s.section(strings.ToUpper(acc.DisplayName()), acc.Address+" · "+acc.Provider.Name())
	name := gtk.NewEntry()
	name.SetText(acc.Name)
	name.SetPlaceholderText(acc.DisplayName())
	name.SetSizeRequest(260, -1)
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
	sigBox := gtk.NewBox(gtk.OrientationVerticalValue, 6)
	sigBox.AddCssClass("field")
	l := gtk.NewLabel(i18n.T("Signature"))
	l.AddCssClass("title")
	l.SetXalign(0)
	gtkx.Append(sigBox, &l.Widget)
	sub := gtk.NewLabel(i18n.T("Under what you write, after a line with two dashes; kept as you type."))
	sub.AddCssClass("sub")
	sub.SetXalign(0)
	gtkx.Append(sigBox, &sub.Widget)
	gtkx.Append(sigBox, &sig.Widget)
	gtkx.Append(body, &sigBox.Widget)

	how := i18n.T("With a password, at its IMAP and SMTP servers")
	if acc.OAuth() {
		how = i18n.Tf("On %s's page, in the browser", acc.Provider.Name())
	} else {
		how += fmt.Sprintf(" · %s · %s", acc.IMAP, acc.SMTP)
	}
	again := gtk.NewButtonWithLabel(i18n.T("Sign in again"))
	again.AddCssClass("outlined")
	s.pptrs = append(s.pptrs, w.h.click(again, func() {
		a := x.s.Acc
		s.win.Close()
		w.win.Present()
		w.showWelcome(a.Provider, &a)
	}))
	s.row(body, i18n.T("Sign-in"), how, &again.Widget)

	rm := gtk.NewButtonWithLabel(i18n.T("Remove…"))
	rm.AddCssClass("mail-settings-danger")
	s.pptrs = append(s.pptrs, w.h.click(rm, func() { s.win.Close(); w.askRemove(x) }))
	s.row(body, i18n.T("Remove the account"), i18n.T("Forgets it on this computer: its sign-in and the mail kept. The mail on the server stays."), &rm.Widget)
}

// setAccount changes an account in the settings and its session, keeps
// it, and redraws the index.
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
	if err := w.settings.Save(mail.SettingsPath()); err != nil {
		s.saved = i18n.Tf("Not saved: %v", err)
	}
	w.drawSide()
}

func (s *settingsWin) readingPage() {
	w := s.w
	r := &w.settings.Reading
	body := s.section(i18n.T("READING"), "")
	at := 0
	if r.Text {
		at = 1
	}
	s.row(body, i18n.T("HTML mail"), i18n.T("Formatted keeps its headings, emphasis, links and pictures; plain text keeps only the words."),
		s.choice([]string{i18n.T("Formatted"), i18n.T("Plain text")}, at, func(i int) {
			r.Text = i == 1
			s.save()
			w.lastDrawn = ""
			w.drawReader()
		}))
	img := map[string]int{"": 0, "ask": 0, "always": 1, "never": 2}[r.Images]
	s.row(body, i18n.T("Pictures from the web"), i18n.T("A picture a message loads from the web tells its sender you opened it, and when. Asking shows a line to load them, for the message or always for its sender."),
		s.choice([]string{i18n.T("Ask"), i18n.T("Always load"), i18n.T("Never load")}, img, func(i int) {
			r.Images = []string{"ask", "always", "never"}[i]
			s.save()
			w.lastDrawn = ""
			w.drawReader()
		}))
	if len(r.ImagesFrom) > 0 {
		clear := gtk.NewButtonWithLabel(i18n.T("Forget them"))
		clear.AddCssClass("outlined")
		s.pptrs = append(s.pptrs, w.h.click(clear, func() { r.ImagesFrom = nil; s.save(); s.draw() }))
		s.row(body, i18n.Nf("Pictures load from %d sender", "Pictures load from %d senders", len(r.ImagesFrom), len(r.ImagesFrom)),
			strings.Join(r.ImagesFrom, ", "), &clear.Widget)
	}
	s.row(body, i18n.T("Mark as read when opened"), i18n.T("Off: a conversation stays unread until you mark it (U)."),
		s.toggle(!r.KeepUnread, func(on bool) { r.KeepUnread = !on; s.save() }))
}

func (s *settingsWin) writingPage() {
	w := s.w
	body := s.section(i18n.T("WRITING"), "")
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
	body := s.section(i18n.T("ASSISTANT"), "")
	s.row(body, i18n.T("Sort new mail"), i18n.T("Tags new mail the rules leave (needs a reply, newsletter, receipt …) with the assistant's quick choices. It sends each message's sender, subject and first 400 characters."),
		s.toggle(w.settings.Assistant.Sort, func(on bool) { w.settings.Assistant.Sort = on; s.save() }))
	note := gtk.NewLabel(i18n.T("Everything else the assistant does in Mail (summaries, reply ideas, drafting, search from a sentence, phishing judgments) happens only when you ask. The assistant itself is set in Settings → AI."))
	note.AddCssClass("sub")
	note.AddCssClass("mail-settings-note")
	note.SetXalign(0)
	note.SetWrap(true)
	note.SetMaxWidthChars(70)
	gtkx.Append(body, &note.Widget)
}

func (s *settingsWin) advancedPage() {
	w := s.w
	o := w.settings.OAuth
	body := s.section(i18n.T("YOUR OWN OAUTH CLIENT"), "")
	note := gtk.NewLabel(i18n.T("For a Google Cloud project or an Azure app of your own. Empty fields use inua's. The secret is kept in your keyring, not in mail.toml. Accounts already signed in with a provider sign in again after its client changes."))
	note.AddCssClass("sub")
	note.AddCssClass("mail-settings-note")
	note.SetXalign(0)
	note.SetWrap(true)
	note.SetMaxWidthChars(70)
	gtkx.Append(body, &note.Widget)
	entry := func(value, placeholder string) *gtk.Entry {
		e := gtk.NewEntry()
		e.AddCssClass("mono-entry")
		e.SetText(value)
		e.SetPlaceholderText(placeholder)
		e.SetSizeRequest(300, -1)
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
	s.row(body, i18n.T("Google client ID"), "", &gID.Widget)
	s.row(body, i18n.T("Google client secret"), "", &gSecret.Widget)
	s.row(body, i18n.T("Microsoft client ID"), "", &mID.Widget)
	s.row(body, i18n.T("Microsoft tenant"), i18n.T("common, or your organization's"), &mTenant.Widget)
	saveBtn := gtk.NewButtonWithLabel(i18n.T("Save"))
	saveBtn.AddCssClass("outlined")
	saveBtn.SetHalign(gtk.AlignStartValue)
	saveBtn.SetMarginTop(10)
	gen := s.gen
	s.pptrs = append(s.pptrs, w.h.click(saveBtn, func() {
		saveBtn.SetSensitive(false)
		w.saveClients(strings.TrimSpace(gID.GetText()), strings.TrimSpace(gSecret.GetText()),
			strings.TrimSpace(mID.GetText()), strings.TrimSpace(mTenant.GetText()), func(err error) {
				if w.settingsWin != s || s.gen != gen {
					return
				}
				if err != nil {
					s.saved = err.Error()
				} else {
					s.saved = i18n.T("Saved.")
				}
				s.draw()
			})
	}))
	gtkx.Append(body, &saveBtn.Widget)

	cache := s.section(i18n.T("CACHE"), "")
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
	clearBtn := gtk.NewButtonWithLabel(i18n.T("Clear"))
	clearBtn.AddCssClass("outlined")
	s.pptrs = append(s.pptrs, w.h.click(clearBtn, func() {
		for _, x := range w.accts {
			w.store.Forget(x.s.Acc.ID)
		}
		s.saved = i18n.T("Cleared. Mail fetches what it shows again from the servers, at the next start.")
		s.draw()
	}))
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	gtkx.Append(row, &size.Widget)
	gtkx.Append(row, &clearBtn.Widget)
	s.row(cache, i18n.T("Mail kept on this computer"), i18n.Tf("The indexes and the messages opened, in %s. A copy of the servers': clearing it loses nothing.", tilde(dir)), &row.Widget)
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
