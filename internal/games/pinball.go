package games

import "math"

// Space Pinball's table, in units of a 420 × 760 field with y pointing
// down. The ball rolls under gravity in small fixed steps, so nothing
// tunnels through a wall or a raised flipper. The flippers are rotating
// capsules that hand their speed to the ball where it touches them.

const (
	TableW, TableH = 420.0, 760.0
	BallR          = 9.0
	pinGravity     = 1150.0
	pinStep        = 1.0 / 720
	pinMaxSpeed    = 1900.0
	FlipperLen     = 64.0
	flipperRest    = 0.52  // radians below the horizontal
	flipperUp      = -0.46 // radians above it
	ballSaveTime   = 7.0
)

// Wall is a straight piece of the table: a capsule from A to B.
type Wall struct {
	A, B   Vec
	R      float64 // half the thickness
	Bounce float64 // restitution
}

// Bumper is a pop bumper: it kicks the ball away.
type Bumper struct {
	At Vec
	R  float64
}

// Sling is a slingshot above a flipper: its face kicks the ball back
// into play.
type Sling struct{ A, B, C Vec } // the face is A–C; B is the back corner

// Target is a drop target: hit, it falls, and the bank pays when all
// three are down.
type Target struct{ A, B Vec }

var (
	// LanePosts separate the three rollover lanes under the dome.
	LanePosts = [4]Vec{{141, 92}, {181, 92}, {221, 92}, {261, 92}}
	LaneY     = 112.0
	PostLen   = 34.0

	PinBumpers = [3]Bumper{{Vec{130, 250}, 23}, {Vec{262, 250}, 23}, {Vec{196, 338}, 25}}
	Slings     = [2]Sling{
		{Vec{42, 498}, Vec{42, 574}, Vec{100, 616}},
		{Vec{348, 498}, Vec{348, 574}, Vec{290, 616}},
	}
	Targets = [3]Target{
		{Vec{24, 300}, Vec{24, 324}},
		{Vec{24, 334}, Vec{24, 358}},
		{Vec{24, 368}, Vec{24, 392}},
	}
	FlipperPivots = [2]Vec{{120, 668}, {270, 668}}

	// Dome is the table's round top: a half circle over the field.
	DomeCenter = Vec{210, 210}
	DomeR      = 196.0

	LaneX     = [2]float64{376, 406} // the launch lane, between these
	LaneTop   = 300.0                // where its inner wall ends
	PlungerY  = 722.0                // the plunger's top, at rest
	PlungerAt = Vec{391, PlungerY - BallR}
	GateA     = Vec{376, 300}
	GateB     = Vec{406, 262}
	DrainY    = TableH + 30
	ApronL    = Vec{150, TableH}
	ApronR    = Vec{240, TableH}
)

