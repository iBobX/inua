package termui

import (
	"fmt"

	"github.com/lyricat/inua/internal/term/host"
	"github.com/lyricat/inua/internal/term/settings"
	"github.com/lyricat/inua/internal/term/vt"
)

// The settings' look of the terminals: the font and its size (Ctrl+=,
// Ctrl+- and Ctrl+0 change it in this window), the cursor and its blink,
// copying on select, scrollback.

// fontSteps is how far Ctrl+= and Ctrl+- go each time, in points.
const fontStep = 1

// useSettings takes term.toml: the keys now, the look for every pane,
// the window's theme, the terminals' colours, and the shell for new
// panes.
func (a *App) useSettings(s settings.Settings) {
	a.set, a.keymap = s, keymapOf(s.Keys)
	a.cols.tips(a)
	a.updateNerd()
	l := a.look()
	for _, t := range a.tiles {
		t.pane.look = l
		t.pane.setFont(l.Font)
		t.pane.term.SetDefaults(l.Scrollback, l.Cursor, l.Blink)
	}
	switch {
	case s.Look.Theme != a.used.Theme:
		a.c.ReloadStyle() // the colours follow, through ThemeChanged
	case s.Look.Scheme != a.used.Scheme:
		a.schemeChanged()
	}
	a.used = s.Look
	if prog := s.Shell.Program; prog != a.usedShell {
		a.usedShell = prog
		a.ask("shell", func(c *host.Client) error { return c.SetShell(prog) })
	}
}

// look is the panes' look from the settings.
func (a *App) look() paneLook {
	s := a.set
	cursor := vt.CursorBlock
	switch s.Cursor.Style {
	case "bar":
		cursor = vt.CursorBar
	case "underline":
		cursor = vt.CursorUnderline
	}
	size := max(s.Font.Size+a.fontStep, 6)
	return paneLook{
		Font:         fmt.Sprintf("%s, IBM Plex Mono, monospace %g", s.Font.Family, size),
		Cursor:       cursor,
		Blink:        s.Cursor.Blink,
		CopyOnSelect: s.Terminal.CopyOnSelect,
		Scrollback:   s.Terminal.Scrollback,
	}
}

// zoomFont makes the font larger (1), smaller (-1) or as set (0).
func (a *App) zoomFont(dir int) {
	if dir == 0 {
		a.fontStep = 0
	} else {
		a.fontStep += float64(dir * fontStep)
	}
	font := a.look().Font
	for _, t := range a.tiles {
		t.pane.setFont(font)
	}
}

// startBlink blinks the focused pane's cursor, when it should, every
// 530 ms (GTK's own rate).
func (a *App) startBlink() {
	a.onBlink = func(uintptr) bool {
		s := a.shown()
		if s == nil {
			return true
		}
		if t := a.tiles[s.Focus]; t != nil && t.pane.cur.Blink {
			t.pane.blinkOff = !t.pane.blinkOff
			t.pane.widget.QueueDraw()
		}
		return true
	}
	glibTimeout(530, &a.onBlink)
}
