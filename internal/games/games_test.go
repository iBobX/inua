package games

import (
	"math"
	"testing"
)

func TestLastTileMapsHaveFullCoinEscape(t *testing.T) {
	var solve func(LastTile) bool
	solve = func(g LastTile) bool {
		if g.Result == Won {
			return g.Coins == 4
		}
		if g.Result != Playing {
			return false
		}
		for _, d := range Directions {
			next := g
			next.Move(g.Player.Add(d))
			if next.Steps > g.Steps && solve(next) {
				return true
			}
		}
		return false
	}
	for i := 0; i < 10; i++ {
		g := NewLastTile(i)
		if !solve(*g) {
			t.Fatalf("map %d cannot collect all four coins and escape", i)
		}
		before := *g
		g.Move(Point{4, 0})
		if *g != before {
			t.Fatal("nonadjacent move changed board")
		}
		g.Result = Lost
		before = *g
		g.Move(Point{0, 3})
		if *g != before {
			t.Fatal("terminal board accepted a move")
		}
	}
}

func TestOfficeRoutesHaveEscape(t *testing.T) {
	for level := 0; level < OfficeLevels; level++ {
		g0 := NewClockOut(level)
		for i, r := range g0.Routes {
			for j, p := range r {
				if q := r[(j+1)%len(r)]; !adjacent(p, q) || g0.Wall(p) || !p.Inside(g0.W, g0.H) {
					t.Fatalf("office %d: route %d breaks at %v", level, i, p)
				}
			}
		}
		queue := []ClockOut{*g0}
		seen := map[[3]int]bool{}
		won := false
		for len(queue) > 0 {
			g := queue[0]
			queue = queue[1:]
			if g.Result == Won {
				won = true
				break
			}
			if g.Result != Playing {
				continue
			}
			key := [3]int{g.Player.X, g.Player.Y, g.Steps}
			if seen[key] {
				continue
			}
			seen[key] = true
			for _, d := range append(Directions[:], Point{}) {
				next := g
				next.Move(g.Player.Add(d))
				if next.Steps > g.Steps {
					queue = append(queue, next)
				}
			}
		}
		if !won {
			t.Fatalf("office %d is not solvable", level)
		}
	}
}

func TestOfficeCrossingPatrolLoses(t *testing.T) {
	g := NewClockOut(0)
	g.Player = Point{1, 3}
	g.Move(Point{0, 3}) // patrol moves from 0,3 to 1,3
	if g.Result != Lost {
		t.Fatal("crossing a patrol must lose")
	}
}

func TestMinesFirstRevealAndWin(t *testing.T) {
	for _, first := range []int{0, 40, 80} {
		for range 30 {
			g := NewMines(9, 9, 10)
			g.ToggleFlag(first)
			g.Reveal(first)
			if g.Started {
				t.Fatal("flagged first square started the board")
			}
			g.ToggleFlag(first)
			g.Reveal(first)
			if g.Result == Lost || g.Around(first) != 0 {
				t.Fatal("first click not protected")
			}
			bombs := 0
			for i, b := range g.Bomb {
				if b {
					bombs++
				} else {
					g.Reveal(i)
				}
			}
			if bombs != 10 || g.Result != Won {
				t.Fatalf("bombs=%d result=%d", bombs, g.Result)
			}
		}
	}
}

func TestMinesWrongFlagChordLoses(t *testing.T) {
	g := NewMines(4, 4, 1)
	g.Started, g.Bomb[0], g.Open[5], g.Flag[1] = true, true, true, true
	g.Reveal(5)
	if g.Result != Lost {
		t.Fatal("chording with a wrong flag must expose the mine")
	}
	before := append([]bool(nil), g.Open...)
	g.Reveal(15)
	for i, v := range before {
		if g.Open[i] != v {
			t.Fatal("lost board changed")
		}
	}
}

