package checkersai

import (
	"context"
	"errors"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/games"
)

func settings(n int, teams ...int) games.CheckersSettings {
	var s games.CheckersSettings
	for i := range n {
		team := i
		if i < len(teams) {
			team = teams[i]
		}
		s.Players = append(s.Players, games.CheckersSeat{Name: string(rune('A' + i)), Team: team})
	}
	s.NoParking = true
	return s
}

// The computer brings its marbles home: games of two, three, four and six,
// and partners, end with a winner well before the rounds run out.
func TestComputerFinishes(t *testing.T) {
	for _, c := range []struct {
		name  string
		s     games.CheckersSettings
		level Level
	}{
		{"two, normal", settings(2), Normal},
		{"two, hard", settings(2), Hard},
		{"three", settings(3), Normal},
		{"four in teams", settings(4, 0, 1, 0, 1), Normal},
		{"six, hard", settings(6), Hard},
	} {
		g := games.NewCheckers(c.s)
		rng := rand.New(rand.NewPCG(1, 2))
		for !g.Over {
			if !Play(g, c.level, rng) {
				t.Fatalf("%s: no move in round %d", c.name, g.Round)
			}
		}
		if g.WinTeam < 0 || g.Round > 150 {
			t.Errorf("%s: team %d won in round %d", c.name, g.WinTeam, g.Round)
		}
	}
}

// Hard beats Normal more often than not, and Normal beats Easy.
func TestLevels(t *testing.T) {
	wins := func(a, b Level) int {
		n := 0
		for seed := range uint64(6) {
			g := games.NewCheckers(settings(2))
			rng := rand.New(rand.NewPCG(seed, 7))
			for !g.Over {
				level := a
				if g.Turn == 1 {
					level = b
				}
				Play(g, level, rng)
			}
			if g.WinTeam == 0 {
				n++
			}
		}
		return n
	}
	if n := wins(Normal, Easy); n < 5 {
		t.Errorf("Normal beat Easy %d of 6", n)
	}
	if n := wins(Hard, Normal); n < 4 {
		t.Errorf("Hard beat Normal %d of 6", n)
	}
}

// The model picks one of the best moves; when it fails three times in a
// row, the computer plays on for it.
func TestDriver(t *testing.T) {
	g := games.NewCheckers(settings(2))
	asked, pick := 0, 1
	var fail error
	d := New(func(_ context.Context, profile, _, _ string, options []string) (int, error) {
		asked++
		if profile != "p" || len(options) != Choices {
			t.Errorf("asked %q with %d options", profile, len(options))
		}
		return pick, fail
	}, func(f func()) { f() }, 1)
	want := Moves(g)[pick]
	done := make(chan bool, 1)
	d.Next(g, "p", 0, func() { done <- true })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("no move")
	}
	if g.Holes[want.To] != 0 || g.Turn != 1 {
		t.Errorf("not the move picked: turn %d", g.Turn)
	}
	fail = errors.New("no answer")
	for range GiveUp + 1 {
		before := g.Turn
		d.Next(g, "p", 0, func() { done <- true })
		<-done
		if g.Turn == before {
			t.Fatal("the computer did not move for the model")
		}
	}
	if asked != GiveUp+1 || !d.off {
		t.Errorf("asked %d times, off %v", asked, d.off)
	}
}
