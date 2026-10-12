package gamesui

import (
	"math"
	"sync"

	"github.com/lyricat/inua/internal/games"
)

// A city's track: its forty squares laid end to end along a closed loop
// shaped like the city (the Yamanote Line, Manhattan, Puxi in the Huangpu's
// bend, Rome's walls), on the board's paper. Each square is a band across
// the loop fAcross deep, its colour band on the side facing the middle;
// the corners (Start, Jail, the Fountain, Go to Jail) are half as long
// again as the others.

const (
	fAcross = 76.0 // a square's depth, across the track
	fBand   = 22.0 // a street's colour band, where its buildings stand
)

// tile is one square of a track.
type tile struct {
	outline       []games.Vec // its edge on the board
	band          []games.Vec // a street's colour band: the strip on the inner side
	center        games.Vec
	tan, in       games.Vec // along the track in play order; into the loop
	along, across float64
}

// at is a point of the square in its own frame: x along the track, y
// across it, outwards (the side away from the middle) positive.
func (t tile) at(x, y float64) games.Vec {
	return games.Vec{X: t.center.X + t.tan.X*x - t.in.X*y, Y: t.center.Y + t.tan.Y*x - t.in.Y*y}
}

// turn is the angle that turns the square's frame onto the board: its
// inner side up when 0.
func (t tile) turn() float64 { return math.Atan2(t.in.X, -t.in.Y) }

// track is a city's squares, the edges of the loop they make, and the
// edges of its branch roads.
type track struct {
	tiles        [len(games.Board)]tile
	inner, outer []games.Vec   // the loop's edges, closed
	roads        [][]games.Vec // each branch road's outline, closed
	flares       [][]games.Vec // where a road widens out of the ring, or back into it
	curbs        [][]games.Vec // the flares' edges
}

// cityLoops are the cities' tracks: points on each loop's middle line, in
// play order from Start, clockwise on the board, smoothed into a curve.
var cityLoops = [][]games.Vec{
	{ // Tokyo: the Yamanote Line, Shinagawa at Start, Tokyo Bay below on the right
		{X: 548, Y: 628}, {X: 420, Y: 660}, {X: 292, Y: 648}, {X: 180, Y: 600}, {X: 100, Y: 512},
		{X: 64, Y: 400}, {X: 76, Y: 280}, {X: 128, Y: 170}, {X: 230, Y: 92}, {X: 360, Y: 62},
		{X: 488, Y: 76}, {X: 590, Y: 132}, {X: 646, Y: 228}, {X: 656, Y: 344}, {X: 636, Y: 452}, {X: 600, Y: 548},
	},
	{ // New York: Manhattan from the Battery up the Hudson to Inwood, down the East River
		{X: 174, Y: 640}, {X: 93, Y: 562}, {X: 150, Y: 431}, {X: 231, Y: 309}, {X: 312, Y: 186}, {X: 390, Y: 74},
		{X: 554, Y: 76}, {X: 619, Y: 221}, {X: 541, Y: 338}, {X: 460, Y: 461}, {X: 378, Y: 576}, {X: 272, Y: 652},
	},
	{ // Shanghai: Puxi, the Huangpu bending in on its east by Lujiazui
		{X: 560, Y: 624}, {X: 424, Y: 648}, {X: 286, Y: 640}, {X: 168, Y: 604}, {X: 90, Y: 508},
		{X: 62, Y: 384}, {X: 84, Y: 258}, {X: 156, Y: 150}, {X: 278, Y: 80}, {X: 420, Y: 64},
		{X: 548, Y: 100}, {X: 632, Y: 186}, {X: 616, Y: 286}, {X: 556, Y: 364}, {X: 580, Y: 452}, {X: 636, Y: 540},
	},
	{ // Rome: the Aurelian walls, the Gianicolo bulging west across the Tiber
		{X: 540, Y: 626}, {X: 410, Y: 648}, {X: 288, Y: 630}, {X: 196, Y: 600}, {X: 120, Y: 548},
		{X: 62, Y: 452}, {X: 70, Y: 350}, {X: 120, Y: 270}, {X: 108, Y: 176}, {X: 182, Y: 100},
		{X: 300, Y: 64}, {X: 430, Y: 58}, {X: 548, Y: 96}, {X: 636, Y: 184}, {X: 660, Y: 318}, {X: 640, Y: 452}, {X: 600, Y: 556},
	},
}

