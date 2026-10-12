package games

import "math"

// Vec is a point or a velocity on a game's table, in its own units.
type Vec struct{ X, Y float64 }

func (v Vec) Add(w Vec) Vec             { return Vec{v.X + w.X, v.Y + w.Y} }
func (v Vec) Sub(w Vec) Vec             { return Vec{v.X - w.X, v.Y - w.Y} }
func (v Vec) Mul(k float64) Vec         { return Vec{v.X * k, v.Y * k} }
func (v Vec) Dot(w Vec) float64         { return v.X*w.X + v.Y*w.Y }
func (v Vec) Len() float64              { return math.Hypot(v.X, v.Y) }
func (v Vec) Lerp(w Vec, t float64) Vec { return v.Add(w.Sub(v).Mul(t)) }

// Unit is v scaled to length 1; the zero vector stays zero.
func (v Vec) Unit() Vec {
	if l := v.Len(); l > 0 {
		return v.Mul(1 / l)
	}
	return Vec{}
}

// closest is the point of segment ab nearest to p, and its place along
// the segment, 0 at a and 1 at b.
func closest(p, a, b Vec) (Vec, float64) {
	d := b.Sub(a)
	l := d.Dot(d)
	if l == 0 {
		return a, 0
	}
	t := max(0, min(1, p.Sub(a).Dot(d)/l))
	return a.Add(d.Mul(t)), t
}
