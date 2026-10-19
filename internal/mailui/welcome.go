package mailui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/mail"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// welcome adds an account, in the window's place: the address (the way
// to sign in is worked out from it), the sign-in (the provider's page in
// the browser, or a password), and the first sync, shown as it goes.
type welcome struct {
	w     *window
	step  int // 1 the address, 2 signing in, 3 the first sync
	p     mail.Provider
	again *mail.Account // the account signing in again
	ptrs  []uintptr

	addr    string
	address *gtk.Entry
	rows    map[mail.Provider]*gtk.Button

	name, user, imapAddr, smtpAddr *gtk.Entry
	password                       *gtk.PasswordEntry
	servers                        bool // their fields shown

	si     *mail.SignIn
	since  time.Time
	log    []logLine
	logBox *gtk.Box
	err    string
	busy   bool
	cancel context.CancelFunc
	acc    mail.Account
	synced bool
	gen    int // a draw's number, so a tick of an old one stops
}

// logLine is a line of the sign-in's and the sync's log.
type logLine struct {
	state int // lineWait, lineDone or lineBad
	text  string
}

const (
	lineWait = iota
	lineDone
	lineBad
)

// showWelcome shows the welcome: at its start (p ""), or at a provider's
// sign-in; again is the account to sign in again, nil for a new one.
func (w *window) showWelcome(p mail.Provider, again *mail.Account) {
	if w.welcome != nil && w.welcome.busy {
		w.top.SetVisibleChildName("welcome")
		return
	}
	w.closeWelcome()
	wl := &welcome{w: w, step: 1, again: again, p: p}
	w.welcome = wl
	w.top.SetVisibleChildName("welcome")
	if again != nil {
		wl.addr = again.Address
	}
	switch {
	case p == mail.Google || p == mail.Microsoft:
		wl.startOAuth()
	case p == mail.Password && again != nil:
		wl.step = 2
		wl.draw()
	default:
		wl.draw()
	}
}

// closeWelcome goes back to the three columns, stopping what it waited for.
func (w *window) closeWelcome() {
	wl := w.welcome
	if wl == nil {
		return
	}
	wl.stop()
	w.h.forget(wl.ptrs)
	w.welcome = nil
	gtkx.Clear(&w.welcomeBox)
	if len(w.accts) > 0 {
		w.top.SetVisibleChildName("main")
	}
}

func (wl *welcome) stop() {
	wl.gen++
	if wl.cancel != nil {
		wl.cancel()
		wl.cancel = nil
	}
	if wl.si != nil {
		wl.si.Close()
		wl.si = nil
	}
}

// key takes the welcome's keys: Escape goes back a step, or out.
func (wl *welcome) key(keyval uint32, _ gdk.ModifierType) bool {
	if keyval != uint32(gdk.KEY_Escape) {
		return false
	}
	wl.back()
	return true
}

func (wl *welcome) back() {
	switch {
	case wl.step == 2 && wl.again == nil:
		wl.stop()
		wl.step, wl.err, wl.log = 1, "", nil
		wl.draw()
	case len(wl.w.accts) > 0 && !wl.busy:
		wl.w.closeWelcome()
	}
}

// --- drawing ---

func (wl *welcome) draw() {
	w := wl.w
	w.h.forget(wl.ptrs)
	wl.ptrs = nil
	wl.gen++
	gtkx.Clear(&w.welcomeBox)
	box := &w.welcomeBox

	head := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	head.AddCssClass("mail-welcome-head")
	brand := gtk.NewLabel("")
	brand.SetMarkup(`<span alpha="55%">inua/</span>mail`)
	brand.AddCssClass("mail-brand")
	brand.SetHexpand(true)
	brand.SetXalign(0)
	gtkx.Append(head, &brand.Widget)
	if len(w.accts) > 0 && !wl.busy {
		wl.link(head, i18n.T("Close"), func() { w.closeWelcome() })
	}
	gtkx.Append(box, &head.Widget)
	gtkx.Append(box, &wl.steps().Widget)

	switch wl.step {
	case 1:
		wl.drawAddress(box)
	case 2:
		if wl.p == mail.Password {
			wl.drawIMAP(box)
		} else {
			wl.drawOAuth(box)
		}
	case 3:
		wl.drawSync(box)
	}
}

