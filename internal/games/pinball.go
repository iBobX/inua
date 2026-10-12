package games

import (
	"math"
	"math/rand/v2"
)

// Space Pinball's table, in units of a 460 × 860 field with y pointing
// down (docs/feat/pinball.md). Balls roll under gravity in small fixed
// steps, so nothing tunnels through a wall or a raised flipper. The
// flippers are rotating capsules that hand their speed to the ball where
// it touches them. On the two ramps a ball leaves the playfield and rides
// a path to its exit; a saucer holds a ball for a moment, then kicks it
// out. What the balls hit is reported to the rules (pinball_rules.go).

const (
	TableW, TableH = 460.0, 860.0
	BallR          = 9.0
	pinGravity     = 1250.0
	pinStep        = 1.0 / 720
	pinMaxSpeed    = 2100.0
	FlipperLen     = 68.0
	flipperRest    = 0.52  // radians below the horizontal
	flipperUp      = -0.46 // radians above it
	rampSpeed      = 820.0 // along a ramp, units a second
)

// The plunger's launch speed, at no pull and its range to a full one.
var launchMin, launchRange = 1260.0, 640.0

// feedFull is the pull above which a launch orbits the dome instead of
// dropping into a re-entry lane.
const feedFull = 0.92

// Wall is a straight piece of the table: a capsule from A to B.
type Wall struct {
	A, B   Vec
	R      float64 // half the thickness
	Bounce float64 // restitution
}

// Bumper is an attack bumper: it kicks the ball away.
type Bumper struct {
	At Vec
	R  float64
}

// Sling is a slingshot above a flipper: its face A–C kicks the ball back
// into play; B is the back corner.
type Sling struct{ A, B, C Vec }

// Target is a standup or a drop target, a short capsule.
type Target struct{ A, B Vec }

// Saucer is a hole that holds the ball: the black hole and the wormhole.
type Saucer struct {
	At Vec
	R  float64
}

// Ramp is a path a ball rides from its mouth to its exit, where it rolls
// on with Exit's velocity.
type Ramp struct {
	Path []Vec
	Exit Vec
}

// trigger is a line that reports a ball crossing it in direction dir:
// rollover lanes and the ramps' mouths.
type trigger struct {
	a, b, dir Vec
	shot      Shot
	i         int
}

// The saucers, by index.
const (
	BlackHole = 0
	Wormhole  = 1
)

// The ramps, by index; after the two ramps come the feeds that drop a
// launched ball into re-entry lane 0, 1 or 2.
const (
	HyperRamp  = 0
	LaunchRamp = 1
	laneFeed   = 2
)

var (
	DomeCenter = Vec{230, 230}
	DomeR      = 214.0

	LaneX     = [2]float64{414, 444} // the launch lane, between these
	LaneTop   = 300.0                // where its inner wall ends
	PlungerY  = 800.0                // the plunger's top, at rest
	PlungerAt = Vec{429, PlungerY - BallR}
	GateA     = Vec{414, 300}
	GateB     = Vec{444, 262}
	DrainY    = TableH + 24

	FlipperPivots = [2]Vec{{138, 760}, {292, 760}}

	// The re-entry lanes under the dome, between these posts.
	LanePosts = [4]Vec{{170, 92}, {210, 92}, {250, 92}, {290, 92}}
	PostLen   = 36.0
	LaneY     = 128.0

	Bumpers = [3]Bumper{{Vec{202, 214}, 21}, {Vec{262, 216}, 21}, {Vec{240, 284}, 21}}
	Slings  = [2]Sling{
		{Vec{74, 548}, Vec{74, 620}, Vec{122, 676}},
		{Vec{356, 548}, Vec{356, 620}, Vec{308, 676}},
	}
	// MissionTargets are three standups on the left wall.
	MissionTargets = [3]Target{
		{Vec{25, 462}, Vec{25, 482}},
		{Vec{25, 494}, Vec{25, 514}},
		{Vec{25, 526}, Vec{25, 546}},
	}
	// BoosterTargets are three drop targets on the right.
	BoosterTargets = [3]Target{
		{Vec{405, 430}, Vec{405, 450}},
		{Vec{405, 462}, Vec{405, 482}},
		{Vec{405, 494}, Vec{405, 514}},
	}
	Saucers = [2]Saucer{{Vec{104, 452}, 15}, {Vec{350, 196}, 14}}
	// blackHoleKick is the black hole's kick, towards the right flipper.
	blackHoleKick = Vec{150, 380}

	// The ramps' mouths: the hyperspace channel along the left wall, the
	// launch ramp's right of the middle.
	HyperMouth  = [2]Vec{{16, 300}, {86, 300}}
	LaunchMouth = [2]Vec{{236, 330}, {288, 330}}

	Ramps = []Ramp{
		{ // hyperspace: up the left, over the dome, down to the right inlane
			Path: []Vec{{50, 300}, {40, 220}, {56, 140}, {104, 78}, {170, 44}, {240, 36}, {310, 48},
				{372, 90}, {414, 160}, {430, 250}, {434, 380}, {432, 500}, {418, 560}, {392, 586}, {371, 600}},
			Exit: Vec{0, 120},
		},
		{ // the launch ramp: up the right, round over the bumpers, down to the left inlane
			Path: []Vec{{262, 330}, {284, 260}, {292, 192}, {262, 150}, {200, 134}, {142, 150},
				{102, 192}, {80, 260}, {64, 380}, {62, 500}, {60, 570}, {59, 600}},
			Exit: Vec{0, 120},
		},
	}
)

