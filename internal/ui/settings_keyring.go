package ui

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/keyring"
	"github.com/lyricat/inua/internal/proc"
	"github.com/lyricat/inua/internal/system"
)

// keyringReadsShown is how many rows the Keyring page lists.
const keyringReadsShown = 60

// keyringSettings is the Keyring page: who serves the Secret Service, the
// switch for read notifications, and the reads inua-keyring recorded
// (keyring.AccessPath), read again each time the page is shown. A read
// clicked opens the side panel with what is known of it.
type keyringSettings struct {
	state, none gtk.Label
	notify      gtk.Switch
	reads       gtk.Box
	refresh     gtk.Button
	detail      gtk.Revealer
	head        gtk.Label
	fields      gtk.Box
	close       gtk.Button

	groups []keyring.Reads // the rows, as listed
	rows   []*gtk.Button
	open   int // the row the panel shows; -1 for none
}

func (s *shellUI) getKeyringSettings(ui *gtkx.Builder) {
	k := &s.settings.keyring
	ui.Get("keyring_state", &k.state)
	ui.Get("keyring_none", &k.none)
	ui.Get("keyring_notify", &k.notify)
	ui.Get("keyring_reads", &k.reads)
	ui.Get("keyring_refresh", &k.refresh)
	ui.Get("keyring_detail", &k.detail)
	ui.Get("keyring_detail_head", &k.head)
	ui.Get("keyring_detail_fields", &k.fields)
	ui.Get("keyring_detail_close", &k.close)
	k.open = -1
	k.notify.ConnectStateSet(&s.onSettingsSwitch)
	k.refresh.ConnectClicked(&s.onSettingsButton)
	k.close.ConnectClicked(&s.onSettingsButton)
}

// keyringButton handles the page's buttons; false for another page's.
func (s *shellUI) keyringButton(name string) bool {
	switch {
	case name == "keyring_refresh":
		s.showKeyringReads()
	case name == "keyring_detail_close":
		s.openKeyringRead(-1)
	case strings.HasPrefix(name, "keyring-read:"):
		i, _ := strconv.Atoi(strings.TrimPrefix(name, "keyring-read:"))
		if i == s.settings.keyring.open {
			i = -1 // a second click closes it
		}
		s.openKeyringRead(i)
	default:
		return false
	}
	return true
}

// openKeyringRead shows row i in the side panel, marking it; -1 closes
// the panel.
func (s *shellUI) openKeyringRead(i int) {
	k := &s.settings.keyring
	if i >= len(k.groups) {
		i = -1
	}
	for j, row := range k.rows {
		if j == i {
			row.AddCssClass("on")
		} else {
			row.RemoveCssClass("on")
		}
	}
	k.open = i
	if i < 0 {
		k.detail.SetRevealChild(false)
		return
	}
	head := i18n.T("<span alpha='55%'>[</span>READ<span alpha='55%'>]</span>")
	if k.groups[i].Kind == keyring.Passphrase {
		head = i18n.T("<span alpha='55%'>[</span>PASSPHRASE<span alpha='55%'>]</span>")
	}
	k.head.SetMarkup(head)
	gtkx.Clear(&k.fields)
	for _, f := range keyringFields(k.groups[i]) {
		gtkx.Append(&k.fields, &keyringField(f).Widget)
	}
	k.detail.SetRevealChild(true)
}

// keyringFieldText is a field of the side panel: a name, its value, and
// whether the value is a path or a command, set in a fixed-width face.
type keyringFieldText struct {
	name, value string
	mono        bool
}

// keyringFields is what the side panel says of a row; what the record
// does not hold (it was written before inua-keyring kept it) is left out.
func keyringFields(g keyring.Reads) []keyringFieldText {
	where := g.Pass
	switch {
	case g.Kind == keyring.Passphrase: // only what gpg decrypts, if it said
	case where == "" && g.Collection == keyring.Session:
		where = i18n.T("Not in pass: the session keyring is kept in memory")
	case where == "":
		where = i18n.T("Not recorded")
	}
	// To the second: reads in a row are often seconds apart.
	at := func(t time.Time) string { return i18n.Time(t.Local(), "Jan 2 15:04:05") }
	when := at(g.Time)
	if g.N > 1 {
		when = i18n.Nf("%d reads, %s to %s", "%d reads, %s to %s", g.N, g.N, at(g.First), when)
	}
	process := i18n.T("Not known")
	switch {
	case g.PID > 0 && g.Exe == "": // an older record: running or not cannot be told
		process = i18n.Tf("pid %d", g.PID)
	case g.PID > 0 && proc.Exe(g.PID) == g.Exe: // a PID is reused once its process ends
		process = i18n.Tf("pid %d, still running", g.PID)
	case g.PID > 0:
		process = i18n.Tf("pid %d, ended", g.PID)
	}
	secret, value, answer := i18n.T("Secret"), g.Secret, ""
	if g.Kind == keyring.Passphrase {
		secret, answer = i18n.T("For"), keyringAnswer(g.Answer)
		if value == g.Pass {
			value = "" // In pass says it
		}
	}
	fields := []keyringFieldText{
		{secret, value, false},
		{i18n.T("Answer"), answer, false},
		{i18n.T("In pass"), where, g.Pass != ""},
		{i18n.T("Keyring"), g.Collection, false},
		{i18n.T("App"), g.App, false},
		{i18n.T("Program"), g.Exe, true},
		{i18n.T("Command line"), g.Command, true},
		{i18n.T("Runs under"), g.Chain, false},
		{i18n.T("Process"), process, false},
		{i18n.T("Bus name"), g.Bus, true},
		{i18n.T("When"), when, false},
	}
	out := fields[:0]
	for _, f := range fields {
		if f.value != "" {
			out = append(out, f)
		}
	}
	return out
}

