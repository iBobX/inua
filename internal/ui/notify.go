package ui

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"

	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/notify"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

// Notifications, after iOS: a new one shows as a banner at the top right
// for a moment, then waits in the notification center (the bell in the
// bar, or Super+N), grouped by app, until the user opens or clears it. Focus keeps
// banners quiet. The center's contents live in notify.Center, saved to the
// state directory, so they outlast a restart.

const (
	maxBanners = 3
	bannerMS   = 180 // transition-duration of a banner's revealer
)

type notes struct {
	srv   *notify.Server
	stop  context.CancelFunc // ends serving; nil while not serving
	owner bool               // holds org.freedesktop.Notifications
	c     notify.Center
	dirty bool // a save is pending

	// Banners: one layer surface, a revealer per banner, newest on top.
	bwin    *gtk.ApplicationWindow
	bstack  gtk.Box
	brefs   gtkx.Scope
	banners []*banner
	ticking bool // the one-second banner clock is running

	// The center: built on open, destroyed on close, like the control
	// center.
	open, closing   bool
	win             gtk.ApplicationWindow
	backdrop        gtk.Box
	reveal          gtk.Revealer
	list, empty     gtk.Box
	scroll          gtk.ScrolledWindow
	count           gtk.Label
	focusNote       gtk.Label
	focus, clearAll gtk.Button
	refs            gtkx.Scope
	expanded        map[string]bool          // stacks the user opened, by app
	groupBox        map[string]*gtk.Box      // each app's group in the list, borrowed
	groupRev        map[string]*gtk.Revealer // and the revealer it is in
	clearing        bool                     // a clear is animating

	apps map[string]*desktop.Entry // by desktop ID, for names and icons
}

type banner struct {
	id      uint32
	note    notify.Notification
	reveal  *gtk.Revealer
	left    int // seconds before it goes; -1 for critical, which stay
	hover   bool
	leaving bool
}

// noteHandler takes the server's calls onto the main thread.
type noteHandler struct{ s *shellUI }

func (h noteHandler) Notify(n notify.Notification) { gtkx.Post(func() { h.s.postNote(n) }) }
func (h noteHandler) Close(id uint32)              { gtkx.Post(func() { h.s.withdrawNote(id) }) }

// startNotify loads the center and, if enabled, starts serving.
func (s *shellUI) startNotify() {
	c, err := notify.Load(notify.StatePath())
	if err != nil {
		log.Printf("notifications: %v", err)
	}
	s.notes.c = c
	s.notes.expanded = map[string]bool{}
	s.syncNotify()
	s.showUnread()
}

// syncNotify starts or stops the server to follow notifications.enable.
func (s *shellUI) syncNotify() {
	n := &s.notes
	switch want := s.cfg.Notify.Enable; {
	case want && n.stop == nil:
		conn, err := system.SessionBus()
		if err != nil {
			log.Printf("notifications: %v", err)
			return
		}
		ctx, stop := context.WithCancel(context.Background())
		n.stop = stop
		n.srv = notify.NewServer(conn, noteHandler{s})
		n.srv.SetNextID(n.c.MaxID() + 1)
		srv := n.srv
		go func() {
			err := srv.Serve(ctx, func(on bool) { gtkx.Post(func() { s.setNoteOwner(srv, on) }) })
			logErr("notifications", err)
			conn.Close()
		}()
	case !want && n.stop != nil:
		n.stop()
		n.stop, n.srv, n.owner = nil, nil, false
		s.clearBanners()
		s.applyFocusTile()
	}
}

func (s *shellUI) setNoteOwner(srv *notify.Server, on bool) {
	if s.notes.srv != srv { // a server since stopped
		return
	}
	s.notes.owner = on
	if on {
		log.Printf("notifications: showing them (org.freedesktop.Notifications)")
	} else {
		log.Printf("notifications: another daemon has org.freedesktop.Notifications; waiting for it to exit")
	}
	s.applyFocusTile()
	if s.settings.win != nil {
		s.showSettings()
	}
}

