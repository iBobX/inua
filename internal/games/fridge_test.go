package games

import "testing"

// solve packs the rest of the fridge, filling the first empty square
// each time; true when everything fits.
func solve(g *Fridge) bool {
	first := -1
	for i, v := range g.Cells {
		if v == 0 {
			first = i
			break
		}
	}
	if first < 0 {
		return g.Result == Won
	}
	at := Point{first % g.W, first / g.W}
	for f := range g.Foods {
		if g.Placed[f] {
			continue
		}
		for r := range 4 {
			shape := Rotate(g.Foods[f].Shape, r)
			// The shape's first square, in reading order, goes on at.
			lead := shape[0]
			for _, p := range shape {
				if p.Y < lead.Y || p.Y == lead.Y && p.X < lead.X {
					lead = p
				}
			}
			corner := Point{at.X - lead.X, at.Y - lead.Y}
			if g.Place(f, r, corner) {
				if solve(g) {
					return true
				}
				g.Remove(at)
			}
		}
	}
	return false
}

func TestFridgesCanBePacked(t *testing.T) {
	for level := range FridgeLevels {
		g := NewFridge(level)
		cells := 0
		for _, f := range g.Foods {
			cells += len(f.Shape)
		}
		if cells != g.W*g.H {
			t.Fatalf("fridge %d: %d squares of food for %d of room", level, cells, g.W*g.H)
		}
		if !solve(g) {
			t.Fatalf("fridge %d cannot be packed", level)
		}
	}
}

func TestFridgeZonesAndRemoval(t *testing.T) {
	g := NewFridge(0)
	if g.Place(0, 0, Point{0, 2}) {
		t.Fatal("frozen food on a fresh shelf")
	}
	if !g.Place(0, 0, Point{0, 0}) || g.Place(0, 0, Point{3, 0}) {
		t.Fatal("placed once, and only once")
	}
	if g.Place(1, 0, Point{0, 0}) {
		t.Fatal("food on top of food")
	}
	if g.Remove(Point{1, 0}) != 0 || g.Placed[0] || g.Cells[0] != 0 {
		t.Fatal("removal")
	}
	if len(Rotate(shapeL4, 1)) != 4 || Rotate(shapeI3, 1)[2] != (Point{0, 2}) {
		t.Fatalf("rotation %v", Rotate(shapeI3, 1))
	}
}
