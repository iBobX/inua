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