func init() {
	for i := range 3 {
		Ramps = append(Ramps, feedPath((LanePosts[i].X+LanePosts[i+1].X)/2))
	}
}

// feedPath takes a launched ball from the top of the launch lane round
// the inside of the dome to above the re-entry lane at x, and drops it in.
func feedPath(x float64) Ramp {
	const r = 200.0
	end := math.Acos((x - DomeCenter.X) / r)
	var p []Vec
	for a := 0.0; a < end; a += 0.08 {
		p = append(p, DomeCenter.Add(Vec{math.Cos(a), -math.Sin(a)}.Mul(r)))
	}
	p = append(p, Vec{x, DomeCenter.Y - math.Sqrt(r*r-(x-DomeCenter.X)*(x-DomeCenter.X))}, Vec{x, 72})
	return Ramp{Path: p, Exit: Vec{0, 220}}
}

// PinWalls are the table's fixed walls, the dome drawn as short pieces.
var PinWalls = func() []Wall {
	w := []Wall{
		{Vec{16, 230}, Vec{16, TableH}, 4, 0.5},                  // left side
		{Vec{44, 596}, Vec{44, 640}, 3, 0.5},                     // left lane post
		{Vec{44, 640}, FlipperPivots[0], 4, 0.4},                 // left inlane
		{Vec{386, 596}, Vec{386, 640}, 3, 0.5},                   // right lane post
		{Vec{386, 640}, FlipperPivots[1], 4, 0.4},                // right inlane
		{Vec{140, 770}, Vec{194, TableH}, 4, 0.3},                // the apron, under the flippers
		{Vec{290, 770}, Vec{236, TableH}, 4, 0.3},                //
		{Vec{414, LaneTop}, Vec{414, TableH}, 4, 0.5},            // the launch lane's inner wall
		{Vec{444, 230}, Vec{444, TableH}, 4, 0.3},                // and its outer one
		{Vec{414, PlungerY + 4}, Vec{444, PlungerY + 4}, 4, 0.1}, // the plunger
		{Vec{84, 290}, Vec{128, 410}, 4, 0.5},                    // the hyperspace channel, along the right flipper's shots
		{Vec{238, 322}, Vec{214, 420}, 4, 0.5},                   // the launch ramp's mouth, along the left flipper's
		{Vec{290, 322}, Vec{266, 420}, 4, 0.5},                   //
	}
	const n = 40
	for i := range n {
		a, b := math.Pi+math.Pi*float64(i)/n, math.Pi+math.Pi*float64(i+1)/n
		w = append(w, Wall{
			DomeCenter.Add(Vec{math.Cos(a), math.Sin(a)}.Mul(DomeR)),
			DomeCenter.Add(Vec{math.Cos(b), math.Sin(b)}.Mul(DomeR)), 4, 0.6})
	}
	for _, p := range LanePosts {
		w = append(w, Wall{p, p.Add(Vec{0, PostLen}), 3, 0.5})
	}
	for _, s := range Slings { // the slings' backs; their faces kick
		w = append(w, Wall{s.A, s.B, 3, 0.4}, Wall{s.B, s.C, 3, 0.4})
	}
	return w
}()

