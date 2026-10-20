package mailui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/mail"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// viewKind is what the middle column lists.
type viewKind int

const (
	viewInboxes viewKind = iota // every account's inbox
	viewReply                   // conversations tagged REPLY
	viewStarred                 // conversations with a starred message
	viewBox                     // one mailbox of one account
)

type view struct {
	kind    viewKind
	account string
	mailbox string
}

// filter narrows the list.
type filter int

const (
	filterAll filter = iota
	filterUnread
	filterFiles
	filterReply
)

// acct is an account's session and how it is doing.
type acct struct {
	s       *mail.Session
	boxes   []mail.Mailbox
	state   string    // what is wrong, in words; "" when fine
	signIn  bool      // the state is that it must sign in again
	busy    bool      // connecting or syncing the first time
	syncing int       // syncs running
	last    time.Time // the last sync that ended well
	more    bool      // its other folders are shown
	synced  map[string]bool
	stop    context.CancelFunc // its IDLE
}

// window is Mail's window.
type window struct {
	c   *appui.Ctx
	h   *hub
	win *gtk.ApplicationWindow

	top                                     gtk.Stack
	side, filters, reader, welcomeBox       gtk.Box
	statusMark, undoBar                     gtk.Box
	undoText                                gtk.Label
	undoBtn                                 gtk.Button
	statusState                             gtk.Label
	compose, settingsButton                 gtk.Button
	search                                  gtk.SearchEntry
	searchNote, listEmpty, listError, older gtk.Label
	readerEmpty                             gtk.Label
	listStack, readerStack                  gtk.Stack
	listScroll, readerScroll                gtk.ScrolledWindow

	model *gtk.StringList
	sel   *gtk.SingleSelection
	list  *gtk.ListView
	rows  map[uintptr]*convRow // a list item → its row

	settings mail.Settings
	store    *mail.Store
	secrets  *mail.Secrets
	drafts   *mail.Drafts
	accts    []*acct

	view         view
	filter       filter
	query        string
	parsed       mail.Query
	found        map[mail.Key]bool // what the server's search found
	asked        string            // the sentence the assistant turned into the query
	convs        []*mail.Conversation
	ids          []string // the list's model, by conversation
	open         *mail.Conversation
	openID       string
	reading      map[mail.Key]*reading
	sumState     map[string]*aiText // conversation → its summary
	ideas        map[string]*aiText // conversation → reply ideas, a line each
	judged       map[mail.Key]*aiText
	actErr       string // an action's error, said on the conversation
	folded       map[mail.Key]bool
	quotes       map[mail.Key]bool
	checks       map[mail.Key]bool       // the phishing checks unfolded
	answers      map[mail.Key]*aiText    // an invitation's answer: busy, sent (text), or why not
	fileSums     map[string]*aiText      // an attachment's summary, by fileKey
	quick        map[string]*quickReply  // a conversation's quick reply, by its ID
	sideSig      string                  // the index's counts when last drawn
	avatars      map[string]*gdk.Texture // accounts' photos, by ID
	avatarAsked  map[string]bool
	loadingOlder bool                      // older mail is being fetched
	settingsWin  *settingsWin              // the settings, while open
	flipped      map[mail.Key]bool         // messages shown not as the settings say: text for formatted, or formatted for text
	picsOK       map[mail.Key]bool         // messages whose pictures from the web the user loaded
	blocks       map[mail.Key][]mail.Block // the formatted messages' blocks
	pics         map[string]*pic           // pictures, by where they come from and the width drawn
	picsDue      bool                      // a redraw for pictures is coming

	sideBtns   []uintptr // the index's buttons, forgotten when it is made again
	readerBtns []uintptr // the conversation's buttons, likewise
	filterBtns map[filter]*gtk.Button
	question   *dialog
	welcome    *welcome
	pending    *pending // an action the undo bar can still take back
	composers  []*composer
	ai         aiState
	sorting    bool
	ctx        context.Context
	cancel     context.CancelFunc
	closed     bool
	lastDrawn  string // the reader's signature when last drawn
}

// reading is a message of the conversation open: read, or being read.
type reading struct {
	m       *mail.Message
	err     string
	loading bool
	judged  mail.Judgment
}

