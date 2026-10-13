package ui

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/convert"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/shell"
)

// maxRows is how many results fit in launcher.ui's fixed height.
const maxRows = 7

// launcher shows shell.Launcher. Its window (launcher.ui) exists exactly
// while the launcher is open: it is built on every open and destroyed on
// close, because re-showing a hidden layer surface hung GTK's GPU renderers
// on the 4th time (docs/toolkit.md), and a new surface does not.
type launcher struct {
	st       shell.Launcher
	rates    *convert.Rates
	fetching bool // exchange rates are being read

	win        *gtk.ApplicationWindow // nil while closed
	mode       gtk.Label
	modeDesc   gtk.Label
	query      gtk.Entry
	results    gtk.Box
	messageBox gtk.Box
	messageTag gtk.Label
	message    gtk.Label
	refs       gtkx.Scope
	hasFocus   bool
	opens      int
}

const (
	keyEscape     = 0xff1b
	keySpace      = 0x20
	keyReturn     = 0xff0d
	keyKPEnter    = 0xff8d
	keyUp         = 0xff52
	keyDown       = 0xff54
	keyLeft       = 0xff51
	keyRight      = 0xff53
	keyPageUp     = 0xff55
	keyPageDown   = 0xff56
	keyTab        = 0xff09
	keyISOLeftTab = 0xfe20
)

func (s *shellUI) bindLauncherHandlers() {
	s.onKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		return s.launcherKey(keyval)
	}
	s.onQueryChanged = func(uintptr, uintptr) {
		l := &s.launcher
		// renderLauncher setting the text also lands here.
		if q := l.query.GetText(); l.win != nil && q != l.st.Query {
			l.st.SetQuery(q)
			s.renderLauncher()
		}
	}
	s.onQueryActivate = func(gtk.Entry) { s.executeLauncher() }
	s.onQueryFocus = func(gtk.EventControllerFocus) {
		s.launcher.hasFocus = true
	}
}

func (s *shellUI) showLauncher(m shell.Mode) {
	s.freshenRates()
	s.launcher.st.Show(m)
	s.syncLauncher()
	log.Printf("launcher: open mode=%s", m)
}

func (s *shellUI) cancelLauncher() {
	a := s.launcher.st.Cancel()
	s.syncLauncher()
	if a.Kind == shell.Focus {
		s.focusWindow(a.Address)
	}
}

func (s *shellUI) executeLauncher() {
	l := &s.launcher
	a := l.st.Execute()
	switch a.Kind {
	case shell.Launch:
		log.Printf("launcher: launch %s", a.Entry.ID)
		if err := desktop.LaunchWith(a.Entry, s.cfg.TerminalCommand()); err != nil {
			log.Printf("launch %s: %v", a.Entry.ID, err)
			l.st.Done(fmt.Errorf("could not start %s: %v", a.Entry.Name, err))
		} else {
			l.st.Done(nil)
		}
	case shell.Copy:
		if err := copyText(a.Text); err != nil {
			l.st.Done(fmt.Errorf("could not copy: %v", err))
		} else {
			l.st.Done(nil)
		}
	case shell.Focus:
		log.Printf("launcher: focus %s", a.Address)
		s.focusWindow(a.Address)
		l.st.Done(nil)
	case shell.Fill:
		l.query.SetText(a.Text)
		l.query.SetPosition(-1)
	case shell.Open:
		log.Printf("launcher: open %s", a.Text)
		if err := desktop.Spawn([]string{"xdg-open", a.Text}, ""); err != nil {
			l.st.Done(fmt.Errorf("could not open it: %v", err))
		} else {
			l.st.Done(nil)
		}
	case shell.Ask:
		log.Printf("launcher: ask the assistant")
		if err := desktop.Spawn([]string{"inua-assistant", "--ask", a.Text}, ""); err != nil {
			l.st.Done(fmt.Errorf("could not start the assistant: %v", err))
		} else {
			l.st.Done(nil)
		}
	case shell.Run:
		log.Printf("launcher: %s %s", a.Text, a.Arg)
		l.st.Done(nil)
		// After the launcher is gone: a screenshot must not catch it, and a
		// window command finds its window focused again.
		s.runAfter(120, func() { s.runCommand(a.Text, a.Arg, a.Address) })
	}
	s.syncLauncher()
}