var triggers = func() []trigger {
	t := []trigger{
		{HyperMouth[0], HyperMouth[1], Vec{0, -1}, ShotHyper, 0},
		{LaunchMouth[0], LaunchMouth[1], Vec{0, -1}, ShotLaunch, 0},
		{Vec{84, 626}, Vec{84, 688}, Vec{1, 0}, ShotInlane, 0},
		{Vec{346, 626}, Vec{346, 688}, Vec{-1, 0}, ShotInlane, 1},
		{Vec{16, 720}, Vec{104, 720}, Vec{0, 1}, ShotOutlane, 0},
		{Vec{326, 720}, Vec{414, 720}, Vec{0, 1}, ShotOutlane, 1},
	}
	for i := range 3 {
		t = append(t, trigger{Vec{LanePosts[i].X, LaneY}, Vec{LanePosts[i+1].X, LaneY}, Vec{0, 1}, ShotLane, i})
	}
	return t
}()

// BallMode is where a ball is.
type BallMode uint8

const (
	OnPlunger BallMode = iota // waiting to be launched
	Rolling                   // on the playfield or in the launch lane
	OnRamp                    // riding a ramp
	InSaucer                  // held in the black hole or the wormhole
)

// Ball is one ball on the table.
type Ball struct {
	Pos, Vel Vec
	Mode     BallMode
	Ramp     int     // the ramp it rides
	Along    float64 // how far along it
	Saucer   int     // the saucer holding it
	hold     float64 // seconds it stays there
	clear    int     // a saucer it was just kicked out of, -1 none, until it leaves
	gateShut bool    // it has left the launch lane
	auto     float64 // seconds until it launches itself (multiball), 0 none
	feed     int     // the re-entry lane a launch drops it into, -1 none
}

// InLane reports whether a rolling ball is still in the launch lane,
// below its gate.
func (b Ball) InLane() bool { return b.Mode == Rolling && !b.gateShut }

// RampPos is where a ball riding a ramp is.
func (b *Ball) rampPos() Vec {
	p := Ramps[b.Ramp].Path
	d := b.Along
	for i := 1; i < len(p); i++ {
		l := p[i].Sub(p[i-1]).Len()
		if d <= l {
			return p[i-1].Lerp(p[i], d/l)
		}
		d -= l
	}
	return p[len(p)-1]
}

func rampLen(i int) float64 {
	p, l := Ramps[i].Path, 0.0
	for j := 1; j < len(p); j++ {
		l += p[j].Sub(p[j-1]).Len()
	}
	return l
}

// Pull starts or stops pulling the plunger; letting go launches the ball
// on it with the strength the pull reached.
func (g *Pinball) Pull(on bool) {
	if g.Result != Playing {
		return
	}
	b := g.onPlunger()
	if on {
		g.pulling = b != nil
		return
	}
	if g.pulling && b != nil {
		g.launch(b, g.Charge)
	}
	g.pulling, g.Charge = false, 0
}

// Pulling reports whether the plunger is being pulled.
func (g *Pinball) Pulling() bool { return g.pulling }

func (g *Pinball) onPlunger() *Ball {
	for i := range g.Balls {
		if g.Balls[i].Mode == OnPlunger {
			return &g.Balls[i]
		}
	}
	return nil
}

func (g *Pinball) launch(b *Ball, charge float64) {
	b.Vel = Vec{0, -(launchMin + launchRange*charge)}
	b.Mode, b.auto = Rolling, 0
	// Up to a full pull, the harder the launch, the further left the lane.
	b.feed = -1
	if charge < feedFull {
		b.feed = 2 - int(charge/feedFull*3)
	}
	g.sound(SndLaunch)
	g.launched()
}

