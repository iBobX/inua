package gamesui

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/i18n"
)

// mines is Minesweeper, in three sizes, with a clock and best times.
type mines struct {
	f           *frame
	g           *games.Mines
	cv          *canvas
	size        int
	left, clock *gtk.Label
	best        *gtk.Label
	started     time.Time
	elapsed     float64
	hover       int
	cursor      int
	keyboard    bool // the cursor shows once the keys are used
	sizes       *choices
	lost        int // the square that exploded
}

var mineSizes = []struct {
	name       string
	w, h, mine int
}{
	{i18n.M("Small · 9 × 9"), 9, 9, 10},
	{i18n.M("Medium · 16 × 16"), 16, 16, 40},
	{i18n.M("Large · 30 × 16"), 30, 16, 99},
}

const mineCell = 30.0

func (m *mines) setup(f *frame) {
	m.f, m.hover = f, -1
	m.left = f.stat(i18n.T("MINES"))
	m.clock = f.stat(i18n.T("TIME"))
	m.best = f.stat(i18n.T("BEST"))
	m.cv = newCanvas(1, 1, m.paint)
	f.add(m.cv)
	names := []string{}
	for _, s := range mineSizes {
		names = append(names, i18n.T(s.name))
	}
	m.sizes = f.choices(names, func(i int) {
		m.size = i
		m.reset()
	})
	m.cv.clicks(func(button int, at games.Vec) {
		i := m.cellAt(at)
		if i < 0 {
			return
		}
		m.keyboard = false
		m.cursor = i
		switch button {
		case 3:
			m.g.ToggleFlag(i)
		case 2:
			if m.g.Open[i] {
				m.reveal(i)
			}
		default:
			m.reveal(i)
		}
		m.update()
	})
	m.cv.hovers(func(at games.Vec) {
		if i := m.cellAt(at); i != m.hover {
			m.hover = i
			m.cv.redraw()
		}
	})
	f.keyDown = m.key
}

func (m *mines) reset() {
	s := mineSizes[m.size]
	m.g = games.NewMines(s.w, s.h, s.mine)
	m.cv.w, m.cv.h = float64(s.w)*mineCell+2, float64(s.h)*mineCell+2
	m.elapsed, m.lost = 0, -1
	m.cursor = s.h/2*s.w + s.w/2
	m.sizes.set(m.size)
	m.f.win.SetDefaultSize(int32(max(560, s.w*int(mineCell)+60)), int32(s.h*int(mineCell)+300))
	m.update()
}

func (m *mines) redraw() { m.cv.redraw() }

func (m *mines) bestKey() string {
	s := mineSizes[m.size]
	return fmt.Sprintf("minesweeper-%dx%d", s.w, s.h)
}

func (m *mines) cellAt(at games.Vec) int {
	x, y := int(math.Floor((at.X-1)/mineCell)), int(math.Floor((at.Y-1)/mineCell))
	if x < 0 || y < 0 || x >= m.g.Width || y >= m.g.Height {
		return -1
	}
	return y*m.g.Width + x
}

func (m *mines) reveal(i int) {
	if m.g.Result != games.Playing {
		return
	}
	if !m.g.Started {
		m.started = time.Now()
		m.cv.animate(func(float64) bool {
			if m.g.Result == games.Playing {
				m.elapsed = time.Since(m.started).Seconds()
			}
			m.clock.SetText(strconv.Itoa(int(m.elapsed)))
			return m.g.Result == games.Playing && m.g.Started
		})
	}
	m.g.Reveal(i)
	if m.g.Result == games.Lost {
		m.lost = i
		for j := range m.g.Bomb { // the one that went off, when chording
			if m.g.Bomb[j] && m.g.Open[j] {
				m.lost = j
			}
		}
	}
	if m.g.Result == games.Won {
		m.elapsed = time.Since(m.started).Seconds()
		for j, b := range m.g.Bomb {
			m.g.Flag[j] = b
		}
		if best := loadBest(m.bestKey()); best == 0 || int(m.elapsed*10) < best {
			saveBest(m.bestKey(), int(m.elapsed*10))
		}
	}
}

func (m *mines) key(k uint32) bool {
	w, h := m.g.Width, m.g.Height
	x, y := m.cursor%w, m.cursor/w
	switch k {
	case keyLeft:
		x = max(0, x-1)
	case keyRight:
		x = min(w-1, x+1)
	case keyUp:
		y = max(0, y-1)
	case keyDown:
		y = min(h-1, y+1)
	case ' ', keyEnter:
		m.reveal(m.cursor)
	case 'f':
		m.g.ToggleFlag(m.cursor)
	default:
		return false
	}
	m.cursor, m.keyboard = y*w+x, true
	m.update()
	return true
}

