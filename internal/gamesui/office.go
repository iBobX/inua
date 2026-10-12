package gamesui

import (
	"fmt"
	"image/color"
	"math"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/games"
)

// clockOut is Clock Out: across the office to the exit, out of the way
// of colleagues who walk their rounds as you move.
type clockOut struct {
	f           *frame
	g           *games.ClockOut
	cv          *canvas
	level       int
	offices     *choices
	turns, done *gtk.Label
	fromMe      games.Point
	fromThem    []games.Point
	slide       float64
}

const officeCell = 74.0

func (o *clockOut) setup(f *frame) {
	o.f = f
	f.win.SetDefaultSize(600, 820)
	o.turns = f.stat("TURNS")
	o.done = f.stat("OFFICES")
	o.cv = newCanvas(1, 1, o.paint)
	f.add(o.cv)
	var names []string
	for i := range games.OfficeLevels {
		names = append(names, fmt.Sprint(i+1))
	}
	o.offices = f.choices(names, func(i int) {
		o.level = i
		o.reset()
	})
	o.cv.clicks(func(_ int, at games.Vec) {
		if o.g.Result != games.Playing {
			o.next()
			return
		}
		o.move(games.Point{X: int(math.Floor(at.X / officeCell)), Y: int(math.Floor(at.Y / officeCell))})
	})
	f.keyDown = o.key
}

func (o *clockOut) reset() {
	o.g = games.NewClockOut(o.level)
	o.cv.w, o.cv.h = float64(o.g.W)*officeCell, float64(o.g.H)*officeCell
	o.fromMe, o.fromThem, o.slide = o.g.Player, o.g.Guards(0), 1
	o.offices.set(o.level)
	o.update()
}

func (o *clockOut) redraw() { o.cv.redraw() }

func (o *clockOut) next() {
	o.level = (o.level + 1) % games.OfficeLevels
	o.reset()
}

func (o *clockOut) key(k uint32) bool {
	if o.g.Result != games.Playing && (k == ' ' || k == keyEnter) {
		o.next()
		return true
	}
	if k == ' ' || k == '.' {
		o.move(o.g.Player)
		return true
	}
	d, ok := map[uint32]games.Point{
		'w': {Y: -1}, keyUp: {Y: -1}, 'a': {X: -1}, keyLeft: {X: -1},
		's': {Y: 1}, keyDown: {Y: 1}, 'd': {X: 1}, keyRight: {X: 1},
	}[k]
	if ok {
		o.move(o.g.Player.Add(d))
	}
	return ok
}

func (o *clockOut) move(p games.Point) {
	me, them := o.g.Player, o.g.Guards(o.g.Steps)
	if !o.g.Move(p) {
		return
	}
	o.fromMe, o.fromThem, o.slide = me, them, 0
	if o.g.Result == games.Won {
		done := loadBest("clock-out")
		saveBest("clock-out", done|1<<o.level)
	}
	o.update()
	o.cv.animate(func(dt float64) bool {
		o.slide = min(1, o.slide+dt/0.16)
		return o.slide < 1
	})
}

func (o *clockOut) update() {
	g := o.g
	o.turns.SetText(fmt.Sprintf("%d / %d", g.Steps, g.Turns))
	done, n := loadBest("clock-out"), 0
	for i := range games.OfficeLevels {
		if done&(1<<i) != 0 {
			n++
		}
	}
	o.done.SetText(fmt.Sprintf("%d / %d", n, games.OfficeLevels))
	switch g.Result {
	case games.Won:
		o.f.say(fmt.Sprintf("Out of the office in %d turns. Space for the next one.", g.Steps), "good")
	case games.Lost:
		if g.Steps >= g.Turns {
			o.f.say("Too late: the evening is gone. N to try again.", "bad")
		} else {
			o.f.say("Caught for a quick chat. N to try again.", "bad")
		}
	default:
		o.f.say("Colleagues move when you do; the marks show where they step next. Space waits a turn.", "")
	}
	o.cv.redraw()
}

func cellCentre(p games.Point) games.Vec {
	return games.Vec{X: (float64(p.X) + 0.5) * officeCell, Y: (float64(p.Y) + 0.5) * officeCell}
}

// person draws someone seen from above: shoulders and a head.
func person(pn pen, at games.Vec, body, head color.NRGBA) {
	pn.Color(body)
	pn.RoundRect(at.X-17, at.Y-8, 34, 20, 10)
	pn.Fill()
	pn.Color(head)
	pn.Circle(at.X, at.Y, 10)
	pn.Fill()
}

