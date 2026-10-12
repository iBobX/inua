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

// inside reports whether a rolling ball is within the table's walls.
func inside(p Vec) bool {
	if p.Y < DomeCenter.Y {
		return p.Sub(DomeCenter).Len() < DomeR
	}
	return p.X > 16 && p.X < 444
}

// live puts a ball at p, rolling, past the launch lane's gate.
func live(g *Pinball, p, v Vec) *Ball {
	g.Balls = []Ball{{Pos: p, Vel: v, Mode: Rolling, gateShut: true, clear: -1, feed: -1}}
	return &g.Balls[0]
}

func TestPinballLaunchReachesThePlayfield(t *testing.T) {
	g := NewPinball()
	g.Pull(true)
	run(g, 1, nil)
	g.Pull(false)
	top := TableH
	run(g, 1.2, func() {
		if len(g.Balls) > 0 {
			top = min(top, g.Balls[0].Pos.Y)
		}
	})
	if top > 120 || len(g.Balls) == 0 || !g.Balls[0].gateShut {
		t.Fatalf("a full pull rose to %.0f", top)
	}
	if g.Save == 0 {
		t.Error("no ball save after the launch")
	}
}

// How hard the plunger is pulled picks the re-entry lane the ball drops
// into: light the right one, harder the middle, harder still the left;
// a full pull orbits the dome.
func TestPinballPullPicksTheLane(t *testing.T) {
	for _, c := range []struct {
		charge float64
		lane   int
	}{{0, 2}, {0.5, 1}, {0.8, 0}, {1, -1}} {
		g := NewPinball()
		g.launch(&g.Balls[0], c.charge)
		lane := -1
		run(g, 3, func() {
			if lane < 0 {
				for i, lit := range g.Lanes {
					if lit {
						lane = i
					}
				}
			}
		})
		if lane != c.lane {
			t.Errorf("pull %.1f: lane %d, want %d", c.charge, lane, c.lane)
		}
	}
}

func TestPinballFlipperShootsUp(t *testing.T) {
	for _, i := range []int{0, 1} {
		g := NewPinball()
		p, tip := FlipperPivots[i], g.FlipperTip(i)
		b := live(g, p.Lerp(tip, 0.7).Add(Vec{0, -20}), Vec{})
		run(g, 0.15, nil)
		g.Press(i, true)
		top := TableH
		run(g, 0.8, func() { top = min(top, b.Pos.Y) })
		if top > 420 {
			t.Errorf("flipper %d: ball only rose to %.0f", i, top)
		}
	}
}

// Flipper shots from all along both flippers, at every timing, reach both
// ramps: the right flipper the hyperspace ramp, the left the launch ramp.
func TestPinballRampsAreReachable(t *testing.T) {
	reached := map[int]int{}
	for i := range 2 {
		for k := range 40 {
			for _, wait := range []float64{0, 0.02, 0.04, 0.06, 0.08} {
				g := NewPinball()
				p := FlipperPivots[i]
				along := 0.25 + 0.7*float64(k)/40
				b := live(g, p.Lerp(g.FlipperTip(i), along).Add(Vec{0, -24}), Vec{})
				run(g, 0.12+wait, nil)
				g.Press(i, true)
				run(g, 1.2, func() {
					if b.Mode == OnRamp {
						reached[b.Ramp]++
						b.Mode = InSaucer // stop counting it
						b.hold = 99
					}
				})
			}
		}
	}
	if reached[HyperRamp] == 0 || reached[LaunchRamp] == 0 {
		t.Fatalf("ramps reached: %v", reached)
	}
	t.Logf("of 400 shots: hyperspace %d, launch ramp %d", reached[HyperRamp], reached[LaunchRamp])
}