func (m *mines) update() {
	g := m.g
	flags := 0
	for _, f := range g.Flag {
		if f {
			flags++
		}
	}
	m.left.SetText(strconv.Itoa(g.Count - flags))
	m.clock.SetText(strconv.Itoa(int(m.elapsed)))
	if b := loadBest(m.bestKey()); b > 0 {
		m.best.SetText(fmt.Sprintf("%.1f", float64(b)/10))
	} else {
		m.best.SetText("—")
	}
	switch g.Result {
	case games.Won:
		m.f.say(i18n.Tf("Cleared in %.1f seconds. N for another field.", m.elapsed), "good")
	case games.Lost:
		m.f.say(i18n.T("A mine went off. N for a new field."), "bad")
	default:
		if !g.Started {
			m.f.say(i18n.T("Click any square to start: the first one is always safe."), "")
		} else {
			m.f.say(i18n.T("Right-click to flag. Click a number with all its flags placed to open around it."), "")
		}
	}
	m.cv.redraw()
}

// The numbers' colours, close to the classic ones.
func (m *mines) numberColor(n int) color.NRGBA {
	c := m.f.col
	return [...]color.NRGBA{c.t0, c.accent, {0x6D, 0xBE, 0x7A, 255}, c.danger, {0x9B, 0x8C, 0xE0, 255},
		{0xC9, 0x81, 0x4E, 255}, {0x4E, 0x9C, 0x9A, 255}, c.t0, c.t2}[n]
}

func (m *mines) paint(pn pen) {
	c, g := m.f.col, m.g
	pn.Translate(1, 1)
	pn.Color(c.line)
	pn.Rect(-1, -1, float64(g.Width)*mineCell+2, float64(g.Height)*mineCell+2)
	pn.Fill()
	for i := range g.Open {
		x, y := float64(i%g.Width)*mineCell, float64(i/g.Width)*mineCell
		open := g.Open[i] || g.Result != games.Playing && g.Bomb[i]
		switch {
		case i == m.lost:
			pn.Color(c.danger)
		case open:
			pn.Color(c.surface)
		case i == m.hover && g.Result == games.Playing && !g.Flag[i]:
			pn.Color(mix(c.surface2, c.t0, 0.14))
		default:
			pn.Color(mix(c.surface2, c.t0, 0.06))
		}
		pn.Rect(x+0.5, y+0.5, mineCell-1, mineCell-1)
		pn.Fill()
		if !open {
			pn.Width(1)
			pn.Color(mix(c.surface2, c.t0, 0.16))
			pn.MoveTo(x+1.5, y+mineCell-1.5)
			pn.LineTo(x+1.5, y+1.5)
			pn.LineTo(x+mineCell-1.5, y+1.5)
			pn.Stroke()
		}
		cx, cy := x+mineCell/2, y+mineCell/2
		switch {
		case g.Flag[i] && g.Result == games.Lost && !g.Bomb[i]:
			pn.Width(2.5)
			pn.Color(c.danger)
			pn.Line(cx-7, cy-7, cx+7, cy+7)
			pn.Line(cx+7, cy-7, cx-7, cy+7)
		case g.Flag[i]:
			pn.Width(2)
			pn.Color(c.t1)
			pn.Line(cx-3, cy-9, cx-3, cy+9)
			pn.Line(cx-8, cy+9, cx+5, cy+9)
			pn.MoveTo(cx-3, cy-9)
			pn.LineTo(cx+8, cy-4.5)
			pn.LineTo(cx-3, cy)
			pn.Close()
			pn.Color(c.danger)
			pn.Fill()
		case open && g.Bomb[i]:
			ink := c.t0
			if i == m.lost {
				ink = c.app
			}
			pn.Width(2)
			pn.Color(ink)
			for a := 0.0; a < math.Pi; a += math.Pi / 4 {
				dx, dy := math.Cos(a)*10, math.Sin(a)*10
				pn.Line(cx-dx, cy-dy, cx+dx, cy+dy)
			}
			pn.Circle(cx, cy, 6.5)
			pn.Fill()
		case open:
			if n := g.Around(i); n > 0 {
				pn.Text(strconv.Itoa(n), "IBM Plex Sans Bold 13", cx, cy, 0.5, 0.5, m.numberColor(n))
			}
		}
	}
	if m.keyboard && g.Result == games.Playing {
		x, y := float64(m.cursor%g.Width)*mineCell, float64(m.cursor/g.Width)*mineCell
		pn.Width(2)
		pn.Color(c.accent)
		pn.Rect(x+1.5, y+1.5, mineCell-3, mineCell-3)
		pn.Stroke()
	}
}
