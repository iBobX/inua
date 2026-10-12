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
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"

	"github.com/lyricat/inua/internal/apps/tailscale"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/assistant"
	"github.com/lyricat/inua/internal/assistant/keys"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/ipc"
)

// Register adds the assistant to the light apps, as "assistant".
func Register() {
	appui.Register("assistant", "Assistant", "Assistant", func() appui.View { return &view{} })
}

// suggestions start an empty conversation.
var suggestions = []string{
	"What is using the most CPU right now?",
	"Turn on Focus and set the volume to 30%",
	"What did I miss? Sum up my notifications",
	"Which devices on my tailnet are online?",
	"What is the weather this afternoon in Kyoto?",
}

type view struct {
	c        *appui.Ctx
	model    gtk.Label
	notice   gtk.Label
	noticeBx gtk.Box
	profile  gtk.DropDown
	talk     gtk.Box
	scroll   gtk.ScrolledWindow
	question gtk.Entry
	send     gtk.Button

	as      *assistant.Assistant
	stamp   string   // config.toml's and the keys' times when as was made
	names   []string // the profile drop-down's
	running bool
	cancel  context.CancelFunc
	answer  *gtk.Label // the answer being streamed
	first   []string   // the question of the first start (--ask)

	mu        sync.Mutex
	approvals map[string]chan bool // by card, while a tool waits
	nextCard  int

	onSend, onReset, onCard, onSuggestion func(gtk.Button)
	onSettings                            func(gtk.Button)
	onProfile, onActive                   func(uintptr, uintptr, uintptr)
	onActivate                            func(gtk.Entry)
	onKey                                 func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onAsk                                 func(gio.SimpleAction, uintptr)
}

func (v *view) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	v.c = c
	v.approvals = map[string]chan bool{}
	win := &gtk.ApplicationWindow{}
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
		v.toolLine(v.names[i] + " answers from here")
	}
	// Back from the settings: what changed applies to the next question.
	v.onActive = func(uintptr, uintptr, uintptr) {
		if win.IsActive() {
			v.refresh()
		}
	}
	v.onActivate = func(gtk.Entry) { v.submit() }
	v.onReset = func(gtk.Button) {
		if v.running || v.as == nil {
			return
		}
		v.as.Reset()
		gtkx.Clear(&v.talk)
		v.showSuggestions()
	}
	v.onSuggestion = func(b gtk.Button) { v.ask(b.GetName()) }
	v.onCard = func(b gtk.Button) {
		verb, id, _ := strings.Cut(b.GetName(), ":")
		v.mu.Lock()
		ch := v.approvals[id]
		delete(v.approvals, id)
		v.mu.Unlock()
		if ch != nil {
			ch <- verb == "allow"
		}
		// The buttons give way to what was decided.
		if row := b.GetParent(); row != nil {
			if card := row.GetParent(); card != nil {
				l := gtk.NewLabel(map[bool]string{true: "Allowed", false: "Not allowed"}[verb == "allow"])
				l.AddCssClass("as-tool")
				l.SetXalign(0)
				box := gtk.BoxNewFromInternalPtr(card.GoPointer())
				box.Remove(row)
				gtkx.Append(box, &l.Widget)
			}
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
	v.showSuggestions()
	v.question.GrabFocus()
	return win
}

func (v *view) ThemeChanged() {}

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
	v.stamp = stamp()
	cfg, _, err := config.Load(config.DefaultPath())
	if err != nil {
		cfg = config.Default()
	}
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
		say("The assistant is turned off. Turn it on in Settings → AI.")
		return
	}
	// The keyring may ask for its passphrase (pass: gpg's) here.
	k := keys.New()
	savedAt, err := k.Saved()
	if err != nil {
		say("Cannot read the saved keys: " + err.Error())
		return
	}
	saved := map[string]string{}
	for _, p := range cfg.Assistant.Profiles {
		if savedAt[p.Name] == 0 {
			continue
		}
		if saved[p.Name], err = k.Get(p.Name); err != nil {
			say(fmt.Sprintf("Cannot read the key of %s: %v", p.Name, err))
			return
		}
	}
	set, err := assistant.SettingsFrom(cfg.Assistant, saved)
	if errors.Is(err, assistant.ErrNoKey) {
		p, _ := cfg.Assistant.ProfileNamed(cfg.Assistant.Profile)
		msg := fmt.Sprintf("No API key for %s (%s): type one in Settings → AI", p.Name, p.Model)
		if p.APIKeyEnv != "" {
			msg += fmt.Sprintf(", or set %s in your environment (~/.config/environment.d/) and log in again", p.APIKeyEnv)
		}
		say(msg + ".")
		return
	}
	if err != nil {
		say(err.Error())
		return
	}
	v.as, err = assistant.New(set, assistant.Tools(desktopOf(cfg)), v.approve)
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
}