// steps is the line of the three steps, the current one marked.
func (wl *welcome) steps() *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	row.AddCssClass("mail-steps-line")
	for i, s := range []string{i18n.T("ADDRESS"), i18n.T("SIGN IN"), i18n.T("FIRST SYNC")} {
		if i > 0 {
			sep := gtk.NewLabel("──")
			sep.AddCssClass("mail-step-sep")
			gtkx.Append(row, &sep.Widget)
		}
		l := gtk.NewLabel(fmt.Sprintf("%02d %s", i+1, s))
		l.AddCssClass("mail-step-name")
		switch {
		case i+1 == wl.step:
			l.AddCssClass("current")
		case i+1 < wl.step:
			l.AddCssClass("done")
		}
		gtkx.Append(row, &l.Widget)
	}
	return row
}

func (wl *welcome) title(box *gtk.Box, title, text string) {
	t := gtk.NewLabel(title)
	t.AddCssClass("mail-welcome-title")
	t.SetXalign(0)
	t.SetWrap(true)
	gtkx.Append(box, &t.Widget)
	if text != "" {
		l := gtk.NewLabel(text)
		l.AddCssClass("mail-welcome-text")
		l.SetXalign(0)
		l.SetWrap(true)
		l.SetMaxWidthChars(60)
		gtkx.Append(box, &l.Widget)
	}
}

// link adds a text button to a row.
func (wl *welcome) link(row *gtk.Box, text string, f func()) *gtk.Button {
	b := gtk.NewButtonWithLabel(text)
	b.AddCssClass("mail-link")
	wl.ptrs = append(wl.ptrs, wl.w.h.click(b, f))
	gtkx.Append(row, &b.Widget)
	return b
}

// foot is the row of the step's buttons and the keys.
func (wl *welcome) foot(box *gtk.Box, keys string) *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	row.AddCssClass("mail-welcome-foot")
	k := gtk.NewLabel(keys)
	k.AddCssClass("mail-keys")
	k.SetHexpand(true)
	k.SetXalign(0)
	gtkx.Append(row, &k.Widget)
	gtkx.Append(box, &row.Widget)
	return row
}

func (wl *welcome) primary(row *gtk.Box, text string, f func()) *gtk.Button {
	b := gtk.NewButtonWithLabel(text)
	b.AddCssClass("suggested-action")
	wl.ptrs = append(wl.ptrs, wl.w.h.click(b, f))
	gtkx.Append(row, &b.Widget)
	return b
}

func (wl *welcome) errLine(box *gtk.Box) {
	if wl.err == "" {
		return
	}
	e := gtk.NewLabel(wl.err)
	e.AddCssClass("mail-error")
	e.SetXalign(0)
	e.SetWrap(true)
	e.SetMaxWidthChars(64)
	e.SetSelectable(true)
	gtkx.Append(box, &e.Widget)
}

// drawLog shows the log, a mark and a mono line each.
func (wl *welcome) drawLog(box *gtk.Box) {
	wl.logBox = gtk.NewBox(gtk.OrientationVerticalValue, 5)
	wl.logBox.AddCssClass("mail-log")
	wl.fillLog()
	gtkx.Append(box, &wl.logBox.Widget)
}

func (wl *welcome) fillLog() {
	if wl.logBox == nil {
		return
	}
	gtkx.Clear(wl.logBox)
	for _, l := range wl.log {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		m := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		m.AddCssClass("mark")
		m.AddCssClass([]string{"wait", "on", "bad"}[l.state])
		m.SetValign(gtk.AlignCenterValue)
		gtkx.Append(row, &m.Widget)
		t := gtk.NewLabel(l.text)
		t.AddCssClass("mail-log-line")
		if l.state == lineBad {
			t.AddCssClass("bad")
		}
		t.SetXalign(0)
		t.SetWrap(true)
		t.SetWrapMode(2)
		t.SetMaxWidthChars(70)
		gtkx.Append(row, &t.Widget)
		gtkx.Append(wl.logBox, &row.Widget)
	}
	wl.logBox.SetVisible(len(wl.log) > 0)
}

// say adds a line to the log, or settles the last waiting one.
func (wl *welcome) say(state int, text string) {
	if n := len(wl.log); n > 0 && wl.log[n-1].state == lineWait {
		wl.log[n-1] = logLine{state, text}
	} else {
		wl.log = append(wl.log, logLine{state, text})
	}
	wl.fillLog()
}

// --- 1: the address ---

