// Package games contains the rules for the light games, independent of GTK.
package games

type Outcome uint8

const (
	Playing Outcome = iota
	Won
	Lost
)

type Point struct{ X, Y int }

func (p Point) Add(q Point) Point    { return Point{p.X + q.X, p.Y + q.Y} }
func (p Point) Inside(w, h int) bool { return p.X >= 0 && p.Y >= 0 && p.X < w && p.Y < h }
