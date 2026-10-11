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
	for level := 0; level < 3; level++ {
		queue := []ClockOut{*NewClockOut(level)}
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
			key := [3]int{g.Player.X, g.Player.Y, g.Steps % 24}
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
			t.Fatalf("shift %d is not solvable", level)
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

func TestFridgePackingAndRemoval(t *testing.T) {
	g := &Fridge{}
	if g.Place(Point{0, 2}) {
		t.Fatal("frozen food allowed on fresh shelf")
	}
	placements := []struct {
		at       Point
		rotation int
	}{
		{Point{0, 0}, 0}, {Point{1, 0}, 2}, {Point{3, 0}, 0}, {Point{3, 1}, 0},
		{Point{0, 2}, 0}, {Point{2, 2}, 0}, {Point{4, 2}, 0}, {Point{0, 4}, 0}, {Point{3, 4}, 0},
	}
	for i, p := range placements {
		g.Selected, g.Rotation = i, p.rotation
		if !g.Place(p.at) {
			t.Fatalf("food %d did not fit", i)
		}
	}
	if g.Result != Won {
		t.Fatal("complete packing did not win")
	}
	for _, v := range g.Cells {
		if v == 0 {
			t.Fatal("packing left a hole")
		}
	}
	g.Remove(Point{0, 0})
	if g.Result != Playing || g.Placed[0] {
		t.Fatal("cannot revise a completed packing")
	}
	if !g.Place(Point{0, 0}) || g.Result != Won {
		t.Fatal("replacing a removed food did not finish")
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
	for range 4 {
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

func TestFortunePurchaseRentAndRoundLimit(t *testing.T) {
	g := NewFortune()
	g.Roll(1)
	g.Buy()
	if g.Properties[1].Owner != 1 || g.Players[0].Cash != 520 {
		t.Fatal("purchase not charged exactly once")
	}
	g.Buy()
	g.Roll(1)
	if g.Players[0].Cash != 520 || g.Players[0].Position != 1 {
		t.Fatal("extra action in end phase")
	}
	g.Players[1].Position = 0
	g.land(1, 1)
	if g.Players[0].Cash != 540 || g.Players[1].Cash != 580 {
		t.Fatal("rent transfer wrong")
	}
	g.Players[0].Cash, g.Players[1].Cash = 10000, 10000
	for g.Result == Playing {
		if g.Phase == "roll" {
			g.Roll(1)
		}
		g.EndTurn()
	}
	if g.Round != 12 {
		t.Fatalf("ended in round %d", g.Round)
	}
	worth := g.Worth(0)
	g.EndTurn()
	g.Roll(1)
	if g.Worth(0) != worth {
		t.Fatal("game changed after settlement")
	}
}

func TestFortuneBankruptcyAndJail(t *testing.T) {
	g := NewFortune()
	g.Players[0] = Player{Position: 4, Cash: 10}
	g.Roll(1)
	if g.Result != Lost || g.Winner != 1 {
		t.Fatal("tax bankruptcy not immediate")
	}
	g = NewFortune()
	g.Players[0].Position = 14
	g.Roll(1)
	if g.Players[0].Skip != 1 {
		t.Fatal("jail did not schedule skipped turn")
	}
	g.Phase = "roll"
	g.Roll(6)
	if g.Players[0].Position != 15 || g.Players[0].Skip != 0 || g.Phase != "end" {
		t.Fatal("jail turn not consumed")
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

func TestPinballBumperFlipperDrain(t *testing.T) {
	g := NewPinball()
	g.Active, g.Ball, g.Velocity = true, Vec{135, 112}, Vec{0, 200}
	g.Step(0.02, false, false)
	if g.Score != 100 || g.Velocity.Y >= 0 {
		t.Fatalf("bumper response: %+v", g)
	}
	g.Ball, g.Velocity = Vec{150, 460}, Vec{0, 250}
	for range 10 {
		g.Step(0.01, true, false)
	}
	if g.Velocity.Y >= 0 {
		t.Fatalf("raised flipper did not return the ball: %+v", g)
	}
	for lives := 2; lives >= 0; lives-- {
		g.Active, g.Ball = true, Vec{210, 581}
		g.Velocity = Vec{0, 100}
		g.Step(0.01, false, false)
		if g.Lives != lives || g.Active {
			t.Fatal("drain did not consume exactly one ball")
		}
	}
	g.Launch()
	if g.Active || g.Result != Lost {
		t.Fatal("launch accepted after third ball")
	}
}

func TestPinballStaysFinite(t *testing.T) {
	g := NewPinball()
	g.Launch()
	for i := 0; i < 30000; i++ {
		if !g.Active && g.Result == Playing {
			g.Launch()
		}
		g.Step(1.0/120, i%71 < 20, i%83 < 24)
		if math.IsNaN(g.Ball.X) || math.IsNaN(g.Ball.Y) || math.Hypot(g.Velocity.X, g.Velocity.Y) > 900.001 {
			t.Fatalf("unstable simulation at step %d: %+v", i, g)
		}
		if g.Result != Playing {
			return
		}
	}
	t.Fatal("three-ball simulation did not finish in 250 seconds")
}

func TestPinballLaunchEntersUpperPlayfield(t *testing.T) {
	g := NewPinball()
	g.Launch()
	for range 360 {
		g.Step(1.0/120, false, false)
		if g.Ball.X < 340 && g.Ball.Y < 200 && g.Active {
			return
		}
		if g.Lives != 3 {
			t.Fatal("launch drained before reaching the playfield")
		}
	}
	t.Fatal("launch did not leave the chute")
}