func (wl *welcome) drawAddress(box *gtk.Box) {
	wl.title(box, i18n.T("Which address?"), i18n.T("Mail works out how to sign in from it, and never sees a Google or Microsoft password."))
	field := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	field.AddCssClass("mail-prompt")
	p := gtk.NewLabel("›")
	p.AddCssClass("mail-prompt-mark")
	gtkx.Append(field, &p.Widget)
	wl.address = gtk.NewEntry()
	wl.address.AddCssClass("mail-prompt-entry")
	wl.address.SetPlaceholderText("you@example.com")
	wl.address.SetHexpand(true)
	wl.address.SetText(wl.addr)
	wl.ptrs = append(wl.ptrs, wl.w.h.edit(wl.address, wl.typed), wl.w.h.enter(wl.address, wl.next))
	gtkx.Append(field, &wl.address.Widget)
	gtkx.Append(box, &field.Widget)

	list := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	list.AddCssClass("mail-ways")
	wl.rows = map[mail.Provider]*gtk.Button{}
	for _, way := range []struct {
		p           mail.Provider
		glyph, name string
		what, how   string
	}{
		{mail.Google, "G", "Google", i18n.T("Gmail and Google Workspace"), i18n.T("in the browser")},
		{mail.Microsoft, "M", "Microsoft", i18n.T("Outlook, Hotmail and Microsoft 365"), i18n.T("in the browser")},
		{mail.Password, "@", "IMAP", i18n.T("Fastmail, iCloud, Yahoo, your own server"), i18n.T("a password")},
	} {
		b := gtk.NewButton()
		b.AddCssClass("mail-way")
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 14)
		g := gtk.NewLabel(way.glyph)
		g.AddCssClass("mail-way-glyph")
		g.SetValign(gtk.AlignCenterValue)
		gtkx.Append(row, &g.Widget)
		n := gtk.NewLabel(way.name)
		n.AddCssClass("mail-way-name")
		n.SetXalign(0)
		n.SetSizeRequest(96, -1)
		gtkx.Append(row, &n.Widget)
		what := gtk.NewLabel(way.what)
		what.AddCssClass("mail-way-what")
		what.SetXalign(0)
		what.SetHexpand(true)
		what.SetEllipsize(3)
		gtkx.Append(row, &what.Widget)
		how := gtk.NewLabel(way.how)
		how.AddCssClass("mail-way-how")
		if _, err := wl.w.settings.Endpoint(way.p); way.p != mail.Password && err != nil {
			how.SetText(i18n.T("no client ID"))
			b.SetTooltipText(i18n.T("This build has no client ID for it: see docs/mail.md."))
		}
		gtkx.Append(row, &how.Widget)
		b.SetChild(&row.Widget)
		p := way.p
		wl.ptrs = append(wl.ptrs, wl.w.h.click(b, func() { wl.p = p; wl.next() }))
		wl.rows[way.p] = b
		gtkx.Append(list, &b.Widget)
	}
	gtkx.Append(box, &list.Widget)
	wl.errLine(box)
	row := wl.foot(box, i18n.T("Enter continues · a click picks another way · Esc goes back"))
	wl.primary(row, i18n.T("Continue"), wl.next)
	wl.typed()
	wl.address.GrabFocus()
	wl.address.SetPosition(-1)
}

// typed marks the way the address signs in.
func (wl *welcome) typed() {
	if wl.address == nil {
		return
	}
	wl.addr = strings.TrimSpace(wl.address.GetText())
	p := mail.Detect(wl.addr)
	for q, b := range wl.rows {
		setClass(&b.Widget, "current", q == p && strings.Contains(wl.addr, "@"))
	}
}

// next goes on from the address: the way picked, else the one detected.
func (wl *welcome) next() {
	if wl.address != nil {
		wl.addr = strings.TrimSpace(wl.address.GetText())
	}
	if wl.p == "" {
		if !strings.Contains(wl.addr, "@") {
			wl.err = i18n.T("Type your address, or pick a way below.")
			wl.draw()
			return
		}
		wl.p = mail.Detect(wl.addr)
	}
	wl.err = ""
	if wl.p == mail.Password {
		if !strings.Contains(wl.addr, "@") {
			wl.p = ""
			wl.err = i18n.T("Type your address first: the IMAP servers come from it.")
			wl.draw()
			return
		}
		wl.step = 2
		wl.draw()
		return
	}
	wl.startOAuth()
}

// --- 2: the provider's page ---