// serve puts a new ball on the plunger; auto launches it by itself.
func (g *Pinball) serve(auto bool) {
	b := Ball{Pos: PlungerAt, Mode: OnPlunger, clear: -1, feed: -1}
	if auto {
		b.auto = 0.7
	}
	g.Balls = append(g.Balls, b)
}

// FlipperTip is the end of a flipper, 0 left and 1 right.
func (g *Pinball) FlipperTip(i int) Vec { return flipperTip(i, g.Flipper[i]) }

func flipperTip(i int, angle float64) Vec {
	d := Vec{math.Cos(angle), math.Sin(angle)}
	if i == 1 {
		d.X = -d.X
	}
	return FlipperPivots[i].Add(d.Mul(FlipperLen))
}

// FlipperR is a flipper's half thickness at t along it, 0 at the pivot.
func FlipperR(t float64) float64 { return 9 - 4*t }

// Press reports a flipper button going down or up. A press also moves the
// lit re-entry lanes along, so that the player can aim for the dark one.
func (g *Pinball) Press(i int, down bool) {
	if g.Tilted && down {
		return
	}
	if down && g.turn[i] <= 0 && g.Result == Playing {
		if i == 0 {
			g.Lanes = [3]bool{g.Lanes[1], g.Lanes[2], g.Lanes[0]}
		} else {
			g.Lanes = [3]bool{g.Lanes[2], g.Lanes[0], g.Lanes[1]}
		}
		g.sound(SndFlipper)
	}
	if down {
		g.turn[i] = 1
	} else {
		g.turn[i] = -1
	}
}

// Nudge bumps the table: -1 left, 1 right, 0 up. Too many nudges in a
// short while tilt it.
func (g *Pinball) Nudge(dir int) {
	if g.Result != Playing || g.Tilted {
		return
	}
	push := Vec{float64(dir) * 170, -90}
	if dir == 0 {
		push = Vec{0, -230}
	}
	for i := range g.Balls {
		if g.Balls[i].Mode == Rolling && g.Balls[i].gateShut {
			g.Balls[i].Vel = g.Balls[i].Vel.Add(push)
		}
	}
	g.Shake = 0.15
	g.sound(SndNudge)
	g.nudged()
}

// Step advances the table by dt seconds.
func (g *Pinball) Step(dt float64) {
	if g.Result != Playing || dt <= 0 {
		return
	}
	dt = min(dt, 1.0/30)
	for range int(math.Ceil(dt / pinStep)) {
		g.step(pinStep)
	}
}

func (g *Pinball) step(h float64) {
	g.tick(h)
	if g.pulling {
		g.Charge = min(1, g.Charge+h/0.9)
	}
	// The flippers swing up fast and fall back slower.
	var before [2]float64
	for i := range g.Flipper {
		before[i] = g.Flipper[i]
		if g.turn[i] > 0 && !g.Tilted {
			g.Flipper[i] = max(flipperUp, g.Flipper[i]-26*h)
		} else {
			g.Flipper[i] = min(flipperRest, g.Flipper[i]+15*h)
		}
	}
	for i := range g.Balls {
		g.stepBall(&g.Balls[i], before, h)
	}
	g.collideBalls()
	// Drained balls leave the table.
	kept := g.Balls[:0]
	for _, b := range g.Balls {
		if b.Mode == Rolling && b.Pos.Y > DrainY {
			continue
		}
		kept = append(kept, b)
	}
	if n := len(g.Balls) - len(kept); n > 0 {
		g.Balls = kept
		g.drained(n)
	}
}

