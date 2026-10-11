// Package games contains the rules for the light games, independent of GTK.
package games

import "math/rand/v2"

type Outcome uint8

const (
	Playing Outcome = iota
	Won
	Lost
)

type Point struct{ X, Y int }

func (p Point) Add(q Point) Point    { return Point{p.X + q.X, p.Y + q.Y} }
func (p Point) Inside(w, h int) bool { return p.X >= 0 && p.Y >= 0 && p.X < w && p.Y < h }

var Directions = [...]Point{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}

func adjacent(a, b Point) bool {
	d := a.X - b.X
	e := a.Y - b.Y
	return d*d+e*e == 1
}

// LastTile has ten repeatable maps. A seeded self-avoiding walk provides
// a ten-step route through every coin; extra tiles allow shorter escapes.
type LastTile struct {
	Level, Steps, Coins int
	Player, Exit        Point
	Tiles, Gold         [25]bool
	Result              Outcome
}

func NewLastTile(level int) *LastTile {
	g := &LastTile{Level: level % 10, Player: Point{0, 4}}
	r := rand.New(rand.NewPCG(uint64(g.Level+1), 41))
	var path []Point
	for {
		path = []Point{g.Player}
		used := map[Point]bool{g.Player: true}
		for len(path) < 11 {
			var next []Point
			for _, d := range Directions {
				p := path[len(path)-1].Add(d)
				if p.Inside(5, 5) && !used[p] {
					next = append(next, p)
				}
			}
			if len(next) == 0 {
				break
			}
			p := next[r.IntN(len(next))]
			used[p] = true
			path = append(path, p)
		}
		if len(path) == 11 && path[10].Y < 3 {
			break
		}
	}
	for i := range g.Tiles {
		g.Tiles[i] = r.IntN(4) != 0
	}
	for i, p := range path {
		g.Tiles[p.Y*5+p.X] = true
		if i > 0 && i < 10 && i%2 == 0 {
			g.Gold[p.Y*5+p.X] = true
		}
	}
	g.Exit = path[10]
	return g
}

func (g *LastTile) Move(p Point) {
	if g.Result != Playing || !p.Inside(5, 5) || !adjacent(g.Player, p) || !g.Tiles[p.Y*5+p.X] {
		return
	}
	g.Tiles[g.Player.Y*5+g.Player.X] = false
	g.Player = p
	g.Steps++
	if g.Gold[p.Y*5+p.X] {
		g.Coins++
		g.Gold[p.Y*5+p.X] = false
	}
	if p == g.Exit {
		g.Result = Won
		return
	}
	if g.Steps >= 10 {
		g.Result = Lost
		return
	}
	for _, d := range Directions {
		q := p.Add(d)
		if q.Inside(5, 5) && g.Tiles[q.Y*5+q.X] {
			return
		}
	}
	g.Result = Lost
}

// ClockOut advances every patrol only after a valid move or a wait.
// Crossing a patrol is a collision too; waiting is part of the puzzle.
type ClockOut struct {
	Level, Steps int
	Player       Point
	Walls        [36]bool
	Routes       [][]Point
	Result       Outcome
}

func NewClockOut(level int) *ClockOut {
	g := &ClockOut{Level: level % 3, Player: Point{0, 5}}
	for _, p := range []Point{{1, 1}, {1, 2}, {3, 3}, {4, 3}, {3, 0}} {
		g.Walls[p.Y*6+p.X] = true
	}
	g.Routes = [][]Point{
		{{0, 3}, {1, 3}, {2, 3}, {2, 2}, {2, 1}, {2, 2}, {2, 3}, {1, 3}},
		{{3, 1}, {4, 1}, {5, 1}, {5, 2}, {4, 2}, {3, 2}},
	}
	return g
}

func (g *ClockOut) Guards(step int) []Point {
	out := make([]Point, len(g.Routes))
	for i, route := range g.Routes {
		out[i] = route[(step+g.Level*2)%len(route)]
	}
	return out
}

func (g *ClockOut) Move(p Point) {
	if g.Result != Playing || !p.Inside(6, 6) || (p != g.Player && !adjacent(g.Player, p)) || g.Walls[p.Y*6+p.X] {
		return
	}
	before := g.Guards(g.Steps)
	old := g.Player
	g.Player = p
	g.Steps++
	for i, guard := range g.Guards(g.Steps) {
		if guard == p || (before[i] == p && guard == old) {
			g.Result = Lost
			return
		}
	}
	if p == (Point{5, 0}) {
		g.Result = Won
	} else if g.Steps >= 30 {
		g.Result = Lost
	}
}

type Food struct {
	Name, Mark string
	Shape      []Point
	Frozen     bool
}

var Foods = []Food{
	{"Ice cream", "I", []Point{{0, 0}, {1, 0}, {0, 1}}, true},
	{"Peas", "P", []Point{{0, 0}, {1, 0}, {0, 1}}, true},
	{"Fish", "F", []Point{{0, 0}, {1, 0}, {2, 0}}, true},
	{"Berries", "B", []Point{{0, 0}, {1, 0}, {2, 0}}, true},
	{"Milk", "M", []Point{{0, 0}, {1, 0}, {0, 1}, {1, 1}}, false},
	{"Eggs", "E", []Point{{0, 0}, {1, 0}, {0, 1}, {1, 1}}, false},
	{"Tofu", "T", []Point{{0, 0}, {1, 0}, {0, 1}, {1, 1}}, false},
	{"Carrots", "C", []Point{{0, 0}, {1, 0}, {2, 0}}, false},
	{"Greens", "G", []Point{{0, 0}, {1, 0}, {2, 0}}, false},
}

type Fridge struct {
	Cells              [30]int // zero is empty, food index + 1 otherwise
	Placed             [9]bool
	Selected, Rotation int
	Result             Outcome
}

func (g *Fridge) Shape() []Point {
	shape := append([]Point(nil), Foods[g.Selected].Shape...)
	for range g.Rotation {
		minX, minY := 99, 99
		for i, p := range shape {
			shape[i] = Point{-p.Y, p.X}
			minX = min(minX, -p.Y)
			minY = min(minY, p.X)
		}
		for i := range shape {
			shape[i].X -= minX
			shape[i].Y -= minY
		}
	}
	return shape
}

func (g *Fridge) Place(at Point) bool {
	if g.Result != Playing || g.Placed[g.Selected] {
		return false
	}
	shape := g.Shape()
	for _, d := range shape {
		p := at.Add(d)
		if !p.Inside(6, 5) || g.Cells[p.Y*6+p.X] != 0 || (p.Y < 2) != Foods[g.Selected].Frozen {
			return false
		}
	}
	for _, d := range shape {
		p := at.Add(d)
		g.Cells[p.Y*6+p.X] = g.Selected + 1
	}
	g.Placed[g.Selected] = true
	for i, placed := range g.Placed {
		if !placed {
			g.Selected, g.Rotation = i, 0
			return true
		}
	}
	g.Result = Won
	return true
}

func (g *Fridge) Remove(at Point) {
	if !at.Inside(6, 5) {
		return
	}
	id := g.Cells[at.Y*6+at.X]
	if id == 0 {
		return
	}
	for i, v := range g.Cells {
		if v == id {
			g.Cells[i] = 0
		}
	}
	g.Placed[id-1] = false
	g.Selected, g.Rotation, g.Result = id-1, 0, Playing
}
