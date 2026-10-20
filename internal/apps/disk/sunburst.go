package disk

import (
	"cmp"
	"image/color"
	"math"
	"slices"
)

// The map is a sunburst: the folder in view is the disc in the middle, the
// things in it the first ring around it, sized by their share of the
// angle, what is in those the next ring, and so on outwards.

// Rings are how many rings the map draws around its middle.
const Rings = 5

// inner is the middle's radius, as a share of the map's.
const inner = 0.26

// minAngle is the smallest slice drawn, in radians: what is smaller is too
// thin to see or to point at.
const minAngle = 0.015

// Arc is one slice of the map.
type Arc struct {
	Node   *Node
	Ring   int          // 1 for the first ring around the middle
	A0, A1 float64      // radians, clockwise from the top
	Top    *Node        // its ancestor in the first ring, whose colour it takes
	Span   int          // how many rings it covers, from Ring outwards; 0 is 1
	Fill   *color.NRGBA // its colour, rather than its node's
}

func (a Arc) span() int { return max(a.Span, 1) }

// Layout lays out the map of focus.
func Layout(focus *Node) []Arc {
	var arcs []Arc
	var lay func(n *Node, ring int, a0, a1 float64, top *Node)
	lay = func(n *Node, ring int, a0, a1 float64, top *Node) {
		if ring > Rings || n.Size <= 0 {
			return
		}
		a := a0
		for _, k := range n.Kids {
			span := (a1 - a0) * float64(k.Size) / float64(n.Size)
			if span < minAngle {
				a += span
				continue
			}
			t := top
			if t == nil {
				t = k
			}
			arcs = append(arcs, Arc{Node: k, Ring: ring, A0: a, A1: a + span, Top: t})
			if k.Dir {
				lay(k, ring+1, a, a+span, t)
			}
			a += span
		}
	}
	lay(focus, 1, 0, 2*math.Pi, nil)
	return arcs
}

// Geometry is where the map sits in a square of side px.
type Geometry struct{ CX, CY, R, Ring, Middle float64 }

// Measure is the map's geometry in a w×h area, centred.
func Measure(w, h float64) Geometry {
	r := math.Min(w, h)/2 - 2
	return Geometry{CX: w / 2, CY: h / 2, R: r, Middle: r * inner, Ring: r * (1 - inner) / Rings}
}

// polar is a point's distance from the centre and its angle, clockwise
// from the top.
func (g Geometry) polar(x, y float64) (float64, float64) {
	dx, dy := x-g.CX, y-g.CY
	a := math.Atan2(dx, -dy)
	if a < 0 {
		a += 2 * math.Pi
	}
	return math.Hypot(dx, dy), a
}

// Hit is the arc at x, y; -1 for none, and middle for the disc in the
// middle.
func Hit(arcs []Arc, g Geometry, x, y float64) (i int, middle bool) {
	r, a := g.polar(x, y)
	if r <= g.Middle {
		return -1, true
	}
	ring := int((r-g.Middle)/g.Ring) + 1
	if ring > Rings {
		return -1, false
	}
	for i, arc := range arcs {
		if arc.Ring == ring && a >= arc.A0 && a < arc.A1 {
			return i, false
		}
	}
	return -1, false
}

// Paint is how the map is coloured.
type Paint struct {
	Back   color.NRGBA // the ground, and the gaps between slices
	Middle color.NRGBA
	Plain  color.NRGBA    // a slice no rule knows
	Kind   [4]color.NRGBA // by Kind: a slice a rule knows
	Hot    color.NRGBA    // the slice pointed at, or chosen
	HotArc map[*Node]bool // those slices, and what is in them
	Fade   func(ring int) float64
	Guide  color.NRGBA // hairlines where the rings meet, and around them
}

// ColorOf is an arc's colour: its own, else its kind's, the hot one's,
// faded by its ring and when it is the rest of a folder.
func (p Paint) ColorOf(a Arc) color.NRGBA {
	if a.Fill != nil {
		return *a.Fill
	}
	c := p.Plain
	if r := ruleOf(a.Node); r != nil {
		c = p.Kind[r.Kind]
	}
	if p.HotArc[a.Node] {
		c = p.Hot
	}
	f := 1.0
	if p.Fade != nil {
		f = p.Fade(a.Ring)
	}
	if a.Node != nil && a.Node.Rest {
		f *= 0.45
	}
	c.A = uint8(float64(c.A) * f)
	return c
}