func openWindow(c *appui.Ctx) *window {
	w := &window{
		c: c, h: newHub(), win: &gtk.ApplicationWindow{}, rows: map[uintptr]*convRow{},
		store: mail.NewStore(), secrets: mail.NewSecrets(), drafts: mail.NewDrafts(),
		reading: map[mail.Key]*reading{}, sumState: map[string]*aiText{}, ideas: map[string]*aiText{},
		judged: map[mail.Key]*aiText{}, folded: map[mail.Key]bool{}, quotes: map[mail.Key]bool{}, checks: map[mail.Key]bool{},
		answers: map[mail.Key]*aiText{}, fileSums: map[string]*aiText{}, quick: map[string]*quickReply{}, avatars: map[string]*gdk.Texture{}, avatarAsked: map[string]bool{},
		flipped: map[mail.Key]bool{}, picsOK: map[mail.Key]bool{}, blocks: map[mail.Key][]mail.Block{}, pics: map[string]*pic{},
		filterBtns: map[filter]*gtk.Button{},
	}
	w.ctx, w.cancel = context.WithCancel(context.Background())
	b := c.Load("mail.ui")
	b.Get("mail", w.win)
	for name, ptr := range map[string]gobject.Ptr{
		"side": &w.side, "filters": &w.filters, "reader": &w.reader,
		"compose": &w.compose, "settings_button": &w.settingsButton, "older": &w.older,
		"top": &w.top, "welcome": &w.welcomeBox, "status_mark": &w.statusMark,
		"status_state": &w.statusState,
		"undo_bar":     &w.undoBar, "undo_text": &w.undoText, "undo": &w.undoBtn,
		"search": &w.search, "search_note": &w.searchNote, "list_empty": &w.listEmpty, "list_error": &w.listError,
		"reader_empty": &w.readerEmpty, "list_stack": &w.listStack, "reader_stack": &w.readerStack,
		"list_scroll": &w.listScroll, "reader_scroll": &w.readerScroll,
	} {
		b.Get(name, ptr)
	}
	b.Close()

	w.model = gtk.NewStringList(nil)
	w.sel = gtk.NewSingleSelection(w.model)
	w.sel.SetAutoselect(false)
	w.sel.SetCanUnselect(true)
	w.sel.Ref()
	w.h.selection(w.sel, w.selectionChanged)
	f := w.h.factory(w.setupRow, w.bindRow, w.teardownRow)
	w.list = gtk.NewListView(w.sel, &f.ListItemFactory)
	w.list.AddCssClass("mail-conversations")
	w.listScroll.SetChild(&w.list.Widget)

	w.h.click(&w.compose, func() { w.newComposer(mail.Draft{}, nil) })
	w.h.click(&w.settingsButton, func() { w.showSettings("") })
	w.h.notify(w.listScroll.GetVadjustment(), "value", w.nearEnd)
	w.h.click(&w.undoBtn, w.undo)
	w.h.searched(&w.search, w.searchChanged)
	w.h.enter(&w.search, w.searchEntered)
	w.h.key(&w.win.Widget, false, w.key)
	w.h.close(&w.win.Window, func() bool { w.quit(); return false })

	for _, f := range []struct {
		f    filter
		name string
	}{{filterAll, i18n.T("ALL")}, {filterUnread, i18n.T("UNREAD")}, {filterFiles, i18n.T("FILES")}, {filterReply, i18n.T("REPLY")}} {
		btn := gtk.NewButtonWithLabel(f.name)
		btn.AddCssClass("mail-filter")
		btn.SetTooltipText(map[filter]string{
			filterAll: i18n.T("Every conversation"), filterUnread: i18n.T("With messages not read"),
			filterFiles: i18n.T("With files attached"), filterReply: i18n.T("Waiting for your reply"),
		}[f.f])
		ff := f.f
		w.h.click(btn, func() { w.setFilter(ff) })
		w.filterBtns[f.f] = btn
		gtkx.Append(&w.filters, &btn.Widget)
	}
	w.syncFilters()

	w.loadSettings()
	w.h.later(3*60*1000, w.tick)
	return w
}

// loadSettings reads mail.toml and starts a session for each account.
func (w *window) loadSettings() {
	s, err := mail.LoadSettings(mail.SettingsPath())
	s.Keyring = w.secrets
	w.settings = s
	if err != nil {
		w.say(&w.listError, err.Error())
	}
	for _, a := range s.Accounts {
		w.addSession(a)
	}
	w.drawSide()
	w.refresh()
	w.h.later(1000, w.statusTick)
}

// addSession starts an account: its cache shown at once, then the server.
func (w *window) addSession(a mail.Account) *acct {
	for _, x := range w.accts {
		if x.s.Acc.ID == a.ID {
			x.s.Close()
			if x.stop != nil {
				x.stop()
			}
			w.accts = slices.DeleteFunc(w.accts, func(y *acct) bool { return y == x })
		}
	}
	x := &acct{s: mail.NewSession(a, w.settings, w.store, w.secrets), busy: true, synced: map[string]bool{}}
	x.boxes = x.s.Cached()
	w.accts = append(w.accts, x)
	w.connect(x)
	return x
}

func (w *window) acct(id string) *acct {
	for _, x := range w.accts {
		if x.s.Acc.ID == id {
			return x
		}
	}
	return nil
}

// connect lists an account's mailboxes, syncs its inbox and Sent, then
// waits for new mail in IDLE.
func (w *window) connect(x *acct) {
	x.syncing++
	go func() {
		boxes, err := x.s.Mailboxes(w.ctx)
		if err != nil {
			gtkx.Post(func() { x.syncing--; w.failed(x, err) })
			return
		}
		gtkx.Post(func() {
			x.boxes, x.state, x.signIn = boxes, "", false
			w.drawSide()
		})
		inbox := x.s.Role(mail.RoleInbox)
		_, err = x.s.Sync(w.ctx, inbox)
		gtkx.Post(func() {
			x.busy = false
			x.syncing--
			if err != nil {
				w.failed(x, err)
				return
			}
			x.last = time.Now()
			x.synced[inbox] = true
			w.counted(x, inbox)
			w.refresh()
			w.sortNew(x, nil) // all not sorted yet: the cache may hold some from before
		})
		if sent := x.s.Role(mail.RoleSent); sent != "" {
			if _, err := x.s.Sync(w.ctx, sent); err == nil {
				gtkx.Post(func() { x.synced[sent] = true; w.refresh() })
			}
		}
		if v := w.viewNow(); v.kind == viewBox && v.account == x.s.Acc.ID && v.mailbox != inbox {
			w.syncBox(x, v.mailbox)
		}
		ctx, cancel := context.WithCancel(w.ctx)
		gtkx.Post(func() { x.stop = cancel })
		x.s.Watch(ctx, func() { w.newMail(x) })
	}()
}

// viewNow reads the view from a goroutine.
func (w *window) viewNow() view {
	v, _ := gtkx.Call(time.Second, func() view { return w.view })
	return v
}

