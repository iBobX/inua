package ui

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/pinentry"
	"github.com/lyricat/inua/internal/shell"
)

// Asking for a passphrase for gpg (inua-pinentry), in the polkit dialog's
// card: `call password getpin|confirm|message` with the request as JSON.
// The answer is the reply: the passphrase as its result, or the error
// "cancelled" (or "not ok", the third button of a question). The shell
// keeps nothing; the reply goes back over the user's own socket.

type passPrompts struct {
	slot chan struct{} // held by the dialog on screen
	cur  *passDialog
	// mask hides the key's owner, its fingerprint and the pass entry, by
	// the user's choice or while the screen is captured; kept across
	// dialogs.
	mask shell.PassMask
}

type passDialog struct {
	q    pinentry.Request
	done chan ipc.Response

	win                          gtk.Window
	title, message, prompt, info gtk.Label
	password, password2          gtk.PasswordEntry
	refs                         gtkx.Scope
	closed                       bool

	// What the mask hides and shows (applyPassMask).
	mask                                      gtk.Button
	subtitle, maskNote                        gtk.Label
	maskBox                                   gtk.Box
	card, keyCard, asks, primaryRow, forRow   gtk.Widget
	name, email, role, algo, fpr              gtk.Label
	primary, dates                            gtk.Label
	forKicker, forValue, askerApp, askerChain gtk.Label
	kickers                                   map[string]*gtk.Label // by row: what a copy flashes
	kickerText                                map[string]string     // a kicker's own text while it says "Copied"
	copies                                    map[string]int        // copies made of a row: the newest restores its kicker
}

// askPassword answers a `password` request, waiting for the user: it runs
// on the request's goroutine, the dialog on the main loop.
func (s *shellUI) askPassword(req ipc.Request) ipc.Response {
	var q pinentry.Request
	if len(req.Args) != 1 || json.Unmarshal([]byte(req.Args[0]), &q) != nil {
		return ipc.Response{Error: i18n.T("password: want one JSON request")}
	}
	q.Mode = pinentry.Mode(req.Function)
	switch q.Mode {
	case pinentry.GetPin, pinentry.Confirm, pinentry.Message:
	default:
		return ipc.Response{Error: "password: unknown mode " + req.Function}
	}
	p := &s.pass
	p.slot <- struct{}{} // one at a time
	defer func() { <-p.slot }()
	d := &passDialog{q: q, done: make(chan ipc.Response, 1)}
	gtkx.Post(func() { s.openPass(d) })
	if q.Timeout > 0 {
		select {
		case r := <-d.done:
			return r
		case <-time.After(time.Duration(q.Timeout) * time.Second):
			gtkx.Post(func() { s.finishPass(d, ipc.Response{Error: "cancelled"}) })
		}
	}
	return <-d.done
}

func (s *shellUI) bindPassHandlers() {
	later := func(f func()) { gtkx.Post(f) } // the dialog sent the signal
	s.onPassOK = func(gtk.Button) { later(s.answerPass) }
	s.onPassActivate = func(e gtk.PasswordEntry) {
		d := s.pass.cur
		if d != nil && d.q.Repeat != "" && e.GoPointer() == d.password.GoPointer() {
			d.password2.GrabFocus() // then the repeat
			return
		}
		later(s.answerPass)
	}
	s.onPassCancel = func(gtk.Button) {
		if d := s.pass.cur; d != nil {
			later(func() { s.finishPass(d, ipc.Response{Error: "cancelled"}) })
		}
	}
	s.onPassNotOK = func(gtk.Button) {
		if d := s.pass.cur; d != nil {
			later(func() { s.finishPass(d, ipc.Response{Error: i18n.T("not ok")}) })
		}
	}
	s.onPassMask = func(gtk.Button) {
		later(func() {
			s.pass.mask.Toggle()
			s.applyPassMask()
		})
	}
	s.onPassRow = func(b gtk.Button) {
		name := b.GetName()
		later(func() {
			if d := s.pass.cur; d != nil && !d.closed {
				s.copyPassRow(d, name)
			}
		})
	}
	s.onPassKey = func(_ gtk.EventControllerKey, keyval, _ uint32, mods gdk.ModifierType) bool {
		d := s.pass.cur
		if d != nil && (keyval == 'h' || keyval == 'H') && mods&gdk.ControlMaskValue != 0 && d.mask.GetVisible() {
			later(func() { // Ctrl+H: the mask, from the passphrase field
				s.pass.mask.Toggle()
				s.applyPassMask()
			})
			return true
		}
		if keyval != keyEscape || d == nil {
			return false
		}
		later(func() { s.finishPass(d, ipc.Response{Error: "cancelled"}) })
		return true
	}
}

