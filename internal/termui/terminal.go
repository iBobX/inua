package termui

import (
	"log"
	"sync"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/term/vt"
)

// pad is the space around a pane's cells, in pixels.
const pad = 8

// fontName is the terminal's font, as the shell's labels have it.
const fontName = "Geist Mono, IBM Plex Mono, monospace 11"

// backend is where a pane's program is: the host, through a client.
type backend interface {
	Input(b []byte) error
	Resize(cols, rows int) error
}

// termPane is one terminal: the window's own engine for drawing, and the
// program on the host, drawn by its widget. The engine is touched on the
// main thread only.
type termPane struct {
	id     int // the host's
	widget *gtk.Widget
	term   *vt.Terminal
	be     backend
	font   *pango.FontDescription
	fonts  [4]*pango.FontDescription // by bold and italic
	fg, bg gdk.RGBA
	cursor gdk.RGBA
	cw, ch int
	cols   int
	rows   int
	exited bool
	lines  [][]vt.Run // the screen's rows as the engine last said
	cur    vt.Cursor

	mu        sync.Mutex
	pending   []byte // read from the PTY, not yet fed to the engine
	scheduled bool

	onKey func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
}

func newTermPane(be backend, cols, rows int, colors map[string]string) (*termPane, error) {
	p := &termPane{
		font:   pango.FontDescriptionFromString(fontName),
		fg:     rgba(colors["t0"]),
		bg:     rgba(colors["app"]),
		cursor: rgba(colors["accent"]),
		be:     be,
		cols:   cols,
		rows:   rows,
	}
	for i := range p.fonts {
		f := pango.FontDescriptionFromString(fontName)
		if i&1 != 0 {
			f.SetWeight(pango.WeightBoldValue)
		}
		if i&2 != 0 {
			f.SetStyle(pango.StyleItalicValue)
		}
		p.fonts[i] = f
	}
	var err error
	if p.term, err = vt.New(p.cols, p.rows); err != nil {
		return nil, err
	}
	p.widget = newPaneWidget(p)
	p.widget.SetFocusable(true)
	p.widget.SetHexpand(true)
	p.widget.SetVexpand(true)
	keys := gtk.NewEventControllerKey()
	p.onKey = func(_ gtk.EventControllerKey, keyval, _ uint32, state gdk.ModifierType) bool {
		if b := keyBytes(keyval, state); b != nil && !p.exited {
			if err := p.be.Input(b); err != nil {
				log.Printf("term: input: %v", err)
			}
			return true
		}
		return false
	}
	keys.ConnectKeyPressed(&p.onKey)
	p.widget.AddController(&keys.EventController)
	return p, nil
}

// feed takes the program's output on the client's goroutine and hands it
// to the main thread, a burst at a time.
func (p *termPane) feed(b []byte) {
	p.mu.Lock()
	p.pending = append(p.pending, b...)
	post := !p.scheduled
	p.scheduled = true
	p.mu.Unlock()
	if post {
		gtkx.Post(p.drain)
	}
}

// drain feeds what was read to the engine and redraws once.
func (p *termPane) drain() {
	p.mu.Lock()
	b := p.pending
	p.pending, p.scheduled = nil, false
	p.mu.Unlock()
	p.term.Write(b)
	p.update(false)
}

// update takes the rows that changed from the engine and redraws.
func (p *termPane) update(all bool) {
	f, err := p.term.Frame(all)
	if err != nil {
		log.Printf("term: %v", err)
		return
	}
	if len(p.lines) != f.Rows {
		p.lines = make([][]vt.Run, f.Rows)
	}
	for _, l := range f.Lines {
		if l.Y < len(p.lines) {
			p.lines[l.Y] = l.Runs
		}
	}
	p.cur = f.Cursor
	p.widget.QueueDraw()
}

func (p *termPane) exit(code int) {
	if p.exited {
		return
	}
	p.drain()
	p.exited = true
	p.term.Write([]byte("\r\n\x1b[2m[exited " + itoa(code) + "]\x1b[0m"))
	p.update(false)
}

func (p *termPane) allocate(w *gtk.Widget, width, height int) {
	p.cw, p.ch = cellSize(w, p.font)
	cols, rows := max((width-2*pad)/p.cw, 2), max((height-2*pad)/p.ch, 1)
	if cols == p.cols && rows == p.rows {
		return
	}
	p.cols, p.rows = cols, rows
	if err := p.term.Resize(cols, rows); err != nil {
		log.Printf("term: %v", err)
	}
	if !p.exited {
		if err := p.be.Resize(cols, rows); err != nil {
			log.Printf("term: resize: %v", err)
		}
	}
	p.update(true)
}

