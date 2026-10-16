package ui

import (
	"cmp"
	"fmt"
	"log"
	"slices"
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

// keyringSettings is the Keyring page: who serves the Secret Service,
// whether it is unlocked, when it locks, the switch for read
// notifications, the apps trusted with each secret
// (docs/feat/keyring-trust.md), and the reads inua-keyring recorded
// (keyring.AccessPath), read again each time the page is shown. A read
// clicked opens the side panel with what is known of it.
type keyringSettings struct {
	state, none gtk.Label
	notify      gtk.Switch
	reads       gtk.Box
	refresh     gtk.Button
	detail      gtk.Revealer
	panel       sidePanel
	scroll      gtk.ScrolledWindow // the reads' page
	lastRead    int                // the read the panel showed last
	head        gtk.Label
	fields      gtk.Box
	close       gtk.Button

	lockState, problemText, trustedNone gtk.Label
	trustedSum, trustedCount            gtk.Label
	lock, reseal, trustedOpen, back     gtk.Button
	lockAfter, untrusted                gtk.DropDown
	problem, trusted                    gtk.Box
	pages                               gtk.Stack
	filter                              gtk.SearchEntry
	onFilter                            func(gtk.SearchEntry)
	apps                                []keyringApp // the trusted apps
	shown                               []keyringApp // those the filter lets through, as listed

	// The paths whose programs read every secret (keyring.PathRule).
	paths                    gtk.Box
	pathsCount, trustedTotal gtk.Label
	pathEntry                gtk.Entry
	pathAdd                  gtk.Button
	pathNote                 gtk.Label
	rules                    []keyring.PathRule

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
	ui.Get("scroll_keyring", &k.scroll)
	s.initPanel(&k.panel, &k.detail, &k.scroll, 300)
	ui.Get("keyring_detail_head", &k.head)
	ui.Get("keyring_detail_fields", &k.fields)
	ui.Get("keyring_detail_close", &k.close)
	ui.Get("keyring_lock_state", &k.lockState)
	ui.Get("keyring_lock", &k.lock)
	ui.Get("keyring_lock_after", &k.lockAfter)
	ui.Get("keyring_untrusted", &k.untrusted)
	ui.Get("keyring_problem", &k.problem)
	ui.Get("keyring_problem_text", &k.problemText)
	ui.Get("keyring_reseal", &k.reseal)
	ui.Get("keyring_trusted", &k.trusted)
	ui.Get("keyring_trusted_none", &k.trustedNone)
	ui.Get("keyring_trusted_sum", &k.trustedSum)
	ui.Get("keyring_trusted_count", &k.trustedCount)
	ui.Get("keyring_trusted_open", &k.trustedOpen)
	ui.Get("keyring_trusted_back", &k.back)
	ui.Get("keyring_pages", &k.pages)
	ui.Get("keyring_trusted_filter", &k.filter)
	ui.Get("keyring_paths", &k.paths)
	ui.Get("keyring_paths_count", &k.pathsCount)
	ui.Get("keyring_trusted_total", &k.trustedTotal)
	ui.Get("keyring_path_entry", &k.pathEntry)
	ui.Get("keyring_path_add", &k.pathAdd)
	ui.Get("keyring_path_note", &k.pathNote)
	k.open, k.lastRead = -1, -1
	k.notify.ConnectStateSet(&s.onSettingsSwitch)
	k.onFilter = func(gtk.SearchEntry) { s.listTrustedApps() }
	k.filter.ConnectSearchChanged(&k.onFilter)
	for _, b := range []*gtk.Button{&k.refresh, &k.close, &k.lock, &k.reseal, &k.trustedOpen, &k.back, &k.pathAdd} {
		b.ConnectClicked(&s.onSettingsButton)
	}
	for _, d := range []*gtk.DropDown{&k.lockAfter, &k.untrusted} {
		s.settings.drops[d.GoPointer()] = d
		gtkx.ConnectNotify(d, "selected", &s.onSettingsDrop)
	}
}

// The choices of the page's drop-downs, in their order.
var (
	keyringLockAfter = []int{0, 5, 15, 30, 60, 240} // minutes
	keyringUntrusted = []string{"allow", "deny"}
)

// showKeyringSettings sets the drop-downs as the configuration has them.
func (s *shellUI) showKeyringSettings() {
	k := &s.settings.keyring
	c := s.cfg.Keyring
	k.lockAfter.SetSelected(uint32(max(0, slices.Index(keyringLockAfter, c.LockAfter))))
	k.untrusted.SetSelected(uint32(max(0, slices.Index(keyringUntrusted, c.Untrusted))))
}

// keyringApp is an app trusted with a secret, as the page lists it.
type keyringApp struct {
	item   string // "collection/item"
	secret string // the item's label
	app    keyring.Trusted
}

// keyringButton handles the page's buttons; false for another page's.
func (s *shellUI) keyringButton(name string) bool {
	k := &s.settings.keyring
	switch {
	case name == "keyring_refresh":
		s.showKeyringReads()
	case name == "keyring_trusted_open":
		s.openKeyringRead(-1)
		k.pages.SetVisibleChildName("trusted")
		k.filter.GrabFocus()
	case name == "keyring_trusted_back":
		k.filter.SetText("")
		k.pages.SetVisibleChildName("main")
	case name == "keyring_lock":
		k.lock.SetSensitive(false)
		go func() {
			if conn, err := system.SessionBus(); err == nil {
				keyring.LockKeyring(conn)
				conn.Close()
			}
			gtkx.Post(s.showKeyringReads)
		}()
	case name == "keyring_reseal":
		k.reseal.SetSensitive(false)
		go func() { // inua-keyring asks the user, in the shell's dialog
			if conn, err := system.SessionBus(); err == nil {
				if _, err := keyring.ResealTrust(conn); err != nil {
					log.Printf("settings: keyring: %v", err)
				}
				conn.Close()
			}
			gtkx.Post(s.showKeyringReads)
		}()
	case name == "keyring_path_add":
		pattern := strings.TrimSpace(k.pathEntry.GetText())
		if pattern == "" {
			return true
		}
		k.pathAdd.SetSensitive(false)
		k.pathNote.SetVisible(false)
		go func() { // inua-keyring asks the user, in the shell's dialog
			note, added := i18n.T("Cannot reach inua-keyring"), false
			if conn, err := system.SessionBus(); err == nil {
				var kept string
				switch kept, added, err = keyring.AllowPath(conn, pattern); {
				case err != nil:
					note = i18n.Tf("Not added: %s", err.Error())
				case !added:
					note = i18n.T("Not added: you said no.")
				default:
					note = i18n.Tf("Added %s.", kept)
				}
				conn.Close()
			}
			gtkx.Post(func() {
				if s.settings.win == nil {
					return
				}
				k.pathAdd.SetSensitive(true)
				k.pathNote.SetText(note)
				k.pathNote.SetVisible(true)
				toggle(&k.pathNote.Widget, "warn-text", !added)
				if added {
					k.pathEntry.SetText("")
				}
				s.showKeyringReads()
			})
		}()
	case strings.HasPrefix(name, "keyring-path-rm:"):
		i, _ := strconv.Atoi(strings.TrimPrefix(name, "keyring-path-rm:"))
		if i < 0 || i >= len(k.rules) {
			return true
		}
		pattern := k.rules[i].Pattern
		go func() {
			if conn, err := system.SessionBus(); err == nil {
				if err := keyring.DisallowPath(conn, pattern); err != nil {
					log.Printf("settings: keyring: %v", err)
				}
				conn.Close()
			}
			gtkx.Post(s.showKeyringReads)
		}()
	case strings.HasPrefix(name, "keyring-untrust:"):
		i, _ := strconv.Atoi(strings.TrimPrefix(name, "keyring-untrust:"))
		if i < 0 || i >= len(k.shown) {
			return true
		}
		a := k.shown[i]
		go func() {
			if conn, err := system.SessionBus(); err == nil {
				if err := keyring.UntrustApp(conn, a.item, a.app.Identity); err != nil {
					log.Printf("settings: keyring: %v", err)
				}
				conn.Close()
			}
			gtkx.Post(s.showKeyringReads)
		}()
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
		var row *gtk.Widget
		if k.lastRead >= 0 && k.lastRead < len(k.rows) {
			row = &k.rows[k.lastRead].Widget
		}
		s.showPanel(&k.panel, false, row)
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
	k.lastRead = i
	s.showPanel(&k.panel, true, &k.rows[i].Widget)
}

// keyringFieldText is a field of the side panel: a name and its value.
type keyringFieldText struct{ name, value string }

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
		{secret, value},
		{i18n.T("Answer"), answer},
		{i18n.T("Decision"), keyringDecision(g.Decision)},
		{i18n.T("In pass"), where},
		{i18n.T("Keyring"), g.Collection},
		{i18n.T("App"), g.App},
		{i18n.T("Program"), g.Exe},
		{i18n.T("Command line"), g.Command},
		{i18n.T("Runs under"), g.Chain},
		{i18n.T("Process"), process},
		{i18n.T("Bus name"), g.Bus},
		{i18n.T("When"), when},
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
	value.AddCssClass("mono-sub") // every value alike: paths, commands, names
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
	folder := s.cfg.Keyring.Folder
	go func() {
		state := i18n.T("Cannot reach the session bus")
		lockState, unlocked, problem := i18n.T("inua-keyring is not running"), false, ""
		if conn, err := system.SessionBus(); err == nil {
			switch who, ours := keyring.Serving(conn); {
			case ours:
				state = i18n.Tf("%s serves it, keeping the secrets in pass", who)
				if on, p, err := keyring.KeyringState(conn); err == nil {
					unlocked, problem = on, p
					lockState = i18n.T("Locked: the next read decrypts with gpg, which may ask for your passphrase")
					if on {
						lockState = i18n.T("Kept in protected memory since the first read, so reads ask for nothing")
					}
				}
			case who != "":
				state = i18n.Tf("%s serves it, not inua-keyring; inua keyring replace switches", who)
			default:
				state = i18n.T("No one serves it now; D-Bus starts it on the first request")
			}
			conn.Close()
		}
		apps := keyringApps(folder)
		rules := keyring.ReadPaths(keyring.TrustPath())
		reads, err := keyring.ReadAccess(keyring.AccessPath())
		gtkx.Post(func() {
			if s.settings.win == nil {
				return
			}
			k.state.SetText(state)
			k.lockState.SetText(lockState)
			k.lock.SetSensitive(unlocked)
			k.problem.SetVisible(problem != "")
			k.problemText.SetText(keyringProblem(problem))
			k.reseal.SetSensitive(true)
			s.showTrustedApps(apps, rules, problem)
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
	if g.Decision == keyring.DecDenied {
		icon, head = "action-unavailable-symbolic", i18n.Tf("%s was denied “%s”", g.App, g.Secret)
	}
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
	if g.Decision != "" && g.Decision != keyring.DecTrusted {
		sub = i18n.Tf("%s · %s", sub, keyringDecision(g.Decision))
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

// keyringProblem says why the trust list is not trusted.
func keyringProblem(p string) string {
	if p == "the list was changed outside inua-keyring" {
		return i18n.T("It was changed outside inua-keyring, so every app is asked about until you trust it again. Look through it first.")
	}
	return i18n.Tf("It cannot be read: %s. Every app is asked about.", p)
}

// keyringApps lists the trusted apps, each with the label of its secret,
// the newest first.
func keyringApps(folder string) []keyringApp {
	trust := keyring.ReadTrust(keyring.TrustPath())
	store, err := keyring.OpenPass(keyring.Pass{Dir: keyring.StoreDir(), Folder: folder})
	var out []keyringApp
	for item, apps := range trust {
		label := item
		if coll, id, ok := strings.Cut(item, "/"); ok && err == nil {
			if it, err := store.Item(coll, id); err == nil {
				label = cmp.Or(it.Label, item)
			}
		}
		for _, a := range apps {
			out = append(out, keyringApp{item: item, secret: label, app: a})
		}
	}
	slices.SortFunc(out, func(a, b keyringApp) int { return b.app.Since.Compare(a.app.Since) })
	return out
}

// showTrustedApps takes the trusted apps and paths read, says how many on
// the Keyring page, and lists them on their own page; problem is why the
// list is not trusted.
func (s *shellUI) showTrustedApps(apps []keyringApp, rules []keyring.PathRule, problem string) {
	k := &s.settings.keyring
	k.apps, k.rules = apps, rules
	s.listKeyringPaths()
	secrets := map[string]bool{}
	for _, a := range apps {
		secrets[a.item] = true
	}
	count := i18n.Nf("%d app", "%d apps", len(apps), len(apps))
	if len(apps) > 0 {
		count = i18n.Nf("%s, trusted with %d secret", "%s, trusted with %d secrets", len(secrets), count, len(secrets))
	}
	k.trustedCount.SetText(count)
	sum := count
	switch {
	case problem != "":
		sum = i18n.T("Not trusted: changed outside inua-keyring. Every app is asked about.")
	case len(apps) == 0 && len(rules) == 0:
		sum = i18n.T("None yet: each app is asked about the first time it reads a secret")
	}
	if problem == "" && len(rules) > 0 {
		sum = i18n.Nf("%s · %d allowed path", "%s · %d allowed paths", len(rules), count, len(rules))
	}
	k.trustedSum.SetText(sum)
	toggle(&k.trustedSum.Widget, "warn-text", problem != "")
	k.trustedTotal.SetText(sum)
	toggle(&k.trustedTotal.Widget, "warn-text", problem != "")
	k.pathsCount.SetText(i18n.Nf("%d path", "%d paths", len(rules), len(rules)))
	if len(rules) == 0 {
		k.pathsCount.SetText(i18n.T("None"))
	}
	s.listTrustedApps()
}

// keyringAppMatches says whether a trusted app has every word of the
// filter in its name, program, package or secret.
func keyringAppMatches(a keyringApp, q string) bool {
	hay := strings.ToLower(strings.Join([]string{a.app.Name(), a.app.Exe, a.app.Script, a.app.Flatpak, a.app.Package, a.secret}, " "))
	for _, w := range strings.Fields(strings.ToLower(q)) {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// listTrustedApps lists the trusted apps the filter lets through: the app
// and its secret, its program and package, since when; each with Remove.
func (s *shellUI) listTrustedApps() {
	k := &s.settings.keyring
	q := k.filter.GetText()
	k.shown = nil
	for _, a := range k.apps {
		if keyringAppMatches(a, q) {
			k.shown = append(k.shown, a)
		}
	}
	gtkx.Clear(&k.trusted)
	switch {
	case len(k.apps) == 0:
		k.trustedNone.SetText(i18n.T("No app is trusted yet: each app is asked about the first time it reads a secret."))
	case len(k.shown) == 0:
		k.trustedNone.SetText(i18n.Tf("No trusted app matches “%s”.", strings.TrimSpace(q)))
	}
	k.trustedNone.SetVisible(len(k.shown) == 0)
	for i, a := range k.shown {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		row.AddCssClass("row")
		img := gtk.NewImageFromIconName("security-high-symbolic")
		img.SetValign(gtk.AlignCenterValue)
		gtkx.Append(row, &img.Widget)
		text := gtk.NewBox(gtk.OrientationVerticalValue, 0)
		text.SetHexpand(true)
		title := gtk.NewLabel(i18n.Tf("%s reads “%s”", a.app.Name(), a.secret))
		title.AddCssClass("title")
		title.SetXalign(0)
		title.SetEllipsize(3)
		gtkx.Append(text, &title.Widget)
		where := a.app.Exe
		switch {
		case a.app.Flatpak != "":
			where = i18n.Tf("Flatpak: %s", a.app.Flatpak)
		case a.app.Script != "":
			where = a.app.Exe + " " + a.app.Script
		}
		if a.app.Package != "" {
			where = i18n.Tf("%s · %s", where, a.app.Package)
		}
		sub := gtk.NewLabel(where)
		sub.AddCssClass("sub")
		sub.AddCssClass("mono-sub")
		sub.SetXalign(0)
		sub.SetEllipsize(2)
		sub.SetTooltipText(where)
		gtkx.Append(text, &sub.Widget)
		gtkx.Append(row, &text.Widget)
		if !a.app.Since.IsZero() {
			when := gtk.NewLabel(i18n.Tf("since %s", i18n.Time(a.app.Since.Local(), "Jan 2, 2006")))
			when.AddCssClass("sub")
			when.SetValign(gtk.AlignCenterValue)
			gtkx.Append(row, &when.Widget)
		}
		rm := gtk.NewButtonWithLabel(i18n.T("Remove"))
		rm.AddCssClass("outlined")
		rm.SetValign(gtk.AlignCenterValue)
		rm.SetName(fmt.Sprintf("keyring-untrust:%d", i))
		rm.SetTooltipText(i18n.T("Stop trusting it: it is asked about at its next read"))
		rm.ConnectClicked(&s.onSettingsButton)
		gtkx.Append(row, &rm.Widget)
		gtkx.Append(&k.trusted, &row.Widget)
	}
}

// listKeyringPaths lists the allowed paths, each with when and Remove.
func (s *shellUI) listKeyringPaths() {
	k := &s.settings.keyring
	gtkx.Clear(&k.paths)
	for i, r := range k.rules {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		row.AddCssClass("row")
		img := gtk.NewImageFromIconName("folder-symbolic")
		img.SetValign(gtk.AlignCenterValue)
		gtkx.Append(row, &img.Widget)
		l := gtk.NewLabel(r.Pattern)
		l.AddCssClass("mono-sub")
		l.SetXalign(0)
		l.SetHexpand(true)
		l.SetEllipsize(2) // middle
		l.SetTooltipText(r.Pattern)
		gtkx.Append(row, &l.Widget)
		if !r.Since.IsZero() {
			when := gtk.NewLabel(i18n.Tf("since %s", i18n.Time(r.Since.Local(), "Jan 2, 2006")))
			when.AddCssClass("sub")
			when.SetValign(gtk.AlignCenterValue)
			gtkx.Append(row, &when.Widget)
		}
		rm := gtk.NewButtonWithLabel(i18n.T("Remove"))
		rm.AddCssClass("outlined")
		rm.SetValign(gtk.AlignCenterValue)
		rm.SetName(fmt.Sprintf("keyring-path-rm:%d", i))
		rm.SetTooltipText(i18n.T("Stop allowing it: its programs are asked about again"))
		rm.ConnectClicked(&s.onSettingsButton)
		gtkx.Append(row, &rm.Widget)
		gtkx.Append(&k.paths, &row.Widget)
	}
}

// keyringDecision says how a read went with the trust list.
func keyringDecision(d string) string {
	switch d {
	case keyring.DecTrusted:
		return i18n.T("trusted")
	case keyring.DecOnce:
		return i18n.T("allowed once")
	case keyring.DecAllowed:
		return i18n.T("allowed, and trusted from now on")
	case keyring.DecDenied:
		return i18n.T("denied")
	case keyring.DecUnasked:
		return i18n.T("allowed: no one to ask")
	}
	return d
}