// runCommand carries out a launcher command (shell.Commands) on the
// window it was opened over, if it is one for a window.
func (s *shellUI) runCommand(id, arg, window string) {
	w := "address:" + window
	switch id {
	case "lock":
		if err := s.lockScreen(); err != nil {
			log.Printf("lock: %v", err)
		}
	case "suspend", "reboot", "poweroff", "logout":
		s.doPower(id)
	case "trash":
		go func() {
			if out, err := exec.Command("gio", "trash", "--empty").CombinedOutput(); err != nil {
				log.Printf("empty trash: %v %s", err, out)
			}
		}()
	case "settings":
		s.openSettings()
	case "shot-area", "shot-window", "shot-screen", "record", "color":
		fn := map[string]string{"shot-area": "area", "shot-window": "pick-window", "shot-screen": "pick-screen", "record": "record", "color": "color"}[id]
		s.screenshotRequest(fn)
	case "clear-notes":
		s.clearNotes(s.notes.c.ClearAll())
	case "focus":
		s.controlAction("dnd")
	case "wifi":
		s.controlAction("wifi")
	case "bluetooth":
		s.controlAction("bt")
	case "restart-shell":
		if err := s.scheduleRestart(); err != nil {
			log.Printf("restart: %v", err)
		}
	case "workspace":
		s.switchWorkspace(arg)
	case "move-window":
		s.dispatch("movetoworkspace " + arg + "," + w)
	case "close-window":
		s.dispatch("closewindow " + w)
	case "float":
		s.dispatch("togglefloating " + w)
	case "fullscreen":
		s.dispatch("focuswindow " + w)
		s.dispatch("fullscreen 0")
	case "center":
		s.dispatch("focuswindow " + w)
		s.dispatch("centerwindow")
	default:
		log.Printf("launcher: unknown command %q", id)
	}
}

// applyLauncherWeb gives the launcher the web search and quicklinks, and
// the assistant's row when it is installed and on.
func (s *shellUI) applyLauncherWeb() {
	_, err := exec.LookPath("inua-assistant")
	s.launcher.st.SetAssistant(s.cfg.Assistant.Enable && err == nil)
	var links []shell.Quicklink
	for _, q := range s.cfg.Launcher.Quicklinks {
		links = append(links, shell.Quicklink{Keyword: q.Keyword, Name: q.Name, URL: q.URL})
	}
	s.launcher.st.SetWeb(s.cfg.Launcher.Search, links)
}

func (s *shellUI) launcherKey(keyval uint32) bool {
	l := &s.launcher
	switch keyval {
	case keyEscape:
		s.cancelLauncher()
		return true
	case keyReturn, keyKPEnter:
		s.executeLauncher()
		return true
	case keyDown:
		l.st.Move(1)
	case keyUp:
		l.st.Move(-1)
	case keyPageDown:
		l.st.Move(maxRows)
	case keyPageUp:
		l.st.Move(-maxRows)
	case keyTab, keyISOLeftTab:
		l.st.ToggleMode()
	default:
		return false
	}
	s.renderLauncher()
	return true
}

// syncLauncher makes the window match the state: built when the launcher
// opened, destroyed when it closed, redrawn while open.
func (s *shellUI) syncLauncher() {
	l := &s.launcher
	switch {
	case l.st.Open && l.win == nil:
		s.buildLauncher()
		s.renderLauncher()
		l.win.Present()
		l.query.GrabFocus()
	case !l.st.Open && l.win != nil:
		l.win.Destroy()
		l.refs.Release()
		l.win, l.hasFocus = nil, false
	case l.st.Open:
		s.renderLauncher()
	}
}