func (g *Pinball) stepBall(b *Ball, before [2]float64, h float64) {
	switch b.Mode {
	case OnPlunger:
		if b.auto > 0 {
			if b.auto -= h; b.auto <= 0 {
				g.launch(b, 1)
			}
		}
		return
	case OnRamp:
		b.Along += rampSpeed * h
		if b.Along >= rampLen(b.Ramp) {
			r := Ramps[b.Ramp]
			b.Pos, b.Vel, b.Mode = r.Path[len(r.Path)-1], r.Exit, Rolling
			b.gateShut = true
		} else {
			b.Pos = b.rampPos()
		}
		return
	case InSaucer:
		if b.hold -= h; b.hold <= 0 {
			g.kick(b)
		}
		return
	}

	prev := b.Pos
	b.Vel.Y += pinGravity * h
	b.Pos = b.Pos.Add(b.Vel.Mul(h))

	// Flippers first: walls have the last word, so a flipper can never
	// push the ball through one.
	for i := range g.Flipper {
		hitFlipper(b, i, g.Flipper[i], before[i], h)
	}
	for _, w := range PinWalls {
		hitWall(b, w.A, w.B, w.R, w.Bounce)
	}
	if b.gateShut {
		hitWall(b, GateA, GateB, 2, 0.4)
	} else if b.Pos.X < LaneX[0]-BallR-4 {
		b.gateShut = true
	} else if b.feed >= 0 && b.Pos.Y < DomeCenter.Y && b.Vel.Y < 0 {
		// At the top of the launch lane: the feed takes it to its lane.
		b.Mode, b.Ramp, b.Along, b.feed = OnRamp, laneFeed+b.feed, 0, -1
		b.Pos = Ramps[b.Ramp].Path[0]
		return
	}
	for i, bu := range Bumpers {
		d := b.Pos.Sub(bu.At)
		l := d.Len()
		if l >= bu.R+BallR {
			continue
		}
		n := d.Unit()
		if l == 0 {
			n = Vec{0, -1}
		}
		b.Pos = bu.At.Add(n.Mul(bu.R + BallR))
		if vn := b.Vel.Dot(n); vn < 0 {
			b.Vel = b.Vel.Sub(n.Mul(vn))
		}
		if g.Tilted { // dead bumpers
			b.Vel = b.Vel.Add(n.Mul(max(0, 120-b.Vel.Dot(n))))
			continue
		}
		// A kick a little off the normal, as a real bumper's ring gives:
		// no ball bounces between a bumper and the dome for ever.
		a := (rand.Float64() - 0.5) * 0.3
		n = Vec{n.X*math.Cos(a) - n.Y*math.Sin(a), n.X*math.Sin(a) + n.Y*math.Cos(a)}
		b.Vel = b.Vel.Add(n.Mul(max(0, 560-b.Vel.Dot(n))))
		if g.BumperFlash[i] < 0.1 {
			g.hit(ShotBumper, i)
		}
		g.BumperFlash[i] = 0.18
	}
	for i, s := range Slings {
		if n, ok := hitWall(b, s.A, s.C, 3, 0.4); ok && g.SlingFlash[i] == 0 && !g.Tilted {
			b.Vel = b.Vel.Add(n.Mul(440))
			g.SlingFlash[i] = 0.14
			g.hit(ShotSling, i)
		}
	}
	for i, t := range MissionTargets {
		if _, ok := hitWall(b, t.A, t.B, 4, 0.45); ok && g.TargetFlash[i] == 0 {
			g.TargetFlash[i] = 0.25
			g.hit(ShotTarget, i)
		}
	}
	for i, t := range BoosterTargets {
		if g.Down[i] {
			continue
		}
		if _, ok := hitWall(b, t.A, t.B, 4, 0.35); ok {
			g.Down[i] = true
			g.hit(ShotBooster, i)
		}
	}
	for i, s := range Saucers {
		d := b.Pos.Sub(s.At).Len()
		if b.clear == i {
			if d > s.R+BallR+4 {
				b.clear = -1
			}
			continue
		}
		if d < s.R*0.75 && b.Vel.Len() < 1500 {
			b.Mode, b.Saucer, b.Pos, b.Vel = InSaucer, i, s.At, Vec{}
			b.hold = 1.1
			g.captured(b, i)
			return
		}
	}
	for _, t := range triggers {
		if b.Vel.Dot(t.dir) <= 0 || !crossed(prev, b.Pos, t.a, t.b) {
			continue
		}
		switch t.shot {
		case ShotHyper, ShotLaunch:
			ramp := HyperRamp
			if t.shot == ShotLaunch {
				ramp = LaunchRamp
			}
			if b.Vel.Y > -260 { // too slow to climb it
				continue
			}
			b.Mode, b.Ramp, b.Along = OnRamp, ramp, 0
			b.Pos = Ramps[ramp].Path[0]
			g.hit(t.shot, 0)
			return
		default:
			g.hit(t.shot, t.i)
		}
	}
	if s := b.Vel.Len(); s > pinMaxSpeed {
		b.Vel = b.Vel.Mul(pinMaxSpeed / s)
	}
	// Back on the plunger: a launch too weak to leave the lane.
	if !b.gateShut && b.Pos.X > LaneX[0] && b.Pos.Y >= PlungerAt.Y-0.5 && math.Abs(b.Vel.Y) < 40 {
		b.Mode, b.Pos, b.Vel = OnPlunger, PlungerAt, Vec{}
	}
}

