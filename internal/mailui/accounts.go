package mailui

import (
	"context"
	"errors"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/mail"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// accountDialog adds an account, or signs one in again: the provider
// chosen, then its sign-in page in the browser, or the IMAP form.
type accountDialog struct {
	w     *window
	win   *gtk.Window
	pages *gtk.Stack
	ptrs  []uintptr

	chooseErr                                 *gtk.Label
	oauthTitle, oauthText, oauthURL, oauthErr *gtk.Label
	name, address, user, imapAddr, smtpAddr   *gtk.Entry
	password                                  *gtk.PasswordEntry
	imapErr                                   *gtk.Label
	add                                       *gtk.Button
	again                                     *mail.Account // the account signing in again
	cancel                                    context.CancelFunc
	signIn                                    *mail.SignIn
	serversTouched                            bool
}

// showAccounts opens the dialog: at the start (p ""), or on a provider's
// sign-in; again is the account to sign in again, nil for a new one.
func (w *window) showAccounts(p mail.Provider, again *mail.Account) {
	if w.accounts != nil {
		w.accounts.win.Present()
		return
	}
	d := &accountDialog{w: w, again: again}
	w.accounts = d
	d.build()
	switch p {
	case mail.Google, mail.Microsoft:
		d.startOAuth(p)
	case mail.Password:
		d.showIMAP()
	default:
		d.pages.SetVisibleChildName("choose")
	}
	d.win.Present()
}

func (d *accountDialog) build() {
	w := d.w
	d.win = gtk.NewWindow()
	d.win.SetTransientFor(&w.win.Window)
	d.win.SetModal(true)
	d.win.SetDestroyWithParent(true)
	d.win.SetResizable(false)
	d.win.SetTitle(i18n.T("Add account"))
	for _, c := range []string{"app", "settings", "mail", "mail-dialog"} {
		d.win.AddCssClass(c)
	}
	d.ptrs = append(d.ptrs, w.h.close(d.win, func() bool { d.close(); return false }))
	d.ptrs = append(d.ptrs, w.h.key(&d.win.Widget, false, func(keyval uint32, _ gdk.ModifierType) bool {
		if keyval == uint32(gdk.KEY_Escape) {
			d.win.Close()
			return true
		}
		return false
	}))
	d.pages = gtk.NewStack()
	d.pages.SetSizeRequest(460, -1)
	d.pages.SetVhomogeneous(false)
	d.pages.AddNamed(&d.choosePage().Widget, "choose")
	d.pages.AddNamed(&d.oauthPage().Widget, "oauth")
	d.pages.AddNamed(&d.imapPage().Widget, "imap")
	d.win.SetChild(&d.pages.Widget)
}

func page(title, text string) (*gtk.Box, *gtk.Label, *gtk.Label) {
	box := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	box.AddCssClass("page")
	t := gtk.NewLabel(title)
	t.AddCssClass("mail-dialog-title")
	t.SetXalign(0)
	t.SetWrap(true)
	gtkx.Append(box, &t.Widget)
	l := gtk.NewLabel(text)
	l.AddCssClass("sub")
	l.SetXalign(0)
	l.SetWrap(true)
	l.SetMaxWidthChars(56)
	l.SetVisible(text != "")
	gtkx.Append(box, &l.Widget)
	return box, t, l
}

func errLabel() *gtk.Label {
	e := gtk.NewLabel("")
	e.AddCssClass("mail-error")
	e.SetXalign(0)
	e.SetWrap(true)
	e.SetMaxWidthChars(56)
	e.SetSelectable(true)
	e.SetVisible(false)
	return e
}

func sayIn(l *gtk.Label, text string) { l.SetText(text); l.SetVisible(text != "") }

func (d *accountDialog) button(box *gtk.Box, text string, class string, f func()) *gtk.Button {
	b := gtk.NewButtonWithLabel(text)
	b.AddCssClass(class)
	d.ptrs = append(d.ptrs, d.w.h.click(b, f))
	gtkx.Append(box, &b.Widget)
	return b
}

func buttons() *gtk.Box {
	b := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	b.SetHalign(gtk.AlignEndValue)
	b.SetMarginTop(8)
	return b
}

func (d *accountDialog) choosePage() *gtk.Box {
	box, _, _ := page(i18n.T("Add an account"), i18n.T("Gmail and Outlook sign in on their own page, in your browser: Mail never sees your password. Other accounts sign in to their IMAP server with a password; for Gmail, iCloud or Yahoo with two-step sign-in, an app password."))
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	row.SetMarginTop(6)
	d.button(row, i18n.T("Google"), "outlined", func() { d.startOAuth(mail.Google) })
	d.button(row, i18n.T("Microsoft"), "outlined", func() { d.startOAuth(mail.Microsoft) })
	d.button(row, i18n.T("Other (IMAP)"), "outlined", d.showIMAP)
	gtkx.Append(box, &row.Widget)
	d.chooseErr = errLabel()
	gtkx.Append(box, &d.chooseErr.Widget)
	foot := buttons()
	d.button(foot, i18n.T("Cancel"), "outlined", func() { d.win.Close() })
	gtkx.Append(box, &foot.Widget)
	return box
}

func (d *accountDialog) oauthPage() *gtk.Box {
	box, t, l := page("", i18n.T("Your browser opens the sign-in page. Sign in there, allow Mail to read and send your mail, then come back here."))
	d.oauthTitle, d.oauthText = t, l
	d.oauthURL = gtk.NewLabel("")
	d.oauthURL.AddCssClass("mail-dialog-item")
	d.oauthURL.SetXalign(0)
	d.oauthURL.SetEllipsize(2)
	d.oauthURL.SetMaxWidthChars(48)
	d.oauthURL.SetSelectable(true)
	gtkx.Append(box, &d.oauthURL.Widget)
	links := gtk.NewBox(gtk.OrientationHorizontalValue, 16)
	links.AddCssClass("mail-links")
	var copyBtn *gtk.Button
	copyBtn = d.button(links, i18n.T("Copy the link"), "mail-link", func() {
		if d.signIn != nil {
			d.win.GetClipboard().SetText(d.signIn.URL)
			copyBtn.SetLabel(i18n.T("Copied"))
		}
	})
	d.button(links, i18n.T("Open it again"), "mail-link", func() {
		if d.signIn != nil {
			desktop.Spawn([]string{"xdg-open", d.signIn.URL}, "")
		}
	})
	gtkx.Append(box, &links.Widget)
	d.oauthErr = errLabel()
	gtkx.Append(box, &d.oauthErr.Widget)
	foot := buttons()
	d.button(foot, i18n.T("Cancel"), "outlined", func() { d.win.Close() })
	gtkx.Append(box, &foot.Widget)
	return box
}

func field(box *gtk.Box, label string, e *gtk.Widget) {
	f := gtk.NewBox(gtk.OrientationVerticalValue, 4)
	f.AddCssClass("mail-dialog-field")
	l := gtk.NewLabel(label)
	l.AddCssClass("label")
	l.SetXalign(0)
	gtkx.Append(f, &l.Widget)
	gtkx.Append(f, e)
	gtkx.Append(box, &f.Widget)
}

func (d *accountDialog) imapPage() *gtk.Box {
	box, _, _ := page(i18n.T("Other account"), i18n.T("The password is kept in your keyring, not in mail.toml."))
	d.name, d.address, d.user = gtk.NewEntry(), gtk.NewEntry(), gtk.NewEntry()
	d.imapAddr, d.smtpAddr = gtk.NewEntry(), gtk.NewEntry()
	d.password = gtk.NewPasswordEntry()
	d.password.SetShowPeekIcon(true)
	d.name.SetPlaceholderText(i18n.T("The name on what you send"))
	d.address.SetPlaceholderText("you@example.com")
	d.user.SetPlaceholderText(i18n.T("The address, when left empty"))
	d.imapAddr.SetPlaceholderText("imap.example.com:993")
	d.smtpAddr.SetPlaceholderText("smtp.example.com:465")
	field(box, i18n.T("Your name"), &d.name.Widget)
	field(box, i18n.T("Address"), &d.address.Widget)
	field(box, i18n.T("Password"), &d.password.Widget)
	servers := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	servers.SetHomogeneous(true)
	field(servers, i18n.T("IMAP server"), &d.imapAddr.Widget)
	field(servers, i18n.T("SMTP server"), &d.smtpAddr.Widget)
	gtkx.Append(box, &servers.Widget)
	field(box, i18n.T("User name"), &d.user.Widget)
	note := gtk.NewLabel(i18n.T("Port 993 and 465 use TLS; others start TLS after connecting."))
	note.AddCssClass("sub")
	note.SetXalign(0)
	note.SetWrap(true)
	gtkx.Append(box, &note.Widget)
	d.imapErr = errLabel()
	gtkx.Append(box, &d.imapErr.Widget)
	d.ptrs = append(d.ptrs,
		d.w.h.edit(d.address, d.guess),
		d.w.h.edit(d.imapAddr, func() { d.serversTouched = d.imapAddr.HasFocus() || d.serversTouched }),
		d.w.h.enter(d.password, d.addIMAP),
	)
	foot := buttons()
	d.button(foot, i18n.T("Cancel"), "outlined", func() { d.win.Close() })
	d.add = d.button(foot, i18n.T("Add"), "suggested-action", d.addIMAP)
	gtkx.Append(box, &foot.Widget)
	return box
}

func (d *accountDialog) close() {
	if d.cancel != nil {
		d.cancel()
	}
	if d.signIn != nil {
		d.signIn.Close()
	}
	d.w.h.forget(d.ptrs)
	d.w.accounts = nil
}

// --- OAuth ---

func (d *accountDialog) startOAuth(p mail.Provider) {
	ep, err := d.w.settings.Endpoint(p)
	if err != nil {
		d.pages.SetVisibleChildName("choose")
		msg := err.Error()
		if errors.Is(err, mail.ErrNoClient) {
			_, msg, _ = strings.Cut(msg, ": ")
		}
		sayIn(d.chooseErr, msg)
		return
	}
	hint := ""
	if d.again != nil && d.again.Provider == p {
		hint = d.again.Address
	}
	si, err := mail.StartSignIn(ep, hint)
	if err != nil {
		sayIn(d.chooseErr, err.Error())
		return
	}
	d.signIn = si
	d.oauthTitle.SetText(i18n.Tf("Sign in with %s", p.Name()))
	d.oauthURL.SetText(si.URL)
	d.oauthURL.SetTooltipText(si.URL)
	sayIn(d.oauthErr, "")
	d.pages.SetVisibleChildName("oauth")
	if err := desktop.Spawn([]string{"xdg-open", si.URL}, ""); err != nil {
		sayIn(d.oauthErr, i18n.Tf("No browser opened (%v): copy the link into one.", err))
	}
	ctx, cancel := context.WithTimeout(d.w.ctx, 15*time.Minute)
	d.cancel = cancel
	go func() {
		tok, err := si.Wait(ctx)
		gtkx.Post(func() { d.signedIn(p, tok, err) })
	}()
}

func (d *accountDialog) signedIn(p mail.Provider, tok mail.Token, err error) {
	if d.w.accounts != d {
		return // closed
	}
	switch {
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		sayIn(d.oauthErr, i18n.T("The sign-in took too long. Close this and try again."))
		return
	case err != nil:
		sayIn(d.oauthErr, i18n.Tf("Not signed in: %v", err))
		return
	case tok.Email == "":
		sayIn(d.oauthErr, i18n.T("Signed in, but the provider did not say the address."))
		return
	case tok.Refresh == "":
		sayIn(d.oauthErr, i18n.T("Signed in, but the provider gave no lasting sign-in. Remove Mail's access in your account's settings and try again."))
		return
	}
	acc := mail.Account{Name: tok.Name, Address: tok.Email, Provider: p}
	d.save(acc, mail.SecretRefresh, tok.Refresh)
}

// save keeps a new account (or the one signed in again) and its secret,
// and starts it.
func (d *accountDialog) save(acc mail.Account, kind, secret string) {
	w := d.w
	if d.again != nil && acc.Name == "" {
		acc.Name = d.again.Name
	}
	acc = w.settings.Add(acc)
	if err := w.secrets.Set(acc.ID, kind, secret); err != nil {
		d.fail(i18n.Tf("Could not keep the sign-in in the keyring: %v", err))
		return
	}
	if err := w.settings.Save(mail.SettingsPath()); err != nil {
		d.fail(err.Error())
		return
	}
	x := w.addSession(acc)
	d.win.Close()
	w.view = view{kind: viewBox, account: x.s.Acc.ID, mailbox: "INBOX"}
	w.drawSide()
	w.refresh()
	if w.readerStack.GetVisibleChildName() == "start" {
		w.readerStack.SetVisibleChildName("empty")
	}
}

func (d *accountDialog) fail(text string) {
	if d.pages.GetVisibleChildName() == "imap" {
		sayIn(d.imapErr, text)
	} else {
		sayIn(d.oauthErr, text)
	}
}

// --- IMAP ---

func (d *accountDialog) showIMAP() {
	if a := d.again; a != nil && a.Provider == mail.Password {
		d.name.SetText(a.Name)
		d.address.SetText(a.Address)
		d.imapAddr.SetText(a.IMAP)
		d.smtpAddr.SetText(a.SMTP)
		d.user.SetText(a.Username)
		d.serversTouched = true
	}
	d.pages.SetVisibleChildName("imap")
	if d.address.GetText() == "" {
		d.name.GrabFocus()
	} else {
		d.password.GrabFocus()
	}
}

// guess fills the servers of a provider inua knows from the address.
func (d *accountDialog) guess() {
	if d.serversTouched {
		return
	}
	if i, s, ok := mail.Guess(d.address.GetText()); ok {
		d.imapAddr.SetText(i)
		d.smtpAddr.SetText(s)
	}
}

// addIMAP signs in to both servers, then keeps the account.
func (d *accountDialog) addIMAP() {
	acc := mail.Account{
		Name: strings.TrimSpace(d.name.GetText()), Address: strings.TrimSpace(d.address.GetText()), Provider: mail.Password,
		IMAP: strings.TrimSpace(d.imapAddr.GetText()), SMTP: strings.TrimSpace(d.smtpAddr.GetText()),
		Username: strings.TrimSpace(d.user.GetText()),
	}
	pw := d.password.GetText()
	switch {
	case !strings.Contains(acc.Address, "@"):
		sayIn(d.imapErr, i18n.T("Type the address."))
		return
	case pw == "":
		sayIn(d.imapErr, i18n.T("Type the password."))
		return
	case acc.IMAP == "" || acc.SMTP == "":
		sayIn(d.imapErr, i18n.T("Type both servers."))
		return
	}
	sayIn(d.imapErr, "")
	d.add.SetSensitive(false)
	d.add.SetLabel(i18n.T("Signing in…"))
	ctx, cancel := context.WithTimeout(d.w.ctx, 40*time.Second)
	d.cancel = cancel
	go func() {
		err := mail.Check(ctx, acc, mail.Auth{User: acc.User(), Password: pw})
		gtkx.Post(func() {
			if d.w.accounts != d {
				return
			}
			d.add.SetSensitive(true)
			d.add.SetLabel(i18n.T("Add"))
			if err != nil {
				sayIn(d.imapErr, i18n.Tf("Not signed in: %v", err))
				return
			}
			d.save(acc, mail.SecretPassword, pw)
		})
	}()
}