// newMail is IDLE saying the inbox changed: sync it, sort what came, and
// say so when the window is not in front.
func (w *window) newMail(x *acct) {
	inbox := x.s.Role(mail.RoleInbox)
	gtkx.Post(func() { x.syncing++; w.drawStatus() })
	res, err := x.s.Sync(w.ctx, inbox)
	gtkx.Post(func() { x.syncing--; w.drawStatus() })
	if err != nil {
		return
	}
	gtkx.Post(func() {
		x.last = time.Now()
		w.counted(x, inbox)
		w.refresh()
		w.sortNew(x, res.New)
		if len(res.New) > 0 && !w.win.IsActive() {
			w.notifyNew(x, inbox, res.New)
		}
	})
}

// syncBox syncs a mailbox on its account's goroutine; the list shows the
// cache meanwhile.
func (w *window) syncBox(x *acct, mailbox string) {
	x.syncing++
	w.drawStatus()
	go func() {
		_, err := x.s.Sync(w.ctx, mailbox)
		gtkx.Post(func() {
			x.syncing--
			if err == nil {
				x.last = time.Now()
			}
			w.drawStatus()
			if err != nil {
				w.failed(x, err)
				return
			}
			x.synced[mailbox] = true
			w.counted(x, mailbox)
			w.refresh()
		})
	}()
}

// tick syncs what is in view every few minutes (IDLE covers the inbox).
func (w *window) tick() {
	if w.closed {
		return
	}
	if w.view.kind == viewBox {
		if x := w.acct(w.view.account); x != nil && w.view.mailbox != x.s.Role(mail.RoleInbox) {
			w.syncBox(x, w.view.mailbox)
		}
	}
	w.h.later(3*60*1000, w.tick)
}

// failed says on the account what went wrong.
func (w *window) failed(x *acct, err error) {
	x.busy = false
	switch {
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, mail.ErrSignInAgain), errors.Is(err, mail.ErrAuth):
		x.state, x.signIn = i18n.T("Signed out"), true
	case errors.Is(err, mail.ErrNoClient):
		x.state, x.signIn = err.Error(), false
	default:
		x.state = i18n.Tf("Offline: %v", shortErr(err))
	}
	w.drawSide()
	w.refresh()
}

func shortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i > 0 && len(s) > 80 {
		s = s[i+2:]
	}
	return s
}

// counted updates a mailbox's unread count from its index.
func (w *window) counted(x *acct, mailbox string) {
	n := 0
	for _, it := range x.s.Items(mailbox) {
		if !it.Has(mail.Seen) {
			n++
		}
	}
	for i := range x.boxes {
		if x.boxes[i].Name == mailbox && x.boxes[i].Unread != n {
			x.boxes[i].Unread = n
			w.drawSide()
		}
	}
}

func (w *window) quit() {
	w.flushPending()
	w.closed = true
	w.cancel()
	w.ai.close()
	for _, x := range w.accts {
		x.s.Close()
	}
	for _, c := range slices.Clone(w.composers) {
		c.save()
	}
	cleanOpened()
}

// --- the index ---

// drawSide makes the index again: the views across accounts, then each
// account's mailboxes.
func (w *window) drawSide() {
	w.h.forget(w.sideBtns)
	w.sideBtns = nil
	gtkx.Clear(&w.side)
	w.drawStatus()
	if len(w.accts) == 0 {
		w.listStack.SetVisibleChildName("empty")
		w.listEmpty.SetText(i18n.T("No account yet"))
		if w.welcome == nil {
			w.showWelcome("", nil)
		}
		return
	}
	unread := 0
	for _, x := range w.accts {
		for _, b := range x.boxes {
			if b.Role == mail.RoleInbox {
				unread += b.Unread
			}
		}
	}
	w.sideItem(i18n.T("All inboxes"), unread, view{kind: viewInboxes})
	w.sideItem(i18n.T("Needs reply"), len(w.conversations(view{kind: viewReply})), view{kind: viewReply})
	w.sideItem(i18n.T("Starred"), 0, view{kind: viewStarred})
	if ds, _ := w.drafts.List(); len(ds) > 0 {
		btn := w.sideButton(i18n.T("Unsent"), len(ds), false)
		w.sideBtns = append(w.sideBtns, w.h.click(btn, w.showUnsent))
	}
	for _, x := range w.accts {
		xx := x
		// A panel of its own: who, then the account's mailboxes.
		panel := gtk.NewBox(gtk.OrientationVerticalValue, 1)
		panel.AddCssClass("mail-acct")
		head := gtk.NewButton()
		head.AddCssClass("mail-acct-head")
		head.SetTooltipText(i18n.T("This account's settings"))
		hb := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		gtkx.Append(hb, w.avatarWidget(x))
		who := gtk.NewBox(gtk.OrientationVerticalValue, 1)
		who.SetValign(gtk.AlignCenterValue)
		who.SetHexpand(true)
		name := gtk.NewLabel(x.s.Acc.DisplayName())
		name.AddCssClass("mail-acct-name")
		name.SetXalign(0)
		name.SetEllipsize(3)
		gtkx.Append(who, &name.Widget)
		addr := gtk.NewLabel(x.s.Acc.Address)
		addr.AddCssClass("mail-acct-addr")
		addr.SetXalign(0)
		addr.SetEllipsize(2)
		gtkx.Append(who, &addr.Widget)
		gtkx.Append(hb, &who.Widget)
		head.SetChild(&hb.Widget)
		w.sideBtns = append(w.sideBtns, w.h.click(head, func() { w.showSettings(xx.s.Acc.ID) }))
		gtkx.Append(panel, &head.Widget)
		if x.state != "" {
			st := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
			st.AddCssClass("mail-account-state")
			l := gtk.NewLabel(x.state)
			l.SetXalign(0)
			l.SetWrap(true)
			l.SetHexpand(true)
			gtkx.Append(st, &l.Widget)
			if x.signIn {
				b := gtk.NewButtonWithLabel(i18n.T("Sign in"))
				b.AddCssClass("mail-plain")
				w.sideBtns = append(w.sideBtns, w.h.click(b, func() { acc := xx.s.Acc; w.showWelcome(acc.Provider, &acc) }))
				gtkx.Append(st, &b.Widget)
			}
			gtkx.Append(panel, &st.Widget)
		} else if x.busy && len(x.boxes) == 0 {
			l := gtk.NewLabel(i18n.T("Connecting…"))
			l.AddCssClass("mail-account-state")
			l.SetXalign(0)
			gtkx.Append(panel, &l.Widget)
		}
		boxes := x.boxes
		if len(boxes) == 0 {
			boxes = []mail.Mailbox{{Name: "INBOX", Role: mail.RoleInbox}}
		}
		others := 0
		for _, b := range boxes {
			if b.Role == mail.RoleOther {
				others++
				if !x.more {
					continue
				}
			}
			n := 0
			if b.Role != mail.RoleSent && b.Role != mail.RoleTrash && b.Role != mail.RoleJunk && b.Role != mail.RoleArchive {
				n = b.Unread
			}
			w.sideItemIn(panel, boxName(b), n, view{kind: viewBox, account: x.s.Acc.ID, mailbox: b.Name})
		}
		if others > 0 {
			label := i18n.Nf("%d more folder", "%d more folders", others, others)
			if x.more {
				label = i18n.T("Fewer folders")
			}
			more := w.sideButtonIn(panel, label, 0, false)
			more.AddCssClass("mail-more")
			w.sideBtns = append(w.sideBtns, w.h.click(more, func() { xx.more = !xx.more; w.drawSide() }))
		}
		gtkx.Append(&w.side, &panel.Widget)
	}
}

