package gamesui

import (
	"fmt"
	"image/color"
	"math"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/gtkx"
)

// pinball is Space Pinball: the table drawn on a canvas, played from the
// keyboard or by holding the table's halves and its launch lane.
type pinball struct {
	f                  *frame
	g                  *games.Pinball
	cv                 *canvas
	score, ball, best  *gtk.Label
	paused             bool
	record             int
	held               int // what the pointer holds: -1 none, 0–1 a flipper, 2 the plunger
	onPress, onRelease func(gtk.GestureClick, int32, float64, float64)
	onCancel           func(gtk.GestureClick)
}

const pinMargin = 12

func (p *pinball) setup(f *frame) {
	p.f, p.held = f, -1
	p.cv = newCanvas(games.TableW+2*pinMargin, games.TableH+2*pinMargin, p.paint)
	gtkx.Append(&f.body, &p.cv.area.Widget)
	p.score = f.stat("SCORE")
	p.ball = f.stat("BALL")
	p.best = f.stat("BEST")
	p.record = loadBest("space-pinball")
	f.keyDown = func(k uint32) bool { return p.key(k, true) }
	f.keyUp = func(k uint32) { p.key(k, false) }

	p.onPress = func(_ gtk.GestureClick, _ int32, x, y float64) {
		at := p.cv.at(x, y).Sub(pinOrigin)
		switch {
		case at.X > games.LaneX[0]:
			p.held = 2
		case at.X < games.TableW/2-10:
			p.held = 0
		default:
			p.held = 1
		}
		p.control(p.held, true)
	}
	p.onRelease = func(gtk.GestureClick, int32, float64, float64) { p.letGo() }
	p.onCancel = func(gtk.GestureClick) { p.letGo() }
	click := gtk.NewGestureClick()
	click.ConnectPressed(&p.onPress)
	click.ConnectReleased(&p.onRelease)
	click.ConnectStopped(&p.onCancel)
	p.cv.area.AddController(&click.EventController)
}

var pinOrigin = games.Vec{X: pinMargin, Y: pinMargin}

func (p *pinball) letGo() {
	if p.held >= 0 {
		p.control(p.held, false)
		p.held = -1
	}
}

func (p *pinball) reset() {
	p.g = games.NewPinball()
	p.paused = false
	p.update()
	p.run()
}

func (p *pinball) redraw() { p.cv.redraw() }

func (p *pinball) key(k uint32, down bool) bool {
	switch k {
	case 'a', 'z', keyLeft, keyShift:
		p.control(0, down)
	case 'd', 'm', '/', keyRight, keyRShft:
		p.control(1, down)
	case ' ', keyDown, 's', keyEnter:
		p.control(2, down)
	case 'p':
		if down && p.g.Result == games.Playing {
			p.paused = !p.paused
			p.update()
			p.run()
		}
	default:
		return false
	}
	return true
}

// control works a flipper (0, 1) or the plunger (2).
func (p *pinball) control(c int, down bool) {
	if down && p.paused {
		p.paused = false
		p.update()
	}
	if c == 2 {
		p.g.Pull(down)
	} else {
		p.g.Press(c, down)
	}
	p.run()
}

// run keeps the table moving while the window is in front and the game
// is on; leaving the window pauses it.
func (p *pinball) run() {
	p.cv.animate(func(dt float64) bool {
		if !p.f.win.IsActive() && !p.paused && p.g.State == games.BallLive {
			p.paused = true
			p.g.Press(0, false)
			p.g.Press(1, false)
			p.g.Pull(false)
			p.update()
		}
		if p.paused {
			return false
		}
		was := p.g.Result
		p.g.Step(dt)
		p.update()
		if was == games.Playing && p.g.Result != games.Playing && p.g.Score > p.record {
			p.record = p.g.Score
			saveBest("space-pinball", p.record)
		}
		return p.g.Result == games.Playing || p.g.MessageTime > 0
	})
}

func (p *pinball) update() {
	g := p.g
	p.score.SetText(thousands(g.Score))
	p.ball.SetText(fmt.Sprintf("%d / %d", g.BallNo, g.Balls))
	p.best.SetText(thousands(max(p.record, g.Score)))
	switch {
	case g.Result != games.Playing:
		if g.Score >= p.record && g.Score > 0 {
			p.f.say(fmt.Sprintf("Game over: %s points, a new best. N plays again.", thousands(g.Score)), "good")
		} else {
			p.f.say(fmt.Sprintf("Game over: %s points. N plays again.", thousands(g.Score)), "")
		}
	case p.paused:
		p.f.say("Paused. Press P, or any control, to play on.", "")
	case g.State == games.BallReady:
		p.f.say(fmt.Sprintf("Ball %d: hold Space to pull the plunger, let go to launch.", g.BallNo), "")
	case g.Save > 0:
		p.f.say("Ball save is lit: a drain now gives the ball back.", "good")
	case g.Mult > 1:
		p.f.say(fmt.Sprintf("Every point counts ×%d. Light the lanes again for more.", g.Mult), "")
	default:
		p.f.say("Light all three lanes for a multiplier; drop the three targets for 1,500.", "")
	}
}