// notesNative reports whether inua shows notifications itself, so Focus
// is its own rather than mako's.
func (s *shellUI) notesNative() bool { return s.notes.owner }

// postNote takes a notification from an app.
func (s *shellUI) postNote(n notify.Notification) {
	nt := &s.notes
	banner := nt.c.Post(n, notify.Policy{CriticalInFocus: s.cfg.Notify.CriticalInFocus})
	s.saveNotesSoon()
	if s.bannerIndex(n.ID) >= 0 {
		s.replaceBanner(n) // an update to a banner on screen
	} else if banner {
		s.showBanner(n)
	}
	s.showUnread()
	if nt.open {
		s.fillCenter()
	}
}

// withdrawNote is an app closing its notification.
func (s *shellUI) withdrawNote(id uint32) {
	nt := &s.notes
	removed := nt.c.Remove(id)
	s.dropBanner(id)
	if nt.srv != nil {
		nt.srv.Closed(id, notify.Closed)
	}
	if removed {
		s.saveNotesSoon()
		s.showUnread()
		if nt.open {
			s.fillCenter()
		}
	}
}

// activateNote runs what clicking a notification does: its default action,
// then it is done with, unless the app marked it resident.
func (s *shellUI) activateNote(id uint32) {
	n, ok := s.noteByID(id)
	if !ok {
		return
	}
	if !n.Default {
		// Nothing to open: the click only puts the banner away; it stays
		// in the center.
		s.dropBanner(id)
		return
	}
	s.invokeNote(n, "default")
}

func (s *shellUI) invokeNote(n notify.Notification, key string) {
	nt := &s.notes
	switch {
	case n.Entry == shotEntry:
		s.shotNoteAction(n, key)
	case n.Entry == alarmEntry:
		s.alarmNoteAction(n, key)
	case n.Entry == recordingEntry:
		s.recordingNoteAction(n, key)
	case n.Entry == updateEntry:
		s.updateNoteAction(n, key)
	case nt.srv != nil:
		nt.srv.Invoke(n.ID, key, activationToken())
	}
	s.dropBanner(n.ID)
	if !n.Resident {
		s.dismissNote(n.ID)
	}
	if nt.open && key == "default" {
		s.closeCenter() // the app comes forward
	}
}

// dismissNote clears a notification from the center and tells its app.
func (s *shellUI) dismissNote(id uint32) {
	nt := &s.notes
	s.stopAlarmSound(id)
	nt.c.Remove(id)
	s.dropBanner(id)
	if nt.srv != nil {
		nt.srv.Closed(id, notify.Dismissed)
	}
	s.saveNotesSoon()
	s.showUnread()
	if nt.open {
		s.fillCenter()
	}
}

func (s *shellUI) clearNotes(ids []uint32) {
	nt := &s.notes
	for _, id := range ids {
		s.dropBanner(id)
		if nt.srv != nil {
			nt.srv.Closed(id, notify.Dismissed)
		}
	}
	s.saveNotesSoon()
	s.showUnread()
	if nt.open {
		s.fillCenter()
	}
}

// noteByID finds a notification in the center, or one only on a banner
// (transient).
func (s *shellUI) noteByID(id uint32) (notify.Notification, bool) {
	if n, ok := s.notes.c.Get(id); ok {
		return n, true
	}
	for _, b := range s.notes.banners {
		if b.id == id {
			return b.note, true
		}
	}
	return notify.Notification{}, false
}

func (s *shellUI) setFocus(on bool) {
	s.notes.c.Focus = on
	if on {
		for _, b := range s.notes.banners {
			if n, ok := s.noteByID(b.id); !ok || n.Urgency != notify.Critical {
				s.dropBanner(b.id)
			}
		}
	}
	s.saveNotesSoon()
	s.applyFocusTile()
	s.showUnread()
	if s.notes.open {
		s.fillCenter()
	}
}