// sideItem adds a view's button to the index.
func (w *window) sideItem(title string, n int, v view) { w.sideItemIn(&w.side, title, n, v) }

func (w *window) sideItemIn(parent *gtk.Box, title string, n int, v view) {
	btn := w.sideButtonIn(parent, title, n, v == w.view)
	w.sideBtns = append(w.sideBtns, w.h.click(btn, func() { w.setView(v) }))
}

func (w *window) sideButton(title string, n int, current bool) *gtk.Button {
	return w.sideButtonIn(&w.side, title, n, current)
}

func (w *window) sideButtonIn(parent *gtk.Box, title string, n int, current bool) *gtk.Button {
	btn := gtk.NewButton()
	btn.AddCssClass("index-item")
	btn.AddCssClass("mail-box")
	if current {
		btn.AddCssClass("selected")
	}
	box := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	l := gtk.NewLabel(title)
	l.AddCssClass("title")
	l.SetXalign(0)
	l.SetHexpand(true)
	l.SetEllipsize(3)
	gtkx.Append(box, &l.Widget)
	if n > 0 {
		c := gtk.NewLabel(fmt.Sprint(n))
		c.AddCssClass("mail-count")
		gtkx.Append(box, &c.Widget)
	}
	btn.SetChild(&box.Widget)
	gtkx.Append(parent, &btn.Widget)
	return btn
}

// boxName is a mailbox's name in the index: its role's, else its own,
// the last part of a path.
func boxName(b mail.Mailbox) string {
	switch b.Role {
	case mail.RoleInbox:
		return i18n.T("Inbox")
	case mail.RoleDrafts:
		return i18n.T("Drafts")
	case mail.RoleSent:
		return i18n.T("Sent")
	case mail.RoleArchive:
		return i18n.T("Archive")
	case mail.RoleJunk:
		return i18n.T("Spam")
	case mail.RoleTrash:
		return i18n.T("Trash")
	}
	name := b.Name
	for _, sep := range []string{"/", "."} {
		if i := strings.LastIndex(name, sep); i >= 0 && i < len(name)-1 {
			name = name[i+1:]
			break
		}
	}
	return name
}

// accountName is what the index calls an account: its domain's first
// name (STUDIO for ana@studio.dev), or its ID for a mail provider's
// address (ana-gmail).
func accountName(a mail.Account) string {
	_, domain, _ := strings.Cut(a.Address, "@")
	d, _, _ := strings.Cut(domain, ".")
	switch d {
	case "gmail", "googlemail", "outlook", "hotmail", "live", "icloud", "me", "fastmail", "yahoo", "proton", "protonmail", "qq", "163":
		return a.ID
	}
	return d
}

// setView shows a view: its conversations from the cache, then synced.
func (w *window) setView(v view) {
	if v == w.view {
		return
	}
	w.view = v
	w.found, w.asked = nil, ""
	if v.kind == viewReply {
		w.filter = filterAll
		w.syncFilters()
	}
	w.drawSide()
	w.refresh()
	if v.kind == viewBox {
		if x := w.acct(v.account); x != nil && !x.synced[v.mailbox] {
			w.syncBox(x, v.mailbox)
		}
	}
	w.nearEnd()
}

func (w *window) setFilter(f filter) {
	w.filter = f
	w.syncFilters()
	w.refresh()
}

func (w *window) syncFilters() {
	for f, b := range w.filterBtns {
		setClass(&b.Widget, "current", f == w.filter)
	}
}

// --- the list ---

