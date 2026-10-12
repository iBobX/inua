package gamesui

import (
	"fmt"
	"math"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/games"
)

// harbor is One-button Harbor: hold to turn, let go to sail straight,
// and come into the berth heading right.
type harbor struct {
	f              *frame
	g              *games.Harbor
	cv             *canvas
	level          int
	courses        *choices
	clock, best    *gtk.Label
	turning        bool
	started, pause bool
	wake           []games.Vec
	wakeT, waves   float64
	onPress        func(gtk.GestureClick, int32, float64, float64)
	onRelease      func(gtk.GestureClick, int32, float64, float64)
	onStop         func(gtk.GestureClick)
}

const seaEdge = 10.0

func (h *harbor) setup(f *frame) {
	h.f = f
	f.win.SetDefaultSize(820, 700)
	h.clock = f.stat("TIME LEFT")
	h.best = f.stat("BEST")
	h.cv = newCanvas(games.SeaW+2*seaEdge, games.SeaH+2*seaEdge, h.paint)
	f.add(h.cv)
	h.courses = f.choices([]string{"Course 1", "Course 2", "Course 3"}, func(i int) {
		h.level = i
		h.reset()
	})
	f.keyDown = func(k uint32) bool {
		switch k {
		case ' ', keyEnter:
			h.hold(true)
		case 'p':
			if h.started && h.g.Result == games.Playing {
				h.pause = !h.pause
				h.turning = false
				h.update()
				h.run()
			}
		default:
			return false
		}
		return true
	}
	f.keyUp = func(k uint32) {
		if k == ' ' || k == keyEnter {
			h.hold(false)
		}
	}
	h.onPress = func(gtk.GestureClick, int32, float64, float64) { h.hold(true) }
	h.onRelease = func(gtk.GestureClick, int32, float64, float64) { h.hold(false) }
	h.onStop = func(gtk.GestureClick) { h.hold(false) }
	click := gtk.NewGestureClick()
	click.ConnectPressed(&h.onPress)
	click.ConnectReleased(&h.onRelease)
	click.ConnectStopped(&h.onStop)
	h.cv.area.AddController(&click.EventController)
}

func (h *harbor) reset() {
	h.g = games.NewHarbor(h.level)
	h.started, h.pause, h.turning = false, false, false
	h.wake = nil
	h.courses.set(h.level)
	h.update()
	h.run()
}

func (h *harbor) redraw() { h.cv.redraw() }

func (h *harbor) bestKey() string { return fmt.Sprintf("harbor-%d", h.level+1) }

// hold presses or lets go of the one button; the first press casts off.
func (h *harbor) hold(down bool) {
	if h.g.Result != games.Playing {
		if down {
			h.reset()
		}
		return
	}
	if down && (!h.started || h.pause) {
		h.started, h.pause = true, false
		h.update()
	}
	h.turning = down && h.started
	h.run()
}

func (h *harbor) run() {
	h.cv.animate(func(dt float64) bool {
		h.waves += dt
		if !h.started || h.pause {
			return !h.pause // the sea moves while waiting to cast off
		}
		if !h.f.win.IsActive() {
			h.pause, h.turning = true, false
			h.update()
			return false
		}
		h.g.Step(dt, h.turning)
		if h.wakeT += dt; h.wakeT > 0.06 {
			h.wakeT = 0
			h.wake = append(h.wake, h.g.Position)
			if len(h.wake) > 40 {
				h.wake = h.wake[1:]
			}
		}
		h.clock.SetText(fmt.Sprintf("%.1f", max(0, games.HarborTime-h.g.Elapsed)))
		if h.g.Result != games.Playing {
			if h.g.Result == games.Won {
				if b := loadBest(h.bestKey()); b == 0 || int(h.g.Elapsed*10) < b {
					saveBest(h.bestKey(), int(h.g.Elapsed*10))
				}
			}
			h.update()
			return false
		}
		return true
	})
}

func (h *harbor) update() {
	g := h.g
	h.clock.SetText(fmt.Sprintf("%.1f", max(0, games.HarborTime-g.Elapsed)))
	if b := loadBest(h.bestKey()); b > 0 {
		h.best.SetText(fmt.Sprintf("%.1f s", float64(b)/10))
	} else {
		h.best.SetText("—")
	}
	switch {
	case g.Result == games.Won:
		h.f.say(fmt.Sprintf("Docked in %.1f seconds. Press Space to sail again.", g.Elapsed), "good")
	case g.Result == games.Lost && g.Elapsed >= games.HarborTime:
		h.f.say("Out of time. Press Space to try again.", "bad")
	case g.Result == games.Lost:
		h.f.say("Aground. Press Space to try again.", "bad")
	case h.pause:
		h.f.say("Paused. Press Space or P to sail on.", "")
	case !h.started:
		h.f.say("Hold Space, or hold the sea, to cast off and turn; let go to sail straight. Dock heading right.", "")
	default:
		h.f.say("Hold to turn right, let go to sail straight. Come into the berth heading right.", "")
	}
	h.cv.redraw()
}