// showUnread updates the bars' notification buttons.
func (s *shellUI) showUnread() {
	for _, b := range s.bars {
		s.showNotesButton(b)
	}
}

// showNotesButton shows a bell, or a moon while Focus is on, marked while
// the center holds notifications the user has not seen.
func (s *shellUI) showNotesButton(b *bar) {
	icon, tip := "preferences-system-notifications-symbolic", i18n.T("Notifications")
	if s.notes.c.Focus {
		icon, tip = "weather-clear-night-symbolic", i18n.T("Notifications (Focus is on)")
	}
	b.notesIcon.SetFromIconName(icon)
	b.notes.SetTooltipText(tip)
	b.unread.SetVisible(s.notes.c.Unread() > 0)
}

func (s *shellUI) saveNotesSoon() {
	if s.notes.dirty {
		return
	}
	s.notes.dirty = true
	glib.TimeoutAdd(500, &s.onNotesSave, 0)
}

// activationToken asks the compositor for a token that lets the app raise
// its window (xdg-activation), "" if there is none.
func activationToken() string {
	ctx := gdk.DisplayGetDefault().GetAppLaunchContext()
	if ctx == nil {
		return ""
	}
	defer ctx.Unref()
	return ctx.GetStartupNotifyId(&gio.AppInfoBase{}, nil)
}

// --- cards ---

// buildNote makes a notification's card. A banner has no time, and its
// close button only puts the banner away.
func (s *shellUI) buildNote(n notify.Notification, inBanner bool) *gtk.Box {
	var refs gtkx.Scope
	ui, err := gtkx.Load(s.readUI("note.ui"), &refs)
	if err != nil {
		log.Fatalf("note.ui: %v", err)
	}
	defer refs.Release()
	var card, actions gtk.Box
	var icon, picture gtk.Image
	var app, timeL, summary, body gtk.Label
	var closeB gtk.Button
	ui.Take("note", &card)
	ui.Get("app_icon", &icon)
	ui.Get("app", &app)
	ui.Get("time", &timeL)
	ui.Get("close", &closeB)
	ui.Get("summary", &summary)
	ui.Get("body", &body)
	ui.Get("picture", &picture)
	ui.Get("actions", &actions)
	ui.Close()

	id := strconv.FormatUint(uint64(n.ID), 10)
	card.SetName("note:" + id)
	if n.Urgency == notify.Critical {
		card.AddCssClass("critical")
	}
	if n.Default {
		card.AddCssClass("actionable")
	}
	name, iconName := s.noteApp(n)
	app.SetText(name)
	setIcon(&icon, iconName)
	icon.SetPixelSize(16)
	timeL.SetText(shell.Ago(n.Time, time.Now()))
	timeL.SetVisible(!inBanner)
	summary.SetText(shell.OneParagraph(n.Summary))
	summary.SetVisible(n.Summary != "")
	body.SetMarkup(shell.OneParagraph(n.Body))
	body.SetVisible(n.Body != "")
	switch {
	case n.Image != nil:
		if tex := imageTexture(n.Image); tex != nil {
			picture.SetFromPaintable(tex)
			tex.Unref()
			picture.SetVisible(true)
		}
	case n.ImgPath != "":
		picture.SetFromFile(n.ImgPath)
		picture.SetVisible(true)
	}
	closeB.SetName("close:" + id)
	if inBanner {
		closeB.SetName("hide:" + id)
		closeB.SetTooltipText(i18n.T("Hide"))
	} else {
		closeB.SetTooltipText(i18n.T("Clear"))
	}
	closeB.ConnectClicked(&s.onNoteButton)
	for i, a := range n.Actions {
		b := gtk.NewButton()
		// Capped like the text, so a long label cannot widen the card.
		l := gtk.NewLabel(a.Label)
		l.SetEllipsize(pango.EllipsizeEndValue)
		l.SetMaxWidthChars(20)
		b.SetChild(&l.Widget)
		l.Unref()
		b.AddCssClass("outlined")
		b.SetName(fmt.Sprintf("act:%s:%d", id, i))
		b.ConnectClicked(&s.onNoteButton)
		gtkx.Append(&actions, &b.Widget)
	}
	actions.SetVisible(len(n.Actions) > 0)

	click := gtk.NewGestureClick()
	click.ConnectReleased(&s.onNoteClicked)
	card.AddController(&click.EventController)
	return &card
}

