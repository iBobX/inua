package termui

import (
	"time"
	"unicode"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/vt"
)

// Copy mode (Ctrl+Shift+Space) moves a cursor of its own over the screen
// and the scrollback with vi's keys, and selects from where v (by
// character) or V (by line) was pressed; y copies and leaves, Escape or q
// leaves. The keys are the mode's while it lasts, not the program's.

type copyMode struct {
	x, y   int // the copy cursor, in the viewport
	anchor *[2]int
	lines  bool // V: whole lines
	badge  *gtk.Label
	seq    int // the selection's presses so far
}

func (p *termPane) startCopyMode() {
	if p.copy != nil {
		return
	}
	c := &copyMode{x: p.cur.X, y: p.cur.Y}
	if !p.cur.Visible {
		c.x, c.y = 0, max(p.rows-1, 0)
	}
	c.badge = gtk.NewLabel(i18n.T("COPY"))
	c.badge.AddCssClass("term-copy-badge")
	c.badge.SetHalign(gtk.AlignEndValue)
	c.badge.SetValign(gtk.AlignEndValue)
	c.badge.SetMarginEnd(12)
	c.badge.SetMarginBottom(10)
	p.root.AddOverlay(&c.badge.Widget)
	p.copy = c
	p.widget.QueueDraw()
}

func (p *termPane) endCopyMode() {
	if p.copy == nil {
		return
	}
	p.root.RemoveOverlay(&p.copy.badge.Widget)
	p.copy = nil
	p.term.ClearSelection()
	p.selected = false
	p.term.Scroll(vt.ScrollBottom, 0)
	p.update(false)
}

// copyKey is a key in copy mode; it is always the mode's.
func (p *termPane) copyKey(keyval uint32, state gdk.ModifierType) bool {
	c := p.copy
	ctrl := state&gdk.ControlMaskValue != 0
	half := max(p.rows/2, 1)
	switch k := int32(keyval); {
	case k == gdk.KEY_Escape || k == gdk.KEY_q:
		p.endCopyMode()
		return true
	case k == gdk.KEY_y || k == gdk.KEY_Return:
		if p.selected {
			p.copySelection(true)
		}
		p.endCopyMode()
		return true
	case k == gdk.KEY_v && !ctrl:
		c.toggle(false)
	case k == gdk.KEY_V:
		c.toggle(true)
	case k == gdk.KEY_h || k == gdk.KEY_Left:
		c.x = max(c.x-1, 0)
	case k == gdk.KEY_l || k == gdk.KEY_Right:
		c.x = min(c.x+1, p.cols-1)
	case k == gdk.KEY_j || k == gdk.KEY_Down:
		p.copyMove(1)
	case k == gdk.KEY_k || k == gdk.KEY_Up:
		p.copyMove(-1)
	case ctrl && k == gdk.KEY_d:
		p.copyMove(half)
	case ctrl && k == gdk.KEY_u:
		p.copyMove(-half)
	case k == gdk.KEY_0 || k == gdk.KEY_Home:
		c.x = 0
	case k == gdk.KEY_dollar || k == gdk.KEY_End:
		c.x = max(len(p.rowText(c.y))-1, 0)
	case k == gdk.KEY_w:
		c.x = nextWord(p.rowText(c.y), c.x)
	case k == gdk.KEY_b:
		c.x = prevWord(p.rowText(c.y), c.x)
	case k == gdk.KEY_g:
		p.term.Scroll(vt.ScrollTop, 0)
		c.y, c.x = 0, 0
	case k == gdk.KEY_G:
		p.term.Scroll(vt.ScrollBottom, 0)
		c.y = max(p.rows-1, 0)
	default:
		return true // the mode's, doing nothing
	}
	p.copySelect()
	return true
}

func (c *copyMode) toggle(lines bool) {
	if c.anchor != nil && c.lines == lines {
		c.anchor = nil
		return
	}
	c.anchor, c.lines = &[2]int{c.x, c.y}, lines
}

