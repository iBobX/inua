package games

import "math"

type Vec struct{ X, Y float64 }
type Rock struct{ X, Y, Radius float64 }

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
	if p.X < 12 || p.X > 588 || p.Y < 12 || p.Y > 388 || g.Elapsed >= 60 {
		g.Result = Lost
		return
	}
	for _, r := range g.Rocks {
		if math.Hypot(p.X-r.X, p.Y-r.Y) < r.Radius+10 {
			g.Result = Lost
			return
		}
	}
	if p.X >= 515 && p.X <= 567 && p.Y >= 64 && p.Y <= 96 && math.Cos(g.Angle) > 0.85 {
		g.Result = Won
	}
}

type Pinball struct {
	Ball, Velocity Vec
	Lives, Score   int
	Active         bool
	Result         Outcome
	Left, Right    float64 // flipper angle, from 0 (rest) to 1 (raised)
	Cooldown       [3]float64
}

var Bumpers = [...]Rock{{135, 150, 28}, {285, 150, 28}, {210, 250, 32}}

// The right-hand launch lane opens at the top. Its diagonal roof sends
// a newly launched ball into the playfield, clear of the lower guide rail.
var PinballRails = [...][2]Vec{
	{{18, 365}, {88, 450}},
	{{360, 365}, {332, 450}},
	{{365, 190}, {365, 550}},
	{{350, 18}, {402, 70}},
}

func NewPinball() *Pinball { return &Pinball{Ball: Vec{385, 490}, Lives: 3} }

func (g *Pinball) Launch() {
	if g.Active || g.Result != Playing {
		return
	}
	g.Ball, g.Velocity, g.Active = Vec{385, 490}, Vec{0, -710}, true
}

// Flipper endpoints are shared by the renderer and collision code.
func (g *Pinball) Flippers() [2][2]Vec {
	return [2][2]Vec{{{95, 455}, {195, 490 - 65*g.Left}}, {{325, 455}, {225, 490 - 65*g.Right}}}
}

func approach(v, target, d float64) float64 {
	if v < target {
		return min(target, v+d)
	}
	return max(target, v-d)
}

func (g *Pinball) Step(dt float64, left, right bool) {
	if g.Result != Playing || dt <= 0 {
		return
	}
	dt = min(dt, 0.05)
	// Small bounded substeps prevent a fast ball from tunneling through
	// bumpers or a raised flipper after a slow frame.
	n := int(math.Ceil(dt * 240))
	h := dt / float64(n)
	for range n {
		oldL, oldR := g.Left, g.Right
		tl, tr := 0., 0.
		if left {
			tl = 1
		}
		if right {
			tr = 1
		}
		g.Left = approach(g.Left, tl, h*12)
		g.Right = approach(g.Right, tr, h*12)
		if !g.Active {
			continue
		}
		g.Velocity.Y += 390 * h
		g.Ball.X += g.Velocity.X * h
		g.Ball.Y += g.Velocity.Y * h
		if g.Ball.X < 18 {
			g.Ball.X = 18
			g.Velocity.X = math.Abs(g.Velocity.X) * 0.92
		}
		if g.Ball.X > 402 {
			g.Ball.X = 402
			g.Velocity.X = -math.Abs(g.Velocity.X) * 0.92
		}
		if g.Ball.Y < 18 {
			g.Ball.Y = 18
			g.Velocity.Y = math.Abs(g.Velocity.Y) * 0.92
		}
		for i, b := range Bumpers {
			g.Cooldown[i] = max(0, g.Cooldown[i]-h)
			dx, dy := g.Ball.X-b.X, g.Ball.Y-b.Y
			d := math.Hypot(dx, dy)
			if d < b.Radius+8 {
				if d < 0.001 {
					dx, dy, d = 0, 1, 1
				}
				nx, ny := dx/d, dy/d
				g.Ball = Vec{b.X + nx*(b.Radius+8), b.Y + ny*(b.Radius+8)}
				v := g.Velocity.X*nx + g.Velocity.Y*ny
				if v < 0 {
					g.Velocity.X -= 2 * v * nx
					g.Velocity.Y -= 2 * v * ny
				}
				g.Velocity.X += nx * 95
				g.Velocity.Y += ny * 95
				if g.Cooldown[i] == 0 {
					g.Score += 100
					g.Cooldown[i] = 0.12
				}
			}
		}
		// Sloping side rails funnel the ball toward the two flippers.
		for _, rail := range PinballRails {
			g.segment(rail[0], rail[1], 0, 2)
		}
		f := g.Flippers()
		g.segment(f[0][0], f[0][1], -65*(g.Left-oldL)/h, 6)
		g.segment(f[1][0], f[1][1], -65*(g.Right-oldR)/h, 6)
		speed := math.Hypot(g.Velocity.X, g.Velocity.Y)
		if speed > 900 {
			g.Velocity.X *= 900 / speed
			g.Velocity.Y *= 900 / speed
		}
		if g.Ball.Y > 580 {
			g.Lives--
			g.Active = false
			g.Ball = Vec{385, 490}
			g.Velocity = Vec{}
			if g.Lives == 0 {
				g.Result = Lost
			}
		}
	}
}

func (g *Pinball) segment(a, b Vec, tipVelocity, thickness float64) {
	dx, dy := b.X-a.X, b.Y-a.Y
	t := max(0, min(1, ((g.Ball.X-a.X)*dx+(g.Ball.Y-a.Y)*dy)/(dx*dx+dy*dy)))
	x, y := a.X+t*dx, a.Y+t*dy
	nx, ny := g.Ball.X-x, g.Ball.Y-y
	d := math.Hypot(nx, ny)
	if d >= 8+thickness {
		return
	}
	if d < 0.001 {
		nx, ny, d = 0, -1, 1
	}
	nx, ny = nx/d, ny/d
	g.Ball = Vec{x + nx*(8+thickness), y + ny*(8+thickness)}
	v := g.Velocity.X*nx + (g.Velocity.Y-tipVelocity*t)*ny
	if v < 0 {
		g.Velocity.X -= 1.85 * v * nx
		g.Velocity.Y -= 1.85 * v * ny
	}
}