func (p *pinball) paint(pn pen) {
	c := p.f.col
	g := p.g
	pn.Translate(pinMargin, pinMargin)
	wall := mix(c.line, c.t2, 0.45)
	dim := mix(c.surface, c.t2, 0.35)

	// The table: the dome and the cabinet's sides.
	pn.MoveTo(games.LaneX[1]+8, games.TableH)
	pn.LineTo(games.LaneX[1]+8, games.DomeCenter.Y)
	pn.ArcBack(games.DomeCenter.X, games.DomeCenter.Y, games.DomeR+8, 0, -math.Pi)
	pn.LineTo(6, games.TableH)
	pn.Close()
	pn.Color(c.surface)
	pn.FillKeep()
	pn.Width(1)
	pn.Color(c.line)
	pn.Stroke()

	// Orbits and a few stars, for the space.
	pn.Width(1)
	pn.Dash(3, 6)
	for _, r := range []float64{118, 168} {
		pn.Alpha(c.line, 0.9)
		pn.Circle(196, 296, r)
		pn.Stroke()
	}
	pn.Dash()
	for i := range 40 {
		x, y := float64((i*73+31)%340+26), float64((i*97+43)%440+60)
		pn.Alpha(c.t2, 0.25+float64(i%3)*0.12)
		pn.Circle(x, y, 0.8+float64(i%2)*0.5)
		pn.Fill()
	}
	pn.Text("S P A C E", "Geist Mono, IBM Plex Mono Bold 11", 196, 430, 0.5, 0.5, dim)

	// The multiplier's lights.
	for i := range 5 {
		x := 156 + float64(i)*20
		if i < g.Mult {
			pn.Glow(x, 466, 10, c.accent, 0.35)
			pn.Color(c.accent)
			pn.Circle(x, 466, 4.5)
			pn.Fill()
		} else {
			pn.Width(1.2)
			pn.Color(dim)
			pn.Circle(x, 466, 4)
			pn.Stroke()
		}
	}
	pn.Text(fmt.Sprintf("×%d", g.Mult), "Geist Mono, IBM Plex Mono 9", 196, 482, 0.5, 0, dim)

	// Walls, the dome, posts and the launch lane.
	pn.Width(8)
	pn.Color(wall)
	pn.Arc(games.DomeCenter.X, games.DomeCenter.Y, games.DomeR, math.Pi, 2*math.Pi)
	pn.Stroke()
	for _, w := range games.PinWalls {
		if math.Abs(w.A.Sub(games.DomeCenter).Len()-games.DomeR) < 0.5 && math.Abs(w.B.Sub(games.DomeCenter).Len()-games.DomeR) < 0.5 {
			continue
		}
		pn.Width(2 * w.R)
		pn.Color(wall)
		pn.Line(w.A.X, w.A.Y, w.B.X, w.B.Y)
	}
	pn.Width(3)
	if g.GateShut {
		pn.Color(wall)
	} else {
		pn.Alpha(wall, 0.5)
		pn.Dash(4, 4)
	}
	pn.Line(games.GateA.X, games.GateA.Y, games.GateB.X, games.GateB.Y)
	pn.Dash()

	// Rollover lanes.
	for i, lit := range g.Lanes {
		x := (games.LanePosts[i].X + games.LanePosts[i+1].X) / 2
		y := games.LanePosts[i].Y + games.PostLen/2
		on := lit || g.LaneFlash > 0 && int(g.LaneFlash*10)%2 == 0
		if on {
			pn.Glow(x, y, 14, c.accent, 0.4)
			pn.Color(c.accent)
			pn.Circle(x, y, 5)
			pn.Fill()
		} else {
			pn.Width(1.5)
			pn.Color(dim)
			pn.Circle(x, y, 5)
			pn.Stroke()
		}
	}

	// Pop bumpers.
	for i, b := range games.PinBumpers {
		flash := g.BumperFlash[i] / 0.18
		if flash > 0 {
			pn.Glow(b.At.X, b.At.Y, b.R*2.2, c.accent, 0.5*flash)
		}
		pn.Color(c.surface2)
		pn.Circle(b.At.X, b.At.Y, b.R)
		pn.Fill()
		pn.Width(3)
		pn.Color(mix(c.accent, c.t0, flash))
		pn.Circle(b.At.X, b.At.Y, b.R-1.5)
		pn.Stroke()
		pn.Color(mix(mix(c.surface2, c.accent, 0.5), c.accent, flash))
		pn.Circle(b.At.X, b.At.Y, b.R*0.42)
		pn.Fill()
	}

	// Slingshots.
	for i, s := range games.Slings {
		pn.MoveTo(s.A.X, s.A.Y)
		pn.LineTo(s.B.X, s.B.Y)
		pn.LineTo(s.C.X, s.C.Y)
		pn.Close()
		pn.Color(c.surface2)
		pn.Fill()
		flash := g.SlingFlash[i] / 0.14
		if flash > 0 {
			m := s.A.Lerp(s.C, 0.5)
			pn.Glow(m.X, m.Y, 36, c.accent, 0.45*flash)
		}
		pn.Width(5)
		pn.Color(mix(c.accent, c.t0, flash))
		pn.Line(s.A.X, s.A.Y, s.C.X, s.C.Y)
	}

	// Drop targets.
	for i, t := range games.Targets {
		if g.Down[i] {
			pn.Width(2)
			pn.Alpha(c.t2, 0.4)
		} else {
			pn.Width(8)
			pn.Color(c.warn)
		}
		pn.Line(t.A.X+2, t.A.Y+2, t.B.X+2, t.B.Y-2)
	}

	// Flippers: tapered, from a round pivot to a round tip.
	for i := range 2 {
		piv, tip := games.FlipperPivots[i], g.FlipperTip(i)
		d := tip.Sub(piv).Unit()
		a := math.Atan2(d.Y, d.X)
		n := games.Vec{X: -d.Y, Y: d.X}
		r0, r1 := games.FlipperR(0), games.FlipperR(1)
		pn.MoveTo(piv.X+n.X*r0, piv.Y+n.Y*r0)
		pn.LineTo(tip.X+n.X*r1, tip.Y+n.Y*r1)
		pn.ArcBack(tip.X, tip.Y, r1, a+math.Pi/2, a-math.Pi/2)
		pn.LineTo(piv.X-n.X*r0, piv.Y-n.Y*r0)
		pn.ArcBack(piv.X, piv.Y, r0, a-math.Pi/2, a-3*math.Pi/2)
		pn.Close()
		pn.Color(c.t0)
		pn.Fill()
		pn.Color(c.surface2)
		pn.Circle(piv.X, piv.Y, 3)
		pn.Fill()
	}

	// Ball save, between the flippers.
	save := dim
	if g.Save > 0 && (g.Save > 2 || int(g.Save*6)%2 == 0) {
		save = c.ok
	}
	pn.Text("SAVE", "Geist Mono, IBM Plex Mono Bold 8", 195, 712, 0.5, 0.5, save)

	// The plunger, its spring squeezed as it is pulled.
	pull := g.Charge * 24
	lx := (games.LaneX[0] + games.LaneX[1]) / 2
	top := games.PlungerY + 4 + pull
	pn.Width(1.5)
	pn.Color(wall)
	pn.MoveTo(lx, top+6)
	for i := 1; i <= 8; i++ {
		y := top + 6 + (games.TableH-top-6)*float64(i)/8
		pn.LineTo(lx+float64(i%2*2-1)*7, y)
	}
	pn.Stroke()
	pn.Color(mix(c.t1, c.warn, g.Charge))
	pn.RoundRect(games.LaneX[0]+5, top-4, games.LaneX[1]-games.LaneX[0]-10, 8, 2)
	pn.Fill()

	// The ball.
	if g.State != games.BallDrained {
		b := g.Ball
		if g.State == games.BallReady {
			b.Y += pull
		}
		pn.Alpha(c.app, 0.35)
		pn.Circle(b.X+2, b.Y+3, games.BallR)
		pn.Fill()
		pn.Color(c.t0)
		pn.Circle(b.X, b.Y, games.BallR)
		pn.Fill()
		pn.Alpha(color.NRGBA{255, 255, 255, 255}, 0.65)
		pn.Circle(b.X-3, b.Y-3, 2.6)
		pn.Fill()
	}

	// What just happened, large, fading out.
	if g.MessageTime > 0 || g.Result != games.Playing {
		a := min(1, g.MessageTime/0.4)
		if g.Result != games.Playing {
			a = 1
		}
		t := c.t0
		t.A = uint8(255 * a)
		pn.Text(g.Message, "IBM Plex Sans Bold 22", 196, 560, 0.5, 0.5, t)
		if g.Result != games.Playing {
			pn.Text("Press N for a new game", "IBM Plex Sans 11", 196, 590, 0.5, 0.5, c.t1)
		}
	}
	if p.paused {
		pn.Alpha(c.app, 0.55)
		pn.Rect(-pinMargin, -pinMargin, games.TableW+2*pinMargin, games.TableH+2*pinMargin)
		pn.Fill()
		pn.Text("PAUSED", "IBM Plex Sans Bold 22", 196, 360, 0.5, 0.5, c.t0)
		pn.Text("P or any control plays on", "IBM Plex Sans 11", 196, 390, 0.5, 0.5, c.t1)
	}
}

// thousands writes n with commas: 12,340.
func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// Best scores are kept in the light apps' data, games.json.
func loadBest(game string) int {
	var best map[string]int
	store.Load("games", &best)
	return best[game]
}

func saveBest(game string, score int) {
	best := map[string]int{}
	store.Load("games", &best)
	best[game] = score
	store.Save("games", best)
}