// copyMove moves the copy cursor by rows, scrolling at the edges.
func (p *termPane) copyMove(n int) {
	c := p.copy
	y := c.y + n
	switch {
	case y < 0:
		p.term.Scroll(vt.ScrollRows, y)
		if c.anchor != nil {
			c.anchor[1] -= y // the anchor moves down with the text
		}
		y = 0
	case y >= p.rows:
		over := y - p.rows + 1
		p.term.Scroll(vt.ScrollRows, over)
		if c.anchor != nil {
			c.anchor[1] -= over
		}
		y = p.rows - 1
	}
	c.y = y
}

// copySelect makes the engine's selection from the anchor to the copy
// cursor, and redraws.
func (p *termPane) copySelect() {
	c := p.copy
	p.update(false)
	if c.anchor == nil || p.cw == 0 {
		p.term.ClearSelection()
		p.selected = false
		p.update(false)
		return
	}
	ax, ay, bx, by := c.anchor[0], c.anchor[1], c.x, c.y
	if c.lines {
		if ay < by || ay == by {
			ax, bx = 0, p.cols-1
		} else {
			ax, bx = p.cols-1, 0
		}
	}
	at := func(kind vt.PointerKind, x, y int) {
		y = min(max(y, 0), p.rows-1)
		// Presses ten seconds apart, with a millisecond to count as one:
		// each is a single click, never a double or triple one.
		c.seq++
		p.term.Select(vt.Pointer{
			Kind: kind, X: x, Y: y,
			PX: float64(x*p.cw + p.cw/4), PY: float64(y*p.ch + p.ch/2),
			Time: time.Duration(c.seq) * 10 * time.Second, Repeat: time.Millisecond,
			CellW: p.cw, Height: p.rows * p.ch,
		})
	}
	if bx > ax || by > ay {
		bx++ // dragging right takes the cell itself
	}
	at(vt.PointerPress, ax, ay)
	at(vt.PointerDrag, min(bx, p.cols-1), by)
	if bx >= p.cols {
		at(vt.PointerDrag, p.cols-1, by)
	}
	at(vt.PointerRelease, min(bx, p.cols-1), by)
	p.selected = true
	p.update(false)
}

// rowText is a viewport row as the frames gave it, a rune a cell (a wide
// character's tail is a space).
func (p *termPane) rowText(y int) []rune {
	if y < 0 || y >= len(p.lines) {
		return nil
	}
	var out []rune
	for _, r := range p.lines[y] {
		for len(out) < r.X {
			out = append(out, ' ')
		}
		rs := []rune(r.Text)
		for i, ch := range rs {
			out = append(out, ch)
			if r.Cells > len(rs) && i < r.Cells-len(rs) && isWide(ch) {
				out = append(out, ' ')
			}
		}
	}
	for len(out) > 0 && out[len(out)-1] == ' ' {
		out = out[:len(out)-1]
	}
	return out
}

func isWide(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hangul, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || r >= 0x1f300 && r <= 0x1faff || r >= 0xff00 && r <= 0xff60
}

func wordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

// nextWord is where the next word after x starts, as vi's w.
func nextWord(row []rune, x int) int {
	i := x
	if i < len(row) && wordRune(row[i]) {
		for i < len(row) && wordRune(row[i]) {
			i++
		}
	} else if i < len(row) && !unicode.IsSpace(row[i]) {
		i++
	}
	for i < len(row) && unicode.IsSpace(row[i]) {
		i++
	}
	if i >= len(row) {
		return max(len(row)-1, x)
	}
	return i
}

// prevWord is where the word before x starts, as vi's b.
func prevWord(row []rune, x int) int {
	i := min(x, len(row)) - 1
	for i > 0 && unicode.IsSpace(row[i]) {
		i--
	}
	if i > 0 && wordRune(row[i]) {
		for i > 0 && wordRune(row[i-1]) {
			i--
		}
	}
	return max(i, 0)
}
