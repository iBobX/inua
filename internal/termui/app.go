package termui

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/host"
	"github.com/lyricat/inua/internal/term/model"
)

// App is inua-term's window, a client of the host: the workspaces in
// column 1, the shown workspace's sessions in column 2, and the shown
// session's panes tiled in column 3. What it shows is the host's State;
// it asks the host for every change and draws the State that comes back.
type App struct {
	c      *appui.Ctx
	win    *gtk.ApplicationWindow
	client *host.Client

	state *host.State
	info  map[int]host.PaneInfo
	tiles map[int]*tile // the shown session's panes

	// live is the panes with a widget, for the client's goroutine, which
	// delivers their output.
	mu   sync.Mutex
	live map[int]*termPane

	cols   columns
	stage  *gtk.Overlay
	tilesW *gtk.Box
	tiling tiling
	sheet  *gtk.Box // the rename sheet, while it asks
	// swapping is set after Ctrl+Shift+X, until the arrow.
	swapping bool

	onClose                    func(gtk.Window) bool
	onFolder                   gio.AsyncReadyCallback
	onRenameOK, onRenameCancel func(gtk.Button)
	onRenameActivate           func(gtk.Entry)
	onRenameKey                func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
}

// Register makes inua-term an appui app, "term".
func Register() {
	appui.Register("term", "Term", i18n.M("Terminal"), func() appui.View { return &App{} })
}

func (a *App) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	a.c = c
	a.info, a.tiles, a.live = map[int]host.PaneInfo{}, map[int]*tile{}, map[int]*termPane{}
	a.win = &gtk.ApplicationWindow{}
	ui := c.Load("term.ui")
	ui.Get("term", a.win)
	a.buildColumns(ui)
	a.stage, a.tilesW = &gtk.Overlay{}, &gtk.Box{}
	ui.Get("stage", a.stage)
	ui.Get("tiles", a.tilesW)
	ui.Close()
	a.win.SetTitle(i18n.T("Terminal"))
	a.buildTiling()
	a.onClose = func(gtk.Window) bool {
		a.closeWindow()
		return false
	}
	a.win.ConnectCloseRequest(&a.onClose)
	// The host may need starting: off the main thread, the state after.
	go func() {
		st, err := a.connect()
		gtkx.Post(func() {
			if err != nil {
				a.fail(err)
				return
			}
			a.apply(st)
		})
	}()
	return a.win
}

// connect reaches the host, starting it if none runs, and opens the home
// folder's workspace when there is none.
func (a *App) connect() (*host.State, error) {
	path := host.SocketPath()
	client, err := host.Dial(path, a.output, a.event)
	if err != nil {
		if err := startHost(); err != nil {
			return nil, err
		}
		for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(20 * time.Millisecond) {
			if client, err = host.Dial(path, a.output, a.event); err == nil || time.Now().After(deadline) {
				break
			}
		}
		if err != nil {
			return nil, fmt.Errorf("the terminal host did not start: %w", err)
		}
	}
	ready := make(chan host.Colors, 1)
	gtkx.Post(func() {
		a.client = client
		ready <- a.colors()
	})
	if err := client.SetColors(<-ready); err != nil {
		log.Printf("term: colors: %v", err)
	}
	go func() {
		<-client.Done()
		gtkx.Post(func() {
			if a.client == client {
				a.win.Close() // the host ended: its last pane closed
			}
		})
	}()
	st, err := client.State()
	if err != nil {
		return nil, err
	}
	if len(st.Model.Workspaces) == 0 {
		home, _ := os.UserHomeDir()
		if _, err := client.Open(home, ""); err != nil {
			return nil, err
		}
		return client.State()
	}
	if st.Model.Shown.Workspace == 0 {
		client.Show(st.Model.Workspaces[0].ID, 0)
		return client.State()
	}
	return st, nil
}

// ask sends the host a request off the main thread; a failure shows in
// the log.
func (a *App) ask(what string, f func(*host.Client) error) {
	client := a.client
	if client == nil {
		return
	}
	go func() {
		if err := f(client); err != nil {
			log.Printf("term: %s: %v", what, err)
		}
	}()
}