// PinWalls are the table's fixed walls, the dome drawn as short pieces.
var PinWalls = func() []Wall {
	w := []Wall{
		{Vec{14, 210}, Vec{14, 590}, 4, 0.5},       // left side
		{Vec{14, 590}, FlipperPivots[0], 4, 0.45},  // left inlane
		{Vec{376, 590}, FlipperPivots[1], 4, 0.45}, // right inlane
		{FlipperPivots[0], ApronL, 4, 0.3},         // the apron, under the flippers
		{FlipperPivots[1], ApronR, 4, 0.3},
		{Vec{376, LaneTop}, Vec{376, 590}, 4, 0.5},                // right side, the lane's inner wall
		{Vec{376, 590}, Vec{376, TableH}, 4, 0.3},                 // lane, below the inlane
		{Vec{406, 210}, Vec{406, TableH}, 4, 0.3},                 // lane, outside
		{Vec{376, PlungerY + 4}, Vec{406, PlungerY + 4}, 4, 0.15}, // plunger
	}
	const n = 36
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

// BallState is where the ball is in its life.
type BallState uint8

const (
	BallReady   BallState = iota // on the plunger
	BallLive                     // in play, or rising in the lane
	BallDrained                  // between balls, or the game is over
)

// Pinball is one game: three balls, a score and the table's lights.
type Pinball struct {
	Ball, Velocity Vec
	State          BallState
	BallNo, Balls  int // the ball in play (1–3) and how many there are
	Score, Mult    int // points, and the multiplier the lanes raise (1–5)
	Result         Outcome

	Flipper [2]float64 // angle below the horizontal, in radians
	turn    [2]float64 // angular speed, radians a second
	Charge  float64    // the plunger, pulled from 0 to 1
	pulling bool

	Lanes    [3]bool // lit rollover lanes
	Down     [3]bool // dropped targets
	GateShut bool    // the lane's gate, shut once the ball is in play
	Save     float64 // seconds of ball save left
	saved    bool    // a ball save has been spent on this ball

	// Flashes are seconds of light after a hit, for the drawing.
	BumperFlash [3]float64
	SlingFlash  [2]float64
	LaneFlash   float64
	BankReset   float64 // seconds until a completed bank stands again

	Message     string
	MessageTime float64
	lastLaneY   float64
}

func NewPinball() *Pinball {
	g := &Pinball{Balls: 3, BallNo: 1, Mult: 1}
	g.Flipper = [2]float64{flipperRest, flipperRest}
	g.ready()
	return g
}

func (g *Pinball) ready() {
	g.State, g.Ball, g.Velocity = BallReady, PlungerAt, Vec{}
	g.GateShut, g.Charge, g.pulling = false, 0, false
	g.saved, g.Save = false, 0
}

// Pull starts or stops pulling the plunger; letting go launches the ball
// with the strength the pull reached.
func (g *Pinball) Pull(on bool) {
	if g.Result != Playing {
		return
	}
	if on {
		g.pulling = g.State == BallReady
		return
	}
	if g.pulling && g.State == BallReady {
		g.Velocity = Vec{0, -(780 + 720*g.Charge)}
		g.State = BallLive
		if !g.saved {
			g.Save = ballSaveTime
		}
	}
	g.pulling, g.Charge = false, 0
}

// Pulling reports whether the plunger is being pulled.
func (g *Pinball) Pulling() bool { return g.pulling }

// FlipperTip is the end of a flipper, 0 left and 1 right.
func (g *Pinball) FlipperTip(i int) Vec {
	return flipperTip(i, g.Flipper[i])
}

func flipperTip(i int, angle float64) Vec {
	d := Vec{math.Cos(angle), math.Sin(angle)}
	if i == 1 {
		d.X = -d.X
	}
	return FlipperPivots[i].Add(d.Mul(FlipperLen))
}

// FlipperR is a flipper's half thickness at t along it, 0 at the pivot.
func FlipperR(t float64) float64 { return 9 - 4*t }

func (g *Pinball) say(s string) { g.Message, g.MessageTime = s, 1.6 }

func (g *Pinball) score(n int) { g.Score += n * g.Mult }

// Press reports a flipper button going down or up. A press also moves the
// lit lanes along, so that the player can aim for the dark one.
func (g *Pinball) Press(i int, down bool) {
	if down && g.turn[i] <= 0 && g.Result == Playing {
		if i == 0 {
			g.Lanes = [3]bool{g.Lanes[1], g.Lanes[2], g.Lanes[0]}
		} else {
			g.Lanes = [3]bool{g.Lanes[2], g.Lanes[0], g.Lanes[1]}
		}
	}
	if down {
		g.turn[i] = 1
	} else {
		g.turn[i] = -1
	}
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
	for i := range g.BumperFlash {
		g.BumperFlash[i] = max(0, g.BumperFlash[i]-h)
	}
	for i := range g.SlingFlash {
		g.SlingFlash[i] = max(0, g.SlingFlash[i]-h)
	}
	g.LaneFlash = max(0, g.LaneFlash-h)
	g.MessageTime = max(0, g.MessageTime-h)
	if g.BankReset > 0 {
		if g.BankReset -= h; g.BankReset <= 0 {
			g.BankReset, g.Down = 0, [3]bool{}
		}
	}
	if g.pulling {
		g.Charge = min(1, g.Charge+h/0.9)
	}
	// The flippers swing up fast and fall back slower.
	var before [2]float64
	for i := range g.Flipper {
		before[i] = g.Flipper[i]
		if g.turn[i] > 0 {
			g.Flipper[i] = max(flipperUp, g.Flipper[i]-26*h)
		} else {
			g.Flipper[i] = min(flipperRest, g.Flipper[i]+15*h)
		}
	}
	if g.State != BallLive {
		return
	}
	if g.Save > 0 && g.Ball.Y < 640 {
		g.Save = max(0, g.Save-h)
	}
	g.Velocity.Y += pinGravity * h
	g.Ball = g.Ball.Add(g.Velocity.Mul(h))

	// Flippers first: walls have the last word, so a flipper can never
	// push the ball through one.
	for i := range g.Flipper {
		g.hitFlipper(i, before[i], h)
	}
	for _, w := range PinWalls {
		g.hitWall(w.A, w.B, w.R, w.Bounce)
	}
	if g.GateShut {
		g.hitWall(GateA, GateB, 2, 0.4)
	} else if g.Ball.X < LaneX[0]-BallR-4 {
		g.GateShut = true
	}
	for i, b := range PinBumpers {
		d := g.Ball.Sub(b.At)
		l := d.Len()
		if l >= b.R+BallR {
			continue
		}
		n := d.Unit()
		if l == 0 {
			n = Vec{0, -1}
		}
		g.Ball = b.At.Add(n.Mul(b.R + BallR))
		vn := g.Velocity.Dot(n)
		if vn < 0 {
			g.Velocity = g.Velocity.Sub(n.Mul(vn))
		}
		g.Velocity = g.Velocity.Add(n.Mul(max(0, 520-g.Velocity.Dot(n))))
		if g.BumperFlash[i] < 0.1 {
			g.score(100)
		}
		g.BumperFlash[i] = 0.18
	}
	for i, s := range Slings {
		if n, ok := g.hitWall(s.A, s.C, 3, 0.4); ok && g.SlingFlash[i] == 0 {
			g.Velocity = g.Velocity.Add(n.Mul(430))
			g.SlingFlash[i] = 0.14
			g.score(10)
		}
	}
	for i, t := range Targets {
		if g.Down[i] {
			continue
		}
		if _, ok := g.hitWall(t.A, t.B, 4, 0.35); ok {
			g.Down[i] = true
			g.score(250)
			if g.Down == [3]bool{true, true, true} {
				g.score(1500)
				g.say("BANK COMPLETE")
				g.BankReset = 1.2
			}
		}
	}
	// Rolling down through a lane lights it; three lit raise the
	// multiplier and start again.
	if g.lastLaneY < LaneY && g.Ball.Y >= LaneY && g.Velocity.Y > 0 {
		for i := range g.Lanes {
			if g.Ball.X > LanePosts[i].X && g.Ball.X < LanePosts[i+1].X {
				g.score(50)
				g.Lanes[i] = true
				if g.Lanes == [3]bool{true, true, true} {
					g.Lanes = [3]bool{}
					g.LaneFlash = 0.8
					g.score(500)
					if g.Mult < 5 {
						g.Mult++
						g.say("MULTIPLIER ×" + string(rune('0'+g.Mult)))
					}
				}
			}
		}
	}
	g.lastLaneY = g.Ball.Y
	if s := g.Velocity.Len(); s > pinMaxSpeed {
		g.Velocity = g.Velocity.Mul(pinMaxSpeed / s)
	}
	// Back on the plunger: a launch too weak to leave the lane.
	if !g.GateShut && g.Ball.X > LaneX[0] && g.Ball.Y >= PlungerAt.Y-0.5 && math.Abs(g.Velocity.Y) < 40 {
		g.State, g.Ball, g.Velocity = BallReady, PlungerAt, Vec{}
		return
	}
	if g.Ball.Y > DrainY {
		g.drain()
	}
}

func (g *Pinball) drain() {
	if g.Save > 0 {
		g.saved = true
		g.ready()
		g.say("BALL SAVED")
		return
	}
	g.Mult, g.Lanes = 1, [3]bool{}
	if g.BallNo == g.Balls {
		g.State, g.Result = BallDrained, Lost
		g.say("GAME OVER")
		return
	}
	g.BallNo++
	g.ready()
	g.say("BALL " + string(rune('0'+g.BallNo)))
}

// hitWall pushes the ball out of the capsule ab and bounces it, giving
// the wall's normal at the contact and whether they touched.
func (g *Pinball) hitWall(a, b Vec, r, bounce float64) (Vec, bool) {
	c, _ := closest(g.Ball, a, b)
	d := g.Ball.Sub(c)
	l := d.Len()
	if l >= r+BallR {
		return Vec{}, false
	}
	n := d.Unit()
	if l == 0 {
		n = Vec{0, -1}
	}
	g.Ball = c.Add(n.Mul(r + BallR))
	if vn := g.Velocity.Dot(n); vn < 0 {
		g.Velocity = g.Velocity.Sub(n.Mul((1 + bounce) * vn))
	}
	return n, true
}

// hitFlipper is hitWall for a moving flipper: the ball bounces off the
// flipper's surface as it moves, gaining its speed at the contact.
func (g *Pinball) hitFlipper(i int, before, h float64) {
	p, tip := FlipperPivots[i], g.FlipperTip(i)
	c, t := closest(g.Ball, p, tip)
	r := FlipperR(t)
	d := g.Ball.Sub(c)
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
	g.Ball = c.Add(n.Mul(r + BallR))
	rel := g.Velocity.Sub(surface)
	if vn := rel.Dot(n); vn < 0 {
		g.Velocity = g.Velocity.Sub(n.Mul(1.3 * vn))
	}
}
