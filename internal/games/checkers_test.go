package games

import (
	"math"
	"slices"
	"testing"
)

// The star: 121 holes, six points of ten clockwise from the top, each
// across from the one three on; neighbours both ways.
func TestStar(t *testing.T) {
	if len(StarHoles) != 121 {
		t.Fatalf("%d holes", len(StarHoles))
	}
	middle := 0
	for i := range StarHoles {
		if CornerOf(i) < 0 {
			middle++
		}
		for d, n := range starNeighbors[i] {
			if n >= 0 && starNeighbors[n][(d+3)%6] != i {
				t.Fatalf("hole %d's neighbour %d does not see it back", i, n)
			}
		}
	}
	if middle != 61 {
		t.Errorf("%d holes in the middle", middle)
	}
	last := -1.0
	for k := range Corners {
		holes := CornerHoles(k)
		if len(holes) != 10 {
			t.Fatalf("point %d has %d holes", k, len(holes))
		}
		// Its tip is the farthest out, and the points go round clockwise
		// from the top, y down.
		x, y := StarXY(holes[0])
		if r := math.Hypot(x, y); math.Abs(r-8*0.8660254037844386) > 0.01 && math.Abs(r-8) > 0.01 {
			t.Errorf("point %d's tip at %.2f", k, r)
		}
		angle := math.Mod(math.Atan2(x, -y)*180/math.Pi+360, 360)
		if k > 0 && angle <= last {
			t.Errorf("point %d at %.0f°, after %.0f°", k, angle, last)
		}
		last = angle
		ox, oy := StarXY(CornerHoles(Opposite(k))[0])
		if math.Abs(x+ox) > 0.01 || math.Abs(y+oy) > 0.01 {
			t.Errorf("point %d's tip is not across from %d's", k, Opposite(k))
		}
		if Distance(Opposite(k), holes[0]) != 16 || Distance(k, holes[0]) != 0 {
			t.Errorf("point %d: distances %d, %d", k, Distance(Opposite(k), holes[0]), Distance(k, holes[0]))
		}
	}
}

func seats(n int) CheckersSettings {
	var s CheckersSettings
	for i := range n {
		s.Players = append(s.Players, CheckersSeat{Name: string(rune('A' + i)), Team: i})
	}
	return s
}

// Each player starts with ten marbles in their point, racing to the one
// across; two, three, four or six play.
func TestCheckersSetup(t *testing.T) {
	for n, corners := range SeatCorners {
		g := NewCheckers(seats(n))
		for i, p := range g.Players {
			if p.Start != corners[i] || p.Goal != Opposite(corners[i]) || len(g.Marbles(i)) != 10 {
				t.Errorf("%d players: %d starts %d, goal %d, %d marbles", n, i, p.Start, p.Goal, len(g.Marbles(i)))
			}
			if g.Left(i) != 140 { // 16·1 + 15·2 + 14·3 + 13·4
				t.Errorf("%d players: %d has %d to go", n, i, g.Left(i))
			}
		}
	}
	if g := NewCheckers(seats(5)); len(g.Players) != 2 {
		t.Errorf("five players: %d", len(g.Players))
	}
}

// emptyGame is a game of two with the board cleared, to set by hand.
func emptyGame() *Checkers {
	g := NewCheckers(seats(2))
	for i := range g.Holes {
		g.Holes[i] = -1
	}
	return g
}

func hole(x, z int) int { return HoleAt(Hole{x, -x - z, z}) }

// A marble steps to a neighbour, or hops over marbles, on as long as it
// can, never through a hole twice; it cannot step after a hop.
func TestCheckersReach(t *testing.T) {
	g := emptyGame()
	from := hole(0, 0)
	g.Holes[from] = 0
	if r := g.Reach(from); len(r) != 6 {
		t.Errorf("alone in the middle: %d places", len(r))
	}
	// Marbles to the right at 1 and 3: hop to 2, then on to 4.
	g.Holes[hole(1, 0)], g.Holes[hole(3, 0)] = 1, 1
	r := g.Reach(from)
	if p := r[hole(4, 0)]; !slices.Equal(p, []int{from, hole(2, 0), hole(4, 0)}) {
		t.Errorf("the chain: %v", p)
	}
	if _, ok := r[hole(5, 0)]; ok {
		t.Error("stepped after a hop")
	}
	if _, ok := r[hole(1, 0)]; ok {
		t.Error("stopped on a marble")
	}
	// Someone else's marble is not the turn's to move.
	if len(g.Reach(hole(1, 0))) != 0 {
		t.Error("moved another player's marble")
	}
	if !g.Move(from, hole(4, 0)) || g.Holes[hole(4, 0)] != 0 || g.Holes[from] != -1 || g.Turn != 1 {
		t.Errorf("the move: turn %d", g.Turn)
	}
	if e := g.Take(); len(e) != 1 || e[0].Kind != CkMove || len(e[0].Path) != 3 {
		t.Errorf("events %+v", e)
	}
	if g.Players[0].Hops != 2 || g.Players[0].Moves != 1 {
		t.Errorf("counted %+v", g.Players[0])
	}
}