func (s *shellUI) openPass(d *passDialog) {
	q := d.q
	var icon gtk.Image
	var command gtk.Label
	var who gtk.Box
	var ok, cancel, notok gtk.Button
	ui := s.load("polkit.ui", &d.refs)
	ui.Get("polkit_dialog", &d.win)
	ui.Get("icon", &icon)
	ui.Get("title", &d.title)
	ui.Get("message", &d.message)
	ui.Get("command", &command)
	ui.Get("who", &who)
	ui.Get("prompt", &d.prompt)
	ui.Get("password", &d.password)
	ui.Get("password2", &d.password2)
	ui.Get("info", &d.info)
	ui.Get("ok", &ok)
	ui.Get("cancel", &cancel)
	ui.Get("notok", &notok)
	var card, keyCard, asks gtk.Box
	var primaryRow, forRow gtk.Button
	ui.Get("card", &card)
	ui.Get("mask", &d.mask)
	ui.Get("subtitle", &d.subtitle)
	ui.Get("mask_note", &d.maskBox)
	ui.Get("mask_note_text", &d.maskNote)
	ui.Get("key_card", &keyCard)
	ui.Get("asks", &asks)
	for id, l := range map[string]*gtk.Label{
		"key_name": &d.name, "key_email": &d.email, "key_role": &d.role,
		"key_algo": &d.algo, "key_fpr": &d.fpr, "key_primary": &d.primary,
		"key_dates": &d.dates, "for_kicker": &d.forKicker, "for_value": &d.forValue,
		"asker_app": &d.askerApp, "asker_chain": &d.askerChain,
	} {
		ui.Get(id, l)
	}
	d.card, d.keyCard, d.asks = card.Widget, keyCard.Widget, asks.Widget
	d.kickers, d.kickerText, d.copies = map[string]*gtk.Label{}, map[string]string{}, map[string]int{}
	for _, r := range []struct{ row, kicker string }{
		{"row_owner", "owner_kicker"}, {"row_type", "type_kicker"}, {"row_fpr", "fpr_kicker"},
		{"row_primary", "primary_kicker"}, {"row_created", "created_kicker"},
		{"row_for", "for_kicker"}, {"row_asker", "asker_kicker"},
	} {
		var b gtk.Button
		ui.Get(r.row, &b)
		b.ConnectClicked(&s.onPassRow)
		b.SetTooltipText(i18n.T("Copy"))
		switch r.row {
		case "row_primary":
			primaryRow = b
		case "row_for":
			forRow = b
		}
		if r.kicker == "for_kicker" {
			d.kickers[r.row] = &d.forKicker
			continue
		}
		var k gtk.Label
		ui.Get(r.kicker, &k)
		d.kickers[r.row] = &k
	}
	d.primaryRow, d.forRow = primaryRow.Widget, forRow.Widget
	ui.Close()

	who.SetVisible(false)
	command.SetVisible(false)
	title := q.Title
	if title == "" || strings.EqualFold(title, "pinentry") {
		title = map[pinentry.Mode]string{pinentry.GetPin: i18n.T("Passphrase Required"), pinentry.Confirm: i18n.T("Confirm"), pinentry.Message: i18n.T("Message")}[q.Mode]
		if q.Mode == pinentry.GetPin && q.Key != nil {
			title = i18n.T("Unlock OpenPGP Key")
		}
	}
	d.title.SetText(title)
	s.fillPassDetails(d)
	d.mask.ConnectClicked(&s.onPassMask)
	getpin := q.Mode == pinentry.GetPin
	if !getpin {
		icon.SetFromIconName("dialog-information-symbolic")
	}
	// gpg's prompt ("Passphrase:", "PIN:") goes in the field, as polkit's
	// "Password" does; a longer one stays above it.
	prompt := strings.TrimSuffix(strings.TrimSpace(q.Prompt), ":")
	if prompt == "" {
		prompt = i18n.T("Passphrase")
	}
	if len(prompt) <= 24 {
		var v gobject.Value
		v.Init(gobject.TypeStringVal)
		v.SetString(prompt)
		d.password.SetProperty("placeholder-text", &v)
		v.Unset()
		if q.Repeat != "" {
			v.Init(gobject.TypeStringVal)
			v.SetString(strings.TrimSuffix(strings.TrimSpace(q.Repeat), ":"))
			d.password2.SetProperty("placeholder-text", &v)
			v.Unset()
		}
	} else {
		d.prompt.SetText(strings.TrimSpace(q.Prompt))
		d.prompt.SetVisible(getpin)
	}
	d.password.SetVisible(getpin)
	d.password2.SetVisible(getpin && q.Repeat != "")
	s.showPassInfo(d, q.Error, true)

	ok.SetLabel(labelOr(q.OK, i18n.T("OK")))
	cancel.SetLabel(labelOr(q.Cancel, i18n.T("Cancel")))
	cancel.SetVisible(q.Mode != pinentry.Message)
	notok.SetLabel(q.NotOK)
	notok.SetVisible(q.Mode == pinentry.Confirm && q.NotOK != "")
	ok.ConnectClicked(&s.onPassOK)
	cancel.ConnectClicked(&s.onPassCancel)
	notok.ConnectClicked(&s.onPassNotOK)
	d.password.ConnectActivate(&s.onPassActivate)
	d.password2.ConnectActivate(&s.onPassActivate)
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(&s.onPassKey)
	d.win.AddController(&keys.EventController)

	d.win.SetApplication(s.app)
	w := d.win.GoPointer()
	if s.layer {
		layershell.InitForWindow(w)
		layershell.SetNamespace(w, "inua-polkit")
		layershell.SetLayer(w, layershell.LayerOverlay)
		layershell.SetKeyboardMode(w, layershell.KeyboardExclusive)
		for _, e := range []layershell.Edge{layershell.EdgeTop, layershell.EdgeBottom, layershell.EdgeLeft, layershell.EdgeRight} {
			layershell.SetAnchor(w, e, true)
		}
		layershell.SetExclusiveZone(w, -1)
		if mon := s.monitorByName(s.snap.FocusedMonitor); mon != nil {
			layershell.SetMonitor(w, mon.GoPointer())
		}
	} else {
		d.win.SetTitle(title)
	}
	s.pass.cur = d
	s.lockPassSize(d)
	d.win.Present()
	if getpin {
		d.password.GrabFocus()
	} else {
		ok.GrabFocus()
	}
	log.Printf("pinentry: asking (%s)", q.Mode)
}