func (wl *welcome) startOAuth() {
	wl.step, wl.err, wl.log = 2, "", nil
	ep, err := wl.w.settings.Endpoint(wl.p)
	if err != nil {
		if errors.Is(err, mail.ErrNoClient) {
			_, msg, _ := strings.Cut(err.Error(), ": ")
			err = errors.New(msg)
		}
		wl.err = err.Error()
		wl.draw()
		return
	}
	hint := ""
	if strings.Contains(wl.addr, "@") {
		hint = wl.addr
	}
	si, err := mail.StartSignIn(ep, hint)
	if err != nil {
		wl.err = err.Error()
		wl.draw()
		return
	}
	wl.si, wl.since = si, time.Now()
	wl.log = []logLine{{lineDone, i18n.Tf("listening on %s for the answer", strings.TrimSuffix(strings.TrimPrefix(si.Redirect(), "http://"), "/"))}}
	host := "accounts.google.com"
	if wl.p == mail.Microsoft {
		host = "login.microsoftonline.com"
	}
	if err := desktop.Spawn([]string{"xdg-open", si.URL}, ""); err != nil {
		wl.log = append(wl.log, logLine{lineBad, i18n.Tf("no browser opened (%v): copy the link into one", err)})
	} else {
		wl.log = append(wl.log, logLine{lineDone, i18n.Tf("opened %s in your browser", host)})
	}
	wl.log = append(wl.log, logLine{lineWait, ""})
	ctx, cancel := context.WithTimeout(wl.w.ctx, 15*time.Minute)
	wl.cancel = cancel
	p := wl.p
	go func() {
		tok, err := si.Wait(ctx)
		gtkx.Post(func() {
			if wl.w.welcome == wl && wl.si == si {
				wl.signedIn(p, tok, err)
			}
		})
	}()
	wl.draw()
}

func (wl *welcome) drawOAuth(box *gtk.Box) {
	wl.title(box, i18n.Tf("Sign in with %s", wl.p.Name()),
		i18n.T("Your browser has the sign-in page. Allow Mail to read and send your mail there; this page goes on by itself."))
	if wl.si != nil {
		wl.drawLog(box)
		wl.tick(wl.gen)
	}
	wl.errLine(box)
	links := gtk.NewBox(gtk.OrientationHorizontalValue, 18)
	links.AddCssClass("mail-links")
	if wl.si != nil {
		var copyBtn *gtk.Button
		copyBtn = wl.link(links, i18n.T("Copy the link"), func() {
			wl.w.win.GetClipboard().SetText(wl.si.URL)
			copyBtn.SetLabel(i18n.T("Copied"))
		})
		wl.link(links, i18n.T("Open it again"), func() { desktop.Spawn([]string{"xdg-open", wl.si.URL}, "") })
	}
	if strings.Contains(wl.addr, "@") {
		wl.link(links, i18n.T("Use a password instead"), func() {
			wl.stop()
			wl.p, wl.err, wl.log = mail.Password, "", nil
			wl.draw()
		})
	}
	gtkx.Append(box, &links.Widget)
	row := wl.foot(box, i18n.T("Esc goes back"))
	if wl.again == nil {
		wl.link(row, i18n.T("Back"), wl.back)
	}
}

// tick counts the seconds waited for the browser, on the log's last line.
func (wl *welcome) tick(gen int) {
	if wl.gen != gen || wl.si == nil || len(wl.log) == 0 || wl.log[len(wl.log)-1].state != lineWait {
		return
	}
	secs := int(time.Since(wl.since).Seconds())
	wl.log[len(wl.log)-1].text = i18n.Tf("waiting for %s · %d s", wl.p.Name(), secs)
	wl.fillLog()
	wl.w.h.later(1000, func() { wl.tick(gen) })
}

func (wl *welcome) signedIn(p mail.Provider, tok mail.Token, err error) {
	wl.si = nil
	switch {
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		wl.say(lineBad, i18n.T("the sign-in took too long; go back and try again"))
		return
	case err != nil:
		wl.say(lineBad, i18n.Tf("not signed in: %v", err))
		return
	case tok.Email == "":
		wl.say(lineBad, i18n.T("signed in, but the provider did not say the address"))
		return
	case tok.Refresh == "":
		wl.say(lineBad, i18n.T("signed in, but the provider gave no lasting sign-in: remove Mail's access in your account's settings and try again"))
		return
	}
	wl.say(lineDone, i18n.Tf("signed in as %s", tok.Email))
	wl.keep(mail.Account{Name: tok.Name, Address: tok.Email, Provider: p}, mail.SecretRefresh, tok.Refresh)
}

// --- 2: a password ---