func TestShopAllocationAndSettlement(t *testing.T) {
	g := NewShop()
	if g.EndDay() {
		t.Fatal("unassigned dice accepted")
	}
	g.Dice = [3]int{2, 4, 6}
	g.Assign(0, 0)
	g.Assign(1, 0)
	if g.Jobs[0] != -1 {
		t.Fatal("die assigned twice")
	}
	g.Assign(0, 0)
	g.Assign(1, 1)
	g.Assign(2, 2)
	if !g.EndDay() || g.Cash != 35 || g.Stock != 0 || g.Condition != 8 || g.Day != 2 {
		t.Fatalf("unexpected settlement: %+v", g)
	}
	g.Day, g.Cash, g.Stock, g.Condition = 7, 80, 0, 0
	g.Dice, g.Jobs = [3]int{1, 1, 1}, [3]int{0, 1, 2}
	g.EndDay()
	if g.Result != Won {
		t.Fatal("cash target not settled on day 7")
	}
	cash := g.Cash
	g.EndDay()
	if g.Cash != cash {
		t.Fatal("terminal shop settled again")
	}
}

func TestGroupsSelectionAndTerminalStates(t *testing.T) {
	for puzzle := range GroupPuzzles {
		g := NewGroups(puzzle)
		g.Toggle(0)
		g.Submit()
		if g.Mistakes != 0 {
			t.Fatal("incomplete guess consumed an attempt")
		}
		g.Selected = [12]bool{}
		for id := 0; id < 12; id++ {
			g.Toggle(id)
			if id%4 == 3 {
				g.Submit()
			}
		}
		if g.Result != Won {
			t.Fatalf("puzzle %d not solved", puzzle)
		}
	}
	g := NewGroups(0)
	for _, id := range []int{0, 1, 2, 4} {
		g.Toggle(id)
	}
	g.Submit()
	g.Submit() // the same guess again: pointed out, not counted
	if g.Mistakes != 1 || g.Message != "Already tried." {
		t.Fatalf("a repeated guess: %d mistakes, %q", g.Mistakes, g.Message)
	}
	for _, other := range []int{5, 6, 8} { // three more, each different
		g.Selected = [12]bool{}
		for _, id := range []int{0, 1, 2, other} {
			g.Toggle(id)
		}
		g.Submit()
	}
	if g.Result != Lost || g.Mistakes != 4 {
		t.Fatal("four wrong guesses should end puzzle")
	}
	g.Submit()
	if g.Mistakes != 4 {
		t.Fatal("terminal puzzle accepted guess")
	}
}

func TestHarborDockCollisionAndClock(t *testing.T) {
	g := NewHarbor(0)
	g.Position, g.Angle = Vec{520, 80}, 0
	g.Step(0.01, false)
	if g.Result != Won {
		t.Fatal("aligned docking not recognized")
	}
	g = NewHarbor(0)
	g.Position = Vec{210, 235}
	g.Step(0.01, false)
	if g.Result != Lost {
		t.Fatal("rock collision not recognized")
	}
	g = NewHarbor(0)
	g.Elapsed = 59.99
	g.Step(0.02, false)
	if g.Result != Lost {
		t.Fatal("time limit not enforced")
	}
}

func TestHarborCoursesHaveRoute(t *testing.T) {
	for level := 0; level < 3; level++ {
		queue := []Harbor{*NewHarbor(level)}
		seen := map[[3]int]bool{}
		won := false
		for head := 0; head < len(queue); head++ {
			g := queue[head]
			if g.Result == Won {
				won = true
				break
			}
			if g.Result != Playing {
				continue
			}
			angle := math.Mod(g.Angle+2*math.Pi, 2*math.Pi)
			key := [3]int{int(g.Position.X / 10), int(g.Position.Y / 10), int(angle / 0.15)}
			if seen[key] {
				continue
			}
			seen[key] = true
			for _, turn := range []bool{false, true} {
				next := g
				for range 8 {
					next.Step(1.0/30, turn)
				}
				if next.Result != Lost {
					queue = append(queue, next)
				}
			}
		}
		if !won {
			t.Fatalf("course %d has no docking route", level)
		}
	}
}