// cityRoadsOut says which cities' branch roads go round the outside of
// the ring (New York's, over the rivers to New Jersey and Brooklyn); the
// others' cut across the loop.
var cityRoadsOut = []bool{false, true, false, false}

var (
	tracksOnce sync.Once
	tracks     []*track
)

// trackOf is a city's track.
func trackOf(city int) *track {
	tracksOnce.Do(func() {
		for c, pts := range cityLoops {
			tr := buildTrack(pts)
			var middle games.Vec // the loop's middle
			for _, q := range pts {
				middle.X += q.X / float64(len(pts))
				middle.Y += q.Y / float64(len(pts))
			}
			for _, b := range games.Branches {
				// The road bows away from the ring: across the loop halfway
				// to its middle, or round the outside well clear of it.
				f, r := tr.tiles[b.Fork].center, tr.tiles[b.Rejoin].center
				mid := games.Vec{X: (f.X + r.X) / 2, Y: (f.Y + r.Y) / 2}
				via := games.Vec{X: mid.X + (middle.X-mid.X)*0.5, Y: mid.Y + (middle.Y-mid.Y)*0.5}
				if cityRoadsOut[c] {
					t := tr.tiles[(b.Fork+b.Rejoin)/2]
					via = t.at(0, 130)
				}
				tr.addRoad(b.Fork, b.First, b.Last, b.Rejoin, []games.Vec{via})
			}
			tracks = append(tracks, tr)
		}
	})
	return tracks[city]
}

// addRoad lays a branch road's squares first to last along a curve from
// the fork's square through via to the square it rejoins the ring at; the
// colour bands face the side the road bends round.
func (tr *track) addRoad(fork, first, last, rejoin int, via []games.Vec) {
	f, r := tr.tiles[fork], tr.tiles[rejoin]
	// The road leaves the fork's square at a slant, the way play runs, and
	// comes back into the square it rejoins at the same slant; a flare at
	// each end widens it into the ring. Inward for a road across the loop,
	// outward for one round the outside.
	out := 1.0
	if len(via) > 0 && !inside(tr.inner, via[0]) {
		out = -1
	}
	const gap = 18.0 // the flare's depth, from the ring to the road's end
	unit := func(v games.Vec) games.Vec { l := math.Hypot(v.X, v.Y); return games.Vec{X: v.X / l, Y: v.Y / l} }
	leave := unit(games.Vec{X: f.tan.X + f.in.X*out*1.3, Y: f.tan.Y + f.in.Y*out*1.3})
	arrive := unit(games.Vec{X: r.tan.X - r.in.X*out*1.3, Y: r.tan.Y - r.in.Y*out*1.3})
	start := f.at(0, -out*(f.across/2+gap))
	end := r.at(0, -out*(r.across/2+gap))
	pts := append(append([]games.Vec{start}, via...), end)
	line := tangentCurve(pts, leave, arrive, 40)
	length := []float64{0}
	for i := 1; i < len(line); i++ {
		length = append(length, length[i-1]+dist(line[i-1], line[i]))
	}
	total := length[len(length)-1]
	pointAt := func(s float64) (p, tan games.Vec) {
		s = max(0, min(total, s))
		i := max(0, min(len(line)-2, searchLength(length, s)))
		a, b := line[i], line[i+1]
		seg := length[i+1] - length[i]
		k := 0.0
		if seg > 0 {
			k = (s - length[i]) / seg
		}
		d := dist(a, b)
		return games.Vec{X: a.X + (b.X-a.X)*k, Y: a.Y + (b.Y-a.Y)*k}, games.Vec{X: (b.X - a.X) / d, Y: (b.Y - a.Y) / d}
	}
	// The bands face the inside of the road's bend: the side its middle
	// leans toward.
	mid, mtan := pointAt(total / 2)
	chord := games.Vec{X: (start.X+end.X)/2 - mid.X, Y: (start.Y+end.Y)/2 - mid.Y}
	side := 1.0
	if -mtan.Y*chord.X+mtan.X*chord.Y < 0 {
		side = -1
	}
	n := last - first + 1
	l := total / float64(n)
	var left, right []games.Vec
	off := func(p, in games.Vec, d float64) games.Vec { return games.Vec{X: p.X + in.X*d, Y: p.Y + in.Y*d} }
	for i := range n {
		var out, inn, bandOut, bandIn []games.Vec
		for k := 0; k <= 8; k++ {
			p, tan := pointAt(l*float64(i) + l*float64(k)/8)
			in := games.Vec{X: -tan.Y * side, Y: tan.X * side}
			out = append(out, off(p, in, -fAcross/2))
			inn = append(inn, off(p, in, fAcross/2))
			bandOut = append(bandOut, off(p, in, fAcross/2-fBand))
			bandIn = append(bandIn, off(p, in, fAcross/2))
		}
		c, tan := pointAt(l*float64(i) + l/2)
		t := &tr.tiles[first+i]
		t.outline = append(out, reversed(inn)...)
		t.band = append(bandOut, reversed(bandIn)...)
		t.center, t.tan, t.in = c, tan, games.Vec{X: -tan.Y * side, Y: tan.X * side}
		t.along, t.across = l, fAcross
		left, right = append(left, out...), append(right, inn...)
	}
	tr.roads = append(tr.roads, append(left, reversed(right)...))
	// The flares: from the ring's edge along the fork's (or the rejoined)
	// square, curving into the road's two sides.
	edgeOf := func(t tile) (a, b games.Vec) {
		return t.at(-t.along/2+2, -out*t.across/2), t.at(t.along/2-2, -out*t.across/2)
	}
	flare := func(a, b, capA, capB, dir games.Vec, ahead float64) {
		if dist(a, capB) < dist(a, capA) {
			capA, capB = capB, capA
		}
		curb := func(from, to games.Vec) []games.Vec { // into the road along dir
			ctl := games.Vec{X: to.X - dir.X*gap*1.2*ahead, Y: to.Y - dir.Y*gap*1.2*ahead}
			var c []games.Vec
			for k := 0; k <= 10; k++ {
				t := float64(k) / 10
				c = append(c, games.Vec{
					X: (1-t)*(1-t)*from.X + 2*(1-t)*t*ctl.X + t*t*to.X,
					Y: (1-t)*(1-t)*from.Y + 2*(1-t)*t*ctl.Y + t*t*to.Y,
				})
			}
			return c
		}
		ca, cb := curb(a, capA), curb(b, capB)
		// A little past the road's end, to cover its end line.
		pa := games.Vec{X: capA.X + dir.X*2*ahead, Y: capA.Y + dir.Y*2*ahead}
		pb := games.Vec{X: capB.X + dir.X*2*ahead, Y: capB.Y + dir.Y*2*ahead}
		poly := append(append(append([]games.Vec{}, ca...), pa, pb), reversed(cb)...)
		tr.flares = append(tr.flares, poly)
		tr.curbs = append(tr.curbs, ca, cb)
	}
	t0, t1 := tr.tiles[first], tr.tiles[last]
	a, b := edgeOf(f)
	flare(a, b, t0.outline[0], t0.outline[len(t0.outline)-1], leave, 1)
	a, b = edgeOf(r)
	flare(a, b, t1.outline[8], t1.outline[9], arrive, -1)
}

