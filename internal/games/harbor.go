package games

import "math"

type Rock struct{ X, Y, Radius float64 }

// The sea is SeaW × SeaH; the berth is the box the boat must be in,
// heading right, to dock. A crossing has HarborTime seconds.
const (
	SeaW, SeaH    = 600.0, 400.0
	HarborCourses = 3
	HarborTime    = 60.0
	BoatR         = 10.0 // how close the hull may come to a rock or the shore
)

var Berth = struct{ X0, Y0, X1, Y1 float64 }{515, 64, 567, 96}

// Harbor uses a 600×400 sea. Hold to turn clockwise, release to sail
// straight. The berth is entered heading right; all levels share controls.
type Harbor struct {
	Level          int
	Position       Vec
	Angle, Elapsed float64
	Rocks          []Rock
	Result         Outcome
}

func NewHarbor(level int) *Harbor {
	g := &Harbor{Level: level % 3, Position: Vec{65, 320}, Angle: -math.Pi / 2}
	g.Rocks = []Rock{{210, 235, 35}, {350, 115, 40}, {435, 295, 30}}
	if g.Level > 0 {
		g.Rocks = append(g.Rocks, Rock{115, 130, 26})
	}
	if g.Level > 1 {
		g.Rocks = append(g.Rocks, Rock{315, 320, 26}, Rock{480, 190, 25})
	}
	return g
}

func (g *Harbor) Step(dt float64, turn bool) {
	if g.Result != Playing || dt <= 0 {
		return
	}
	dt = min(dt, 0.05)
	g.Elapsed += dt
	if turn {
		g.Angle += dt * 2.2
	}
	speed := 75 + float64(g.Level)*8
	g.Position.X += math.Cos(g.Angle) * dt * speed
	g.Position.Y += math.Sin(g.Angle) * dt * speed
	p := g.Position
	if p.X < 12 || p.X > SeaW-12 || p.Y < 12 || p.Y > SeaH-12 || g.Elapsed >= HarborTime {
		g.Result = Lost
		return
	}
	for _, r := range g.Rocks {
		if math.Hypot(p.X-r.X, p.Y-r.Y) < r.Radius+BoatR {
			g.Result = Lost
			return
		}
	}
	if p.X >= Berth.X0 && p.X <= Berth.X1 && p.Y >= Berth.Y0 && p.Y <= Berth.Y1 && math.Cos(g.Angle) > 0.85 {
		g.Result = Won
	}
}
