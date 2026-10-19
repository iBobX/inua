// Package termui is inua-term's window (docs/feat/inua-term.md). This is
// the first spike: one pane, a widget of its own drawing the engine's
// screen in snapshot.
package termui

import (
	"sync"

	"codeberg.org/puregotk/purego"
	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"

	"github.com/lyricat/inua/internal/gtkx"
)

// GTK's own call that puregotk does not bind in a form purego can make:
// the controller's current event (puregotk's wrapper refs a GdkEvent as a
// GObject).
var gobj struct {
	once         sync.Once
	currentEvent func(controller uintptr) uintptr
}

func loadGObject() {
	gobj.once.Do(func() {
		g, err := purego.Dlopen("libgtk-4.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			panic("termui: " + err.Error())
		}
		purego.RegisterLibFunc(&gobj.currentEvent, g, "gtk_event_controller_get_current_event")
	})
}

// newPaneWidget makes a pane's widget, drawn by it.
func newPaneWidget(p *termPane) *gtk.Widget {
	loadGObject()
	return gtkx.NewCanvas(&gtkx.Canvas{Snapshot: p.snapshot, Allocate: p.allocate})
}

func forgetPaneWidget(w *gtk.Widget) { gtkx.ForgetCanvas(w) }

// Drawing helpers: GSK takes these by pointer.
func rect(x, y, w, h float32) *graphene.Rect {
	var r graphene.Rect
	r.Init(x, y, w, h)
	return &r
}

func rgba(hex string) gdk.RGBA {
	var c gdk.RGBA
	if len(hex) == 7 && hex[0] == '#' {
		v := func(i int) float32 {
			var n int
			for _, ch := range hex[i : i+2] {
				n <<= 4
				switch {
				case ch >= '0' && ch <= '9':
					n |= int(ch - '0')
				case ch >= 'a' && ch <= 'f':
					n |= int(ch-'a') + 10
				case ch >= 'A' && ch <= 'F':
					n |= int(ch-'A') + 10
				}
			}
			return float32(n) / 255
		}
		c = gdk.RGBA{Red: v(1), Green: v(3), Blue: v(5), Alpha: 1}
	}
	return c
}

// cellSize is a monospace font's cell in pixels.
func cellSize(w *gtk.Widget, font *pango.FontDescription) (cw, ch int) {
	l := w.CreatePangoLayout("M")
	l.SetFontDescription(font)
	var x, y int32
	l.GetPixelSize(&x, &y)
	l.Unref()
	return max(int(x), 1), max(int(y), 1)
}