// noteApp is the name and icon to show for a notification's app: from its
// desktop entry when it names one, else what it sent.
func (s *shellUI) noteApp(n notify.Notification) (name, icon string) {
	name, icon = n.App, n.Icon
	e := s.notes.apps[n.Entry]
	if e == nil && n.Entry == "" {
		e = s.notes.apps[strings.ToLower(n.App)]
	}
	if e != nil {
		name = e.Name
		if icon == "" {
			icon = e.Icon
		}
	}
	if icon == "" {
		icon = strings.ToLower(n.App)
	}
	if name == "" {
		name = i18n.T("Notification")
	}
	if !s.hasIcon(icon) {
		icon = "preferences-system-notifications-symbolic"
	}
	return name, icon
}

func (s *shellUI) hasIcon(name string) bool {
	if strings.HasPrefix(name, "/") {
		return true
	}
	theme := gtk.IconThemeGetForDisplay(gdk.DisplayGetDefault())
	defer theme.Unref()
	return name != "" && (theme.HasIcon(name) || theme.HasIcon(strings.ToLower(name)))
}

// imageTexture turns image-data pixels into a texture.
func imageTexture(img *notify.Image) *gdk.MemoryTexture {
	format := gdk.MemoryR8g8b8Value
	if img.Alpha {
		format = gdk.MemoryR8g8b8a8Value
	}
	px := img.Pixels() // what the server checked, checked again
	if px == nil {
		return nil
	}
	bytes := glib.NewBytes(px, uint(len(px)))
	defer bytes.Unref()
	return gdk.NewMemoryTexture(int32(img.Width), int32(img.Height), format, bytes, uint(img.Stride))
}

