package termui

import (
	"cmp"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/host"
)

// App is inua-term's window, a client of the host. For now: one pane, the
// first the host has, else a new one running the user's shell.
type App struct {
	c      *appui.Ctx
	win    *gtk.ApplicationWindow
	client *host.Client
	id     int // the pane shown
	pane   *termPane

	// shown is the pane for the client's goroutine, which delivers output.
	mu    sync.Mutex
	shown *termPane

	onClose func(gtk.Window) bool
}

// Register makes inua-term an appui app, "term".
func Register() {
	appui.Register("term", "Term", i18n.M("Terminal"), func() appui.View { return &App{} })
}

func (a *App) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	a.c = c
	a.win = gtk.NewApplicationWindow(c.App)
	a.win.SetTitle(i18n.T("Terminal"))
	a.win.SetDefaultSize(960, 620)
	a.onClose = func(gtk.Window) bool {
		a.closeWindow()
		return false
	}
	a.win.ConnectCloseRequest(&a.onClose)
	// The host may need starting: off the main thread, the pane after.
	go func() {
		err := a.connect()
		gtkx.Post(func() {
			if err != nil {
				a.fail(err)
			}
		})
	}()
	return a.win
}

// connect reaches the host, starting it if none runs, and shows a pane.
func (a *App) connect() error {
	path := host.SocketPath()
	client, err := host.Dial(path, a.output, a.event)
	if err != nil {
		if err := startHost(); err != nil {
			return err
		}
		for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(20 * time.Millisecond) {
			if client, err = host.Dial(path, a.output, a.event); err == nil || time.Now().After(deadline) {
				break
			}
		}
		if err != nil {
			return fmt.Errorf("the terminal host did not start: %w", err)
		}
	}
	a.client = client
	panes, err := client.List()
	if err != nil {
		return err
	}
	cols, rows := 80, 24
	id := 0
	for _, p := range panes {
		if !p.Exited {
			id, cols, rows = p.ID, p.Cols, p.Rows
			break
		}
	}
	if id == 0 {
		home, _ := os.UserHomeDir()
		shell := cmp.Or(os.Getenv("SHELL"), "/bin/sh")
		env := append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=inua-term")
		if id, err = client.Create([]string{shell}, home, env, cols, rows); err != nil {
			return err
		}
	}
	ready := make(chan error, 1)
	var colors host.Colors
	gtkx.Post(func() {
		err := a.show(id, cols, rows)
		if err == nil {
			colors = a.pane.colors()
		}
		ready <- err
	})
	if err := <-ready; err != nil {
		return err
	}
	if err := client.SetColors(colors); err != nil {
		log.Printf("term: colors: %v", err)
	}
	return client.Attach(id) // the replay arrives through output first
}

// show makes the pane's widget.
func (a *App) show(id, cols, rows int) error {
	p, err := newTermPane(paneBackend{a.client, id}, cols, rows, a.c.Colors)
	if err != nil {
		return err
	}
	p.id = id
	a.id, a.pane = id, p
	a.mu.Lock()
	a.shown = p
	a.mu.Unlock()
	a.win.SetChild(&p.root.Widget)
	p.widget.GrabFocus()
	return nil
}

// paneFor is the pane shown, if it is the host's pane id.
func (a *App) paneFor(id int) *termPane {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shown != nil && a.shown.id == id {
		return a.shown
	}
	return nil
}

func (a *App) output(id int, b []byte) {
	if p := a.paneFor(id); p != nil {
		p.feed(b)
	}
}

func (a *App) event(m host.Message) {
	p := a.paneFor(m.Pane)
	if p == nil || m.Code == nil {
		return
	}
	code := *m.Code
	gtkx.Post(func() { p.exit(code) })
}

func (a *App) fail(err error) {
	log.Printf("term: %v", err)
	label := gtk.NewLabel(err.Error())
	label.SetWrap(true)
	a.win.SetChild(&label.Widget)
}

// closeWindow leaves the pane running on the host, unless its program
// has ended.
func (a *App) closeWindow() {
	if a.pane != nil {
		if a.pane.exited && a.client != nil {
			a.client.ClosePane(a.id)
		}
		a.pane.close()
		a.pane = nil
		a.mu.Lock()
		a.shown = nil
		a.mu.Unlock()
	}
	if a.client != nil {
		a.client.Close()
	}
}

func (a *App) ThemeChanged() {
	if a.pane == nil {
		return
	}
	a.pane.fg, a.pane.bg, a.pane.cursor = rgba(a.c.Colors["t0"]), rgba(a.c.Colors["app"]), rgba(a.c.Colors["accent"])
	a.pane.widget.QueueDraw()
	if a.client != nil {
		client, colors := a.client, a.pane.colors()
		go func() {
			if err := client.SetColors(colors); err != nil {
				log.Printf("term: colors: %v", err)
			}
		}()
	}
}

// paneBackend is a pane on the host.
type paneBackend struct {
	client *host.Client
	id     int
}

func (b paneBackend) Input(p []byte) error { return b.client.Input(b.id, p) }
func (b paneBackend) Resize(cols, rows, cellW, cellH int) error {
	return b.client.Resize(b.id, cols, rows, cellW, cellH)
}

// startHost runs `inua-term --host` in a session of its own, so it
// outlives this window.
func startHost() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	cmd := exec.Command(exe, "--host")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = null, null, null
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // reaped if it ends while the window runs
	return nil
}