// refresh makes the assistant again when its settings changed, between
// answers.
func (v *view) refresh() {
	if v.running || stamp() == v.stamp {
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

// desktopOf connects the tools to this session.
func desktopOf(cfg config.Config) assistant.Desktop {
	sock := ipc.SocketPath(cmp.Or(os.Getenv("INUA_INSTANCE"), "default"))
	d := assistant.Desktop{
		Call: func(ctx context.Context, target, fn string, args ...string) (string, error) {
			resp, err := ipc.Call(ctx, sock, ipc.Request{Op: "call", Target: target, Function: fn, Args: args})
			return resp.Result, err
		},
		Launch:    func(e *desktop.Entry) error { return desktop.LaunchWith(e, cfg.TerminalCommand()) },
		Tailscale: tailscale.NewClient(""),
		HTTP:      &http.Client{Timeout: 15 * time.Second},
		ClipRead: func(ctx context.Context) (string, error) {
			out, err := exec.CommandContext(ctx, "wl-paste", "--no-newline", "--type", "text").Output()
			return string(out), err
		},
		ClipWrite: func(ctx context.Context, text string) error {
			cmd := exec.CommandContext(ctx, "wl-copy")
			cmd.Stdin = strings.NewReader(text)
			return cmd.Run()
		},
	}
	var once sync.Once
	var apps []*desktop.Entry
	d.Apps = func() []*desktop.Entry {
		once.Do(func() { apps = desktop.Scan(desktop.DataDirs(), os.Getenv("LANG")) })
		return apps
	}
	if paths, err := hypr.PathsFromEnv(); err == nil {
		client := hypr.NewClient(paths)
		d.HyprRequest = client.Request
		d.HyprDispatch = client.Dispatch
	}
	return d
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
	v.send.SetLabel("Stop")
	ctx, cancel := context.WithCancel(context.Background())
	v.cancel = cancel
	v.profile.SetSensitive(false)
	ev := assistant.Events{
		Answer: func(text string) { gtkx.Post(func() { v.setAnswer(text) }) },
		Tool:   func(_, doing string) { gtkx.Post(func() { v.toolLine("→ " + doing + " …") }) },
		Denied: func(_, doing string) { gtkx.Post(func() { v.toolLine("✕ Not done: " + doing) }) },
		Error:  func(name string, err error) { gtkx.Post(func() { v.toolLine("! " + name + ": " + err.Error()) }) },
	}
	go func() {
		answer, err := v.as.Ask(ctx, q, ev)
		gtkx.Post(func() {
			v.running = false
			v.cancel()
			v.send.SetLabel("Ask")
			v.profile.SetSensitive(true)
			switch {
			case errors.Is(err, context.Canceled):
				v.setAnswer(cmp.Or(answer, "Stopped."))
			case err != nil:
				log.Printf("assistant: %v", err)
				v.setAnswer("Something went wrong: " + err.Error())
				v.answer.AddCssClass("as-error")
			case answer == "":
				v.setAnswer("(no answer)")
			}
			v.answer = nil
			v.question.GrabFocus()
		})
	}()
}

// approve shows a card and waits for Allow or Deny.
func (v *view) approve(ctx context.Context, _, doing string) (bool, error) {
	ch := make(chan bool, 1)
	gtkx.Post(func() {
		v.mu.Lock()
		v.nextCard++
		id := fmt.Sprint(v.nextCard)
		v.approvals[id] = ch
		v.mu.Unlock()
		card := gtk.NewBox(gtk.OrientationVerticalValue, 10)
		card.AddCssClass("as-card")
		l := gtk.NewLabel(doing + "?")
		l.SetXalign(0)
		l.SetWrap(true)
		gtkx.Append(card, &l.Widget)
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
		row.SetHalign(gtk.AlignEndValue)
		for _, b := range [][2]string{{"deny", "Don't"}, {"allow", "Allow"}} {
			btn := gtk.NewButtonWithLabel(b[1])
			btn.SetName(b[0] + ":" + id)
			btn.AddCssClass(map[string]string{"deny": "outlined", "allow": "suggested-action"}[b[0]])
			btn.ConnectClicked(&v.onCard)
			gtkx.Append(row, &btn.Widget)
		}
		gtkx.Append(card, &row.Widget)
		v.insertBeforeAnswer(&card.Widget)
		card.Unref()
	})
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
	if v.answer != nil {
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
	head := gtk.NewLabel("TRY")
	head.AddCssClass("kicker")
	head.SetXalign(0)
	gtkx.Append(box, &head.Widget)
	for _, s := range suggestions {
		b := gtk.NewButtonWithLabel(s)
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