func (s *shellUI) bindNotifyHandlers() {
	s.onNoteButton = func(b gtk.Button) {
		kind, rest, _ := strings.Cut(b.GetName(), ":")
		switch kind {
		case "close":
			if id, err := strconv.ParseUint(rest, 10, 32); err == nil {
				s.dismissNote(uint32(id))
			}
		case "hide":
			if id, err := strconv.ParseUint(rest, 10, 32); err == nil {
				s.dropBanner(uint32(id))
			}
		case "act":
			idS, iS, _ := strings.Cut(rest, ":")
			id, err1 := strconv.ParseUint(idS, 10, 32)
			i, err2 := strconv.Atoi(iS)
			if n, ok := s.noteByID(uint32(id)); err1 == nil && err2 == nil && ok && i < len(n.Actions) {
				s.invokeNote(n, n.Actions[i].Key)
			}
		case "more":
			s.notes.expanded[rest] = !s.notes.expanded[rest]
			s.fillCenter()
		case "clear":
			s.clearAnimated([]string{rest})
		case "focus":
			s.setFocus(!s.notes.c.Focus)
		case "clear_all":
			var keys []string
			for _, g := range s.notes.c.Groups() {
				keys = append(keys, g.Key)
			}
			s.clearAnimated(keys)
		}
	}
	s.onNoteClicked = func(g gtk.GestureClick, _ int32, x, y float64) {
		w := g.GetWidget()
		if w == nil {
			return
		}
		// Clicks on the card's buttons are theirs.
		if hit := w.Pick(x, y, gtk.PickDefaultValue); hit != nil {
			inButton := false
			for p := hit; p != nil && p.GoPointer() != w.GoPointer(); p = p.GetParent() {
				if _, ok := strings.CutPrefix(p.GetName(), "act:"); ok || strings.HasPrefix(p.GetName(), "close:") || strings.HasPrefix(p.GetName(), "hide:") {
					inButton = true
					break
				}
			}
			hit.Unref()
			if inButton {
				return
			}
		}
		if id, err := strconv.ParseUint(strings.TrimPrefix(w.GetName(), "note:"), 10, 32); err == nil {
			s.activateNote(uint32(id))
		}
	}
	s.onNotesSave = func(uintptr) bool {
		s.notes.dirty = false
		if err := s.notes.c.Save(notify.StatePath()); err != nil {
			log.Printf("notifications: %v", err)
		}
		return false
	}
	s.onBannerTick = func(uintptr) bool {
		nt := &s.notes
		for _, b := range append([]*banner(nil), nt.banners...) {
			if b.left > 0 && !b.hover {
				b.left--
				if b.left == 0 {
					s.dropBanner(b.id)
				}
			}
		}
		if len(nt.banners) == 0 {
			nt.ticking = false
			return false
		}
		return true
	}
	s.onBannerGone = func(data uintptr) bool {
		nt := &s.notes
		for i, b := range nt.banners {
			if uintptr(b.id) == data && b.leaving {
				nt.bstack.Remove(&b.reveal.Widget)
				nt.banners = append(nt.banners[:i], nt.banners[i+1:]...)
				break
			}
		}
		if len(nt.banners) == 0 && nt.bwin != nil {
			nt.bwin.SetVisible(false)
		}
		return false
	}
	s.onBannerEnter = func(c gtk.EventControllerMotion, _, _ float64) { s.hoverBanner(c, true) }
	s.onBannerLeave = func(c gtk.EventControllerMotion) { s.hoverBanner(c, false) }
	s.onCenterPressed = func(g gtk.GestureClick, _ int32, x, y float64) {
		nt := &s.notes
		hit := nt.backdrop.Pick(x, y, gtk.PickDefaultValue)
		if hit == nil {
			return
		}
		outside := hit.GoPointer() == nt.backdrop.GoPointer()
		hit.Unref()
		if outside {
			s.closeCenter()
		}
	}
	s.onCenterKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if keyval == keyEscape {
			s.closeCenter()
			return true
		}
		return false
	}
	s.onCenterGone = func(uintptr) bool {
		nt := &s.notes
		nt.win.Destroy()
		nt.refs.Release()
		nt.open, nt.closing = false, false
		return false
	}
	s.onNotesButton = func(gtk.Button) { s.toggleCenter() }
}

// --- banners ---

func (s *shellUI) bannerIndex(id uint32) int {
	for i, b := range s.notes.banners {
		if b.id == id && !b.leaving {
			return i
		}
	}
	return -1
}

func (s *shellUI) ensureBannerWindow() {
	nt := &s.notes
	if nt.bwin != nil {
		return
	}
	nt.bwin = &gtk.ApplicationWindow{}
	ui := s.load("banners.ui", &nt.brefs)
	ui.Get("banners", nt.bwin)
	ui.Get("stack", &nt.bstack)
	ui.Close()
	nt.bwin.SetApplication(s.app)
	w := nt.bwin.GoPointer()
	if s.layer {
		layershell.InitForWindow(w)
		layershell.SetNamespace(w, "inua-banners")
		layershell.SetLayer(w, layershell.LayerOverlay)
		layershell.SetKeyboardMode(w, layershell.KeyboardNone)
		layershell.SetAnchor(w, layershell.EdgeTop, true)
		layershell.SetAnchor(w, layershell.EdgeRight, true)
		layershell.SetMargin(w, layershell.EdgeTop, 6) // below the bar's exclusive zone
		layershell.SetMargin(w, layershell.EdgeRight, 6)
	} else {
		nt.bwin.SetTitle(i18n.T("inua notifications"))
	}
}

