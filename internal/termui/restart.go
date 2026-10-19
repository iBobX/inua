package termui

import (
	"time"

	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/agent"
	"github.com/lyricat/inua/internal/term/host"
)

// Restarting the terminal host (the palette's "Restart the terminal
// host"), as for a new build of it: the host keeps its sessions and
// ends, every pane's program with it, and the window starts a new one,
// which brings the sessions back, each pane's last command at its prompt.

// restartHost asks first: every program in the panes ends.
func (a *App) restartHost() {
	if a.sheet != nil || a.client == nil {
		return
	}
	busy := 0
	for _, x := range a.agents() {
		if st := agent.State(a.info[x.pane].State); st == agent.Working || st == agent.Blocked {
			busy++
		}
	}
	sheet, done := a.sheetFrame(i18n.T("Restart the terminal host?"), 440)
	why := gtk.NewLabel(i18n.T("Every program in the panes ends. The sessions come back, each pane's last command at its prompt, not run; agents come back as their resume command."))
	if busy > 0 {
		why.SetText(why.GetText() + " " + i18n.Nf("%d agent is working or waiting for you.", "%d agents are working or waiting for you.", busy, busy))
	}
	why.AddCssClass("term-sheet-why")
	why.SetXalign(0)
	why.SetWrap(true)
	sheet.Append(&why.Widget)
	buttons := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	buttons.SetHalign(gtk.AlignEndValue)
	cancel := gtk.NewButtonWithLabel(i18n.T("Cancel"))
	cancel.AddCssClass("outlined")
	ok := gtk.NewButtonWithLabel(i18n.T("Restart"))
	ok.AddCssClass("destructive-action")
	buttons.Append(&cancel.Widget)
	buttons.Append(&ok.Widget)
	sheet.Append(&buttons.Widget)
	a.onCloseCancel = func(gtk.Button) { done() }
	a.onCloseOK = func(gtk.Button) {
		done()
		a.restarting = true
		a.ask("quit", func(c *host.Client) error {
			err := c.Quit()
			if err != nil {
				// A host older than the quit message: it runs on.
				gtkx.Post(func() { a.restarting = false })
			}
			return err
		})
	}
	cancel.ConnectClicked(&a.onCloseCancel)
	ok.ConnectClicked(&a.onCloseOK)
	cancel.GrabFocus()
}

// hostEnded follows the host's end when it was restarted: the old panes
// go, and a moment later (its socket let go) a new host starts.
func (a *App) hostEnded() {
	a.restarting = false
	for id := range a.tiles {
		a.dropTile(id)
	}
	client := a.client
	a.client, a.state, a.info = nil, nil, map[int]host.PaneInfo{}
	client.Close()
	a.restartWait = func(uintptr) bool {
		a.start()
		return false
	}
	glib.TimeoutAdd(uint32(restartPause/time.Millisecond), &a.restartWait, 0)
}

// restartPause is how long the window waits for the old host to be gone.
const restartPause = 300 * time.Millisecond