// rock is a rock's outline: a rough circle, the same every time.
func rock(r games.Rock) []games.Vec {
	var out []games.Vec
	for i := range 11 {
		a := float64(i) / 11 * 2 * math.Pi
		j := 0.82 + 0.18*math.Abs(math.Sin(r.X*0.37+float64(i)*1.9+r.Y*0.11))
		out = append(out, games.Vec{X: r.X + math.Cos(a)*r.Radius*j, Y: r.Y + math.Sin(a)*r.Radius*j})
	}
	return out
}

func (h *harbor) paint(pn pen) {
	c, g := h.f.col, h.g
	pn.Translate(seaEdge, seaEdge)
	sea := mix(c.surface, c.accent, 0.08)
	// The shore, all round, and the sea inside it.
	pn.Color(mix(c.surface2, c.warn, 0.12))
	pn.RoundRect(-seaEdge, -seaEdge, games.SeaW+2*seaEdge, games.SeaH+2*seaEdge, 14)
	pn.Fill()
	pn.Color(sea)
	pn.RoundRect(2, 2, games.SeaW-4, games.SeaH-4, 10)
	pn.Fill()
	// Ripples drifting by.
	pn.Width(1.2)
	for i := range 46 {
		x := math.Mod(float64(i*97%600)+h.waves*(6+float64(i%3)*3), games.SeaW-40) + 20
		y := float64(i*53%360) + 20
		pn.Alpha(c.accent, 0.18)
		pn.Arc(x, y+4, 7, math.Pi*1.15, math.Pi*1.85)
		pn.Stroke()
	}

	// The pier and its berth.
	b := games.Berth
	pier := mix(c.surface2, c.warn, 0.35)
	pn.Color(pier)
	pn.Rect(b.X1+6, b.Y0-26, games.SeaW-b.X1-6, b.Y1-b.Y0+52)
	pn.Fill()
	pn.Rect(b.X0-6, b.Y0-26, b.X1-b.X0+18, 12)
	pn.Fill()
	pn.Rect(b.X0-6, b.Y1+14, b.X1-b.X0+18, 12)
	pn.Fill()
	pn.Width(1)
	pn.Color(mix(pier, c.app, 0.3))
	for x := b.X0; x < games.SeaW; x += 9 {
		pn.Line(x, b.Y0-26, x, b.Y0-14)
		pn.Line(x, b.Y1+14, x, b.Y1+26)
	}
	pn.Width(1.5)
	pn.Dash(5, 4)
	pn.Color(c.ok)
	pn.Rect(b.X0, b.Y0, b.X1-b.X0, b.Y1-b.Y0)
	pn.Stroke()
	pn.Dash()
	pn.Text("BERTH →", "Geist Mono, IBM Plex Mono Bold 8", (b.X0+b.X1)/2, b.Y0-36, 0.5, 0.5, c.ok)

	// Rocks, with a ring of foam.
	for _, r := range g.Rocks {
		pts := rock(r)
		pn.Width(1.2)
		pn.Alpha(c.t0, 0.25)
		pn.Dash(3, 5)
		pn.Circle(r.X, r.Y, r.Radius+games.BoatR)
		pn.Stroke()
		pn.Dash()
		pn.MoveTo(pts[0].X, pts[0].Y)
		for _, p := range pts[1:] {
			pn.LineTo(p.X, p.Y)
		}
		pn.Close()
		pn.Color(mix(c.surface2, c.t2, 0.35))
		pn.FillKeep()
		pn.Width(1.5)
		pn.Color(mix(c.surface2, c.t2, 0.6))
		pn.Stroke()
	}

	// The wake, fading behind the boat.
	for i := 1; i < len(h.wake); i++ {
		a, b := h.wake[i-1], h.wake[i]
		pn.Width(1 + float64(i)/10)
		pn.Alpha(c.t0, float64(i)/float64(len(h.wake))*0.35)
		pn.Line(a.X, a.Y, b.X, b.Y)
	}

	// The boat.
	p, a := g.Position, g.Angle
	pn.Save()
	pn.Translate(p.X, p.Y)
	pn.Rotate(a)
	pn.MoveTo(17, 0)
	pn.Curve(10, -8, -6, -8, -12, -7)
	pn.LineTo(-12, 7)
	pn.Curve(-6, 8, 10, 8, 17, 0)
	pn.Close()
	pn.Color(c.t0)
	pn.Fill()
	pn.Color(c.accent)
	pn.RoundRect(-7, -4, 10, 8, 2)
	pn.Fill()
	pn.Restore()
	if h.turning {
		pn.Width(2)
		pn.Alpha(c.accent, 0.6)
		pn.Arc(p.X, p.Y, 22, a+0.4, a+1.4)
		pn.Stroke()
	}

	switch {
	case g.Result == games.Won:
		h.f.veil(pn, games.SeaW, games.SeaH, "Docked", fmt.Sprintf("%.1f seconds · Space to sail again", g.Elapsed), c.ok)
	case g.Result == games.Lost:
		h.f.veil(pn, games.SeaW, games.SeaH, "Aground", "Space to try again", c.danger)
	case h.pause:
		h.f.veil(pn, games.SeaW, games.SeaH, "Paused", "Space or P to sail on", c.t0)
	case !h.started:
		pn.Text("Hold Space to cast off", "IBM Plex Sans Medium 13", p.X+30, p.Y, 0, 0.5, c.t1)
	}
}