// fillPassDetails fills what does not change with the mask: who asks,
// what the key is and when it was made; applyPassMask fills the rest.
func (s *shellUI) fillPassDetails(d *passDialog) {
	q := d.q
	switch {
	case q.Mode != pinentry.GetPin:
	case q.App != "" && q.Keyring:
		d.subtitle.SetText(i18n.Tf("Enter the passphrase so inua-keyring can give %s its secret.", q.App))
	case q.App != "":
		d.subtitle.SetText(i18n.Tf("Enter the passphrase so %s can use this key.", q.App))
	case q.Key != nil:
		d.subtitle.SetText(i18n.T("Enter the passphrase to use this key."))
	}
	d.subtitle.SetVisible(d.subtitle.GetText() != "" && q.Key != nil)

	if k := q.Key; k != nil {
		d.algo.SetText(k.Algo)
		d.role.SetText(keyRole(*k))
		d.primaryRow.SetVisible(k.Subkey && k.Primary != "")
		dates := i18n.Time(k.Created.Local(), "Jan 2, 2006")
		switch {
		case k.Expires.IsZero():
		case k.Expires.Before(time.Now()):
			dates = i18n.Tf("%s · expired %s", dates, i18n.Time(k.Expires.Local(), "Jan 2, 2006"))
		default:
			dates = i18n.Tf("%s · expires %s", dates, i18n.Time(k.Expires.Local(), "Jan 2, 2006"))
		}
		d.dates.SetText(dates)
	}
	d.keyCard.SetVisible(q.Key != nil)

	// Who asks: the program running gpg, or the app inua-keyring reads
	// for; then what it runs under.
	d.askerApp.SetText(q.App)
	chain := ""
	if q.Chain != "" {
		chain = "← " + q.Chain
	}
	d.askerChain.SetText(chain)
	d.askerChain.SetTooltipText(q.Chain)
	d.forKicker.SetText(i18n.T("FOR"))
	if q.Keyring && q.Secret != "" {
		d.forKicker.SetText(i18n.T("SECRET"))
	}
	d.asks.SetVisible(q.App != "")
}