// conversations are a view's conversations, before the filter.
func (w *window) conversations(v view) []*mail.Conversation {
	var items, joining []mail.Item
	for _, x := range w.accts {
		if v.kind == viewBox && v.account != x.s.Acc.ID {
			continue
		}
		sent := x.s.Role(mail.RoleSent)
		switch v.kind {
		case viewInboxes, viewReply:
			items = append(items, x.s.Items(x.s.Role(mail.RoleInbox))...)
		case viewStarred:
			for _, r := range []mail.Role{mail.RoleInbox, mail.RoleArchive, mail.RoleSent} {
				if name := x.s.Role(r); name != "" {
					for _, it := range x.s.Items(name) {
						if it.Has(mail.Flagged) {
							items = append(items, it)
						}
					}
				}
			}
		case viewBox:
			items = append(items, x.s.Items(v.mailbox)...)
			if v.mailbox == sent {
				sent = ""
			}
		}
		if sent != "" && v.kind != viewStarred {
			joining = append(joining, x.s.Items(sent)...)
		}
	}
	cs := mail.Group(items, joining)
	if v.kind == viewReply {
		cs = slices.DeleteFunc(cs, func(c *mail.Conversation) bool { return c.Tag() != mail.TagReply || w.lastMine(c) })
	}
	return cs
}

// lastMine says whether the user wrote a conversation's newest message.
func (w *window) lastMine(c *mail.Conversation) bool { return w.mine(c.Latest().From.Addr) }

// mine says whether an address is one of the user's accounts'.
func (w *window) mine(addr string) bool {
	for _, x := range w.accts {
		if x.s.Mine(addr) {
			return true
		}
	}
	return false
}

// refresh lists the view's conversations again, through the filter and
// the search, changing the model by one splice.
func (w *window) refresh() {
	if len(w.accts) == 0 {
		return
	}
	all := w.conversations(w.view)
	var cs []*mail.Conversation
	for _, c := range all {
		if w.keep(c) {
			cs = append(cs, c)
		}
	}
	w.convs = cs
	ids := make([]string, len(cs))
	for i, c := range cs {
		ids[i] = c.ID
	}
	// The common start and end stay; the middle is replaced.
	p := 0
	for p < len(ids) && p < len(w.ids) && ids[p] == w.ids[p] {
		p++
	}
	s := 0
	for s < len(ids)-p && s < len(w.ids)-p && ids[len(ids)-1-s] == w.ids[len(w.ids)-1-s] {
		s++
	}
	if p != len(ids) || p != len(w.ids) {
		w.ids = ids
		w.model.Splice(uint32(p), w.model.GetNItems()-uint32(p+s), ids[p:len(ids)-s])
	}
	// Rows whose conversation changed in place (read, starred, a reply).
	for _, r := range w.rows {
		w.fillRow(r)
	}
	// The open conversation stays chosen where it went.
	if w.openID != "" {
		if i := slices.Index(ids, w.openID); i >= 0 {
			if w.sel.GetSelected() != uint32(i) {
				w.sel.SetSelected(uint32(i))
			}
			w.open = cs[i]
		} else if w.open != nil && w.gone(w.open) {
			// Its messages left (sent from Drafts, moved or deleted elsewhere).
			w.open, w.openID = nil, ""
			w.readerStack.SetVisibleChildName("empty")
		}
	}
	switch {
	case len(cs) > 0:
		w.listStack.SetVisibleChildName("list")
	default:
		w.listStack.SetVisibleChildName("empty")
		w.listEmpty.SetText(w.emptyText())
	}
	if w.open != nil {
		w.drawReader()
	}
	w.nearEnd()
	w.sideCounts()
}

func (w *window) emptyText() string {
	busy := false
	for _, x := range w.accts {
		busy = busy || x.busy
	}
	switch {
	case w.query != "":
		return i18n.T("Nothing found")
	case busy:
		return i18n.T("Fetching your mail…")
	case w.view.kind == viewReply:
		return i18n.T("Nothing waits for your reply")
	case w.view.kind == viewStarred:
		return i18n.T("No starred conversation")
	case w.filter == filterUnread:
		return i18n.T("All read")
	case w.view.kind == viewInboxes || (w.view.kind == viewBox && w.isInbox(w.view)):
		return i18n.T("Inbox empty. Nothing waits for you.")
	}
	return i18n.T("No mail here")
}

// keep says whether a conversation passes the filter and the search.
func (w *window) keep(c *mail.Conversation) bool {
	switch w.filter {
	case filterUnread:
		if c.Unread() == 0 && c.ID != w.openID {
			return false
		}
	case filterFiles:
		if !c.Attach() {
			return false
		}
	case filterReply:
		if c.Tag() != mail.TagReply || w.lastMine(c) {
			return false
		}
	}
	if w.parsed.Empty() {
		return true
	}
	for _, m := range c.Messages {
		if w.found[m.Key()] || w.parsed.Match(m.Envelope) {
			return true
		}
	}
	return false
}

func (w *window) canLoadOlder() bool {
	if w.view.kind != viewBox {
		return false
	}
	x := w.acct(w.view.account)
	return x != nil && x.s.Older(w.view.mailbox)
}

// nearEnd loads older mail of the mailbox shown when the list is
// scrolled near its end, or does not fill the column.
func (w *window) nearEnd() {
	if w.loadingOlder || !w.canLoadOlder() || w.listStack.GetVisibleChildName() != "list" {
		return
	}
	a := w.listScroll.GetVadjustment()
	if a.GetValue()+a.GetPageSize() < a.GetUpper()-400 {
		return
	}
	w.loadOlder()
}

func (w *window) loadOlder() {
	x := w.acct(w.view.account)
	if x == nil {
		return
	}
	mailbox := w.view.mailbox
	w.loadingOlder = true
	w.older.SetVisible(true)
	go func() {
		_, err := x.s.LoadOlder(w.ctx, mailbox)
		gtkx.Post(func() {
			w.loadingOlder = false
			w.older.SetVisible(false)
			if err != nil {
				w.say(&w.listError, i18n.Tf("Could not load older mail: %v", err))
			}
			w.refresh()
		})
	}()
}

// --- rows ---

type convRow struct {
	box                          gtk.Box
	mark                         gtk.Box
	who, when, subject, count    gtk.Label
	snippet, tag, clip, accountL gtk.Label
	read, star                   gtk.Button // the hover actions that change with the row
	starred                      gtk.Image
	item                         uintptr
	ptrs                         []uintptr
}