// kick sends a ball out of the saucer holding it. Out of the wormhole it
// comes out of the black hole.
func (g *Pinball) kick(b *Ball) {
	b.Mode, b.Pos = Rolling, Saucers[BlackHole].At
	b.Vel, b.clear = blackHoleKick, BlackHole
	g.sound(SndKick)
}

// collideBalls bounces rolling balls off each other.
func (g *Pinball) collideBalls() {
	for i := range g.Balls {
		for j := i + 1; j < len(g.Balls); j++ {
			a, b := &g.Balls[i], &g.Balls[j]
			if a.Mode != Rolling || b.Mode != Rolling {
				continue
			}
			d := b.Pos.Sub(a.Pos)
			l := d.Len()
			if l >= 2*BallR || l == 0 {
				continue
			}
			n := d.Mul(1 / l)
			push := n.Mul((2*BallR - l) / 2)
			a.Pos, b.Pos = a.Pos.Sub(push), b.Pos.Add(push)
			if rel := a.Vel.Sub(b.Vel).Dot(n); rel > 0 {
				a.Vel = a.Vel.Sub(n.Mul(0.95 * rel))
				b.Vel = b.Vel.Add(n.Mul(0.95 * rel))
			}
		}
	}
}

// crossed reports whether the move from p0 to p1 crosses segment ab.
func crossed(p0, p1, a, b Vec) bool {
	ab := b.Sub(a)
	s0 := ab.X*(p0.Y-a.Y) - ab.Y*(p0.X-a.X)
	s1 := ab.X*(p1.Y-a.Y) - ab.Y*(p1.X-a.X)
	if s0 == s1 || (s0 > 0) == (s1 > 0) && s0 != 0 && s1 != 0 {
		return false
	}
	q := p0.Lerp(p1, s0/(s0-s1))
	u := q.Sub(a).Dot(ab) / ab.Dot(ab)
	return u >= 0 && u <= 1
}

// hitWall pushes the ball out of the capsule ab and bounces it, giving
// the wall's normal at the contact and whether they touched.
func hitWall(b *Ball, a, c Vec, r, bounce float64) (Vec, bool) {
	p, _ := closest(b.Pos, a, c)
	d := b.Pos.Sub(p)
	l := d.Len()
	if l >= r+BallR {
		return Vec{}, false
	}
	n := d.Unit()
	if l == 0 {
		n = Vec{0, -1}
	}
	b.Pos = p.Add(n.Mul(r + BallR))
	if vn := b.Vel.Dot(n); vn < 0 {
		b.Vel = b.Vel.Sub(n.Mul((1 + bounce) * vn))
	}
	return n, true
}

// hitFlipper is hitWall for a moving flipper: the ball bounces off the
// flipper's surface as it moves, gaining its speed at the contact.
func hitFlipper(b *Ball, i int, angle, before, h float64) {
	p, tip := FlipperPivots[i], flipperTip(i, angle)
	c, t := closest(b.Pos, p, tip)
	r := FlipperR(t)
	d := b.Pos.Sub(c)
	l := d.Len()
	if l >= r+BallR {
		return
	}
	n := d.Unit()
	if l == 0 {
		n = Vec{0, -1}
	}
	// The surface's speed where the ball touches it.
	old := p.Lerp(flipperTip(i, before), t)
	surface := c.Sub(old).Mul(1 / h)
	b.Pos = c.Add(n.Mul(r + BallR))
	rel := b.Vel.Sub(surface)
	if vn := rel.Dot(n); vn < 0 {
		b.Vel = b.Vel.Sub(n.Mul(1.3 * vn))
	}
}