// passRowText is what a row of the dialog copies: its value in full,
// hidden or not, as an app would want it pasted.
func passRowText(q pinentry.Request, row, dates string) string {
	k := q.Key
	if k == nil {
		k = &pinentry.Key{}
	}
	switch row {
	case "row_owner":
		if k.Name == "" || k.Email == "" {
			return k.Name + k.Email
		}
		return k.Name + " <" + k.Email + ">"
	case "row_type":
		return k.Algo
	case "row_fpr":
		return k.Fingerprint
	case "row_primary":
		return k.Primary
	case "row_created":
		return dates
	case "row_for":
		if q.Keyring && q.Secret != "" {
			return q.Secret
		}
		return q.Entry
	case "row_asker":
		if q.Chain != "" {
			return q.App + " ← " + q.Chain
		}
		return q.App
	}
	return ""
}

// copyPassRow copies a row's value, and says so in its label a moment.
func (s *shellUI) copyPassRow(d *passDialog, row string) {
	text := passRowText(d.q, row, d.dates.GetText())
	kicker := d.kickers[row]
	if text == "" || kicker == nil {
		return
	}
	go func() {
		if err := copyText(text); err != nil {
			log.Printf("pinentry: copy: %v", err)
		}
	}()
	if !kicker.HasCssClass("copied") { // the label to go back to
		d.kickerText[row] = kicker.GetText()
	}
	kicker.SetText(i18n.T("Copied"))
	kicker.AddCssClass("copied")
	d.copies[row]++
	n := d.copies[row]
	time.AfterFunc(1200*time.Millisecond, func() {
		gtkx.Post(func() {
			if !d.closed && d.copies[row] == n { // not copied again meanwhile
				kicker.SetText(d.kickerText[row])
				kicker.RemoveCssClass("copied")
			}
		})
	})
}

// keyRole says what the key does: its use, and whether it is a subkey.
func keyRole(k pinentry.Key) string {
	sub := k.Subkey
	switch {
	case strings.Contains(k.Use, "e") && sub:
		return i18n.T("Encryption subkey")
	case strings.Contains(k.Use, "s") && sub:
		return i18n.T("Signing subkey")
	case strings.Contains(k.Use, "a") && sub:
		return i18n.T("Authentication subkey")
	case sub:
		return i18n.T("Subkey")
	}
	return i18n.T("Primary key")
}

// masked stands for a hidden value: as long whatever it hides.
const masked = "••••••••"

// applyPassMask shows the dialog's private details, or dots instead
// while they are hidden: the key's owner and fingerprint, the pass entry
// or secret, and gpg's description, which names the owner too.
func (s *shellUI) applyPassMask() {
	d := s.pass.cur
	if d == nil || d.closed {
		return
	}
	s.showPassDetails(d, s.pass.mask, s.pass.mask.Hidden())
}

// lockPassSize fills the dialog as the mask has it, and holds its card at
// the size it takes shown or hidden, the larger: hiding or showing the
// details then neither resizes it nor moves it. The values are measured,
// not drawn.
func (s *shellUI) lockPassSize(d *passDialog) {
	var w, h int32
	for _, hide := range []bool{false, true} {
		s.showPassDetails(d, s.pass.mask, hide)
		var min, nat, mb, nb int32
		d.card.Measure(gtk.OrientationHorizontalValue, -1, &min, &nat, &mb, &nb)
		w = max(w, nat)
	}
	for _, hide := range []bool{false, true} {
		s.showPassDetails(d, s.pass.mask, hide)
		var min, nat, mb, nb int32
		d.card.Measure(gtk.OrientationVerticalValue, w, &min, &nat, &mb, &nb)
		h = max(h, nat)
	}
	d.card.SetSizeRequest(w, h)
	s.applyPassMask()
}