func (w *window) setupRow(item uintptr) {
	li := gtk.ListItemNewFromInternalPtr(item)
	r := &convRow{item: item}
	r.box = *gtk.NewBox(gtk.OrientationVerticalValue, 2)
	r.box.AddCssClass("mail-row")
	top := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	r.mark = *gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	r.mark.AddCssClass("mark")
	r.mark.SetValign(gtk.AlignCenterValue)
	gtkx.Append(top, &r.mark.Widget)
	r.who = *label("mail-who", true)
	gtkx.Append(top, &r.who.Widget)
	r.when = *label("mail-when", false)
	gtkx.Append(top, &r.when.Widget)
	// The actions a pointer over the row shows, in the time's place.
	over := gtk.NewOverlay()
	over.SetChild(&top.Widget)
	acts := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	acts.AddCssClass("mail-row-acts")
	acts.SetHalign(gtk.AlignEndValue)
	acts.SetValign(gtk.AlignCenterValue)
	act := func(icon, tip string, f func(c *mail.Conversation)) *gtk.Button {
		b := gtk.NewButtonFromIconName(icon)
		b.AddCssClass("mail-row-act")
		b.SetTooltipText(tip)
		b.SetFocusOnClick(false)
		r.ptrs = append(r.ptrs, w.h.click(b, func() {
			if c := w.rowConv(r); c != nil {
				f(c)
			}
		}))
		gtkx.Append(acts, &b.Widget)
		return b
	}
	act("mail-archive-symbolic", i18n.T("Archive (E)"), w.archiveConv)
	act("user-trash-symbolic", i18n.T("Delete (#)"), w.trashConv)
	r.read = *act("mail-read-symbolic", "", w.toggleRead)
	r.star = *act("mail-starred-symbolic", "", w.starConv)
	over.AddOverlay(&acts.Widget)
	gtkx.Append(&r.box, &over.Widget)
	mid := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
	mid.AddCssClass("mail-row-indent")
	r.subject = *label("mail-subject-line", true)
	gtkx.Append(mid, &r.subject.Widget)
	r.count = *label("mail-n", false)
	gtkx.Append(mid, &r.count.Widget)
	gtkx.Append(&r.box, &mid.Widget)
	r.snippet = *label("mail-snippet", true)
	r.snippet.AddCssClass("mail-row-indent")
	gtkx.Append(&r.box, &r.snippet.Widget)
	foot := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	foot.AddCssClass("mail-row-indent")
	r.tag = *label("mail-tag", false)
	gtkx.Append(foot, &r.tag.Widget)
	r.clip = *label("mail-clip", false)
	r.clip.SetText(i18n.T("FILE"))
	gtkx.Append(foot, &r.clip.Widget)
	r.starred = *gtk.NewImageFromIconName("mail-starred-symbolic")
	r.starred.AddCssClass("mail-row-starred")
	r.starred.SetPixelSize(11)
	gtkx.Append(foot, &r.starred.Widget)
	r.accountL = *label("mail-row-account", false)
	r.accountL.SetHexpand(true)
	r.accountL.SetXalign(1)
	gtkx.Append(foot, &r.accountL.Widget)
	gtkx.Append(&r.box, &foot.Widget)
	li.SetChild(&r.box.Widget)
	r.box.Unref()
	w.rows[item] = r
}

func (w *window) teardownRow(item uintptr) {
	if r := w.rows[item]; r != nil {
		w.h.forget(r.ptrs)
	}
	delete(w.rows, item)
}

// rowConv is the conversation a row shows now.
func (w *window) rowConv(r *convRow) *mail.Conversation {
	pos := gtk.ListItemNewFromInternalPtr(r.item).GetPosition()
	if int(pos) >= len(w.convs) {
		return nil
	}
	return w.convs[pos]
}

func (w *window) bindRow(item uintptr) {
	r := w.rows[item]
	if r == nil {
		return
	}
	w.fillRow(r)
}

// fillRow shows a row's conversation. Its position is read each time: a
// splice moves the rows after it without binding them again.
func (w *window) fillRow(r *convRow) {
	pos := gtk.ListItemNewFromInternalPtr(r.item).GetPosition()
	if int(pos) >= len(w.convs) {
		return
	}
	c := w.convs[pos]
	latest := c.Latest()
	people := c.People(w.mine)
	names := make([]string, 0, 3)
	for i, p := range people {
		if i == 3 {
			break
		}
		names = append(names, firstName(p))
	}
	if len(people) == 1 {
		names = []string{people[0].Short()}
	}
	if w.lastMine(c) && len(c.Messages) > 1 {
		names = append(names, i18n.T("me"))
	}
	r.who.SetText(strings.Join(names, ", "))
	r.when.SetText(when(latest.Date, time.Now()))
	subject := c.Subject()
	if subject == "" {
		subject = i18n.T("(no subject)")
	}
	r.subject.SetText(subject)
	r.count.SetText("")
	if n := len(c.Messages); n > 1 {
		r.count.SetText(fmt.Sprint(n))
	}
	r.snippet.SetText(latest.Snippet)
	r.snippet.SetVisible(latest.Snippet != "")
	unread := c.Unread() > 0
	setClass(&r.box.Widget, "unread", unread)
	setClass(&r.mark.Widget, "on", unread)
	t := c.Tag()
	if w.lastMine(c) && t == mail.TagReply {
		t = mail.TagWait
	}
	tag := ""
	switch t {
	case mail.TagReply, mail.TagWait:
		tag = t.Mark() // the marks to act on; the rest are filters
	}
	if j := w.judgment(latest.Key()); j != nil && j.Verdict == mail.Suspicious {
		tag = i18n.T("SUSPICIOUS")
	}
	r.tag.SetText(tag)
	setClass(&r.tag.Widget, "suspicious", tag == i18n.T("SUSPICIOUS"))
	r.tag.SetVisible(tag != "")
	r.clip.SetVisible(c.Attach())
	starred := c.Starred()
	r.starred.SetVisible(starred)
	setClass(&r.star.Widget, "on", starred)
	if starred {
		r.star.SetTooltipText(i18n.T("Unstar (S)"))
	} else {
		r.star.SetTooltipText(i18n.T("Star (S)"))
	}
	if unread {
		r.read.SetIconName("mail-read-symbolic")
		r.read.SetTooltipText(i18n.T("Mark as read"))
	} else {
		r.read.SetIconName("mail-unread-symbolic")
		r.read.SetTooltipText(i18n.T("Mark as unread (U)"))
	}
	acc := ""
	if w.view.kind != viewBox && len(w.accts) > 1 {
		if x := w.acct(latest.Account); x != nil {
			acc = accountName(x.s.Acc)
		}
	}
	r.accountL.SetText(acc)
	r.accountL.SetVisible(acc != "")
	r.box.GetParent().SetTooltipText(latest.From.String())
}

