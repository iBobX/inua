// Package termui is inua-term's window (docs/feat/inua-term.md). This is
// the first spike: one pane, a widget of its own drawing the engine's
// screen in snapshot.
package termui

import (
	"encoding/binary"
	"sync"

	"codeberg.org/puregotk/purego"
	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gsk"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/term/vt"
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

// A line is drawn once, to a render node kept while the line shows:
// scrolling then draws only the lines coming in, and a program redrawing
// a spinner only the lines it changed. Shaping a run's text is most of a
// frame's time (a screen of an agent's coloured output, every run of
// every row, took 36 ms a frame). The nodes are the pane's colours and
// font: dropLines forgets them when those change.

// drawnLine is a line's node, nil for one with nothing to draw.
type drawnLine struct {
	node *gsk.RenderNode
	used bool // shown in the frame being drawn
}

func (l *drawnLine) free() {
	if l.node != nil {
		l.node.Unref()
	}
}

// lineNode is a line's node, drawn now unless the same line was drawn
// before.
func (p *termPane) lineNode(w *gtk.Widget, runs []vt.Run, cw, ch float32) *gsk.RenderNode {
	key := lineKey(runs)
	if l, ok := p.drawn[key]; ok {
		l.used = true
		return l.node
	}
	s := gtk.NewSnapshot()
	for _, r := range runs {
		p.drawRun(w, s, r, cw, ch)
	}
	l := &drawnLine{node: s.FreeToNode(), used: true}
	if p.drawn == nil {
		p.drawn = map[string]*drawnLine{}
	}
	p.drawn[key] = l
	return l.node
}

// lineKey is what a line draws: each run's place, colours, attributes
// and text.
func lineKey(runs []vt.Run) string {
	n := 0
	for _, r := range runs {
		n += 20 + len(r.Text)
	}
	b := make([]byte, 0, n)
	for _, r := range runs {
		b = binary.LittleEndian.AppendUint32(b, uint32(r.X))
		b = binary.LittleEndian.AppendUint32(b, uint32(r.Cells))
		b = binary.LittleEndian.AppendUint32(b, r.FG)
		b = binary.LittleEndian.AppendUint32(b, r.BG)
		b = binary.LittleEndian.AppendUint16(b, r.Attrs)
		b = binary.LittleEndian.AppendUint16(b, uint16(len(r.Text)))
		b = append(b, r.Text...)
	}
	return string(b)
}

// dropLines forgets the lines drawn, for new colours or a new font.
func (p *termPane) dropLines() {
	for _, l := range p.drawn {
		l.free()
	}
	p.drawn = nil
}

// drawRun draws a run of a line, the line's top at 0.
func (p *termPane) drawRun(w *gtk.Widget, s *gtk.Snapshot, r vt.Run, cw, ch float32) {
	fg, bg := p.color(r.FG, p.fg), p.color(r.BG, p.bg)
	if r.Attrs&vt.Inverse != 0 {
		fg, bg = bg, fg
	}
	switch {
	case r.Attrs&vt.MatchCurrent != 0:
		bg, fg = p.warn, p.bg
	case r.Attrs&vt.Selected != 0:
		bg = p.cursor
		bg.Alpha = 0.3
	case r.Attrs&vt.Match != 0:
		bg = p.warn
		bg.Alpha = 0.3
	}
	if r.BG != vt.DefaultColor || r.Attrs&(vt.Inverse|vt.Selected|vt.Match) != 0 {
		s.AppendColor(&bg, rect(float32(r.X)*cw, 0, float32(r.Cells)*cw, ch))
	}
	if r.Attrs&vt.Invisible != 0 || r.Text == "" {
		return
	}
	if r.Attrs&vt.Faint != 0 {
		fg.Alpha = 0.6
	}
	font := 0
	if r.Attrs&vt.Bold != 0 {
		font |= 1
	}
	if r.Attrs&vt.Italic != 0 {
		font |= 2
	}
	l := w.CreatePangoLayout(r.Text)
	l.SetFontDescription(p.fonts[font])
	s.Save()
	s.Translate(&graphene.Point{X: float32(r.X) * cw})
	s.AppendLayout(l, &fg)
	if r.Attrs&vt.Underline != 0 {
		s.AppendColor(&fg, rect(0, ch-2, float32(r.Cells)*cw, 1))
	}
	if r.Attrs&vt.Strike != 0 {
		s.AppendColor(&fg, rect(0, ch/2, float32(r.Cells)*cw, 1))
	}
	s.Restore()
	l.Unref()
}

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

// rgbaOf is a colour 0xRRGGBB.
func rgbaOf(v uint32) gdk.RGBA {
	b := func(s uint) float32 { return float32(v>>s&0xff) / 255 }
	return gdk.RGBA{Red: b(16), Green: b(8), Blue: b(0), Alpha: 1}
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