// A ball riding a ramp comes out in its inlane and rolls down to a flipper.
func TestPinballRampsReturnToTheFlippers(t *testing.T) {
	for r := range 2 {
		g := NewPinball()
		g.Balls = []Ball{{Mode: OnRamp, Ramp: r, clear: -1}}
		reached := false
		run(g, 3, func() {
			if len(g.Balls) == 1 && g.Balls[0].Mode == Rolling {
				p := g.Balls[0].Pos
				reached = reached || p.Y > 700 && p.Y < 760 && math.Abs(p.X-FlipperPivots[r^1].X) < 70
			}
		})
		if !reached {
			t.Errorf("ramp %d: the ball never reached the flipper", r)
		}
	}
}

func TestPinballSaucersHoldAndKick(t *testing.T) {
	for i, s := range Saucers {
		g := NewPinball()
		b := live(g, s.At.Add(Vec{0, -30}), Vec{0, 200})
		run(g, 0.3, nil)
		if b.Mode != InSaucer || b.Saucer != i {
			t.Fatalf("saucer %d: mode %v", i, b.Mode)
		}
		run(g, 1.5, nil)
		if b.Mode != Rolling || b.Pos.Sub(Saucers[BlackHole].At).Len() < 20 {
			t.Fatalf("saucer %d: not kicked out: %v at %v", i, b.Mode, b.Pos)
		}
	}
}

// Random play with both flippers, many balls: every ball stays inside
// the walls and none comes to rest away from the plunger and saucers.
func TestPinballContainsTheBall(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for game := range 30 {
		g := NewPinball()
		g.Balls0 = 99
		still := 0.0
		for step := range 120 * 90 {
			for i := range 2 {
				if r.IntN(40) == 0 {
					g.Press(i, r.IntN(2) == 0)
				}
			}
			if b := g.onPlunger(); b != nil && r.IntN(60) == 0 {
				g.launch(b, 0.5+r.Float64()/2)
			}
			g.Step(1.0 / 120)
			moving := false
			for _, b := range g.Balls {
				if b.Mode == Rolling && !inside(b.Pos) && b.Pos.Y < FlipperPivots[0].Y {
					t.Fatalf("game %d step %d: ball escaped to %v", game, step, b.Pos)
				}
				if b.Mode != Rolling || b.Vel.Len() > 25 || b.Pos.Y > 740 {
					moving = true
				}
			}
			if still = still + 1.0/120; moving {
				still = 0
			}
			if still > 6 {
				t.Fatalf("game %d: a ball is stuck at %+v", game, g.Balls)
			}
		}
	}
}

func TestPinballMissionAndPromotion(t *testing.T) {
	g := NewPinball()
	g.Balls[0].Mode = Rolling // a ball in play, for the clocks
	g.Selected = 0
	for i := range 3 {
		g.hit(ShotTarget, i)
	}
	if !g.MissionLit {
		t.Fatal("three mission targets did not light a mission")
	}
	sel := g.Selected
	g.hit(ShotLaunch, 0)
	if g.Mission != sel || g.MissionLit {
		t.Fatalf("launch ramp did not accept: mission %d", g.Mission)
	}
	m := Missions[sel]
	for _, st := range m.Steps {
		for range st.Count {
			switch st.Shot {
			case ShotBlackHole, ShotWormhole:
				w := BlackHole
				if st.Shot == ShotWormhole {
					w = Wormhole
				}
				g.captured(&g.Balls[0], w)
			case ShotBank:
				for i := range 3 {
					g.Down[i] = true
					g.hit(ShotBooster, i)
				}
			case ShotAnyTarget:
				g.hit(ShotSling, 0)
			default:
				g.hit(st.Shot, 0)
			}
		}
	}
	if g.Mission != -1 || g.MissionsWon != 1 || g.Rank != 1 {
		t.Fatalf("mission %d won %d rank %d", g.Mission, g.MissionsWon, g.Rank)
	}
}

func TestPinballMissionFailsWithoutFuel(t *testing.T) {
	g := NewPinball()
	g.Balls[0].Mode = Rolling
	g.Mission, g.MissionLeft, g.Fuel = 0, 1000, 1
	for range 2 * 720 {
		g.tick(pinStep)
	}
	if g.Mission != -1 {
		t.Fatal("a mission flew on an empty tank")
	}
	// No fuel, no new mission.
	g.MissionLit, g.Fuel = true, 5
	g.hit(ShotLaunch, 0)
	if g.Mission != -1 || !g.MissionLit {
		t.Fatal("a mission was accepted without fuel")
	}
}