func (s *shellUI) showBanner(n notify.Notification) {
	nt := &s.notes
	s.ensureBannerWindow()
	if s.layer {
		if mon := s.focusedMonitor(); mon != nil {
			layershell.SetMonitor(nt.bwin.GoPointer(), mon.GoPointer())
		}
	}
	// Room for the new one: the oldest goes (it is in the center).
	live := 0
	for _, b := range nt.banners {
		if !b.leaving {
			live++
		}
	}
	for i := len(nt.banners) - 1; i >= 0 && live >= maxBanners; i-- {
		if !nt.banners[i].leaving {
			s.dropBanner(nt.banners[i].id)
			live--
		}
	}

	rev := gtk.NewRevealer()
	rev.SetTransitionType(gtk.RevealerTransitionTypeSlideDownValue)
	rev.SetTransitionDuration(bannerMS)
	card := s.buildNote(n, true)
	card.AddCssClass("banner")
	rev.SetChild(&card.Widget)
	card.Unref()
	motion := gtk.NewEventControllerMotion()
	motion.ConnectEnter(&s.onBannerEnter)
	motion.ConnectLeave(&s.onBannerLeave)
	rev.AddController(&motion.EventController)
	rev.SetName(strconv.FormatUint(uint64(n.ID), 10))
	nt.bstack.Prepend(&rev.Widget)
	rev.Unref() // the stack holds it

	left := s.cfg.Notify.Banner
	if n.Urgency == notify.Critical {
		left = -1
	}
	nt.banners = append([]*banner{{id: n.ID, note: n, reveal: rev, left: left}}, nt.banners...)
	nt.bwin.SetVisible(true)
	nt.bwin.Present()
	rev.SetRevealChild(true)
	if !nt.ticking {
		nt.ticking = true
		glib.TimeoutAdd(1000, &s.onBannerTick, 0)
	}
}

// replaceBanner shows an updated notification in its banner, restarting
// its time.
func (s *shellUI) replaceBanner(n notify.Notification) {
	nt := &s.notes
	i := s.bannerIndex(n.ID)
	b := nt.banners[i]
	card := s.buildNote(n, true)
	card.AddCssClass("banner")
	b.reveal.SetChild(&card.Widget)
	card.Unref()
	b.note = n
	if b.left >= 0 {
		b.left = s.cfg.Notify.Banner
	}
}

// dropBanner slides a banner away; the notification stays in the center.
func (s *shellUI) dropBanner(id uint32) {
	i := s.bannerIndex(id)
	if i < 0 {
		return
	}
	b := s.notes.banners[i]
	b.leaving = true
	b.reveal.SetRevealChild(false)
	glib.TimeoutAdd(bannerMS, &s.onBannerGone, uintptr(id))
}

func (s *shellUI) clearBanners() {
	for _, b := range append([]*banner(nil), s.notes.banners...) {
		s.dropBanner(b.id)
	}
}

func (s *shellUI) hoverBanner(c gtk.EventControllerMotion, on bool) {
	w := c.GetWidget()
	if w == nil {
		return
	}
	for _, b := range s.notes.banners {
		if strconv.FormatUint(uint64(b.id), 10) == w.GetName() {
			b.hover = on
			if !on && b.left == 0 {
				b.left = 1
			}
		}
	}
}

// --- the center ---

func (s *shellUI) toggleCenter() {
	if s.notes.open && !s.notes.closing {
		s.closeCenter()
	} else {
		s.openCenter()
	}
}

func (s *shellUI) openCenter() {
	s.closeClipboard()
	nt := &s.notes
	if nt.open {
		return
	}
	s.closeControl()
	s.closeAgenda()
	s.buildCenter()
	nt.open = true
	s.fillCenter()
	nt.win.Present()
	nt.reveal.SetRevealChild(true)
	s.refocusPointer()
	s.clearBanners() // they are in the list now
	nt.c.Seen = time.Now()
	s.saveNotesSoon()
	s.showUnread()
	for _, b := range s.bars {
		b.notes.AddCssClass("open")
	}
}