// tangentCurve is a Catmull-Rom curve through pts, per points to each
// span, leaving its first point along startTan and reaching its last along
// endTan.
func tangentCurve(pts []games.Vec, startTan, endTan games.Vec, per int) []games.Vec {
	n := len(pts)
	reach := dist(pts[0], pts[n-1])
	at := func(i int) games.Vec {
		switch {
		case i < 0: // a point behind the start, so the curve leaves along startTan
			return games.Vec{X: pts[0].X - startTan.X*reach, Y: pts[0].Y - startTan.Y*reach}
		case i >= n: // and one beyond the end
			return games.Vec{X: pts[n-1].X + endTan.X*reach, Y: pts[n-1].Y + endTan.Y*reach}
		}
		return pts[i]
	}
	var line []games.Vec
	for i := 0; i < n-1; i++ {
		p0, p1, p2, p3 := at(i-1), at(i), at(i+1), at(i+2)
		for k := range per {
			t := float64(k) / float64(per)
			t2, t3 := t*t, t*t*t
			f := func(a, b, c, d float64) float64 {
				return 0.5 * (2*b + (c-a)*t + (2*a-5*b+4*c-d)*t2 + (3*b-a-3*c+d)*t3)
			}
			line = append(line, games.Vec{X: f(p0.X, p1.X, p2.X, p3.X), Y: f(p0.Y, p1.Y, p2.Y, p3.Y)})
		}
	}
	return append(line, pts[n-1])
}

