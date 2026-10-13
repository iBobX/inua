package ui

import (
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/keyring"
	"github.com/lyricat/inua/internal/system"
)

// keyringReadsShown is how many rows the Keyring page lists.
const keyringReadsShown = 60

// keyringSettings is the Keyring page: who serves the Secret Service, the
// switch for read notifications, and the reads inua-keyring recorded
// (keyring.AccessPath), read again each time the page is shown.
type keyringSettings struct {
	state, none gtk.Label
	notify      gtk.Switch
	reads       gtk.Box
	refresh     gtk.Button
}

func (s *shellUI) getKeyringSettings(ui *gtkx.Builder) {
	k := &s.settings.keyring
	ui.Get("keyring_state", &k.state)
	ui.Get("keyring_none", &k.none)
	ui.Get("keyring_notify", &k.notify)
	ui.Get("keyring_reads", &k.reads)
	ui.Get("keyring_refresh", &k.refresh)
	k.notify.ConnectStateSet(&s.onSettingsSwitch)
	k.refresh.ConnectClicked(&s.onSettingsButton)
}

// keyringButton handles the page's buttons; false for another page's.
func (s *shellUI) keyringButton(name string) bool {
	if name != "keyring_refresh" {
		return false
	}
	s.showKeyringReads()
	return true
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
			shown := 0
			for i := 0; i < len(reads) && shown < keyringReadsShown; shown++ {
				a, n := reads[i], 1
				for i+n < len(reads) && reads[i+n].App == a.App && reads[i+n].PID == a.PID && reads[i+n].Secret == a.Secret {
					n++
				}
				i += n
				gtkx.Append(&k.reads, &keyringRow(a, n).Widget)
			}
		})
	}()
}

// keyringRow is a read: the app and the secret, its PID, and when (the
// newest of n reads in a row).
func keyringRow(a keyring.Access, n int) *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	row.AddCssClass("row")
	img := gtk.NewImageFromIconName("dialog-password-symbolic")
	img.SetValign(gtk.AlignCenterValue)
	gtkx.Append(row, &img.Widget)
	text := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	text.SetHexpand(true)
	title := gtk.NewLabel(i18n.Tf("%s read “%s”", a.App, a.Secret))
	title.AddCssClass("title")
	title.SetXalign(0)
	title.SetEllipsize(3) // end
	gtkx.Append(text, &title.Widget)
	sub := i18n.Tf("pid %d", a.PID)
	if n > 1 {
		sub = i18n.Nf("pid %d · %d times in a row", "pid %d · %d times in a row", n, a.PID, n)
	}
	l := gtk.NewLabel(sub)
	l.AddCssClass("sub")
	l.SetXalign(0)
	gtkx.Append(text, &l.Widget)
	gtkx.Append(row, &text.Widget)
	when := gtk.NewLabel(readTime(a.Time, time.Now()))
	when.AddCssClass("sub")
	when.SetValign(gtk.AlignCenterValue)
	gtkx.Append(row, &when.Widget)
	return row
}

// readTime is when a read was: the time today, else the date and time.
func readTime(t, now time.Time) string {
	t = t.Local()
	if y, m, d := t.Date(); now.Year() == y && now.Month() == m && now.Day() == d {
		return t.Format("15:04")
	}
	return i18n.Time(t, "Jan 2 15:04")
}