func TestPinballMultiball(t *testing.T) {
	g := NewPinball()
	g.Balls[0].Mode = Rolling
	for i := range 3 {
		g.Down[i] = true
		g.hit(ShotBooster, i)
	}
	if !g.LockLit {
		t.Fatal("the booster bank did not light the lock")
	}
	g.captured(&g.Balls[0], BlackHole)
	for i := range 3 {
		g.Down[i] = true
		g.hit(ShotBooster, i)
	}
	g.captured(&g.Balls[0], BlackHole)
	if !g.Multiball || len(g.Balls) != 3 {
		t.Fatalf("multiball %v with %d balls", g.Multiball, len(g.Balls))
	}
	score := g.Score
	g.hit(ShotLaunch, 0)
	if g.Score-score < jackpotStart {
		t.Error("the launch ramp paid no jackpot in multiball")
	}
}

func TestPinballHyperspaceLevels(t *testing.T) {
	g := NewPinball()
	for range 3 {
		g.hit(ShotHyper, 0)
	}
	if !g.ExtraLit {
		t.Fatal("hyperspace 3 did not light the extra ball")
	}
	g.captured(&g.Balls[0], BlackHole)
	if g.Extra != 1 {
		t.Fatal("the black hole did not give the extra ball")
	}
	g.hit(ShotHyper, 0)
	g.hit(ShotHyper, 0)
	if g.HyperLevel != 0 {
		t.Fatalf("hyperspace did not start again after its jackpot: %d", g.HyperLevel)
	}
}

func TestPinballTiltLosesTheBonus(t *testing.T) {
	g := NewPinball()
	live(g, Vec{230, 400}, Vec{})
	g.Bonus = 50000
	for range 3 {
		g.Nudge(1)
	}
	if !g.Tilted {
		t.Fatal("three quick nudges did not tilt")
	}
	score := g.Score
	g.hit(ShotBumper, 0)
	g.Balls[0].Pos.Y = DrainY + 1
	g.Step(1.0 / 120)
	if g.Score != score || g.BallNo != 2 || g.Tilted {
		t.Fatalf("score %d → %d, ball %d, tilted %v", score, g.Score, g.BallNo, g.Tilted)
	}
}

func TestPinballBallSaveAndGameOver(t *testing.T) {
	g := NewPinball()
	b := live(g, Vec{215, DrainY - 1}, Vec{0, 300})
	g.Save = 3
	_ = b
	g.Step(1.0 / 60)
	if g.BallNo != 1 || len(g.Balls) != 1 {
		t.Fatalf("ball save: ball %d, %d balls", g.BallNo, len(g.Balls))
	}
	for g.Result == Playing {
		live(g, Vec{215, DrainY + 1}, Vec{0, 300})
		g.Save = 0
		g.Step(1.0 / 120)
	}
	if g.BallNo != 3 {
		t.Fatalf("game over on ball %d", g.BallNo)
	}
}

func TestPinballSkillShot(t *testing.T) {
	g := NewPinball()
	g.Pull(true)
	run(g, 0.2, nil)
	g.Pull(false)
	lane := g.Skill
	score := g.Score
	g.hit(ShotLane, lane)
	if g.Score-score < 25000 {
		t.Fatalf("skill shot paid %d", g.Score-score)
	}
}

func TestCrossed(t *testing.T) {
	a, b := Vec{0, 0}, Vec{10, 0}
	if !crossed(Vec{5, 1}, Vec{5, -1}, a, b) || crossed(Vec{15, 1}, Vec{15, -1}, a, b) || crossed(Vec{5, 1}, Vec{5, 2}, a, b) {
		t.Fatal("crossed")
	}
	_ = math.Pi
}