func (o *clockOut) paint(pn pen) {
	c, g := o.f.col, o.g
	w, h := float64(g.W)*officeCell, float64(g.H)*officeCell
	// Carpet tiles.
	for y := range g.H {
		for x := range g.W {
			pn.Color(c.surface)
			if (x+y)%2 == 1 {
				pn.Color(mix(c.surface, c.surface2, 0.6))
			}
			pn.Rect(float64(x)*officeCell, float64(y)*officeCell, officeCell, officeCell)
			pn.Fill()
		}
	}
	pn.Width(1)
	pn.Color(c.line)
	pn.Rect(0.5, 0.5, w-1, h-1)
	pn.Stroke()

	// Desks, each with a screen and a chair.
	desk := mix(c.surface2, c.warn, 0.22)
	for _, p := range g.Walls {
		x, y := float64(p.X)*officeCell, float64(p.Y)*officeCell
		pn.Color(mix(c.app, c.surface, 0.3))
		pn.RoundRect(x+8, y+12, officeCell-16, officeCell-18, 5)
		pn.Fill()
		pn.Color(desk)
		pn.RoundRect(x+6, y+8, officeCell-12, officeCell-20, 5)
		pn.Fill()
		pn.Color(mix(c.app, c.t2, 0.2))
		pn.RoundRect(x+officeCell/2-14, y+15, 28, 6, 2)
		pn.Fill()
		pn.Color(mix(c.surface2, c.t2, 0.4))
		pn.Circle(x+officeCell/2, y+officeCell-10, 7)
		pn.Fill()
	}

	// The exit.
	ex := cellCentre(g.Exit)
	pn.Alpha(c.ok, 0.16)
	pn.Rect(ex.X-officeCell/2, ex.Y-officeCell/2, officeCell, officeCell)
	pn.Fill()
	pn.Width(3)
	pn.Color(c.ok)
	pn.RoundRect(ex.X-14, ex.Y-24, 28, 38, 2)
	pn.Stroke()
	pn.Circle(ex.X+7, ex.Y-4, 2.5)
	pn.Fill()
	pn.Text("EXIT", "Geist Mono, IBM Plex Mono Bold 8", ex.X, ex.Y+26, 0.5, 0.5, c.ok)

	s := o.slide * o.slide * (3 - 2*o.slide)
	now, next := g.Guards(g.Steps), g.Guards(g.Steps+1)
	// Where each colleague steps next.
	if g.Result == games.Playing && o.slide >= 1 {
		for i, n := range next {
			a, b := cellCentre(now[i]), cellCentre(n)
			pn.Alpha(c.danger, 0.13)
			pn.Rect(b.X-officeCell/2+3, b.Y-officeCell/2+3, officeCell-6, officeCell-6)
			pn.Fill()
			pn.Width(1.5)
			pn.Alpha(c.danger, 0.6)
			pn.Dash(4, 4)
			pn.Rect(b.X-officeCell/2+3, b.Y-officeCell/2+3, officeCell-6, officeCell-6)
			pn.Stroke()
			pn.Dash()
			if a != b {
				d := b.Sub(a).Unit()
				tip := b.Sub(d.Mul(14))
				pn.Width(2.5)
				pn.Line(a.X+d.X*22, a.Y+d.Y*22, tip.X, tip.Y)
				n := games.Vec{X: -d.Y, Y: d.X}
				pn.MoveTo(tip.X+d.X*6, tip.Y+d.Y*6)
				pn.LineTo(tip.X-d.X*4+n.X*6, tip.Y-d.Y*4+n.Y*6)
				pn.LineTo(tip.X-d.X*4-n.X*6, tip.Y-d.Y*4-n.Y*6)
				pn.Close()
				pn.Fill()
			}
		}
	}
	for i := range now {
		from := now[i]
		if i < len(o.fromThem) {
			from = o.fromThem[i]
		}
		at := cellCentre(from).Lerp(cellCentre(now[i]), s)
		person(pn, at, mix(c.danger, c.surface, 0.25), c.danger)
	}
	me := cellCentre(o.fromMe).Lerp(cellCentre(g.Player), s)
	person(pn, me, mix(c.accent, c.surface, 0.25), c.accent)
	pn.Width(2)
	pn.Color(c.t0)
	pn.Circle(me.X, me.Y, 10)
	pn.Stroke()

	if g.Result != games.Playing && o.slide >= 1 {
		if g.Result == games.Won {
			o.f.veil(pn, w, h, "Out of the office", "Space for the next one", c.ok)
		} else {
			o.f.veil(pn, w, h, "Caught", "N to try again", c.danger)
		}
	}
}