func (wl *welcome) drawIMAP(box *gtk.Box) {
	_, domain, _ := strings.Cut(wl.addr, "@")
	wl.title(box, i18n.Tf("Sign in to %s", domain), i18n.T("The password is kept in your keyring. For Gmail, iCloud or Yahoo with two-step sign-in, use an app password."))
	keep := func(e *gtk.Entry) string {
		if e == nil {
			return ""
		}
		return e.GetText()
	}
	name, user, imapA, smtpA := keep(wl.name), keep(wl.user), keep(wl.imapAddr), keep(wl.smtpAddr)
	if a := wl.again; a != nil && name+user+imapA+smtpA == "" {
		name, user, imapA, smtpA = a.Name, a.Username, a.IMAP, a.SMTP
	}
	if imapA == "" && smtpA == "" {
		if i, s, ok := mail.Guess(wl.addr); ok {
			imapA, smtpA = i, s
		} else {
			imapA, smtpA = "imap."+domain+":993", "smtp."+domain+":465"
			wl.servers = true // a guess: shown, to be checked
		}
	}
	pw := ""
	if wl.password != nil {
		pw = wl.password.GetText()
	}
	wl.name, wl.user, wl.imapAddr, wl.smtpAddr = gtk.NewEntry(), gtk.NewEntry(), gtk.NewEntry(), gtk.NewEntry()
	wl.password = gtk.NewPasswordEntry()
	wl.password.SetShowPeekIcon(true)
	wl.name.SetText(name)
	wl.user.SetText(user)
	wl.imapAddr.SetText(imapA)
	wl.smtpAddr.SetText(smtpA)
	wl.password.SetText(pw)
	wl.name.SetPlaceholderText(i18n.T("Your name, on what you send"))
	wl.user.SetPlaceholderText(wl.addr)

	grid := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	grid.AddCssClass("mail-welcome-fields")
	field(grid, i18n.T("Password"), &wl.password.Widget)
	field(grid, i18n.T("Your name"), &wl.name.Widget)
	if wl.servers {
		two := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		two.SetHomogeneous(true)
		field(two, i18n.T("IMAP server"), &wl.imapAddr.Widget)
		field(two, i18n.T("SMTP server"), &wl.smtpAddr.Widget)
		gtkx.Append(grid, &two.Widget)
		field(grid, i18n.T("User name"), &wl.user.Widget)
	} else {
		line := gtk.NewBox(gtk.OrientationHorizontalValue, 14)
		line.AddCssClass("mail-servers")
		l := gtk.NewLabel(fmt.Sprintf("IMAP %s · SMTP %s", imapA, smtpA))
		l.AddCssClass("mail-servers-line")
		l.SetXalign(0)
		l.SetHexpand(true)
		l.SetEllipsize(2)
		gtkx.Append(line, &l.Widget)
		wl.link(line, i18n.T("Change"), func() { wl.servers = true; wl.draw() })
		gtkx.Append(grid, &line.Widget)
	}
	gtkx.Append(box, &grid.Widget)
	wl.ptrs = append(wl.ptrs, wl.w.h.enter(wl.password, wl.signInIMAP))
	wl.drawLog(box)
	wl.errLine(box)
	row := wl.foot(box, i18n.T("Enter signs in · Esc goes back"))
	if wl.again == nil {
		wl.link(row, i18n.T("Back"), wl.back)
	}
	b := wl.primary(row, i18n.T("Sign in"), wl.signInIMAP)
	b.SetSensitive(!wl.busy)
	wl.password.GrabFocus()
}

func (wl *welcome) signInIMAP() {
	if wl.busy {
		return
	}
	acc := mail.Account{
		Name: strings.TrimSpace(wl.name.GetText()), Address: wl.addr, Provider: mail.Password,
		IMAP: strings.TrimSpace(wl.imapAddr.GetText()), SMTP: strings.TrimSpace(wl.smtpAddr.GetText()),
		Username: strings.TrimSpace(wl.user.GetText()),
	}
	pw := wl.password.GetText()
	switch {
	case pw == "":
		wl.err = i18n.T("Type the password.")
		wl.draw()
		return
	case acc.IMAP == "" || acc.SMTP == "":
		wl.err = i18n.T("Type both servers.")
		wl.servers = true
		wl.draw()
		return
	}
	wl.err, wl.busy = "", true
	imapA, smtpA := acc.Servers()
	wl.log = []logLine{{lineWait, i18n.Tf("signing in to %s and %s…", imapA, smtpA)}}
	wl.draw()
	ctx, cancel := context.WithTimeout(wl.w.ctx, 40*time.Second)
	wl.cancel = cancel
	go func() {
		err := mail.Check(ctx, acc, mail.Auth{User: acc.User(), Password: pw})
		gtkx.Post(func() {
			if wl.w.welcome != wl {
				return
			}
			wl.busy = false
			if err != nil {
				wl.say(lineBad, i18n.Tf("not signed in: %v", err))
				wl.draw()
				return
			}
			wl.say(lineDone, i18n.Tf("signed in to %s and %s", imapA, smtpA))
			wl.keep(acc, mail.SecretPassword, pw)
		})
	}()
}