// showPassDetails fills what the mask hides, hidden or not.
func (s *shellUI) showPassDetails(d *passDialog, m shell.PassMask, hide bool) {
	q := d.q
	private := q.Key != nil || q.Entry != "" || q.Secret != "" || pinentry.HasPrivate(q.Desc)
	d.mask.SetVisible(private)
	toggle(&d.mask.Widget, "on", hide)
	if hide {
		d.mask.SetIconName("view-reveal-symbolic")
		d.mask.SetTooltipText(i18n.T("Show the private details (Ctrl+H)"))
	} else {
		d.mask.SetIconName("view-conceal-symbolic")
		d.mask.SetTooltipText(i18n.T("Hide the private details: the key's owner, its fingerprint and the pass entry (Ctrl+H)"))
	}
	show := func(l *gtk.Label, text string) {
		toggle(&l.Widget, "pass-masked", hide && text != "")
		if hide && text != "" {
			text = masked
		}
		l.SetText(text)
		l.SetVisible(text != "")
	}

	if k := q.Key; k != nil {
		name, email := k.Name, k.Email
		if name == "" {
			name, email = email, ""
		}
		show(&d.name, name)
		show(&d.email, email)
		if hide && email == "" && k.Email != "" {
			d.email.SetVisible(false)
		}
		// Two lines, hidden or not: the dialog keeps its height.
		fpr := func(l *gtk.Label, f string) {
			show(l, strings.Replace(pinentry.Grouped(f), "  ", "\n", 1))
			if hide && f != "" {
				l.SetText(masked + "\n" + masked)
			}
		}
		fpr(&d.fpr, k.Fingerprint)
		fpr(&d.primary, k.Primary)
		d.primary.SetVisible(k.Subkey && k.Primary != "")
	}
	what := q.Entry
	if q.Keyring && q.Secret != "" {
		what = q.Secret
	}
	show(&d.forValue, what)
	d.forValue.SetTooltipText(map[bool]string{false: what, true: ""}[hide])
	d.forRow.SetVisible(what != "")

	// gpg's own words: only the first line once the key is shown in
	// parts (the rest names it), the owner and IDs dotted while hidden.
	desc := q.Desc
	if q.Key != nil && q.Mode == pinentry.GetPin {
		desc = ""
		if q.Repeat != "" { // a new passphrase: what to do
			desc, _, _ = strings.Cut(q.Desc, "\n")
		}
	}
	if hide {
		desc = pinentry.MaskPrivate(desc, masked)
	}
	d.message.SetText(desc)
	d.message.SetVisible(desc != "")

	switch {
	case !hide || !m.Auto():
		d.maskBox.SetVisible(false)
	case m.Capture() == shell.Recording:
		d.maskNote.SetText(i18n.T("Hidden while the screen is being recorded or shared."))
		d.maskBox.SetVisible(true)
	default:
		d.maskNote.SetText(i18n.T("Hidden for the screenshot."))
		d.maskBox.SetVisible(true)
	}
}

// passCapture tells the mask the screen began or stopped being captured,
// and hides or shows the dialog's details at once.
func (s *shellUI) passCapture(change func(*shell.PassMask)) {
	change(&s.pass.mask)
	s.applyPassMask()
}

func labelOr(s, def string) string {
	if s = strings.TrimSpace(s); s != "" {
		return s
	}
	return def
}

func (s *shellUI) showPassInfo(d *passDialog, text string, bad bool) {
	d.info.SetText(text)
	d.info.SetVisible(text != "")
	toggle(&d.info.Widget, "error", bad)
}

// answerPass gives the agent what was typed, or OK.
func (s *shellUI) answerPass() {
	d := s.pass.cur
	if d == nil || d.closed {
		return
	}
	if d.q.Mode != pinentry.GetPin {
		s.finishPass(d, ipc.Response{OK: true})
		return
	}
	pin := d.password.GetText()
	if d.q.Repeat != "" && pin != d.password2.GetText() {
		d.password.SetText("")
		d.password2.SetText("")
		s.showPassInfo(d, labelOr(d.q.NoMatch, i18n.T("The passphrases do not match.")), true)
		d.password.GrabFocus()
		return
	}
	s.finishPass(d, ipc.Response{OK: true, Result: pin})
}

func (s *shellUI) finishPass(d *passDialog, r ipc.Response) {
	if d.closed {
		return
	}
	d.closed = true
	if s.pass.cur == d {
		s.pass.cur = nil
	}
	d.password.SetText("")
	d.password2.SetText("")
	d.win.Destroy()
	d.refs.Release()
	d.done <- r
}
