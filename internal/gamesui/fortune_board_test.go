package gamesui

import (
	"math"
	"slices"
	"testing"

	"github.com/lyricat/inua/internal/games"
)

// On every city's track, a click on a square's middle finds that square
// through the camera's projection; off the track, none.
func TestSquareAtScreen(t *testing.T) {
	for city := range games.FortuneCities {
		tr := trackOf(city)
		for i, tl := range tr.tiles {
			if got := squareAtScreen(city, project(tl.center.X, tl.center.Y, 0)); got != i {
				t.Errorf("city %d: square %d's middle: got %d", city, i, got)
			}
		}
		for _, p := range []games.Vec{project(2, 2, 0), project(fBoard-2, fBoard-2, 0), project(fBoard+20, 300, 0)} {
			if got := squareAtScreen(city, p); got != -1 {
				t.Errorf("city %d: %v: got square %d", city, p, got)
			}
		}
	}
}

// Every pawn's place is on its own square, in jail too; every track lies
// on the paper.
func TestTokenAt(t *testing.T) {
	for city := range games.FortuneCities {
		for sq := range games.Board {
			for pi := range 4 {
				if p := tokenAt(city, sq, pi, false); squareAt(city, p.X, p.Y) != sq {
					t.Errorf("city %d: player %d on square %d stands on %d", city, pi, sq, squareAt(city, p.X, p.Y))
				}
			}
		}
		for pi := range 4 {
			if p := tokenAt(city, games.JailAt, pi, true); squareAt(city, p.X, p.Y) != games.JailAt {
				t.Errorf("city %d: player %d in jail stands on %d", city, pi, squareAt(city, p.X, p.Y))
			}
		}
		tr := trackOf(city)
		for _, p := range append(slices.Clone(tr.outer), tr.inner...) {
			if p.X < 4 || p.Y < 4 || p.X > fBoard-4 || p.Y > fBoard-4 {
				t.Errorf("city %d: the track runs off the paper at %v", city, p)
				break
			}
		}
	}
}

// Cash changing between two views starts an effect for each player whose
// cash changed, in its tone, until the effect has played.
func TestMoneyFX(t *testing.T) {
	p := &fortune{}
	p.moneyFrom(games.FortuneView{Cash: []int{1500, 1500, 1500}}, games.FortuneView{Cash: []int{1700, 1500, 1380}})
	if len(p.fx) != 2 || p.fx[0].player != 0 || p.fx[0].amount != 200 || p.fx[1].player != 2 || p.fx[1].amount != -120 {
		t.Fatalf("effects %+v", p.fx)
	}
	if p.moneyTone(0) != 1 || p.moneyTone(1) != 0 || p.moneyTone(2) != -1 {
		t.Errorf("tones %d %d %d", p.moneyTone(0), p.moneyTone(1), p.moneyTone(2))
	}
	if p.stepFX(fxTime/2) || len(p.fx) != 2 {
		t.Error("an effect ended half way")
	}
	if !p.stepFX(fxTime) || len(p.fx) != 0 || p.moneyTone(0) != 0 {
		t.Errorf("effects left %+v", p.fx)
	}
}

// On every city's board, the slip, the dice, the title, the trees and the
// landmarks stand clear of the roads.
func TestCityFurnitureClear(t *testing.T) {
	for city := range games.FortuneCities {
		art := cityArts[city]
		check := func(what string, p games.Vec, r float64) {
			if d := roadDistance(city, p); d < r {
				t.Errorf("city %d: %s at %v is %.0f from a road, want %.0f", city, what, p, d, r)
			}
		}
		for _, d := range []float64{-92, 0, 92} { // the slip, along its width
			check("the slip", games.Vec{X: art.slip.X + d, Y: art.slip.Y}, 12)
		}
		check("the dice", art.dice, 30)
		for _, d := range []float64{-70, 0, 70} { // printed under the roads: a nearer road only hides its edge
			check("the title", games.Vec{X: cityTitles[city].X + d, Y: cityTitles[city].Y + 20}, 20)
		}
		for _, tr := range art.trees {
			check("a tree", games.Vec{X: tr[0], Y: tr[1]}, tr[2]*0.5)
		}
		for _, l := range landmarksOf(city) {
			check("a landmark", l.at, 26)
		}
	}
}

// roadDistance is how far p is from the nearest road square: 0 on one.
func roadDistance(city int, p games.Vec) float64 {
	best := math.Inf(1)
	for _, tl := range trackOf(city).tiles {
		if inside(tl.outline, p) {
			return 0
		}
		for i, a := range tl.outline {
			b := tl.outline[(i+1)%len(tl.outline)]
			best = min(best, segmentDistance(p, a, b))
		}
	}
	return best
}

func segmentDistance(p, a, b games.Vec) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	l := dx*dx + dy*dy
	k := 0.0
	if l > 0 {
		k = max(0, min(1, ((p.X-a.X)*dx+(p.Y-a.Y)*dy)/l))
	}
	return math.Hypot(p.X-a.X-k*dx, p.Y-a.Y-k*dy)
}
