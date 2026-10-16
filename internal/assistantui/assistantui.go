// Package assistantui is the assistant's window, a light app of its own
// (cmd/inua-assistant) so that only it links Mister Morph: the
// conversation, a line for each tool, approval cards, the answer as it
// streams. internal/assistant does the rest.
package assistantui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"slices"
	"strings"
	"time"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/assistant"
	"github.com/lyricat/inua/internal/assistant/keys"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/ipc"
)

// Register adds the assistant to the light apps, as "assistant".
func Register(ctx context.Context, background bool) {
	appui.Register("assistant", "Assistant", i18n.M("Assistant"), func() appui.View {
		return &view{ctx: ctx, background: background}
	})
}

// suggestions start an empty conversation.
var suggestions = []string{
	i18n.M("What is using the most CPU right now?"),
	i18n.M("Turn on Focus and set the volume to 30%"),
	i18n.M("What did I miss? Sum up my notifications"),
	i18n.M("Which devices on my tailnet are online?"),
	i18n.M("What is the weather this afternoon in Kyoto?"),
}

type view struct {
	c          *appui.Ctx
	win        *gtk.ApplicationWindow
	ctx        context.Context
	background bool
	enabled    bool
	model      gtk.Label
	notice     gtk.Label
	noticeBx   gtk.Box
	profile    gtk.DropDown
	talk       gtk.Box
	scroll     gtk.ScrolledWindow
	question   gtk.Entry
	send       gtk.Button

	as        *assistant.Assistant
	stamp     string   // config.toml's and the keys' times when as was made
	names     []string // the profile drop-down's
	running   bool
	cancel    context.CancelFunc
	answer    *gtk.Label // the answer being streamed
	first     []string   // the question of the first start (--ask)
	apiCancel context.CancelFunc
	apiDone   chan struct{}
	reloading bool
	closed    bool
	built     bool // Build has run; before, the window is not shown yet

	approvals map[string]chan bool // by card, while a tool waits
	nextCard  int

	onSend, onReset, onCard, onSuggestion func(gtk.Button)
	onSettings                            func(gtk.Button)
	onProfile, onActive                   func(uintptr, uintptr, uintptr)
	onActivate                            func(gtk.Entry)
	onKey                                 func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onAsk                                 func(gio.SimpleAction, uintptr)
	onClose                               func(gtk.Window) bool
	onShutdown                            func(gio.Application)
	onBackground                          func(gio.SimpleAction, uintptr)
	onRefresh                             glib.SourceFunc
}

