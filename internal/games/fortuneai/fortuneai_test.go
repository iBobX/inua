package fortuneai

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/games"
)

// The AI players sit after the people, before the computer players, and
// take their turns as people do: no PhaseCPU.
func TestAISeats(t *testing.T) {
	g := games.NewFortune(games.FortuneSettings{Humans: []string{"Ann"}, AIs: []string{"AI"}, Rivals: 1}, 1)
	if len(g.Players) != 3 || !g.Players[1].AI || g.Players[1].CPU || !g.Players[2].CPU {
		t.Fatalf("players %+v", g.Players)
	}
	g.Players[0].At = 0
	g.RollWith(2, 3)
	for g.Phase != games.PhaseEnd {
		g.Pass()
		g.LeaveShop()
	}
	g.EndTurn()
	if g.Turn != 1 || g.Phase != games.PhaseRoll {
		t.Fatalf("turn %d phase %v", g.Turn, g.Phase)
	}
	if m := Moves(g); len(m) == 0 || m[0].Do != Roll {
		t.Fatalf("moves %+v", m)
	}
}

// The moves offered are the ones the game takes, in each phase; the
// prompt numbers them.
func TestMovesApply(t *testing.T) {
	for seed := range uint64(30) {
		g := games.NewFortune(games.FortuneSettings{AIs: []string{"A", "B"}}, seed)
		rng := rand.New(rand.NewPCG(seed, 1))
		for range 3000 {
			if g.Phase == games.PhaseOver {
				break
			}
			if g.Phase == games.PhaseCPU {
				g.PlayCPU()
				continue
			}
			moves := Moves(g)
			if len(moves) == 0 {
				t.Fatalf("seed %d: no moves in phase %v", seed, g.Phase)
			}
			q := Ask(g, moves)
			if len(q.Options) != len(moves) || q.Options[len(moves)-1] != moves[len(moves)-1].Text ||
				!strings.Contains(q.State, "round") || !strings.Contains(q.Instructions, "You are "+g.Players[g.Turn].Name) {
				t.Fatalf("seed %d: the question %+v", seed, q)
			}
			m := moves[rng.IntN(len(moves))]
			if !Apply(g, m) {
				t.Fatalf("seed %d: phase %v: move %+v not taken", seed, g.Phase, m)
			}
		}
	}
}

// loop runs what the driver brings back, as the game's main loop would.
type loop struct{ q chan func() }

func newLoop() *loop { return &loop{q: make(chan func(), 64)} }

func (l *loop) run(f func()) { l.q <- f }

// play has the driver play g to its end, the computer's turns included.
func play(t *testing.T, g *games.Fortune, d *Driver) {
	t.Helper()
	l := newLoop()
	d.run = l.run
	var step func()
	step = func() {
		for g.Phase == games.PhaseCPU {
			g.PlayCPU()
		}
		d.Next(g, 0, step)
	}
	step()
	deadline := time.After(20 * time.Second)
	for g.Phase != games.PhaseOver {
		select {
		case f := <-l.q:
			f()
		case <-deadline:
			t.Fatalf("stalled: round %d turn %d phase %v", g.Round, g.Turn, g.Phase)
		}
	}
	d.Stop()
}

func TestDriverPlaysAGame(t *testing.T) {
	var mu sync.Mutex
	asked := 0
	byProfile := map[string]int{}
	ask := func(_ context.Context, profile, _, state string, options []string) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		asked++
		byProfile[profile]++
		return len(state) % len(options), nil // some move or other
	}
	g := games.NewFortune(games.FortuneSettings{AIs: []string{"AI 1", "AI 2"}, Rivals: 1, Rounds: 30}, 7)
	g.Players[0].CPU, g.Phase = true, games.PhaseCPU // no one at the screen: the computer plays the first seat
	d := New(ask, nil)
	d.SetProfiles([]string{"fast", "deep"}) // each AI its own
	play(t, g, d)
	if asked == 0 || byProfile["fast"] == 0 || byProfile["deep"] == 0 || byProfile[""] != 0 {
		t.Fatalf("asked %d: %v", asked, byProfile)
	}
}

// A model that does not answer has the computer finish its turns; after
// GiveUp of them, it is no longer asked.
func TestDriverFallsBack(t *testing.T) {
	var mu sync.Mutex
	asked := 0
	ask := func(context.Context, string, string, string, []string) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		asked++
		return -1, errors.New("offline")
	}
	g := games.NewFortune(games.FortuneSettings{AIs: []string{"AI"}, Rivals: 1, Rounds: 30}, 3)
	g.Players[0].CPU, g.Phase = true, games.PhaseCPU
	d := New(ask, nil)
	play(t, g, d)
	if asked != GiveUp {
		t.Fatalf("asked %d times, want %d", asked, GiveUp)
	}
}