// With NoParking a marble crosses another's point but does not stop there.
func TestCheckersNoParking(t *testing.T) {
	g := emptyGame()
	g.NoParking = true
	// Player 0 starts at the top (0) and goes to the bottom (3); point 1 is
	// no one's here.
	p1 := CornerHoles(1)
	edge := p1[len(p1)-1] // the point's hole nearest the middle
	var from int
	for _, n := range starNeighbors[edge] {
		if n >= 0 && CornerOf(n) < 0 {
			from = n
			break
		}
	}
	g.Holes[from] = 0
	if _, ok := g.Reach(from)[edge]; ok {
		t.Error("stopped in another point")
	}
	g.NoParking = false
	if _, ok := g.Reach(from)[edge]; !ok {
		t.Error("could not stop there with the rule off")
	}
}

// Home: every hole of the goal filled, some with your own; a marble left
// in it by another counts as filled. The last of a team home wins.
func TestCheckersHome(t *testing.T) {
	g := emptyGame()
	goal := CornerHoles(g.Players[0].Goal)
	for _, h := range goal[:9] {
		g.Holes[h] = 0
	}
	last := goal[9]
	var from int
	for _, n := range starNeighbors[last] {
		if n >= 0 && CornerOf(n) < 0 {
			from = n
		}
	}
	g.Holes[from] = 0
	g.Holes[CornerHoles(g.Players[1].Start)[0]] = 1
	if !g.Move(from, last) || !g.Players[0].Home || !g.Over || g.WinTeam != 0 {
		t.Fatalf("home %v, over %v, team %d", g.Players[0].Home, g.Over, g.WinTeam)
	}
	e := g.Take()
	if len(e) != 3 || e[1].Kind != CkHome || e[2].Kind != CkWin || !slices.Equal(e[2].Players, []int{0}) {
		t.Errorf("events %+v", e)
	}
	if s := g.Standings(); s[0] != 0 {
		t.Errorf("standings %v", s)
	}

	// The goal blocked by the other's marble: filled, home all the same.
	g = emptyGame()
	for _, h := range goal[:8] {
		g.Holes[h] = 0
	}
	g.Holes[goal[8]] = 1
	g.Holes[from] = 0
	g.Holes[CornerHoles(g.Players[1].Start)[0]] = 1
	if !g.Move(from, last) || !g.Players[0].Home {
		t.Error("a blocked goal does not count")
	}
}

// Partners: a player home moves a partner's marbles on their turns; the
// team wins when both are home.
func TestCheckersTeams(t *testing.T) {
	s := seats(4)
	s.Players[0].Team, s.Players[2].Team = 0, 0
	s.Players[1].Team, s.Players[3].Team = 1, 1
	g := NewCheckers(s)
	for i := range g.Holes {
		g.Holes[i] = -1
	}
	// Player 0 home already; player 2 one move from it.
	for _, h := range CornerHoles(g.Players[0].Goal) {
		g.Holes[h] = 0
	}
	g.checkHome()
	if !g.Players[0].Home || g.Over {
		t.Fatalf("one partner home: home %v, over %v", g.Players[0].Home, g.Over)
	}
	g.Take()
	goal2 := CornerHoles(g.Players[2].Goal)
	for _, h := range goal2[:9] {
		g.Holes[h] = 2
	}
	var from int
	for _, n := range starNeighbors[goal2[9]] {
		if n >= 0 && CornerOf(n) < 0 {
			from = n
		}
	}
	g.Holes[from] = 2
	for _, k := range []int{1, 3} { // the others, somewhere
		g.Holes[CornerHoles(g.Players[k].Start)[0]] = k
	}
	g.Turn = 0
	if !slices.Equal(g.Movers(), []int{2}) {
		t.Fatalf("player 0 home moves %v", g.Movers())
	}
	if !g.Move(from, goal2[9]) || !g.Over || g.WinTeam != 0 {
		t.Errorf("the team: over %v, team %d", g.Over, g.WinTeam)
	}
}

// A game no one finishes is a draw after its rounds.
func TestCheckersDraw(t *testing.T) {
	s := seats(2)
	s.MaxRounds = 2
	g := NewCheckers(s)
	for !g.Over {
		moved := false
		for _, h := range g.Marbles(g.Movers()[0]) {
			for to := range g.Reach(h) {
				moved = g.Move(h, to)
				break
			}
			if moved {
				break
			}
		}
		if !moved {
			t.Fatal("no move")
		}
	}
	if g.WinTeam != -1 || g.Round != 3 {
		t.Errorf("team %d, round %d", g.WinTeam, g.Round)
	}
}