// judgment is a message's phishing route, when it has been read.
func (w *window) judgment(k mail.Key) *mail.Judgment {
	if rd := w.reading[k]; rd != nil && rd.m != nil {
		return &rd.judged
	}
	return nil
}

func firstName(a mail.Address) string {
	if a.Name == "" {
		local, _, _ := strings.Cut(a.Addr, "@")
		return local
	}
	if f := strings.Fields(a.Name); len(f) > 1 && !strings.Contains(a.Name, ",") {
		return f[0]
	}
	return a.Name
}

// when is a message's time in the list: the time today, the weekday this
// week, else the date.
func when(t, now time.Time) string {
	t = t.Local()
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return t.Format("15:04")
	case now.Sub(t) < 6*24*time.Hour && t.Before(now):
		return i18n.Time(t, "Mon")
	case y1 == y2:
		return i18n.Time(t, "Jan 2")
	}
	return t.Format("2006-01-02")
}

func label(class string, grow bool) *gtk.Label {
	l := gtk.NewLabel("")
	l.AddCssClass(class)
	l.SetXalign(0)
	if grow {
		l.SetHexpand(true)
		l.SetEllipsize(3) // end
	}
	return l
}

func setClass(w *gtk.Widget, class string, on bool) {
	if on {
		w.AddCssClass(class)
	} else {
		w.RemoveCssClass(class)
	}
}

// say shows text in an error or note label, hidden when empty.
func (w *window) say(l *gtk.Label, text string) {
	l.SetText(text)
	l.SetVisible(text != "")
}

// selectionChanged opens the conversation chosen.
func (w *window) selectionChanged() {
	i := w.sel.GetSelected()
	if int(i) >= len(w.convs) {
		return
	}
	c := w.convs[i]
	if c.ID == w.openID {
		return
	}
	w.openConversation(c)
}

// --- search ---

func (w *window) searchChanged() {
	text := strings.TrimSpace(w.search.GetText())
	if text == w.query {
		return
	}
	w.query, w.found, w.asked = text, nil, ""
	w.say(&w.searchNote, "")
	if mail.Looks(text) {
		w.parsed = mail.Query{}
		w.say(&w.searchNote, i18n.T("Press Enter to ask the assistant to search for this."))
		w.refresh()
		return
	}
	w.parsed = mail.ParseQuery(text, time.Now())
	w.refresh()
}

// searchEntered searches the server too, or asks the assistant to turn a
// sentence into a search first.
func (w *window) searchEntered() {
	text := strings.TrimSpace(w.search.GetText())
	if text == "" {
		return
	}
	if mail.Looks(text) && w.asked != text {
		w.askSearch(text)
		return
	}
	w.searchServer()
}

func (w *window) searchServer() {
	q := w.parsed
	if q.Empty() {
		return
	}
	type target struct {
		x       *acct
		mailbox string
	}
	var ts []target
	for _, x := range w.accts {
		switch {
		case w.view.kind == viewBox && w.view.account == x.s.Acc.ID:
			ts = append(ts, target{x, w.view.mailbox})
		case w.view.kind != viewBox:
			ts = append(ts, target{x, x.s.Role(mail.RoleInbox)})
		}
	}
	query := w.query
	w.say(&w.searchNote, i18n.T("Searching the server…"))
	go func() {
		found := map[mail.Key]bool{}
		var errs []string
		for _, t := range ts {
			uids, err := t.x.s.Search(w.ctx, t.mailbox, q)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			for _, u := range uids {
				found[mail.Key{Account: t.x.s.Acc.ID, Mailbox: t.mailbox, UID: u}] = true
			}
		}
		gtkx.Post(func() {
			if w.query != query {
				return
			}
			w.found = found
			note := ""
			if w.asked != "" {
				note = i18n.Tf("Searched for %s", w.query)
			}
			if len(errs) > 0 {
				note = i18n.Tf("The server's search failed: %s", errs[0])
			}
			w.say(&w.searchNote, note)
			w.refresh()
		})
	}()
}

// --- keys ---