// --- 3: the first sync ---

// keep saves the account and its secret, then syncs it the first time,
// saying each step.
func (wl *welcome) keep(acc mail.Account, kind, secret string) {
	w := wl.w
	if wl.again != nil && acc.Name == "" {
		acc.Name = wl.again.Name
	}
	acc = w.settings.Add(acc)
	if err := w.secrets.Set(acc.ID, kind, secret); err != nil {
		wl.say(lineBad, i18n.Tf("could not keep the sign-in in the keyring: %v", err))
		return
	}
	if err := w.settings.Save(mail.SettingsPath()); err != nil {
		wl.say(lineBad, err.Error())
		return
	}
	wl.acc, wl.step, wl.busy = acc, 3, true
	wl.log = append(wl.log, logLine{lineWait, i18n.T("listing the mailboxes…")})
	wl.draw()
	s := mail.NewSession(acc, w.settings, w.store, w.secrets)
	post := func(f func()) {
		gtkx.Post(func() {
			if w.welcome == wl {
				f()
			}
		})
	}
	go func() {
		defer s.Close()
		boxes, err := s.Mailboxes(w.ctx)
		if err != nil {
			post(func() { wl.say(lineBad, err.Error()); wl.synced, wl.busy = true, false; wl.draw() })
			return
		}
		post(func() {
			wl.say(lineDone, i18n.Nf("%d mailbox", "%d mailboxes", len(boxes), len(boxes)))
			wl.say(lineWait, i18n.Tf("Inbox: fetching the newest %d…", mail.FirstSync))
		})
		start := time.Now()
		inbox := s.Role(mail.RoleInbox)
		_, err = s.Sync(w.ctx, inbox)
		n, took := len(s.Items(inbox)), time.Since(start)
		post(func() {
			if err != nil {
				wl.say(lineBad, i18n.Tf("Inbox: %v", err))
			} else {
				wl.say(lineDone, i18n.Nf("Inbox: %d message, in %s", "Inbox: %d messages, in %s", n, n, took.Round(10*time.Millisecond)))
			}
			wl.synced = true
			wl.draw()
		})
		if sent := s.Role(mail.RoleSent); sent != "" && err == nil {
			post(func() { wl.say(lineWait, i18n.T("Sent: fetching…")) })
			_, err := s.Sync(w.ctx, sent)
			n := len(s.Items(sent))
			post(func() {
				if err != nil {
					wl.say(lineBad, i18n.Tf("Sent: %v", err))
				} else {
					wl.say(lineDone, i18n.Nf("Sent: %d message", "Sent: %d messages", n, n))
				}
			})
		}
		post(func() { wl.busy = false; wl.draw() })
	}()
}

func (wl *welcome) drawSync(box *gtk.Box) {
	title := i18n.T("Fetching your mail")
	if wl.synced && !wl.busy {
		title = i18n.T("Ready")
	}
	wl.title(box, title, wl.acc.Address)
	wl.drawLog(box)
	row := wl.foot(box, "")
	if wl.synced {
		b := wl.primary(row, i18n.T("Open the inbox"), wl.open)
		b.GrabFocus()
	}
}

// open starts the account in the window and shows its inbox.
func (wl *welcome) open() {
	w := wl.w
	acc := wl.acc
	x := w.addSession(acc)
	w.closeWelcome()
	w.top.SetVisibleChildName("main")
	w.view = view{kind: viewBox, account: x.s.Acc.ID, mailbox: x.s.Role(mail.RoleInbox)}
	w.drawSide()
	w.refresh()
}

// field adds a labelled field to a box.
func field(box *gtk.Box, label string, e *gtk.Widget) {
	f := gtk.NewBox(gtk.OrientationVerticalValue, 5)
	f.AddCssClass("mail-field-block")
	l := gtk.NewLabel(label)
	l.AddCssClass("mail-field-name")
	l.SetXalign(0)
	gtkx.Append(f, &l.Widget)
	gtkx.Append(f, e)
	gtkx.Append(box, &f.Widget)
}
