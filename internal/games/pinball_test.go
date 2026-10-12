package games

import (
	"math"
	"math/rand/v2"
	"testing"
)

func run(g *Pinball, seconds float64, each func()) {
	for range int(seconds * 120) {
		if each != nil {
			each()
		}
		g.Step(1.0 / 120)
	}
}

func inside(p Vec) bool {
	if p.Y < DomeCenter.Y {
		return p.Sub(DomeCenter).Len() < DomeR
	}
	return p.X > 14 && p.X < 406
}

func TestPinballLaunchReachesThePlayfield(t *testing.T) {
	g := NewPinball()
	g.Pull(true)
	run(g, 1, nil)
	g.Pull(false)
	top := TableH
	run(g, 1.5, func() { top = min(top, g.Ball.Y) })
	if top > 120 || !g.GateShut {
		t.Fatalf("a full pull rose to %.0f, gate shut %v", top, g.GateShut)
	}
}

func TestPinballWeakLaunchFallsBack(t *testing.T) {
	g := NewPinball()
	g.Pull(true)
	g.Pull(false) // no pull at all
	run(g, 3, nil)
	if g.State != BallReady || g.BallNo != 1 {
		t.Fatalf("state %v ball %d", g.State, g.BallNo)
	}
}

func TestPinballFlipperShootsUp(t *testing.T) {
	for _, i := range []int{0, 1} {
		g := NewPinball()
		g.State = BallLive
		g.GateShut = true
		// Resting near the tip of the lowered flipper.
		p, tip := FlipperPivots[i], g.FlipperTip(i)
		g.Ball = p.Lerp(tip, 0.7).Add(Vec{0, -20})
		run(g, 0.15, nil)
		g.Press(i, true)
		top := TableH
		run(g, 0.8, func() { top = min(top, g.Ball.Y) })
		if top > 400 {
			t.Errorf("flipper %d: ball only rose to %.0f", i, top)
		}
	}
}

func TestPinballDrainsBetweenFlippers(t *testing.T) {
	g := NewPinball()
	g.State, g.GateShut = BallLive, true
	g.Ball = Vec{195, 600}
	run(g, 2, nil)
	if g.BallNo != 2 || g.State != BallReady {
		t.Fatalf("ball %d state %v", g.BallNo, g.State)
	}
	g.Save = 5
	g.State, g.GateShut = BallLive, true
	g.Ball = Vec{195, 600}
	run(g, 2, nil)
	if g.BallNo != 2 {
		t.Fatalf("a saved ball was lost: ball %d", g.BallNo)
	}
}

func TestPinballBumperScoresAndKicks(t *testing.T) {
	g := NewPinball()
	g.State, g.GateShut = BallLive, true
	b := PinBumpers[0]
	g.Ball = b.At.Add(Vec{0, -b.R - BallR - 20})
	g.Velocity = Vec{0, 300}
	run(g, 0.2, nil)
	if g.Score < 100 || g.Velocity.Y > 0 && g.Ball.Y < b.At.Y {
		t.Fatalf("score %d velocity %v", g.Score, g.Velocity)
	}
}

func TestPinballLanesRaiseTheMultiplier(t *testing.T) {
	g := NewPinball()
	g.State, g.GateShut = BallLive, true
	for i := range 3 {
		g.Ball = Vec{(LanePosts[i].X + LanePosts[i+1].X) / 2, LaneY - 20}
		g.Velocity = Vec{0, 100}
		g.lastLaneY = g.Ball.Y
		run(g, 0.25, nil)
	}
	if g.Mult != 2 || g.Lanes != [3]bool{} {
		t.Fatalf("mult %d lanes %v", g.Mult, g.Lanes)
	}
	g.Press(0, true)
}

// Thousands of random flips: the ball stays on the table and finite,
// and three balls end the game.
func TestPinballStaysOnTheTable(t *testing.T) {
	for seed := range 20 {
		g := NewPinball()
		r := rand.New(rand.NewPCG(uint64(seed), 7))
		frame := 0
		for g.Result == Playing && frame < 120*600 {
			frame++
			if g.State == BallReady && !g.Pulling() {
				g.Pull(true)
			}
			if g.Pulling() && g.Charge >= 0.6+float64(seed%5)/10 {
				g.Pull(false)
			}
			if r.IntN(30) == 0 {
				g.Press(r.IntN(2), r.IntN(2) == 0)
			}
			g.Step(1.0 / 120)
			if math.IsNaN(g.Ball.X) || g.State == BallLive && g.Ball.Y < DrainY && !inside(g.Ball) {
				t.Fatalf("seed %d frame %d: ball at %v", seed, frame, g.Ball)
			}
		}
		if g.Result != Lost {
			t.Errorf("seed %d: still playing after 10 minutes, score %d, ball %v %v state %v", seed, g.Score, g.Ball, g.Velocity, g.State)
		}
	}
}
