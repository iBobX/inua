package gamesui

import (
	"math"
	"testing"

	"github.com/lyricat/inua/internal/games"
)

// Each hole is found where it is drawn, the board turned any of the six
// ways; its point's foot is at the bottom when turned for that point.
func TestCheckersHoles(t *testing.T) {
	for k := range games.Corners {
		p := &checkers{angle: float64(3-k) * math.Pi / 3}
		for h := range games.StarHoles {
			x, y := p.at(h)
			if got := p.holeAt(games.Vec{X: x + 3, Y: y - 3}); got != h {
				t.Fatalf("turned for %d: hole %d found as %d", k, h, got)
			}
		}
		tip := games.CornerHoles(k)[0]
		if _, y := p.at(tip); y < ckSide/2+6.9*ckUnit {
			t.Errorf("turned for %d: its tip at y %.0f, not at the foot", k, y)
		}
	}
	if (&checkers{}).holeAt(games.Vec{X: 1, Y: 1}) != -1 {
		t.Error("a hole in the corner of the canvas")
	}
	if n := neighbours(games.HoleAt(games.Hole{})); len(n) != 6 {
		t.Errorf("the middle has %d neighbours", len(n))
	}
}
