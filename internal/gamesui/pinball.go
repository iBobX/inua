package gamesui

import (
	"fmt"
	"image/color"
	"math"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// pinball is Space Pinball (docs/feat/pinball.md): the table drawn on a
// canvas beside a display of the career (rank, the mission and its
// progress, fuel, hyperspace, messages), played from the keyboard or by
// holding the table's halves and its launch lane, with sound.
type pinball struct {
	f                  *frame
	g                  *games.Pinball
	cv                 *canvas
	snd                *synth
	score, ball, best  *gtk.Label
	paused, muted      bool
	record             int
	held               int // what the pointer holds: -1 none, 0–1 a flipper, 2 the plunger
	onPress, onRelease func(gtk.GestureClick, int32, float64, float64)
	onCancel           func(gtk.GestureClick)
}

const (
	pinMargin = 12
	panelW    = 236.0
	panelGap  = 14.0
)

var pinOrigin = games.Vec{X: pinMargin, Y: pinMargin}

func (p *pinball) setup(f *frame) {
	p.f, p.held = f, -1
	f.win.SetDefaultSize(780, 1040)
	p.cv = newCanvas(games.TableW+panelGap+panelW+2*pinMargin, games.TableH+2*pinMargin, p.paint)
	gtkx.Append(&f.body, &p.cv.area.Widget)
	p.score = f.stat(i18n.T("SCORE"))
	p.ball = f.stat(i18n.T("BALL"))
	p.best = f.stat(i18n.T("BEST"))
	p.record = loadBest("space-pinball")
	p.snd = newSynth()
	f.keyDown = func(k uint32) bool { return p.key(k, true) }
	f.keyUp = func(k uint32) { p.key(k, false) }

	p.onPress = func(_ gtk.GestureClick, _ int32, x, y float64) {
		at := p.cv.at(x, y).Sub(pinOrigin)
		switch {
		case at.X > games.TableW:
			return // the display
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
	case 'd', '/', keyRight, keyRShft:
		p.control(1, down)
	case ' ', keyDown, 's', keyEnter:
		p.control(2, down)
	case 'x', '.', keyUp:
		if down && !p.paused {
			p.g.Nudge(map[uint32]int{'x': -1, '.': 1, keyUp: 0}[k])
			p.run()
		}
	case 'm':
		if down {
			p.muted = !p.muted
			p.snd.setMuted(p.muted)
			p.cv.redraw()
		}
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

// playing reports whether a ball is in play: not all on the plunger.
func (p *pinball) playing() bool {
	for _, b := range p.g.Balls {
		if b.Mode != games.OnPlunger {
			return true
		}
	}
	return false
}

// run keeps the table moving while the window is in front and the game
// is on; leaving the window pauses it.
func (p *pinball) run() {
	p.cv.animate(func(dt float64) bool {
		if !p.f.win.IsActive() && !p.paused && p.playing() {
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
		for _, s := range p.g.TakeSounds() {
			p.snd.play(s)
		}
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
	ball := fmt.Sprintf("%d / %d", g.BallNo, g.Balls0)
	if g.Extra > 0 {
		ball += fmt.Sprintf(" +%d", g.Extra)
	}
	p.ball.SetText(ball)
	p.best.SetText(thousands(max(p.record, g.Score)))
	name, todo, done, of := g.MissionStatus()
	switch {
	case g.Result != games.Playing:
		if g.Score >= p.record && g.Score > 0 {
			p.f.say(i18n.Tf("Game over as %s: %s points, a new best. N plays again.", g.RankName(), thousands(g.Score)), "good")
		} else {
			p.f.say(i18n.Tf("Game over as %s: %s points. N plays again.", g.RankName(), thousands(g.Score)), "")
		}
	case p.paused:
		p.f.say(i18n.T("Paused. Press P, or any control, to play on."), "")
	case g.Tilted:
		p.f.say(i18n.T("Tilt: the flippers are dead until this ball drains."), "bad")
	case !p.playing():
		lanes := [3]string{"left", "middle", "right"}
		if g.Skill >= 0 {
			p.f.say(i18n.Tf("Ball %d: hold Space and let go. The skill shot is the %s lane: pull light for the right, harder for the left.", g.BallNo, lanes[g.Skill]), "")
		} else {
			p.f.say(i18n.Tf("Ball %d: hold Space to pull the plunger, let go to launch.", g.BallNo), "")
		}
	case g.Multiball:
		p.f.say(i18n.Tf("Multiball: the launch ramp is the jackpot, %s.", thousands(g.Jackpot*g.FieldMult)), "good")
	case g.Mission >= 0:
		p.f.say(i18n.Tf("%s: %s %d / %d, %.0f s left.", name, todo, done, of, math.Max(0, g.MissionLeft)), "")
	case g.MissionLit:
		p.f.say(i18n.Tf("Mission lit: %s. Shoot the launch ramp to accept it.", name), "good")
	default:
		p.f.say(i18n.T("Hit the three yellow mission targets to light a mission; the hyperspace ramp is on the left."), "")
	}
}

// Colours the table adds to the theme's.
var (
	pinYellow = color.NRGBA{0xF2, 0xC1, 0x4E, 255}
	pinRed    = color.NRGBA{0xE0, 0x5F, 0x65, 255}
	pinBlue   = color.NRGBA{0x5B, 0xA4, 0xF5, 255}
	pinPurple = color.NRGBA{0xA8, 0x7B, 0xF0, 255}
)

func (p *pinball) paint(pn pen) {
	c, g := p.f.col, p.g
	pn.Save()
	pn.Translate(pinMargin, pinMargin)
	if g.Shake > 0 {
		pn.Translate(math.Sin(g.Clock*90)*3*g.Shake/0.15, 0)
	}
	p.paintTable(pn)
	pn.Restore()
	pn.Save()
	pn.Translate(pinMargin+games.TableW+panelGap, pinMargin)
	p.paintPanel(pn)
	pn.Restore()
	if p.paused {
		w, h := games.TableW+panelGap+panelW+2*pinMargin, games.TableH+2*pinMargin
		pn.Alpha(c.app, 0.6)
		pn.Rect(0, 0, w, h)
		pn.Fill()
		pn.Text(i18n.T("PAUSED"), "IBM Plex Sans Bold 24", w/2, h/2-14, 0.5, 0.5, c.t0)
		pn.Text(i18n.T("P or any control plays on"), "IBM Plex Sans 12", w/2, h/2+16, 0.5, 0.5, c.t1)
	}
}

// light draws a round lamp, lit or not.
func light(pn pen, x, y, r float64, on bool, col, dim color.NRGBA) {
	if on {
		pn.Glow(x, y, r*2.6, col, 0.45)
		pn.Color(col)
		pn.Circle(x, y, r)
		pn.Fill()
		return
	}
	pn.Width(1.3)
	pn.Color(dim)
	pn.Circle(x, y, r)
	pn.Stroke()
}

// arrow draws a lamp shaped as an arrow pointing at angle a.
func arrow(pn pen, x, y, size, a float64, on bool, col, dim color.NRGBA) {
	pn.Save()
	pn.Translate(x, y)
	pn.Rotate(a)
	pn.MoveTo(size, 0)
	pn.LineTo(-size*0.7, size*0.75)
	pn.LineTo(-size*0.3, 0)
	pn.LineTo(-size*0.7, -size*0.75)
	pn.Close()
	if on {
		pn.Color(col)
		pn.Fill()
	} else {
		pn.Width(1.2)
		pn.Color(dim)
		pn.Stroke()
	}
	pn.Restore()
}

func (p *pinball) paintTable(pn pen) {
	c, g := p.f.col, p.g
	wall := mix(c.line, c.t2, 0.45)
	dim := mix(c.surface, c.t2, 0.35)
	blink := int(g.Clock*4)%2 == 0
	field := mix(c.surface, c.app, 0.35)

	// The cabinet and the field: deep space, a nebula and stars.
	pn.MoveTo(games.LaneX[1]+8, games.TableH)
	pn.LineTo(games.LaneX[1]+8, games.DomeCenter.Y)
	pn.ArcBack(games.DomeCenter.X, games.DomeCenter.Y, games.DomeR+8, 0, -math.Pi)
	pn.LineTo(8, games.TableH)
	pn.Close()
	pn.Color(field)
	pn.FillKeep()
	pn.Width(1)
	pn.Color(c.line)
	pn.Stroke()
	pn.Glow(150, 380, 210, pinPurple, 0.12)
	pn.Glow(300, 230, 170, pinBlue, 0.1)
	pn.Glow(230, 560, 160, c.accent, 0.06)
	for i := range 70 {
		x, y := float64((i*73+31)%400+26), float64((i*97+43)%560+40)
		if x > games.LaneX[0]-6 {
			continue
		}
		tw := 0.5 + 0.5*math.Sin(g.Clock*1.7+float64(i))
		pn.Alpha(c.t1, 0.15+0.25*tw*float64(i%3)/2)
		pn.Circle(x, y, 0.7+float64(i%2)*0.5)
		pn.Fill()
	}
	// Orbits round the bumpers.
	pn.Width(1)
	pn.Dash(3, 6)
	for _, r := range []float64{96, 150} {
		pn.Alpha(c.line, 0.8)
		pn.Circle(232, 250, r)
		pn.Stroke()
	}
	pn.Dash()
	// A ringed planet between the slingshots.
	pn.Glow(215, 650, 70, pinPurple, 0.12)
	pn.Alpha(mix(field, pinPurple, 0.35), 0.9)
	pn.Circle(215, 650, 30)
	pn.Fill()
	pn.Save()
	pn.Translate(215, 650)
	pn.Scale(1, 0.28)
	pn.Width(3)
	pn.Alpha(mix(dim, pinPurple, 0.4), 0.9)
	pn.Circle(0, 0, 54)
	pn.Stroke()
	pn.Restore()

	// Field multiplier lamps.
	for i := 2; i <= 5; i++ {
		x := 152 + float64(i-2)*42
		on := i <= g.FieldMult
		light(pn, x, 506, 9, on, c.accent, dim)
		col := dim
		if on {
			col = c.app
		}
		pn.Text(fmt.Sprintf("×%d", i), "Geist Mono, IBM Plex Mono Bold 8", x, 506, 0.5, 0.5, col)
	}
	// The mission lamp and the fuel gauge's lamps, down the middle.
	missionOn := g.Mission >= 0 || g.MissionLit && blink
	light(pn, 215, 546, 7, missionOn, pinYellow, dim)
	pn.Text(i18n.T("MISSION"), "Geist Mono, IBM Plex Mono 7", 215, 560, 0.5, 0, dim)

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
	// The ramps' mouths, in their ramps' colours.
	for i, w := range games.PinWalls {
		if i < 10 || i > 12 {
			continue
		}
		col := pinYellow
		if i == 10 {
			col = pinBlue
		}
		pn.Width(2*w.R + 1)
		pn.Color(mix(wall, col, 0.55))
		pn.Line(w.A.X, w.A.Y, w.B.X, w.B.Y)
	}
	gateOpen := false
	for _, b := range g.Balls {
		gateOpen = gateOpen || b.Mode == games.OnPlunger || b.InLane()
	}
	pn.Width(3)
	if gateOpen {
		pn.Alpha(wall, 0.5)
		pn.Dash(4, 4)
	} else {
		pn.Color(wall)
	}
	pn.Line(games.GateA.X, games.GateA.Y, games.GateB.X, games.GateB.Y)
	pn.Dash()

	// The hyperspace channel's mouth: its five level arrows.
	for i := range 5 {
		on := i < g.HyperLevel || i == g.HyperLevel && blink && g.RampFlash[games.HyperRamp] == 0
		if g.RampFlash[games.HyperRamp] > 0 {
			on = int(g.RampFlash[games.HyperRamp]*12)%2 == 0
		}
		arrow(pn, 31-float64(i)*1.5, 432-float64(i)*22, 6, -math.Pi/2-0.2, on, pinBlue, dim)
	}
	pn.Save()
	pn.Translate(48, 384)
	pn.Rotate(-math.Pi/2 - 0.12)
	pn.Text(i18n.T("HYPERSPACE"), "Geist Mono, IBM Plex Mono Bold 8", 0, 0, 0.5, 0.5, mix(dim, pinBlue, 0.5))
	pn.Restore()
	// The launch ramp's mouth: lit to accept a mission, or the jackpot.
	lm := games.LaunchMouth[0].Lerp(games.LaunchMouth[1], 0.5)
	launchOn := g.MissionLit && g.Mission < 0 || g.Multiball
	if g.RampFlash[games.LaunchRamp] > 0 {
		launchOn = int(g.RampFlash[games.LaunchRamp]*12)%2 == 0
	}
	arrow(pn, lm.X-8, lm.Y+66, 10, -math.Pi/2+0.25, launchOn && blink || g.RampFlash[games.LaunchRamp] > 0 && launchOn, pinYellow, dim)
	label := i18n.T("LAUNCH")
	if g.Multiball {
		label = i18n.T("JACKPOT")
	}
	pn.Text(label, "Geist Mono, IBM Plex Mono Bold 8", lm.X-12, lm.Y+92, 0.5, 0.5, mix(dim, pinYellow, 0.5))

	// Re-entry lanes, with the skill shot's lamp blinking.
	for i, lit := range g.Lanes {
		x := (games.LanePosts[i].X + games.LanePosts[i+1].X) / 2
		y := games.LanePosts[i].Y + games.PostLen/2
		on := lit || g.LaneFlash > 0 && int(g.LaneFlash*10)%2 == 0
		col := c.accent
		if i == g.Skill && !lit {
			on, col = blink, c.ok
		}
		light(pn, x, y, 5, on, col, dim)
	}
	pn.Text(i18n.T("RE-ENTRY"), "Geist Mono, IBM Plex Mono 7", 230, 80, 0.5, 0.5, dim)

	// The wormhole and the black hole, swirling.
	for i, s := range games.Saucers {
		col := pinPurple
		if i == games.BlackHole {
			col = c.danger
		}
		pn.Glow(s.At.X, s.At.Y, s.R*2.4, col, 0.25+0.3*g.HoleFlash)
		pn.Color(c.app)
		pn.Circle(s.At.X, s.At.Y, s.R)
		pn.Fill()
		pn.Width(1.5)
		for k := range 3 {
			a := g.Clock*float64(2+i) + float64(k)*2*math.Pi/3
			pn.Alpha(col, 0.8)
			pn.Arc(s.At.X, s.At.Y, s.R*(0.45+0.15*float64(k)), a, a+2.2)
			pn.Stroke()
		}
		pn.Width(2)
		pn.Color(col)
		pn.Circle(s.At.X, s.At.Y, s.R)
		pn.Stroke()
	}
	bh := games.Saucers[games.BlackHole].At
	light(pn, bh.X+34, bh.Y-14, 5, g.LockLit && blink, c.accent, dim)
	pn.Text(i18n.T("LOCK"), "Geist Mono, IBM Plex Mono 7", bh.X+44, bh.Y-14, 0, 0.5, dim)
	light(pn, bh.X+34, bh.Y+4, 5, g.ExtraLit && blink, c.ok, dim)
	pn.Text(i18n.T("EXTRA BALL"), "Geist Mono, IBM Plex Mono 7", bh.X+44, bh.Y+4, 0, 0.5, dim)
	for i := range 2 {
		light(pn, bh.X-6+float64(i)*12, bh.Y+28, 3.5, i < g.Locked, c.accent, dim)
	}
	wh := games.Saucers[games.Wormhole].At
	pn.Text(i18n.T("WORMHOLE"), "Geist Mono, IBM Plex Mono 7", wh.X, wh.Y+22, 0.5, 0, dim)

	// Attack bumpers, with their level lamps.
	for i, b := range games.Bumpers {
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
	for i := range 5 {
		light(pn, 196+float64(i)*14, 330, 3.5, i < g.BumperLevel(), c.accent, dim)
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

	// Mission targets (yellow standups) and booster targets (drop).
	for i, t := range games.MissionTargets {
		col := mix(dim, pinYellow, 0.45)
		if g.Targets[i] || g.TargetFlash[i] > 0 {
			col = pinYellow
			pn.Glow(t.A.X+4, (t.A.Y+t.B.Y)/2, 16, pinYellow, 0.35)
		}
		pn.Width(8)
		pn.Color(col)
		pn.Line(t.A.X+1, t.A.Y+2, t.B.X+1, t.B.Y-2)
	}
	for i, t := range games.BoosterTargets {
		if g.Down[i] {
			pn.Width(2)
			pn.Alpha(c.t2, 0.4)
		} else {
			pn.Width(8)
			pn.Color(pinRed)
		}
		pn.Line(t.A.X-1, t.A.Y+2, t.B.X-1, t.B.Y-2)
	}
	pn.Save()
	pn.Translate(394, 472)
	pn.Rotate(math.Pi / 2)
	pn.Text(i18n.T("BOOSTER"), "Geist Mono, IBM Plex Mono 7", 0, 0, 0.5, 0.5, dim)
	pn.Restore()

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
		if g.Tilted {
			pn.Color(dim)
		} else {
			pn.Color(c.t0)
		}
		pn.Fill()
		pn.Color(c.surface2)
		pn.Circle(piv.X, piv.Y, 3)
		pn.Fill()
	}

	// Ball save ("re-deploy"), between the flippers.
	save := dim
	if g.Save > 0 && (g.Save > 2 || blink) {
		save = c.ok
	}
	pn.Text(i18n.T("RE-DEPLOY"), "Geist Mono, IBM Plex Mono Bold 8", 215, 812, 0.5, 0.5, save)

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
	// The pull's three bands: which lane a launch drops into.
	for i, col := range []color.NRGBA{c.ok, c.ok, c.ok} {
		on := g.Pulling() && int(g.Charge/0.92*3) == i
		pn.Alpha(col, map[bool]float64{true: 0.9, false: 0.18}[on])
		pn.Rect(games.LaneX[1]+2, top-30+float64(i)*8, 3, 6)
		pn.Fill()
	}

	// Balls on the playfield, then the ramps over them, then balls on
	// the ramps.
	for _, b := range g.Balls {
		if b.Mode != games.OnRamp {
			p.paintBall(pn, b, pull)
		}
	}
	for i := range 2 {
		r := games.Ramps[i]
		col := pinBlue
		if i == games.LaunchRamp {
			col = pinYellow
		}
		// A wireform: two thin rails, the field showing between them.
		a := 0.45 + 0.5*g.RampFlash[i]
		pn.Width(17)
		pn.Alpha(mix(wall, col, 0.6), a)
		p.path(pn, r.Path)
		pn.Width(13)
		pn.Color(field)
		p.path(pn, r.Path)
		pn.Width(13)
		pn.Alpha(col, 0.06+0.25*g.RampFlash[i])
		p.path(pn, r.Path)
	}
	for _, b := range g.Balls {
		if b.Mode == games.OnRamp {
			p.paintBall(pn, b, pull)
		}
	}

	// What just happened, large, fading out.
	if g.MessageTime > 0 || g.Result != games.Playing {
		a := min(1, g.MessageTime/0.4)
		if g.Result != games.Playing {
			a = 1
		}
		t := c.t0
		t.A = uint8(255 * a)
		pn.Alpha(c.app, 0.55*a)
		pn.RoundRect(40, 588, 350, 46, 6)
		pn.Fill()
		pn.Text(g.Message, "IBM Plex Sans Bold 19", 215, 611, 0.5, 0.5, t)
		if g.Result != games.Playing {
			pn.Text(i18n.T("Press N for a new game"), "IBM Plex Sans 11", 215, 650, 0.5, 0.5, c.t1)
		}
	}
}

func (p *pinball) path(pn pen, pts []games.Vec) {
	pn.MoveTo(pts[0].X, pts[0].Y)
	for _, q := range pts[1:] {
		pn.LineTo(q.X, q.Y)
	}
	pn.Stroke()
}

func (p *pinball) paintBall(pn pen, b games.Ball, pull float64) {
	c := p.f.col
	pos := b.Pos
	r := games.BallR
	switch b.Mode {
	case games.OnPlunger:
		pos.Y += pull
	case games.InSaucer:
		r *= 0.8 // sunk in the hole
	case games.OnRamp:
		r *= 1.1 // raised on the ramp
	}
	pn.Alpha(c.app, 0.35)
	pn.Circle(pos.X+2, pos.Y+3, r)
	pn.Fill()
	pn.Color(c.t0)
	pn.Circle(pos.X, pos.Y, r)
	pn.Fill()
	pn.Alpha(color.NRGBA{255, 255, 255, 255}, 0.7)
	pn.Circle(pos.X-r/3, pos.Y-r/3, r*0.3)
	pn.Fill()
}

// paintPanel draws the display beside the table.
func (p *pinball) paintPanel(pn pen) {
	c, g := p.f.col, p.g
	dim := mix(c.surface, c.t2, 0.5)
	pn.Color(mix(c.surface, c.app, 0.35))
	pn.RoundRect(0, 0, panelW, games.TableH, 10)
	pn.FillKeep()
	pn.Width(1)
	pn.Color(c.line)
	pn.Stroke()
	x, w := 16.0, panelW-32
	kicker := func(y float64, s string) {
		pn.Text(s, "Geist Mono, IBM Plex Mono Bold 8", x, y, 0, 0, c.t2)
	}
	bar := func(y, frac float64, col color.NRGBA) {
		pn.Color(c.surface2)
		pn.RoundRect(x, y, w, 8, 4)
		pn.Fill()
		if frac > 0 {
			pn.Color(col)
			pn.RoundRect(x, y, max(8, w*min(1, frac)), 8, 4)
			pn.Fill()
		}
	}

	// The rank, with its insignia: a bar for each rank earned.
	kicker(18, i18n.T("RANK"))
	pn.Text(g.RankName(), "IBM Plex Sans Bold 17", x, 34, 0, 0, c.t0)
	for i := range len(games.Ranks) - 1 {
		col := dim
		if i < g.Rank {
			col = pinYellow
		}
		pn.Color(col)
		pn.RoundRect(x+float64(i)*(w/8), 62, w/8-4, 5, 2)
		pn.Fill()
	}
	pn.Text(i18n.Nf("%d mission flown", "%d missions flown", g.MissionsWon, g.MissionsWon), "IBM Plex Sans 10", x, 74, 0, 0, c.t2)

	// The mission.
	name, todo, done, of := g.MissionStatus()
	kicker(110, i18n.T("MISSION"))
	state := i18n.T("selected")
	switch {
	case g.Mission >= 0:
		state = i18n.T("in flight")
	case g.MissionLit:
		state = i18n.T("lit")
	}
	pn.Text(state, "Geist Mono, IBM Plex Mono 8", x+w, 110, 1, 0, map[bool]color.NRGBA{true: pinYellow, false: c.t2}[g.Mission >= 0 || g.MissionLit])
	pn.Text(name, "IBM Plex Sans Bold 14", x, 126, 0, 0, c.t0)
	m := games.Missions[g.Selected]
	if g.Mission >= 0 {
		m = games.Missions[g.Mission]
	}
	h := pn.TextWrap(i18n.T(m.Brief), "IBM Plex Sans 10.5", x, 148, w, c.t1)
	y := 156 + h
	if g.Mission >= 0 {
		pn.Text(fmt.Sprintf("%s  %d / %d", todo, done, of), "Geist Mono, IBM Plex Mono 9", x, y, 0, 0, c.t0)
		bar(y+16, float64(done)/float64(of), pinYellow)
		left := math.Max(0, g.MissionLeft)
		tcol := c.t1
		if left < 10 && int(g.Clock*4)%2 == 0 {
			tcol = pinRed
		}
		pn.Text(i18n.Tf("%.0f s", left), "Geist Mono, IBM Plex Mono Bold 9", x+w, y, 1, 0, tcol)
	} else {
		pn.Text(todo, "IBM Plex Sans 10", x, y, 0, 0, c.t2)
		for i, on := range g.Targets {
			light(pn, x+6+float64(i)*16, y+30, 5, on, pinYellow, dim)
		}
	}

	// Fuel.
	kicker(262, i18n.T("FUEL"))
	fcol := c.ok
	if g.Fuel < 25 {
		fcol = pinRed
	}
	pn.Text(fmt.Sprintf("%.0f%%", g.Fuel), "Geist Mono, IBM Plex Mono 8", x+w, 262, 1, 0, c.t1)
	bar(278, g.Fuel/100, fcol)

	// Hyperspace.
	kicker(306, i18n.T("HYPERSPACE"))
	for i := range 5 {
		on := i < g.HyperLevel
		pn.Color(map[bool]color.NRGBA{true: pinBlue, false: c.surface2}[on])
		pn.RoundRect(x+float64(i)*(w/5), 322, w/5-4, 8, 3)
		pn.Fill()
	}
	next := []string{"10K", "25K", i18n.T("EXTRA BALL"), "100K", i18n.T("JACKPOT")}[g.HyperLevel]
	pn.Text("next: "+next, "IBM Plex Sans 10", x, 336, 0, 0, c.t2)

	// The table's state, lamp by lamp.
	kicker(370, i18n.T("TABLE"))
	row := func(y float64, label, value string, on bool) {
		pn.Text(label, "IBM Plex Sans 11", x, y, 0, 0, c.t1)
		col := c.t2
		if on {
			col = c.t0
		}
		pn.Text(value, "Geist Mono, IBM Plex Mono Bold 10", x+w, y, 1, 0, col)
	}
	row(386, i18n.T("Field multiplier"), fmt.Sprintf("×%d", g.FieldMult), g.FieldMult > 1)
	row(406, i18n.T("Bumper level"), fmt.Sprint(g.BumperLevel()), g.BumperLevel() > 1)
	lock := fmt.Sprintf("%d / 2", g.Locked)
	if g.LockLit {
		lock = i18n.Tf("lit · %s", lock)
	}
	if g.Multiball {
		lock = i18n.T("multiball")
	}
	row(426, i18n.T("Lock"), lock, g.LockLit || g.Multiball)
	row(446, i18n.T("Extra ball"), map[bool]string{true: "lit", false: "—"}[g.ExtraLit], g.ExtraLit)
	saveText := "—"
	if g.Save > 0 {
		saveText = i18n.Tf("%.0f s", g.Save)
	}
	row(466, i18n.T("Ball save"), saveText, g.Save > 0)
	tilt := i18n.T("steady")
	if g.Tilted {
		tilt = i18n.T("TILT")
	} else if g.TiltMeter >= 2 {
		tilt = i18n.T("danger")
	}
	row(486, i18n.T("Table"), tilt, g.Tilted || g.TiltMeter >= 2)
	row(506, i18n.T("Bonus"), thousands(g.Bonus*g.FieldMult), g.Bonus > 0)

	// The log, newest first.
	kicker(546, i18n.T("LOG"))
	for i, s := range g.Log {
		a := 1 - float64(i)*0.17
		t := c.t1
		if i == 0 {
			t = c.t0
		}
		t.A = uint8(255 * a)
		pn.Text(s, "IBM Plex Sans 10.5", x, 562+float64(i)*18, 0, 0, t)
	}

	// Sound.
	sound := i18n.T("M mutes the sound")
	switch {
	case !p.snd.available():
		sound = i18n.T("no sound (pw-cat not found)")
	case p.muted:
		sound = i18n.T("sound off: M turns it on")
	}
	pn.Text(i18n.T("SPACE PINBALL"), "Geist Mono, IBM Plex Mono Bold 9", x, games.TableH-46, 0, 0, dim)
	pn.Text(sound, "IBM Plex Sans 9.5", x, games.TableH-28, 0, 0, c.t2)
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