func (v *view) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	v.c = c
	c.StartHidden = v.background
	v.approvals = map[string]chan bool{}
	win := &gtk.ApplicationWindow{}
	v.win = win
	var reset, settings, openSettings gtk.Button
	ui := c.Load("assistant.ui")
	ui.Get("assistant", win)
	ui.Get("model", &v.model)
	ui.Get("notice", &v.notice)
	ui.Get("notice_box", &v.noticeBx)
	ui.Get("profile", &v.profile)
	ui.Get("settings", &settings)
	ui.Get("open_settings", &openSettings)
	ui.Get("talk", &v.talk)
	ui.Get("scroll", &v.scroll)
	ui.Get("question", &v.question)
	ui.Get("send", &v.send)
	ui.Get("reset", &reset)
	ui.Close()

	v.onSend = func(gtk.Button) {
		if v.running {
			v.cancel() // the button is Stop meanwhile
		} else {
			v.submit()
		}
	}
	v.onSettings = func(gtk.Button) { openAISettings() }
	v.onProfile = func(uintptr, uintptr, uintptr) {
		i := int(v.profile.GetSelected())
		if v.as == nil || i >= len(v.names) || v.names[i] == v.as.Profile() {
			return
		}
		if err := v.as.UseProfile(v.names[i]); err != nil {
			v.line("as-error", err.Error())
			return
		}
		v.model.SetText(v.as.Model())
		v.toolLine(i18n.Tf("%s answers from here", v.names[i]))
	}
	// Back from the settings: what changed applies to the next question.
	v.onActive = func(uintptr, uintptr, uintptr) {
		if win.IsActive() {
			v.refresh()
		}
	}
	v.onActivate = func(gtk.Entry) { v.submit() }
	v.onReset = func(gtk.Button) {
		if v.running || v.as == nil || len(v.approvals) != 0 {
			return
		}
		v.as.Reset()
		gtkx.Clear(&v.talk)
		v.showSuggestions()
	}
	v.onSuggestion = func(b gtk.Button) { v.ask(b.GetName()) }
	v.onCard = func(b gtk.Button) {
		verb, id, _ := strings.Cut(b.GetName(), ":")
		ch := v.approvals[id]
		delete(v.approvals, id)
		if ch != nil {
			ch <- verb == "allow"
		}
	}
	v.onKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if keyval == 0xff1b && v.running { // Escape stops the answer
			v.cancel()
			return true
		}
		return false
	}
	v.onAsk = func(_ gio.SimpleAction, param uintptr) {
		q := (*glib.Variant)(unsafe.Add(unsafe.Pointer(nil), param)).GetString(nil)
		win.Present()
		v.ask(q)
	}
	v.onClose = func(gtk.Window) bool {
		if v.enabled || v.reloading {
			win.SetVisible(false)
			return true
		}
		return false
	}
	v.onShutdown = func(gio.Application) {
		v.closed = true
		if v.cancel != nil {
			v.cancel()
		}
		if v.apiCancel != nil {
			v.apiCancel()
		}
	}
	win.ConnectCloseRequest(&v.onClose)
	c.App.ConnectShutdown(&v.onShutdown)
	v.onBackground = func(gio.SimpleAction, uintptr) { v.refresh() }
	background := gio.NewSimpleAction("background", nil)
	background.ConnectActivate(&v.onBackground)
	c.App.AddAction(background)
	background.Unref()
	v.send.ConnectClicked(&v.onSend)
	settings.ConnectClicked(&v.onSettings)
	openSettings.ConnectClicked(&v.onSettings)
	gtkx.ConnectNotify(&v.profile, "selected", &v.onProfile)
	gtkx.ConnectNotify(win, "is-active", &v.onActive)
	v.question.ConnectActivate(&v.onActivate)
	reset.ConnectClicked(&v.onReset)
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(&v.onKey)
	win.AddController(&keys.EventController)
	ask := gio.NewSimpleAction("ask", glib.NewVariantType("s"))
	ask.ConnectActivate(&v.onAsk)
	c.App.AddAction(ask)
	ask.Unref()

	v.setUp()
	v.built = true
	v.showSuggestions()
	v.question.GrabFocus()
	v.onRefresh = func(uintptr) bool {
		if v.closed {
			return false
		}
		v.refresh()
		return true
	}
	glib.TimeoutAdd(2000, &v.onRefresh, 0)
	go func() {
		<-v.ctx.Done()
		gtkx.Post(func() {
			v.closed = true
			if v.cancel != nil {
				v.cancel()
			}
			if v.apiCancel != nil {
				v.apiCancel()
			}
			done := v.apiDone
			go func() {
				if done != nil {
					<-done
				}
				gtkx.Post(func() { c.App.Quit() })
			}()
		})
	}()
	return win
}

func (v *view) ThemeChanged() { v.refresh() }

// Open takes `--ask QUESTION` from the first start.
func (v *view) Open(args []string) {
	for i, a := range args {
		if a == "--ask" && i+1 < len(args) {
			v.ask(args[i+1])
			return
		}
	}
}

// setUp makes the assistant from config.toml and the saved keys, or says
// why it cannot. A conversation going on carries over to the new one.
func (v *view) setUp() {
	v.apiCancel = nil
	v.stamp = stamp()
	cfg, _, err := config.Load(config.DefaultPath())
	if err != nil {
		cfg = config.Default()
	}
	v.enabled = cfg.Assistant.Enable
	old := v.as
	v.as = nil
	say := func(msg string) {
		v.notice.SetText(msg)
		v.noticeBx.SetVisible(true)
		v.question.SetSensitive(false)
		v.send.SetSensitive(false)
		v.profile.SetVisible(false)
		v.model.SetText("")
	}
	if !cfg.Assistant.Enable {
		say(i18n.T("The assistant is turned off. Turn it on in Settings → AI."))
		// Hidden, it has nothing to do: a background start, or a closed
		// window when it was turned off. A window opened says so instead.
		if !v.win.GetVisible() && (v.built || v.background) {
			v.c.App.Quit()
		}
		return
	}
	// The keyring may ask for its passphrase (pass: gpg's) here.
	set, err := assistant.Load(cfg.Assistant, keys.New())
	if err != nil {
		say(err.Error())
		return
	}
	// inua's tools come from its MCP server, this program with --mcp, as
	// for agents (docs/feat/mcp.md).
	exe, err := os.Executable()
	if err != nil {
		say(err.Error())
		return
	}
	set.MCPServer = []string{exe, "--mcp"}
	v.as, err = assistant.New(set, nil, v.approve)
	if err != nil {
		say(err.Error())
		return
	}
	if old != nil {
		v.as.Carry(old)
	}
	v.noticeBx.SetVisible(false)
	v.question.SetSensitive(true)
	v.send.SetSensitive(true)
	v.model.SetText(v.as.Model())
	v.names = v.as.Profiles()
	list := gtk.NewStringList(v.names)
	v.profile.SetModel(list)
	list.Unref()
	v.profile.SetSelected(0)
	v.profile.SetVisible(len(v.names) > 1)
	if set.ServeListen != "" {
		ctx, cancel := context.WithCancel(v.ctx)
		v.apiCancel = cancel
		done := make(chan struct{})
		v.apiDone = done
		as := v.as
		go func() {
			err := as.Serve(ctx)
			close(done)
			gtkx.Post(func() {
				if v.closed || v.apiDone != done || ctx.Err() != nil {
					return
				}
				v.apiCancel = nil
				if err != nil {
					log.Printf("assistant endpoint: %v", err)
					v.notice.SetText(i18n.Tf("Could not start the Console endpoint: %s", err.Error()))
					v.noticeBx.SetVisible(true)
					v.win.Present()
				}
			})
		}()
	}
}