func (s *shellUI) closeCenter() {
	nt := &s.notes
	if !nt.open || nt.closing {
		return
	}
	nt.closing = true
	nt.reveal.SetRevealChild(false)
	for _, b := range s.bars {
		b.notes.RemoveCssClass("open")
	}
	glib.TimeoutAdd(revealMS, &s.onCenterGone, 0)
}

func (s *shellUI) buildCenter() {
	nt := &s.notes
	ui := s.load("center.ui", &nt.refs)
	ui.Get("center", &nt.win)
	ui.Get("backdrop", &nt.backdrop)
	ui.Get("reveal", &nt.reveal)
	ui.Get("list", &nt.list)
	ui.Get("empty", &nt.empty)
	ui.Get("scroll", &nt.scroll)
	ui.Get("count", &nt.count)
	ui.Get("focus_note", &nt.focusNote)
	ui.Get("focus", &nt.focus)
	ui.Get("clear_all", &nt.clearAll)
	ui.Close()
	nt.focus.ConnectClicked(&s.onNoteButton)
	nt.clearAll.ConnectClicked(&s.onNoteButton)

	click := gtk.NewGestureClick()
	click.ConnectPressed(&s.onCenterPressed)
	nt.backdrop.AddController(&click.EventController)
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue)
	keys.ConnectKeyPressed(&s.onCenterKey)
	nt.win.AddController(&keys.EventController)

	nt.win.SetApplication(s.app)
	w := nt.win.GoPointer()
	if s.layer {
		layershell.InitForWindow(w)
		layershell.SetNamespace(w, "inua-center")
		layershell.SetLayer(w, layershell.LayerOverlay)
		layershell.SetKeyboardMode(w, layershell.KeyboardExclusive)
		for _, e := range []layershell.Edge{layershell.EdgeTop, layershell.EdgeBottom, layershell.EdgeLeft, layershell.EdgeRight} {
			layershell.SetAnchor(w, e, true)
		}
		layershell.SetExclusiveZone(w, -1)
		if mon := s.focusedMonitor(); mon != nil {
			layershell.SetMonitor(w, mon.GoPointer())
		}
	} else {
		nt.win.SetTitle(i18n.T("inua notifications"))
		nt.win.SetDefaultSize(400, 640)
	}
}