func (w *window) key(keyval uint32, state gdk.ModifierType) bool {
	if w.welcome != nil && w.top.GetVisibleChildName() == "welcome" {
		return w.welcome.key(keyval, state)
	}
	ctrl := state&gdk.ControlMaskValue != 0
	if focus := w.win.GetFocus(); focus != nil && editing(focus) {
		if keyval == uint32(gdk.KEY_Escape) && focus.GoPointer() == w.search.GoPointer() {
			w.search.SetText("")
			w.list.GrabFocus()
			return true
		}
		return false
	}
	switch {
	case ctrl && (keyval == uint32(gdk.KEY_f) || keyval == uint32(gdk.KEY_k)), keyval == uint32(gdk.KEY_slash):
		w.search.GrabFocus()
	case ctrl && keyval == uint32(gdk.KEY_comma):
		w.showSettings("")
	case ctrl:
		return false
	case keyval == uint32(gdk.KEY_j) || keyval == uint32(gdk.KEY_Down):
		w.move(1)
	case keyval == uint32(gdk.KEY_k) || keyval == uint32(gdk.KEY_Up):
		w.move(-1)
	case keyval == uint32(gdk.KEY_c):
		w.newComposer(mail.Draft{}, nil)
	case keyval == uint32(gdk.KEY_z) && w.pending != nil:
		w.undo()
	case keyval == uint32(gdk.KEY_question):
		w.showKeys()
	case w.open == nil:
		return false
	case keyval == uint32(gdk.KEY_e) || keyval == uint32(gdk.KEY_y):
		w.archive()
	case keyval == uint32(gdk.KEY_numbersign) || keyval == uint32(gdk.KEY_Delete):
		w.trash()
	case keyval == uint32(gdk.KEY_s):
		w.star()
	case keyval == uint32(gdk.KEY_u):
		w.markUnread()
	case keyval == uint32(gdk.KEY_r):
		w.reply(false)
	case keyval == uint32(gdk.KEY_a):
		w.reply(true)
	case keyval == uint32(gdk.KEY_f):
		w.forward()
	case keyval == uint32(gdk.KEY_exclam):
		w.spam()
	case keyval == uint32(gdk.KEY_v):
		w.moveTo()
	default:
		return false
	}
	return true
}

// editing says whether a widget takes typing.
func editing(wd *gtk.Widget) bool {
	for p := wd; p != nil; p = p.GetParent() {
		for _, c := range []string{"entry", "text", "textview"} {
			if p.GetCssName() == c {
				return true
			}
		}
	}
	return false
}

// move chooses the next or the previous conversation.
func (w *window) move(d int) {
	if len(w.convs) == 0 {
		return
	}
	i := int(w.sel.GetSelected())
	if i >= len(w.convs) {
		i = -1
		if d < 0 {
			i = len(w.convs)
		}
	}
	i = max(0, min(len(w.convs)-1, i+d))
	w.sel.SetSelected(uint32(i))
	w.list.ScrollTo(uint32(i), gtk.ListScrollFocusValue, nil)
}

// --- the readout ---

// drawStatus fills the readout at the top of the index: the sync's state
// and the time it last ended well, then the messages kept from the
// inboxes and the unread.
func (w *window) drawStatus() {
	syncing, down := 0, 0
	var last time.Time
	unread := 0
	for _, x := range w.accts {
		syncing += x.syncing
		if x.state != "" {
			down++
		}
		if x.last.After(last) {
			last = x.last
		}
		for _, b := range x.boxes {
			if b.Role == mail.RoleInbox {
				unread += b.Unread
			}
		}
	}
	state, class := i18n.T("Up to date"), "on"
	switch {
	case len(w.accts) == 0:
		state, class = i18n.T("No account yet"), "off"
	case syncing > 0:
		state, class = i18n.T("Checking for mail…"), "busy"
	case down == len(w.accts):
		state, class = i18n.T("Offline: trying again soon"), "warn"
	case down > 0:
		state, class = i18n.Nf("%d account cannot connect", "%d accounts cannot connect", down, down), "warn"
	case !last.IsZero() && time.Since(last) >= time.Minute:
		state = i18n.Tf("Checked %s", ago(last))
	}
	w.statusState.SetText(state)
	if !last.IsZero() {
		w.statusState.SetTooltipText(i18n.Tf("Last checked at %s", last.Format("15:04:05")))
	}
	for _, c := range []string{"on", "off", "busy", "warn"} {
		setClass(&w.statusMark.Widget, c, c == class)
	}
	if unread > 0 {
		w.win.SetTitle(i18n.Tf("Mail · %d unread", unread))
	} else {
		w.win.SetTitle(i18n.T("Mail"))
	}
}

// ago says how long ago a time was, in the status's words.
func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return i18n.T("just now")
	case d < time.Hour:
		n := int(d.Minutes())
		return i18n.Nf("%d minute ago", "%d minutes ago", n, n)
	}
	return t.Format("15:04")
}

// statusTick redraws the readout every few seconds.
func (w *window) statusTick() {
	if w.closed {
		return
	}
	w.drawStatus()
	w.h.later(5000, w.statusTick)
}

// thousands writes n with a comma every three digits.
func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// gone says whether none of a conversation's messages is in its mailbox
// any longer.
func (w *window) gone(c *mail.Conversation) bool {
	for _, m := range c.Messages {
		x := w.acct(m.Account)
		if x == nil {
			continue
		}
		for _, it := range x.s.Items(m.Mailbox) {
			if it.UID == m.UID {
				return false
			}
		}
	}
	return true
}

// isInbox says whether a view is an account's inbox.
func (w *window) isInbox(v view) bool {
	x := w.acct(v.account)
	return x != nil && v.mailbox == x.s.Role(mail.RoleInbox)
}

// sideCounts redraws the index when its counts changed (a sync, an
// action, a reply sent), and the readout.
func (w *window) sideCounts() {
	sig := fmt.Sprint(len(w.conversations(view{kind: viewReply})))
	for _, x := range w.accts {
		for _, b := range x.boxes {
			sig += fmt.Sprintf("/%d", b.Unread)
		}
	}
	if sig != w.sideSig {
		w.sideSig = sig
		w.drawSide()
	} else {
		w.drawStatus()
	}
}