// refresh makes the assistant again when its settings changed, between
// answers.
func (v *view) refresh() {
	if v.closed || v.running || v.reloading || stamp() == v.stamp {
		return
	}
	if v.apiCancel != nil {
		v.reloading = true
		v.question.SetSensitive(false)
		v.send.SetSensitive(false)
		v.apiCancel()
		done := v.apiDone
		go func() {
			<-done
			gtkx.Post(func() {
				if v.closed {
					return
				}
				v.setUp()
				v.reloading = false
			})
		}()
		return
	}
	hadNone := v.as == nil
	v.setUp()
	if hadNone && v.as != nil {
		gtkx.Clear(&v.talk)
		v.showSuggestions()
	}
}

// stamp is when config.toml and the saved keys last changed.
func stamp() string {
	var b strings.Builder
	if info, err := os.Stat(config.DefaultPath()); err == nil {
		fmt.Fprint(&b, info.ModTime().UnixNano(), info.Size(), ";")
	}
	saved, _ := keys.New().Saved()
	for _, n := range slices.Sorted(maps.Keys(saved)) {
		fmt.Fprint(&b, n, "=", saved[n], ";")
	}
	return b.String()
}

// openAISettings opens the shell's settings at the AI page.
func openAISettings() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		sock := ipc.SocketPath(cmp.Or(os.Getenv("INUA_INSTANCE"), "default"))
		if _, err := ipc.Call(ctx, sock, ipc.Request{Op: "call", Target: "settings", Function: "open", Args: []string{"ai"}}); err != nil {
			log.Printf("open settings: %v", err)
		}
	}()
}

func (v *view) submit() {
	q := strings.TrimSpace(v.question.GetText())
	if q == "" {
		return
	}
	v.question.SetText("")
	v.ask(q)
}

// ask puts a question to the assistant; its answer streams in below it.
func (v *view) ask(q string) {
	q = strings.TrimSpace(q)
	if q == "" {
		return
	}
	v.refresh()
	if v.reloading {
		v.question.SetText(q)
		return
	}
	if v.as == nil {
		return
	}
	if v.running { // kept in the field for when this answer is done
		v.question.SetText(q)
		v.question.SetPosition(-1)
		return
	}
	v.dropSuggestions()
	v.line("as-you", q)
	v.answer = v.line("as-answer", "…")
	v.running = true
	v.send.SetLabel(i18n.T("Stop"))
	ctx, cancel := context.WithCancel(v.ctx)
	v.cancel = cancel
	v.profile.SetSensitive(false)
	ev := assistant.Events{
		Answer: func(text string) { gtkx.Post(func() { v.setAnswer(text) }) },
		Tool:   func(_, doing string) { gtkx.Post(func() { v.toolLine("→ " + doing + " …") }) },
		Denied: func(_, doing string) { gtkx.Post(func() { v.toolLine(i18n.Tf("✕ Not done: %s", doing)) }) },
		Error:  func(name string, err error) { gtkx.Post(func() { v.toolLine("! " + name + ": " + err.Error()) }) },
	}
	go func() {
		answer, err := v.as.Ask(ctx, q, ev)
		gtkx.Post(func() {
			if v.closed {
				return
			}
			v.running = false
			v.cancel()
			v.send.SetLabel(i18n.T("Ask"))
			v.profile.SetSensitive(true)
			switch {
			case errors.Is(err, context.Canceled):
				v.setAnswer(cmp.Or(answer, i18n.T("Stopped.")))
			case err != nil:
				log.Printf("assistant: %v", err)
				v.setAnswer(i18n.Tf("Something went wrong: %s", err.Error()))
				v.answer.AddCssClass("as-error")
			case answer == "":
				v.setAnswer(i18n.T("(no answer)"))
			}
			v.answer = nil
			v.question.GrabFocus()
		})
	}()
}