// buildLauncher instantiates launcher.ui and turns it into an overlay layer
// surface with exclusive keyboard focus on the focused monitor.
func (s *shellUI) buildLauncher() {
	l := &s.launcher
	l.win = &gtk.ApplicationWindow{}
	l.opens++
	ui := s.load("launcher.ui", &l.refs)
	ui.Get("launcher", l.win)
	ui.Get("mode", &l.mode)
	ui.Get("mode_desc", &l.modeDesc)
	ui.Get("query", &l.query)
	ui.Get("results", &l.results)
	ui.Get("message_box", &l.messageBox)
	ui.Get("message_tag", &l.messageTag)
	ui.Get("message", &l.message)
	ui.Close()

	l.win.SetApplication(s.app)
	w := l.win.GoPointer()
	if s.layer {
		layershell.InitForWindow(w)
		layershell.SetNamespace(w, "inua-launcher")
		layershell.SetLayer(w, layershell.LayerOverlay)
		layershell.SetKeyboardMode(w, layershell.KeyboardExclusive)
		if mon := s.focusedMonitor(); mon != nil {
			layershell.SetMonitor(w, mon.GoPointer())
		}
		// No anchors: the compositor centers the surface.
	} else {
		l.win.SetTitle(i18n.T("inua launcher"))
	}

	// Keys are handled in the capture phase, before the entry sees them.
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue)
	keys.ConnectKeyPressed(&s.onKey)
	l.win.AddController(&keys.EventController) // takes the only reference

	focus := gtk.NewEventControllerFocus()
	focus.ConnectEnter(&s.onQueryFocus)
	l.query.AddController(&focus.EventController)

	gtkx.ConnectChanged(&l.query, &s.onQueryChanged)
	l.query.ConnectActivate(&s.onQueryActivate)
}

// renderLauncher rebuilds the result rows from row.ui.
func (s *shellUI) renderLauncher() {
	l := &s.launcher
	if l.win == nil {
		return
	}
	if l.query.GetText() != l.st.Query { // Show started over
		l.query.SetText(l.st.Query)
	}
	l.mode.SetText(l.st.Mode.Tag())
	l.modeDesc.SetText(l.st.Mode.Label())
	msg := l.st.Message()
	l.message.SetText(msg)
	l.messageTag.SetVisible(l.st.Failed())
	l.messageBox.SetVisible(msg != "")

	gtkx.Clear(&l.results)
	rowUI := s.readUI("row.ui")
	// The notice takes a row's height; the window must not grow.
	rows := maxRows
	if msg != "" {
		rows--
	}
	// Keep the selection visible in a window of rows.
	start := max(0, l.st.Selected-rows+1)
	for i := start; i < len(l.st.Results) && i < start+rows; i++ {
		r := l.st.Results[i]
		var refs gtkx.Scope
		ui, err := gtkx.Load(rowUI, &refs)
		if err != nil {
			log.Fatalf("row.ui: %v", err)
		}
		var row gtk.Box
		var icon gtk.Image
		var title, detail, glyph gtk.Label
		ui.Take("row", &row)
		ui.Get("icon", &icon)
		ui.Get("glyph", &glyph)
		ui.Get("title", &title)
		ui.Get("detail", &detail)
		ui.Close()
		if r.Glyph != "" {
			icon.SetVisible(false)
			glyph.SetText(r.Glyph)
			glyph.SetVisible(true)
		} else {
			setIcon(&icon, r.Icon)
		}
		title.SetText(r.Title)
		detail.SetText(r.Detail)
		detail.SetVisible(r.Detail != "")
		if i == l.st.Selected {
			row.AddCssClass("selected")
		}
		gtkx.Append(&l.results, &row.Widget)
		refs.Release()
	}
}

// setIcon shows an app icon (a theme name or an absolute path) or a window
// class, which usually names the app's icon too, in any case.
func setIcon(img *gtk.Image, name string) {
	if strings.HasPrefix(name, "/") {
		img.SetFromFile(name)
		return
	}
	theme := gtk.IconThemeGetForDisplay(gdk.DisplayGetDefault())
	defer theme.Unref()
	for _, n := range []string{name, strings.ToLower(name)} {
		if n != "" && theme.HasIcon(n) {
			img.SetFromIconName(n)
			return
		}
	}
	img.SetFromIconName("application-x-executable")
}

// freshenRates gives the launcher's answers exchange rates: the cached
// ones at once, and new ones when those are stale (convert.Rates.Stale).
func (s *shellUI) freshenRates() {
	l := &s.launcher
	if l.rates == nil {
		if r := convert.LoadRates(convert.CachePath()); r != nil {
			l.rates = r
			l.st.SetRates(r)
		}
	}
	if l.fetching || !l.rates.Stale(time.Now()) {
		return
	}
	l.fetching = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		r, err := convert.FetchRates(ctx, &http.Client{})
		cancel()
		if err == nil {
			if err := convert.SaveRates(convert.CachePath(), r); err != nil {
				log.Printf("rates: %v", err)
			}
		}
		gtkx.Post(func() {
			l.fetching = false
			if err != nil {
				log.Printf("rates: %v", err)
				return
			}
			l.rates = r
			l.st.SetRates(r)
			if l.st.Open {
				s.syncLauncher()
			}
		})
	}()
}