// paneFor is a pane with a widget, for the client's goroutine.
func (a *App) paneFor(id int) *termPane {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.live[id]
}

func (a *App) output(id int, b []byte) {
	if p := a.paneFor(id); p != nil {
		p.feed(b)
	}
}

// event is a message from the host, on the client's goroutine.
func (a *App) event(m host.Message) {
	switch {
	case m.Type == host.MsgState && m.State != nil:
		st := m.State
		gtkx.Post(func() { a.apply(st) })
	case m.Type == host.MsgEvent:
		gtkx.Post(func() { a.paneEvent(m) })
	}
}

// paneEvent is a pane's title, folder, exit or clipboard write.
func (a *App) paneEvent(m host.Message) {
	info, ok := a.info[m.Pane]
	if !ok {
		return
	}
	if m.Title != "" {
		info.Title = m.Title
	}
	if m.Dir != "" {
		info.Dir = m.Dir
	}
	if m.Code != nil {
		info.Exited, info.Code = true, *m.Code
	}
	a.info[m.Pane] = info
	if t := a.tiles[m.Pane]; t != nil {
		if m.Code != nil {
			t.pane.exit(*m.Code)
		}
		if m.Clipboard != nil {
			t.pane.setClipboard(*m.Clipboard, m.Primary)
		}
		t.update(info)
	}
	if m.Title != "" || m.Dir != "" || m.Code != nil {
		a.cols.update(a)
	}
}

// apply shows a State: the columns, and the shown session's tiles.
func (a *App) apply(st *host.State) {
	a.state = st
	a.info = map[int]host.PaneInfo{}
	for _, p := range st.Panes {
		a.info[p.ID] = p
	}
	a.cols.update(a)
	a.syncTiles()
	if s := a.shown(); s != nil {
		a.win.SetTitle(s.Name + " — " + i18n.T("Terminal"))
	} else {
		a.win.SetTitle(i18n.T("Terminal"))
	}
}

// shown is the session on screen, nil for none.
func (a *App) shown() *model.Session {
	if a.state == nil {
		return nil
	}
	s, _ := a.state.Model.Session(a.state.Model.Shown.Session)
	return s
}

// shownWorkspace is the workspace on screen, nil for none.
func (a *App) shownWorkspace() *model.Workspace {
	if a.state == nil {
		return nil
	}
	return a.state.Model.Workspace(a.state.Model.Shown.Workspace)
}

func (a *App) fail(err error) {
	log.Printf("term: %v", err)
	label := gtk.NewLabel(err.Error())
	label.SetWrap(true)
	label.AddCssClass("term-error")
	a.stage.SetChild(&label.Widget)
}

// closeWindow leaves the panes running on the host.
func (a *App) closeWindow() {
	for id := range a.tiles {
		a.dropTile(id)
	}
	if a.client != nil {
		client := a.client
		a.client = nil
		client.Close()
	}
}

func (a *App) ThemeChanged() {
	for _, t := range a.tiles {
		t.pane.setTheme(a.c.Colors)
	}
	colors := a.colors()
	a.ask("colors", func(c *host.Client) error { return c.SetColors(colors) })
}

// colors are the theme's, for programs that ask.
func (a *App) colors() host.Colors {
	v := func(name string) uint32 {
		c := rgba(a.c.Colors[name])
		b := func(f float32) uint32 { return uint32(f*255+0.5) & 0xff }
		return b(c.Red)<<16 | b(c.Green)<<8 | b(c.Blue)
	}
	return host.Colors{FG: v("t0"), BG: v("app"), Cursor: v("accent")}
}

// paneBackend is a pane on the host.
type paneBackend struct {
	app *App
	id  int
}

func (b paneBackend) Input(p []byte) error {
	if b.app.client == nil {
		return nil
	}
	return b.app.client.Input(b.id, p)
}

func (b paneBackend) Resize(cols, rows, cellW, cellH int) error {
	b.app.ask("resize", func(c *host.Client) error { return c.Resize(b.id, cols, rows, cellW, cellH) })
	return nil
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