// approve shows a card and waits for Allow or Deny.
func (v *view) approve(ctx context.Context, _, doing string) (allowed bool, err error) {
	ch := make(chan bool, 1)
	var card *gtk.Box
	var id string
	gtkx.Post(func() {
		if v.closed || ctx.Err() != nil {
			return
		}
		v.win.Present()
		v.dropSuggestions()
		v.nextCard++
		id = fmt.Sprint(v.nextCard)
		v.approvals[id] = ch
		card = gtk.NewBox(gtk.OrientationVerticalValue, 10)
		card.AddCssClass("as-card")
		l := gtk.NewLabel(doing + "?")
		l.SetXalign(0)
		l.SetWrap(true)
		gtkx.Append(card, &l.Widget)
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
		row.SetHalign(gtk.AlignEndValue)
		for _, b := range [][2]string{{"deny", i18n.T("Don't")}, {"allow", i18n.T("Allow")}} {
			btn := gtk.NewButtonWithLabel(b[1])
			btn.SetName(b[0] + ":" + id)
			btn.AddCssClass(map[string]string{"deny": "outlined", "allow": "suggested-action"}[b[0]])
			btn.ConnectClicked(&v.onCard)
			gtkx.Append(row, &btn.Widget)
		}
		gtkx.Append(card, &row.Widget)
		v.insertBeforeAnswer(&card.Widget)
	})
	defer func() {
		gtkx.Post(func() {
			delete(v.approvals, id)
			if card == nil {
				return
			}
			defer card.Unref()
			if v.closed {
				return
			}
			gtkx.Clear(card)
			status := i18n.T("Not allowed")
			if err != nil {
				status = i18n.T("Canceled")
			} else if allowed {
				status = i18n.T("Allowed")
			}
			l := gtk.NewLabel(doing + " — " + status)
			l.AddCssClass("as-tool")
			l.SetXalign(0)
			l.SetWrap(true)
			gtkx.Append(card, &l.Widget)
		})
	}()
	select {
	case ok := <-ch:
		return ok, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// line adds a line of text to the conversation.
func (v *view) line(class, text string) *gtk.Label {
	l := gtk.NewLabel(text)
	l.AddCssClass(class)
	l.SetXalign(0)
	l.SetWrap(true)
	l.SetWrapMode(pango.WrapWordCharValue)
	if class == "as-you" {
		l.SetHalign(gtk.AlignEndValue)
		l.SetXalign(1)
	} else {
		l.SetSelectable(true)
		l.SetCanFocus(false) // or the first answer takes focus, all selected
	}
	gtkx.Append(&v.talk, &l.Widget)
	v.toBottom()
	return l
}

// toolLine goes before the answer being streamed, which stays last.
func (v *view) toolLine(text string) {
	if v.closed {
		return
	}
	l := gtk.NewLabel(text)
	l.AddCssClass("as-tool")
	l.SetXalign(0)
	l.SetWrap(true)
	l.SetWrapMode(pango.WrapWordCharValue)
	v.insertBeforeAnswer(&l.Widget)
	l.Unref()
}

func (v *view) insertBeforeAnswer(w *gtk.Widget) {
	if v.answer != nil {
		if prev := v.answer.GetPrevSibling(); prev != nil {
			v.talk.InsertChildAfter(w, prev)
		} else {
			v.talk.Prepend(w)
		}
	} else {
		v.talk.Append(w)
	}
	v.toBottom()
}

func (v *view) setAnswer(text string) {
	if !v.closed && v.answer != nil {
		v.answer.SetText(text)
		v.toBottom()
	}
}

// toBottom scrolls to the newest line once it is laid out.
func (v *view) toBottom() {
	glib.IdleAdd(&scrollDown, uintptr(unsafe.Pointer(v)))
}

var scrollDown glib.SourceFunc = func(p uintptr) bool {
	v := (*view)(unsafe.Add(unsafe.Pointer(nil), p))
	adj := v.scroll.GetVadjustment()
	adj.SetValue(adj.GetUpper() - adj.GetPageSize())
	return false
}

func (v *view) showSuggestions() {
	if v.as == nil { // it cannot answer them
		return
	}
	box := gtk.NewBox(gtk.OrientationVerticalValue, 6)
	box.AddCssClass("as-suggestions")
	box.SetName("suggestions")
	head := gtk.NewLabel(i18n.T("TRY"))
	head.AddCssClass("kicker")
	head.SetXalign(0)
	gtkx.Append(box, &head.Widget)
	for _, s := range suggestions {
		b := gtk.NewButtonWithLabel(i18n.T(s))
		b.AddCssClass("as-suggestion")
		b.SetName(s)
		if l := b.GetChild(); l != nil {
			l.SetHalign(gtk.AlignStartValue)
		}
		b.ConnectClicked(&v.onSuggestion)
		gtkx.Append(box, &b.Widget)
	}
	gtkx.Append(&v.talk, &box.Widget)
}

func (v *view) dropSuggestions() {
	for w := v.talk.GetFirstChild(); w != nil; w = w.GetNextSibling() {
		if w.GetName() == "suggestions" {
			v.talk.Remove(w)
			return
		}
	}
}