// keyringAnswer says how a passphrase was answered.
func keyringAnswer(a string) string {
	switch a {
	case keyring.Entered:
		return i18n.T("Entered")
	case keyring.Cancelled:
		return i18n.T("cancelled")
	case keyring.Failed:
		return i18n.T("the dialog could not be shown")
	}
	return a
}

// keyringField is a field of the side panel: its name over its value,
// which can be selected and copied.
func keyringField(f keyringFieldText) *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVerticalValue, 3)
	name := gtk.NewLabel(f.name)
	name.AddCssClass("sub")
	name.SetXalign(0)
	gtkx.Append(box, &name.Widget)
	value := gtk.NewLabel(f.value)
	value.AddCssClass("title")
	if f.mono {
		value.AddCssClass("mono-sub")
	}
	value.SetXalign(0)
	value.SetWrap(true)
	value.SetWrapMode(2)       // word or character: paths break anywhere
	value.SetMaxWidthChars(30) // the panel's width, not the text's
	value.SetSelectable(true)
	gtkx.Append(box, &value.Widget)
	return box
}

// showKeyringReads says who serves the Secret Service and lists the
// recorded reads, newest first; a process reading one secret again and
// again is one row, counted.
func (s *shellUI) showKeyringReads() {
	k := &s.settings.keyring
	if s.settings.win == nil {
		return
	}
	k.state.SetText(i18n.T("Looking…"))
	go func() {
		state := i18n.T("Cannot reach the session bus")
		if conn, err := system.SessionBus(); err == nil {
			switch who, ours := keyring.Serving(conn); {
			case ours:
				state = i18n.Tf("%s serves it, keeping the secrets in pass", who)
			case who != "":
				state = i18n.Tf("%s serves it, not inua-keyring; inua keyring replace switches", who)
			default:
				state = i18n.T("No one serves it now; D-Bus starts it on the first request")
			}
		}
		reads, err := keyring.ReadAccess(keyring.AccessPath())
		gtkx.Post(func() {
			if s.settings.win == nil {
				return
			}
			k.state.SetText(state)
			gtkx.Clear(&k.reads)
			if err != nil {
				k.none.SetText(i18n.Tf("The record of reads cannot be read: %s", err.Error()))
			} else {
				k.none.SetText(i18n.T("No app has read a secret yet."))
			}
			k.none.SetVisible(err != nil || len(reads) == 0)
			k.groups, k.rows = keyring.GroupReads(reads, keyringReadsShown), nil
			for i, g := range k.groups {
				row := keyringRow(g)
				row.SetName(fmt.Sprintf("keyring-read:%d", i))
				row.ConnectClicked(&s.onSettingsButton)
				k.rows = append(k.rows, row)
				gtkx.Append(&k.reads, &row.Widget)
			}
			s.openKeyringRead(-1)
		})
	}()
}

// keyringRow is a row of reads: the app and the secret, where the secret
// is in pass, and when (the newest). Clicking it opens the side panel.
func keyringRow(g keyring.Reads) *gtk.Button {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	row.AddCssClass("row")
	icon, head := "dialog-password-symbolic", i18n.Tf("%s read “%s”", g.App, g.Secret)
	if g.Kind == keyring.Passphrase {
		icon, head = "system-lock-screen-symbolic", i18n.Tf("%s asked for the passphrase", cmp.Or(g.App, "gpg"))
	}
	img := gtk.NewImageFromIconName(icon)
	img.SetValign(gtk.AlignCenterValue)
	gtkx.Append(row, &img.Widget)
	text := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	text.SetHexpand(true)
	title := gtk.NewLabel(head)
	title.AddCssClass("title")
	title.SetXalign(0)
	title.SetEllipsize(3) // end
	gtkx.Append(text, &title.Widget)
	// Where the secret is; the PID for a record that does not say. A
	// passphrase: what gpg decrypts, or the key, and a refusal.
	sub := g.Pass
	switch {
	case g.Kind == keyring.Passphrase:
		sub = cmp.Or(g.Pass, g.Secret, i18n.Tf("pid %d", g.PID))
		if g.Answer != keyring.Entered {
			sub = i18n.Tf("%s · %s", sub, keyringAnswer(g.Answer))
		}
	case sub == "" && g.Collection == keyring.Session:
		sub = i18n.T("session keyring, in memory")
	case sub == "":
		sub = i18n.Tf("pid %d", g.PID)
	}
	if g.N > 1 {
		sub = i18n.Nf("%s · %d times in a row", "%s · %d times in a row", g.N, sub, g.N)
	}
	l := gtk.NewLabel(sub)
	l.AddCssClass("sub")
	if g.Pass != "" {
		l.AddCssClass("mono-sub")
	}
	l.SetXalign(0)
	l.SetEllipsize(2) // middle: a path's both ends tell most
	gtkx.Append(text, &l.Widget)
	gtkx.Append(row, &text.Widget)
	when := gtk.NewLabel(readTime(g.Time, time.Now()))
	when.AddCssClass("sub")
	when.SetValign(gtk.AlignCenterValue)
	gtkx.Append(row, &when.Widget)
	b := gtk.NewButton()
	b.AddCssClass("keyring-read")
	b.SetChild(&row.Widget)
	b.SetTooltipText(i18n.T("Show the details"))
	return b
}

// readTime is when a read was: the time today, else the date and time.
func readTime(t, now time.Time) string {
	t = t.Local()
	if y, m, d := t.Date(); now.Year() == y && now.Month() == m && now.Day() == d {
		return t.Format("15:04")
	}
	return i18n.Time(t, "Jan 2 15:04")
}