func (p *termPane) snapshot(w *gtk.Widget, s *gtk.Snapshot) {
	s.AppendColor(&p.bg, rect(0, 0, float32(w.GetWidth()), float32(w.GetHeight())))
	if p.cw == 0 {
		p.cw, p.ch = cellSize(w, p.font)
	}
	cw, ch := float32(p.cw), float32(p.ch)
	s.Save()
	s.Translate(&graphene.Point{X: pad, Y: pad})
	for y, runs := range p.lines {
		for _, r := range runs {
			fg, bg := p.color(r.FG, p.fg), p.color(r.BG, p.bg)
			if r.Attrs&vt.Inverse != 0 {
				fg, bg = bg, fg
			}
			if r.BG != vt.DefaultColor || r.Attrs&vt.Inverse != 0 {
				s.AppendColor(&bg, rect(float32(r.X)*cw, float32(y)*ch, float32(r.Cells)*cw, ch))
			}
			if r.Attrs&vt.Invisible != 0 || r.Text == "" {
				continue
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
			s.Translate(&graphene.Point{X: float32(r.X) * cw, Y: float32(y) * ch})
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
	}
	if !p.exited && p.cur.Visible {
		c := p.cursor
		x, y := float32(p.cur.X)*cw, float32(p.cur.Y)*ch
		switch p.cur.Style {
		case vt.CursorBar:
			s.AppendColor(&c, rect(x, y, 2, ch))
		case vt.CursorUnderline:
			s.AppendColor(&c, rect(x, y+ch-2, cw, 2))
		default:
			c.Alpha = 0.55
			s.AppendColor(&c, rect(x, y, cw, ch))
		}
	}
	s.Restore()
}

// color is a run's colour, or the pane's for the terminal's default.
func (p *termPane) color(v uint32, def gdk.RGBA) gdk.RGBA {
	if v == vt.DefaultColor {
		return def
	}
	return gdk.RGBA{Red: float32(v>>16&0xff) / 255, Green: float32(v>>8&0xff) / 255, Blue: float32(v&0xff) / 255, Alpha: 1}
}

func (p *termPane) close() {
	p.term.Close()
	forgetPaneWidget(p.widget)
}

// keyBytes is a key press as the bytes a program reads, for the spike:
// text, Ctrl+letter, and the keys a shell needs. The real encoding is the
// engine's key encoder (kitty keyboard protocol), next.
func keyBytes(keyval uint32, state gdk.ModifierType) []byte {
	switch int32(keyval) {
	case gdk.KEY_Return, gdk.KEY_KP_Enter:
		return []byte{'\r'}
	case gdk.KEY_BackSpace:
		return []byte{0x7f}
	case gdk.KEY_Tab:
		return []byte{'\t'}
	case gdk.KEY_Escape:
		return []byte{0x1b}
	case gdk.KEY_Up:
		return []byte("\x1b[A")
	case gdk.KEY_Down:
		return []byte("\x1b[B")
	case gdk.KEY_Right:
		return []byte("\x1b[C")
	case gdk.KEY_Left:
		return []byte("\x1b[D")
	case gdk.KEY_Home:
		return []byte("\x1b[H")
	case gdk.KEY_End:
		return []byte("\x1b[F")
	case gdk.KEY_Delete:
		return []byte("\x1b[3~")
	case gdk.KEY_Page_Up:
		return []byte("\x1b[5~")
	case gdk.KEY_Page_Down:
		return []byte("\x1b[6~")
	}
	r := rune(gdk.KeyvalToUnicode(keyval))
	if r == 0 {
		return nil
	}
	if state&gdk.ControlMaskValue != 0 && state&gdk.ShiftMaskValue == 0 {
		switch {
		case r >= 'a' && r <= 'z':
			return []byte{byte(r - 'a' + 1)}
		case r >= '@' && r <= '_':
			return []byte{byte(r - '@')}
		}
	}
	b := []byte(string(r))
	if state&gdk.AltMaskValue != 0 {
		b = append([]byte{0x1b}, b...)
	}
	return b
}

func itoa(n int) string {
	if n < 0 {
		return "-" + itoa(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