// fillCenter lists the notifications, by app.
func (s *shellUI) fillCenter() {
	nt := &s.notes
	if !nt.open {
		return
	}
	groups := nt.c.Groups()
	total := len(nt.c.Items)
	switch total {
	case 0:
		nt.count.SetText("")
	case 1:
		nt.count.SetText("1")
	default:
		nt.count.SetText(strconv.Itoa(total))
	}
	setOn(&nt.focus, nt.c.Focus)
	nt.focus.SetTooltipText(map[bool]string{true: i18n.T("Focus is on"), false: i18n.T("Focus")}[nt.c.Focus])
	nt.focusNote.SetVisible(nt.c.Focus)
	nt.clearAll.SetVisible(total > 0)
	nt.empty.SetVisible(total == 0)
	nt.scroll.SetVisible(total > 0)

	gtkx.Clear(&nt.list)
	nt.groupBox, nt.groupRev = map[string]*gtk.Box{}, map[string]*gtk.Revealer{}
	groupUI := s.readUI("group.ui")
	for _, g := range groups {
		var refs gtkx.Scope
		ui, err := gtkx.Load(groupUI, &refs)
		if err != nil {
			log.Fatalf("group.ui: %v", err)
		}
		var box, head, items, edge gtk.Box
		var icon gtk.Image
		var name gtk.Label
		var more, clear gtk.Button
		ui.Take("group", &box)
		ui.Get("head", &head)
		ui.Get("icon", &icon)
		ui.Get("name", &name)
		ui.Get("more", &more)
		ui.Get("clear", &clear)
		ui.Get("items", &items)
		ui.Get("stack_edge", &edge)
		ui.Close()

		// Like iOS, a single notification is just its card; a stack gets a
		// head to open it and to clear it.
		app, iconName := s.noteApp(g.Items[0])
		name.SetText(app)
		setIcon(&icon, iconName)
		icon.SetPixelSize(16)
		clear.SetName("clear:" + g.Key)
		clear.ConnectClicked(&s.onNoteButton)
		head.SetVisible(len(g.Items) > 1)
		open := nt.expanded[g.Key]
		if len(g.Items) > 1 {
			more.SetName("more:" + g.Key)
			more.SetVisible(true)
			if open {
				more.SetLabel(i18n.T("Show less"))
			} else {
				more.SetLabel(i18n.Tf("%d more", len(g.Items)-1))
			}
			more.ConnectClicked(&s.onNoteButton)
		}
		shown := g.Items
		if !open {
			shown = g.Items[:1]
			edge.SetVisible(len(g.Items) > 1)
		}
		for _, n := range shown {
			card := s.buildNote(n, false)
			gtkx.Append(&items, &card.Widget)
		}
		// In a revealer, so a cleared group's room closes smoothly.
		rev := gtk.NewRevealer()
		rev.SetTransitionType(gtk.RevealerTransitionTypeSlideUpValue)
		rev.SetTransitionDuration(clearCollapseMS)
		rev.SetRevealChild(true)
		rev.SetChild(&box.Widget)
		box.Unref()
		nt.groupBox[g.Key], nt.groupRev[g.Key] = &box, rev
		gtkx.Append(&nt.list, &rev.Widget)
		refs.Release()
	}
}

// How clearing looks: each group slides out and fades (CSS, .leaving),
// one after another from the top, then its room closes.
const (
	clearSlideMS    = 180
	clearStaggerMS  = 45
	clearCollapseMS = 200
)

// clearAnimated clears the groups of apps (by key) from the center: they
// slide away first. The notifications cleared are the ones shown now; one
// arriving meanwhile stays.
func (s *shellUI) clearAnimated(keys []string) {
	nt := &s.notes
	if nt.clearing {
		return
	}
	var ids []uint32
	for _, g := range nt.c.Groups() {
		if slices.Contains(keys, g.Key) {
			for _, n := range g.Items {
				ids = append(ids, n.ID)
			}
		}
	}
	if len(ids) == 0 {
		return
	}
	nt.clearing = true
	last := uint32(0)
	for i, k := range keys {
		box, rev := nt.groupBox[k], nt.groupRev[k]
		if box == nil {
			continue
		}
		delay := uint32(clearStaggerMS * min(i, 6))
		last = delay
		current := func() bool { return nt.open && nt.groupRev[k] == rev } // not rebuilt since
		s.runAfter(delay, func() {
			if current() {
				box.AddCssClass("leaving")
			}
		})
		s.runAfter(delay+clearSlideMS, func() {
			if current() {
				rev.SetRevealChild(false)
			}
		})
	}
	s.runAfter(last+clearSlideMS+clearCollapseMS, func() {
		nt.clearing = false
		for _, id := range ids {
			nt.c.Remove(id)
		}
		s.clearNotes(ids)
	})
}

// --- Focus in the control center ---

// applyFocusTile shows Focus on the control center's tile: inua's own
// while it shows notifications, else mako's.
func (s *shellUI) applyFocusTile() {
	c := &s.control
	if !c.open {
		return
	}
	native, mako := s.notesNative(), system.DNDAvailable()
	on := s.sys.DND
	if native {
		on = s.notes.c.Focus
	}
	c.dnd.SetSensitive(native || mako)
	setOn(&c.dnd, on)
	c.dndSub.SetText(shell.FocusTile(native, mako, on))
}