// Shape is a slice as drawn: between radii R0 and R1, angles A0 to A1
// (clockwise from the top), the gaps between slices already taken off.
type Shape struct {
	R0, R1, A0, A1 float64
	Color          color.NRGBA
}

// Gap is the space between slices, in the units of the geometry.
const Gap = 1.0

// Shapes are the map's slices in geometry g, for drawing as paths: what
// is too thin once its gaps are off is left out.
func Shapes(arcs []Arc, g Geometry, p Paint) []Shape {
	out := make([]Shape, 0, len(arcs))
	for _, a := range arcs {
		r0 := g.Middle + float64(a.Ring-1)*g.Ring + Gap/2
		r1 := g.Middle + float64(a.Ring-1+a.span())*g.Ring - Gap/2
		if a.Ring+a.span()-1 > Rings {
			r1 = g.R - Gap/2
		}
		// The angular gap is the same width at the inner edge.
		da := Gap / 2 / r0
		a0, a1 := a.A0+da, a.A1-da
		if a1-a0 < 0.002 || r1 <= r0 {
			continue
		}
		if a.A1-a.A0 >= 2*math.Pi-1e-9 {
			a0, a1 = 0, 2*math.Pi // a whole ring: no gap
		}
		out = append(out, Shape{R0: r0, R1: r1, A0: a0, A1: a1, Color: p.ColorOf(a)})
	}
	return out
}

// Guides are the radii of the hairlines where the rings meet.
func (g Geometry) Guides() []float64 {
	out := make([]float64, 0, Rings+1)
	for k := 0; k <= Rings; k++ {
		out = append(out, g.Middle+float64(k)*g.Ring)
	}
	return out
}

// Used is the map of a filesystem not scanned yet: one band over two
// rings, as much of the turn as is used in fill, the rest in free.
func Used(used, total int64, fill, free color.NRGBA) []Arc {
	if total <= 0 || used <= 0 {
		return nil
	}
	share := math.Min(float64(used)/float64(total), 1)
	return []Arc{
		{Ring: 1, Span: 2, A0: 0, A1: 2 * math.Pi * share, Fill: &fill},
		{Ring: 1, Span: 2, A0: 2 * math.Pi * share, A1: 2 * math.Pi, Fill: &free},
	}
}

// Scanning is the map while a scan runs: the first ring holds the folders
// measured so far (tops), each by its share of what has been counted, in
// its kind's colour; a sweep of cells turns around the rings outside it,
// at phase (radians), brightest at its head.
func Scanning(tops []Top, counted int64, phase float64, kinds [4]color.NRGBA, plain, sweep color.NRGBA) []Arc {
	var arcs []Arc
	a := 0.0
	for _, t := range tops {
		if counted <= 0 {
			break
		}
		span := 2 * math.Pi * float64(t.Size) / float64(counted)
		if a+span > 2*math.Pi {
			span = 2*math.Pi - a
		}
		if span >= minAngle {
			c := plain
			if t.Kind != KindNone {
				c = kinds[t.Kind]
			}
			arcs = append(arcs, Arc{Ring: 1, A0: a, A1: a + span, Fill: &c})
		}
		a += span
	}
	const cells, width, step = 7, 0.07, 0.085
	head := math.Mod(phase, 2*math.Pi)
	for i := range cells {
		a0 := math.Mod(head-float64(i)*step+4*math.Pi, 2*math.Pi)
		if a0+width > 2*math.Pi {
			continue // at the top, where the turn begins again: skipped a moment
		}
		c := sweep
		c.A = uint8(float64(c.A) * (1 - float64(i)/cells))
		arcs = append(arcs, Arc{Ring: 2, Span: Rings - 1, A0: a0, A1: a0 + width, Fill: &c})
	}
	slices.SortFunc(arcs, func(x, y Arc) int { return cmp.Compare(x.A0, y.A0) })
	return arcs
}

// ruleOf is the rule a node or a folder it is in has.
func ruleOf(n *Node) *Rule {
	for a := n; a != nil; a = a.Parent {
		if a.Rule != nil && !a.Rule.Group {
			return a.Rule
		}
	}
	return nil
}

// RuleOf is the rule that knows n or a folder it is in, or nil.
func RuleOf(n *Node) *Rule { return ruleOf(n) }

func clamp(v float64) float64 { return math.Max(0, math.Min(1, v)) }