// buildTrack lays the squares along the loop through pts.
func buildTrack(pts []games.Vec) *track {
	// The middle line: a closed Catmull-Rom curve through the points.
	const per = 48
	n := len(pts)
	var line []games.Vec
	for i := range n {
		p0, p1, p2, p3 := pts[(i+n-1)%n], pts[i], pts[(i+1)%n], pts[(i+2)%n]
		for k := range per {
			t := float64(k) / per
			t2, t3 := t*t, t*t*t
			f := func(a, b, c, d float64) float64 {
				return 0.5 * (2*b + (c-a)*t + (2*a-5*b+4*c-d)*t2 + (3*b-a-3*c+d)*t3)
			}
			line = append(line, games.Vec{X: f(p0.X, p1.X, p2.X, p3.X), Y: f(p0.Y, p1.Y, p2.Y, p3.Y)})
		}
	}
	m := len(line)
	length := make([]float64, m+1) // along the line to each point, and round to the start again
	for i := range m {
		length[i+1] = length[i] + dist(line[i], line[(i+1)%m])
	}
	total := length[m]
	// The inside is on the right of the way round on a clockwise loop.
	side := 1.0
	if area(pts) < 0 {
		side = -1
	}
	// pointAt is the line's point s along it, the way it runs and into the loop.
	pointAt := func(s float64) (p, tan, in games.Vec) {
		s = math.Mod(s+total, total)
		i := max(0, min(m-1, searchLength(length, s)))
		a, b := line[i], line[(i+1)%m]
		seg := length[i+1] - length[i]
		f := 0.0
		if seg > 0 {
			f = (s - length[i]) / seg
		}
		p = games.Vec{X: a.X + (b.X-a.X)*f, Y: a.Y + (b.Y-a.Y)*f}
		d := dist(a, b)
		tan = games.Vec{X: (b.X - a.X) / d, Y: (b.Y - a.Y) / d}
		in = games.Vec{X: -tan.Y * side, Y: tan.X * side}
		return p, tan, in
	}
	off := func(p, in games.Vec, d float64) games.Vec { return games.Vec{X: p.X + in.X*d, Y: p.Y + in.Y*d} }

	tr := &track{}
	edge := total / 42 // 36 squares and four corners half as long again
	s := 0.0
	for i := range games.LoopLen {
		l := edge
		if i%10 == 0 {
			l = edge * 1.5
		}
		steps := 10
		var out, inn, bandIn, bandOut []games.Vec
		for k := 0; k <= steps; k++ {
			p, _, in := pointAt(s + l*float64(k)/float64(steps))
			out = append(out, off(p, in, -fAcross/2))
			inn = append(inn, off(p, in, fAcross/2))
			bandOut = append(bandOut, off(p, in, fAcross/2-fBand))
			bandIn = append(bandIn, off(p, in, fAcross/2))
		}
		c, tan, in := pointAt(s + l/2)
		t := &tr.tiles[i]
		t.outline = append(out, reversed(inn)...)
		t.band = append(bandOut, reversed(bandIn)...)
		t.center, t.tan, t.in, t.along, t.across = c, tan, in, l, fAcross
		s += l
	}
	for k := range m {
		p, _, in := pointAt(length[k])
		tr.inner = append(tr.inner, off(p, in, fAcross/2))
		tr.outer = append(tr.outer, off(p, in, -fAcross/2))
	}
	return tr
}

func searchLength(length []float64, s float64) int {
	lo, hi := 0, len(length)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if length[mid] <= s {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

func dist(a, b games.Vec) float64 { return math.Hypot(b.X-a.X, b.Y-a.Y) }

// area is twice a polygon's signed area: positive when it runs clockwise
// on the board (v down).
func area(pts []games.Vec) float64 {
	a := 0.0
	for i, p := range pts {
		q := pts[(i+1)%len(pts)]
		a += p.X*q.Y - q.X*p.Y
	}
	return a
}

func reversed(pts []games.Vec) []games.Vec {
	out := make([]games.Vec, len(pts))
	for i, p := range pts {
		out[len(pts)-1-i] = p
	}
	return out
}

// inside reports whether p is inside the polygon.
func inside(poly []games.Vec, p games.Vec) bool {
	in := false
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		if (a.Y > p.Y) != (b.Y > p.Y) && p.X < a.X+(p.Y-a.Y)*(b.X-a.X)/(b.Y-a.Y) {
			in = !in
		}
	}
	return in
}

// polyPath adds a closed polygon in board units to the path.
func polyPath(pn pen, pts []games.Vec) {
	pn.MoveTo(pts[0].X, pts[0].Y)
	for _, p := range pts[1:] {
		pn.LineTo(p.X, p.Y)
	}
	pn.Close()
}
