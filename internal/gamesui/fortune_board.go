package gamesui

import (
	"fmt"
	"image/color"
	"math"
	"slices"
	"sort"

	"github.com/lyricat/inua/internal/games"
)

// Small Fortune's board, seen from above at an angle (isometric). The
// board is laid out flat in units of its own (u to the right, v down,
// fBoard square, Start in the bottom right corner); the flat drawing, the
// tiles and their print, goes through cairo with the isometric transform,
// so it lies on the board. What stands on the board (houses, hotels,
// pawns, dice, trees, the fountain) is drawn as solids from projected
// corners, back to front.

const (
	fCorner = 92.0
	fEdge   = 58.0
	fBoard  = 2*fCorner + 9*fEdge
	fBand   = 20.0 // a street's colour band, where its buildings stand
	fSlab   = 16.0 // the board's thickness

	isoW = 1080.0
	isoH = 740.0
)

// isoK is the scale from board units to canvas units; the board's top
// corner is at isoOX, isoOY.
var (
	isoK  = (isoW - 40) / (2 * 0.8660254 * fBoard)
	isoOX = isoW / 2
	isoOY = 64.0
)

// project is the canvas point of board point u, v at height z.
func project(u, v, z float64) games.Vec {
	return games.Vec{X: isoOX + (u-v)*0.8660254*isoK, Y: isoOY + ((u+v)*0.5-z)*isoK}
}

// unproject is the board point under canvas point p, on the board.
func unproject(p games.Vec) (u, v float64) {
	a := (p.X - isoOX) / isoK / 0.8660254 // u - v
	b := (p.Y - isoOY) / isoK / 0.5       // u + v
	return (a + b) / 2, (b - a) / 2
}

// flat makes pn draw in board units on the board's surface, raised by z.
func flat(pn pen, z float64) {
	pn.Translate(isoOX, isoOY-z*isoK)
	pn.Scale(isoK*1.2247449, isoK*0.70710678)
	pn.Rotate(math.Pi / 4)
}

// squareRect is a square's place on the flat board, and the side of it
// that faces the middle: 0 up, 1 right, 2 down, 3 left; -1 for a corner.
func squareRect(i int) (x, y, w, h float64, side int) {
	far := fBoard - fCorner
	switch {
	case i == 0:
		return far, far, fCorner, fCorner, -1
	case i < 10:
		return far - float64(i)*fEdge, far, fEdge, fCorner, 0
	case i == 10:
		return 0, far, fCorner, fCorner, -1
	case i < 20:
		return 0, far - float64(i-10)*fEdge, fCorner, fEdge, 1
	case i == 20:
		return 0, 0, fCorner, fCorner, -1
	case i < 30:
		return fCorner + float64(i-21)*fEdge, 0, fEdge, fCorner, 2
	case i == 30:
		return far, 0, fCorner, fCorner, -1
	default:
		return far, fCorner + float64(i-31)*fEdge, fCorner, fEdge, 3
	}
}

func squareAt(u, v float64) int {
	for i := range games.Board {
		x, y, w, h, _ := squareRect(i)
		if u >= x && u < x+w && v >= y && v < y+h {
			return i
		}
	}
	return -1
}

// squareAtScreen is the square under a canvas point, -1 for none.
func squareAtScreen(p games.Vec) int { return squareAt(unproject(p)) }

// band is a street's colour band: the strip along its inner edge.
func band(i int) (x, y, w, h float64) {
	x, y, w, h, side := squareRect(i)
	switch side {
	case 0:
		return x, y, w, fBand
	case 1:
		return x + w - fBand, y, fBand, h
	case 2:
		return x, y + h - fBand, w, fBand
	case 3:
		return x, y, fBand, h
	}
	return x, y, w, h
}

// tokenAt is where player pi's pawn stands on square sq, on the flat
// board: each player has a place of its own there. In jail, it is behind
// the bars.
func tokenAt(sq, pi int, jailed bool) games.Vec {
	x, y, w, h, side := squareRect(sq)
	if side < 0 {
		off := [4]games.Vec{{X: -14, Y: -14}, {X: 14, Y: -14}, {X: -14, Y: 14}, {X: 14, Y: 14}}[pi]
		if jailed {
			return games.Vec{X: x + w - 26 + off.X*0.6, Y: y + 26 + off.Y*0.6}
		}
		return games.Vec{X: x + w/2 + off.X, Y: y + h/2 + off.Y}
	}
	cx, cy := x+w/2, y+h/2
	d := (float64(pi) - 1.5) * 15
	switch side { // clear of the band, side by side along the tile
	case 0:
		return games.Vec{X: cx + d*0.6, Y: cy + 12 + float64(pi%2)*10}
	case 1:
		return games.Vec{X: cx - 12 - float64(pi%2)*10, Y: cy + d*0.6}
	case 2:
		return games.Vec{X: cx + d*0.6, Y: cy - 12 - float64(pi%2)*10}
	default:
		return games.Vec{X: cx + 12 + float64(pi%2)*10, Y: cy + d*0.6}
	}
}

// token is where a player's pawn is now, and how high it hops.
func (p *fortune) token(pi int) (games.Vec, float64) {
	e := p.cur
	if e != nil && e.Player == pi {
		switch e.Kind {
		case games.EvWalk:
			s := min(p.t/hopTime, float64(e.Steps))
			n := int(s)
			a := (e.From + n) % len(games.Board)
			b := (a + 1) % len(games.Board)
			frac := s - float64(n)
			return tokenAt(a, pi, false).Lerp(tokenAt(b, pi, false), frac), math.Sin(frac*math.Pi) * 16
		case games.EvJump:
			frac := min(1, p.t/jumpTime)
			frac = frac * frac * (3 - 2*frac)
			return tokenAt(e.From, pi, false).Lerp(tokenAt(e.To, pi, e.View.InJail[pi]), frac), math.Sin(frac*math.Pi) * 90
		}
	}
	return tokenAt(p.shown.At[pi], pi, p.shown.InJail[pi]), 0
}

// solid is something standing on the board, drawn in order of depth.
type solid struct {
	depth float64
	draw  func()
}

// shade darkens a colour, for a solid's sides.
func shade(c color.NRGBA, k float64) color.NRGBA {
	return color.NRGBA{uint8(float64(c.R) * k), uint8(float64(c.G) * k), uint8(float64(c.B) * k), c.A}
}

// poly fills a polygon of projected points.
func poly(pn pen, col color.NRGBA, pts ...games.Vec) {
	pn.MoveTo(pts[0].X, pts[0].Y)
	for _, q := range pts[1:] {
		pn.LineTo(q.X, q.Y)
	}
	pn.Close()
	pn.Color(col)
	pn.Fill()
}

// isoEllipse fills the projection of a circle on the board at height z.
func isoEllipse(pn pen, u, v, z, r float64, col color.NRGBA, a float64) {
	c := project(u, v, z)
	pn.Save()
	pn.Translate(c.X, c.Y)
	pn.Scale(1.2247449*isoK*r, 0.70710678*isoK*r)
	pn.Circle(0, 0, 1)
	pn.Restore()
	pn.Alpha(col, a)
	pn.Fill()
}

// box draws a block from u0, v0 to u1, v1, h high, with its two near
// sides and its top.
func box(pn pen, u0, v0, u1, v1, z, h float64, wall, top color.NRGBA) {
	poly(pn, shade(wall, 0.72), project(u1, v0, z), project(u1, v1, z), project(u1, v1, z+h), project(u1, v0, z+h)) // facing +u
	poly(pn, wall, project(u0, v1, z), project(u1, v1, z), project(u1, v1, z+h), project(u0, v1, z+h))              // facing +v
	poly(pn, top, project(u0, v0, z+h), project(u1, v0, z+h), project(u1, v1, z+h), project(u0, v1, z+h))
}

// house draws a house of side s centred on u, v: walls in a pale shade of
// its owner's colour, a pitched roof in the colour itself, a door and
// windows.
func house(pn pen, u, v, s float64, owner color.NRGBA, ink color.NRGBA) {
	h, r := s*0.8, s*0.62
	u0, v0, u1, v1 := u-s/2, v-s/2, u+s/2, v+s/2
	vm := v
	wall := mix(color.NRGBA{0xF2, 0xEC, 0xE0, 255}, owner, 0.18)
	roof := owner
	isoEllipse(pn, u+2, v+2, 0, s*0.8, ink, 0.25) // its shadow
	// The back slope, the +u wall with its gable, the +v wall, the front slope.
	poly(pn, shade(roof, 0.62), project(u0, v0, h), project(u1, v0, h), project(u1, vm, h+r), project(u0, vm, h+r))
	poly(pn, shade(wall, 0.74), project(u1, v0, 0), project(u1, v1, 0), project(u1, v1, h), project(u1, v0, h))
	poly(pn, shade(wall, 0.74), project(u1, v0, h), project(u1, v1, h), project(u1, vm, h+r))
	poly(pn, wall, project(u0, v1, 0), project(u1, v1, 0), project(u1, v1, h), project(u0, v1, h))
	poly(pn, roof, project(u0, v1, h), project(u1, v1, h), project(u1, vm, h+r), project(u0, vm, h+r))
	// A door and a window on the front, a window on the side.
	d := s * 0.18
	poly(pn, shade(owner, 0.45), project(u-d, v1, 0), project(u+d, v1, 0), project(u+d, v1, h*0.55), project(u-d, v1, h*0.55))
	w := s * 0.14
	poly(pn, color.NRGBA{0x8E, 0xC5, 0xE8, 255}, project(u1, v-w, h*0.35), project(u1, v+w, h*0.35), project(u1, v+w, h*0.65), project(u1, v-w, h*0.65))
}

// hotel draws a hotel: a tall block, its windows lit, a band of its
// owner's colour on top.
func hotel(pn pen, u, v, su, sv float64, owner, ink color.NRGBA) {
	h := 30.0
	u0, v0, u1, v1 := u-su/2, v-sv/2, u+su/2, v+sv/2
	wall := mix(color.NRGBA{0xF4, 0xF0, 0xE6, 255}, owner, 0.12)
	isoEllipse(pn, u+3, v+3, 0, max(su, sv)*0.75, ink, 0.28)
	box(pn, u0, v0, u1, v1, 0, h, wall, mix(wall, owner, 0.5))
	box(pn, u0-1, v0-1, u1+1, v1+1, h, 4, owner, mix(owner, color.NRGBA{255, 255, 255, 255}, 0.25))
	glass := color.NRGBA{0xF7, 0xD9, 0x7A, 255}
	for row := range 3 {
		z := 5 + float64(row)*8
		for k := range 3 {
			a := u0 + su*(0.2+0.3*float64(k))
			poly(pn, glass, project(a-1.6, v1, z), project(a+1.6, v1, z), project(a+1.6, v1, z+4), project(a-1.6, v1, z+4))
		}
		for k := range 2 {
			b := v0 + sv*(0.3+0.4*float64(k))
			poly(pn, shade(glass, 0.8), project(u1, b-1.6, z), project(u1, b+1.6, z), project(u1, b+1.6, z+4), project(u1, b-1.6, z+4))
		}
	}
	pn.Text("H", "IBM Plex Sans Bold 7", project(u, v1, h+2).X, project(u, v1, h+2).Y-1, 0.5, 1, owner)
}

// pawn draws a player's pawn standing at u, v, lifted by lift.
func pawn(pn pen, u, v, lift float64, col, ink, ring color.NRGBA, turn bool) {
	isoEllipse(pn, u, v, 0, 9-lift*0.03, ink, 0.3)
	base := project(u, v, lift)
	if turn {
		pn.Width(2.5)
		pn.Color(ring)
		pn.Save()
		pn.Translate(base.X, base.Y)
		pn.Scale(1.2247449*isoK*11, 0.70710678*isoK*11)
		pn.Circle(0, 0, 1)
		pn.Restore()
		pn.Stroke()
	}
	r := 7.5 * isoK
	// The base, the body narrowing up, the head.
	pn.Save()
	pn.Translate(base.X, base.Y)
	pn.Scale(1, 0.5)
	pn.Circle(0, 0, r*1.25)
	pn.Restore()
	pn.Color(shade(col, 0.7))
	pn.Fill()
	top := project(u, v, lift+18)
	pn.MoveTo(base.X-r, base.Y)
	pn.LineTo(top.X-r*0.42, top.Y)
	pn.LineTo(top.X+r*0.42, top.Y)
	pn.LineTo(base.X+r, base.Y)
	pn.Close()
	pn.Color(col)
	pn.Fill()
	head := project(u, v, lift+23)
	pn.Color(col)
	pn.Circle(head.X, head.Y, r*0.78)
	pn.Fill()
	pn.Alpha(color.NRGBA{255, 255, 255, 255}, 0.55)
	pn.Circle(head.X-r*0.25, head.Y-r*0.25, r*0.25)
	pn.Fill()
}

// tree draws a round tree at u, v.
func tree(pn pen, u, v, s float64, ink color.NRGBA) {
	isoEllipse(pn, u+3, v+3, 0, s*0.7, ink, 0.25)
	trunk := project(u, v, 0)
	top := project(u, v, s*0.7)
	pn.Width(2.5 * isoK)
	pn.Color(color.NRGBA{0x7A, 0x5A, 0x3E, 255})
	pn.Line(trunk.X, trunk.Y, top.X, top.Y)
	c := project(u, v, s*1.1)
	pn.Color(color.NRGBA{0x5E, 0x9C, 0x6A, 255})
	pn.Circle(c.X, c.Y, s*0.5*isoK)
	pn.Fill()
	pn.Color(color.NRGBA{0x76, 0xB3, 0x7E, 255})
	pn.Circle(c.X-s*0.15*isoK, c.Y-s*0.15*isoK, s*0.3*isoK)
	pn.Fill()
}

// die draws a die of side s at u, v, z showing n on top.
func die(pn pen, u, v, z, s float64, n int, face, ink color.NRGBA) {
	isoEllipse(pn, u+2, v+2, 0, s*0.75, ink, 0.3)
	box(pn, u-s/2, v-s/2, u+s/2, v+s/2, z, s, face, mix(face, color.NRGBA{255, 255, 255, 255}, 0.3))
	pn.Save()
	flat(pn, z+s)
	pn.Color(ink)
	for _, pip := range pips[n] {
		pn.Circle(u+pip[0]*s*0.27, v+pip[1]*s*0.27, s*0.085)
		pn.Fill()
	}
	pn.Restore()
}

var pips = [7][][2]float64{
	{}, {{0, 0}}, {{-1, -1}, {1, 1}}, {{-1, -1}, {0, 0}, {1, 1}},
	{{-1, -1}, {1, -1}, {-1, 1}, {1, 1}}, {{-1, -1}, {1, -1}, {0, 0}, {-1, 1}, {1, 1}},
	{{-1, -1}, {1, -1}, {-1, 0}, {1, 0}, {-1, 1}, {1, 1}},
}

// rentOf is a property's rent as the view shows it.
func rentOf(v games.FortuneView, sq int) int {
	o := v.Owner[sq]
	if o < 0 {
		return 0
	}
	count := func(kind games.SquareKind) int {
		n := 0
		for i, w := range v.Owner {
			if w == o && games.Board[i].Kind == kind {
				n++
			}
		}
		return n
	}
	switch games.Board[sq].Kind {
	case games.Street:
		whole := true
		for _, i := range games.Group(games.Board[sq].Group) {
			whole = whole && v.Owner[i] == o
		}
		return games.RentAt(sq, v.Houses[sq], whole)
	case games.Pier:
		return games.PierRent(count(games.Pier))
	}
	return 0
}

func (p *fortune) paint(pn pen) {
	c := p.f.col
	v := p.shown
	ink := c.app

	// The board: a slab, its two near sides in shadow.
	slabTop := mix(c.surface2, c.t2, 0.12)
	poly(pn, shade(slabTop, 0.55), project(fBoard, 0, 0), project(fBoard, fBoard, 0), project(fBoard, fBoard, -fSlab), project(fBoard, 0, -fSlab))
	poly(pn, shade(slabTop, 0.75), project(0, fBoard, 0), project(fBoard, fBoard, 0), project(fBoard, fBoard, -fSlab), project(0, fBoard, -fSlab))

	// The flat print: tiles, bands, names, the middle.
	pn.Save()
	flat(pn, 0)
	pn.Color(slabTop)
	pn.Rect(0, 0, fBoard, fBoard)
	pn.Fill()
	// The middle: a park.
	pn.Color(mix(c.surface, color.NRGBA{0x5E, 0x9C, 0x6A, 255}, 0.22))
	pn.Rect(fCorner+6, fCorner+6, fBoard-2*fCorner-12, fBoard-2*fCorner-12)
	pn.Fill()
	pn.Width(10)
	pn.Alpha(c.surface2, 0.9)
	pn.Line(fCorner+6, fBoard/2, fBoard-fCorner-6, fBoard/2)
	pn.Line(fBoard/2, fCorner+6, fBoard/2, fBoard-fCorner-6)
	for i, sq := range games.Board {
		p.paintTile(pn, i, sq, v)
	}
	// The streets the player may raise this turn.
	if p.g.Turn == 0 && !p.busy() {
		for sq := range games.Board {
			if p.g.CanBuild(sq) {
				x, y, w, h, _ := squareRect(sq)
				pn.Width(2.5)
				pn.Dash(6, 4)
				pn.Color(c.ok)
				pn.Rect(x+4, y+4, w-8, h-8)
				pn.Stroke()
				pn.Dash()
			}
		}
	}
	if f := p.focus(); p.selected >= 0 {
		x, y, w, h, _ := squareRect(f)
		pn.Width(3)
		pn.Color(c.accent)
		pn.Rect(x+2, y+2, w-4, h-4)
		pn.Stroke()
	}
	// The game's name, printed across the park so it reads level.
	pn.Save()
	pn.Translate(fBoard/2, fBoard/2)
	pn.Rotate(-math.Pi / 4)
	pn.Text("S M A L L   F O R T U N E", "IBM Plex Sans Bold 26", 0, -150, 0.5, 0.5, mix(c.surface, c.t0, 0.35))
	pn.Restore()
	pn.Restore()

	// What stands on the board, back to front.
	var solids []solid
	add := func(u, v float64, draw func()) { solids = append(solids, solid{u + v, draw}) }
	for i := range games.Board {
		n := v.Houses[i]
		if n == 0 || v.Owner[i] < 0 {
			continue
		}
		owner := p.playerColor(v.Owner[i])
		bx, by, bw, bh := band(i)
		cx, cy := bx+bw/2, by+bh/2
		if n == games.Hotel {
			su, sv := 30.0, 13.0
			if bw < bh {
				su, sv = 13, 30
			}
			add(cx, cy, func() { hotel(pn, cx, cy, su, sv, owner, ink) })
			continue
		}
		for k := range n {
			d := (float64(k) - float64(n-1)/2) * 13
			u, w := cx+d, cy
			if bw < bh {
				u, w = cx, cy+d
			}
			add(u, w, func() { house(pn, u, w, 10, owner, ink) })
		}
	}
	// Trees in the park, and the fountain on its corner.
	for _, t := range [][3]float64{{150, 150, 22}, {190, 128, 16}, {556, 150, 20}, {150, 556, 20}, {560, 548, 24}, {520, 586, 15}, {586, 512, 15}} {
		add(t[0], t[1], func() { tree(pn, t[0], t[1], t[2], ink) })
	}
	fx, fy := fCorner/2, fCorner/2
	add(fx, fy, func() { p.fountain(pn, fx, fy, v.Pot) })
	// The dice, in the middle of the park.
	if p.dice[0] > 0 {
		bounce := 0.0
		if p.cur != nil && p.cur.Kind == games.EvDice {
			bounce = math.Abs(math.Sin(p.t*14)) * 18 * max(0, 1-p.t/diceTime)
		}
		for i, d := range p.dice {
			u, w := fBoard/2-28+float64(i)*46, fBoard/2+18-float64(i)*20
			add(u, w, func() { die(pn, u, w, bounce*(1-0.3*float64(i)), 30, d, c.t0, ink) })
		}
	}
	// Pawns, the one whose turn it is on top.
	for i := range p.g.Players {
		if p.g.Players[i].Out && (p.cur == nil || p.cur.Player != i) {
			continue
		}
		at, lift := p.token(i)
		turn := i == p.g.Turn && p.g.Phase != games.PhaseOver
		depth := 0.0
		if turn {
			depth = 4
		}
		col := p.playerColor(i)
		solids = append(solids, solid{at.X + at.Y + depth, func() { pawn(pn, at.X, at.Y, lift, col, ink, c.t0, turn) }})
	}
	sort.SliceStable(solids, func(a, b int) bool { return solids[a].depth < solids[b].depth })
	for _, s := range solids {
		s.draw()
	}
	p.paintMiddle(pn)
}

// paintTile prints a square on the flat board: its ground (tinted by its
// owner), a street's colour band, its name and price, running along the
// tile's long side so that it reads from the front.
func (p *fortune) paintTile(pn pen, i int, sq games.Square, v games.FortuneView) {
	c := p.f.col
	x, y, w, h, side := squareRect(i)
	bg := mix(c.surface, c.t0, 0.04)
	if o := v.Owner[i]; o >= 0 {
		bg = mix(bg, p.playerColor(o), 0.2)
	}
	if i == p.hover {
		bg = mix(bg, c.t0, 0.08)
	}
	pn.Color(bg)
	pn.Rect(x, y, w, h)
	pn.Fill()
	if sq.Kind == games.Street {
		bx, by, bw, bh := band(i)
		pn.Color(mix(c.surface, groupColors[sq.Group], 0.9))
		pn.Rect(bx, by, bw, bh)
		pn.Fill()
	}
	pn.Width(1)
	pn.Color(c.line)
	pn.Rect(x, y, w, h)
	pn.Stroke()

	// The print, turned to run along the tile's long side: rising to the
	// right on the top and bottom rows, falling on the side columns.
	cx, cy := x+w/2, y+h/2
	long, short := w, h
	pn.Save()
	pn.Translate(cx, cy)
	if side == 0 || side == 2 {
		pn.Rotate(-math.Pi / 2)
		long, short = h, w
	}
	// Leave the band out of the print's room.
	shift := 0.0
	if sq.Kind == games.Street { // the band is at the print's end on sides 0 and 1, its start on 2 and 3
		shift = map[int]float64{0: -fBand / 2, 1: -fBand / 2, 2: fBand / 2, 3: fBand / 2}[side]
		long -= fBand
	}
	pn.Translate(shift, 0)
	price := func(s string, col color.NRGBA) {
		pn.Text(s, "Geist Mono, IBM Plex Mono 9", 0, short/2-4, 0.5, 1, col)
	}
	switch sq.Kind {
	case games.Street:
		pn.TextBox(sq.Name, "IBM Plex Sans Medium 9.5", 0, -short/2+5, long-6, c.t0)
		if o := v.Owner[i]; o >= 0 {
			price(fmt.Sprintf("rent $%d", rentOf(v, i)), c.t1)
		} else {
			price(fmt.Sprintf("$%d", sq.Price), c.t2)
		}
	case games.Pier:
		pn.TextBox(sq.Name, "IBM Plex Sans Medium 9.5", 0, -short/2+5, long-6, c.t0)
		ship(pn, 0, 2, pinBlue)
		if o := v.Owner[i]; o >= 0 {
			price(fmt.Sprintf("rent $%d", rentOf(v, i)), c.t1)
		} else {
			price(fmt.Sprintf("$%d", sq.Price), c.t2)
		}
	case games.Utility:
		pn.TextBox(sq.Name, "IBM Plex Sans Medium 9.5", 0, -short/2+5, long-6, c.t0)
		if i == 12 {
			bolt(pn, 0, 2, c.warn)
		} else {
			drop(pn, 0, 2, pinBlue)
		}
		if v.Owner[i] < 0 {
			price(fmt.Sprintf("$%d", sq.Price), c.t2)
		} else {
			price("dice ×", c.t1)
		}
	case games.ChanceSquare:
		pn.Text("?", "IBM Plex Sans Bold 30", 0, -4, 0.5, 0.5, c.accent)
		price("Chance", c.t1)
	case games.TaxSquare:
		pn.TextBox(sq.Name, "IBM Plex Sans Medium 9.5", 0, -short/2+8, long-6, c.t0)
		price(fmt.Sprintf("pay $%d", sq.Price), c.danger)
	case games.StartSquare:
		pn.Text("START", "IBM Plex Sans Bold 17", 0, -10, 0.5, 0.5, c.ok)
		pn.Text("collect $200", "IBM Plex Sans 9", 0, 12, 0.5, 0.5, c.t1)
	case games.JailSquare:
		pn.Restore()
		pn.Save()
		pn.Color(mix(c.surface, c.t2, 0.3))
		pn.Rect(x+w-52, y, 52, 52)
		pn.Fill()
		pn.Width(2.5)
		pn.Color(c.t2)
		for b := range 4 {
			bx := x + w - 46 + float64(b)*12
			pn.Line(bx, y+4, bx, y+48)
		}
		pn.Text("JAIL", "IBM Plex Sans Bold 13", x+10, y+h-34, 0, 0, c.t1)
		pn.Text("just visiting", "IBM Plex Sans 9", x+10, y+h-16, 0, 0, c.t2)
	case games.Fountain:
		pn.Text("FOUNTAIN", "IBM Plex Sans Bold 11", 0, 30, 0.5, 0.5, c.t0)
	case games.GoToJail:
		pn.Text("GO TO JAIL", "IBM Plex Sans Bold 13", 0, 0, 0.5, 0.5, c.danger)
	}
	pn.Restore()
}

func ship(pn pen, x, y float64, col color.NRGBA) {
	pn.MoveTo(x-14, y)
	pn.LineTo(x+14, y)
	pn.LineTo(x+9, y+7)
	pn.LineTo(x-9, y+7)
	pn.Close()
	pn.Color(col)
	pn.Fill()
	pn.MoveTo(x-2, y-2)
	pn.LineTo(x-2, y-16)
	pn.LineTo(x+9, y-2)
	pn.Close()
	pn.Alpha(col, 0.7)
	pn.Fill()
}

func bolt(pn pen, x, y float64, col color.NRGBA) {
	pn.MoveTo(x+3, y-14)
	pn.LineTo(x-7, y+2)
	pn.LineTo(x, y+2)
	pn.LineTo(x-3, y+14)
	pn.LineTo(x+7, y-3)
	pn.LineTo(x, y-3)
	pn.Close()
	pn.Color(col)
	pn.Fill()
}

func drop(pn pen, x, y float64, col color.NRGBA) {
	pn.MoveTo(x, y-13)
	pn.Curve(x+9, y-1, x+9, y+10, x, y+10)
	pn.Curve(x-9, y+10, x-9, y-1, x, y-13)
	pn.Color(col)
	pn.Fill()
}

// fountain draws the fountain on its corner: a basin, its water, and the
// pot above it.
func (p *fortune) fountain(pn pen, u, v float64, pot int) {
	c := p.f.col
	stone := mix(c.t1, c.surface, 0.3)
	water := color.NRGBA{0x6F, 0xB6, 0xE0, 255}
	isoEllipse(pn, u+3, v+3, 0, 30, c.app, 0.25)
	isoEllipse(pn, u, v, 0, 28, shade(stone, 0.7), 1)
	isoEllipse(pn, u, v, 6, 28, stone, 1)
	isoEllipse(pn, u, v, 6.5, 23, water, 0.95)
	isoEllipse(pn, u, v, 6.5, 8, stone, 1)
	jet := project(u, v, 7)
	top := project(u, v, 30)
	pn.Width(3)
	pn.Alpha(water, 0.9)
	pn.Line(jet.X, jet.Y, top.X, top.Y)
	for k := range 6 {
		a := float64(k) * math.Pi / 3
		end := project(u+math.Cos(a)*16, v+math.Sin(a)*16, 8)
		pn.Width(1.5)
		pn.Alpha(water, 0.7)
		pn.MoveTo(top.X, top.Y)
		pn.Curve(top.X, top.Y-8, end.X, end.Y-14, end.X, end.Y)
		pn.Stroke()
	}
	label := project(u, v, 44)
	pn.Text(fmt.Sprintf("$%d", pot), "Geist Mono, IBM Plex Mono Bold 11", label.X, label.Y, 0.5, 0.5, c.accent)
}

// paintMiddle draws the latest news and whose turn it is over the park;
// at the end, the standings.
func (p *fortune) paintMiddle(pn pen) {
	c := p.f.col
	m := project(fBoard/2, fBoard/2, 0)
	if p.g.Phase == games.PhaseOver && !p.busy() {
		pn.Alpha(c.app, 0.85)
		pn.RoundRect(m.X-200, m.Y-120, 400, 250, 10)
		pn.Fill()
		w := p.g.Winner
		title, tone := fmt.Sprintf("%s won.", p.g.Players[w].Name), c.danger
		if w == 0 {
			title, tone = "You won!", c.ok
		}
		pn.Text(title, "IBM Plex Sans Bold 28", m.X, m.Y-82, 0.5, 0.5, tone)
		idx := []int{}
		for i := range p.g.Players {
			idx = append(idx, i)
		}
		slices.SortFunc(idx, func(a, b int) int { return p.g.Worth(b) - p.g.Worth(a) })
		for n, i := range idx {
			y := m.Y - 32 + float64(n)*32
			pn.Color(p.playerColor(i))
			pn.Circle(m.X-150, y, 7)
			pn.Fill()
			name := p.g.Players[i].Name
			if p.g.Players[i].Out {
				name += " (bankrupt)"
			}
			pn.Text(name, "IBM Plex Sans 13", m.X-134, y, 0, 0.5, c.t0)
			pn.Text("$"+thousands(p.g.Worth(i)), "Geist Mono, IBM Plex Mono 13", m.X+160, y, 1, 0.5, c.t0)
		}
		pn.Text("Press N or Space for a new game", "IBM Plex Sans 11", m.X, m.Y+110, 0.5, 0.5, c.t2)
		return
	}
	if len(p.log) > 0 {
		l := p.log[0]
		col := c.t0
		if l.good {
			col = c.ok
		} else if l.bad {
			col = c.danger
		}
		pn.Alpha(c.app, 0.72)
		pn.RoundRect(m.X-210, m.Y+52, 420, 46, 8)
		pn.Fill()
		pn.TextBox(l.text, "IBM Plex Sans 12.5", m.X, m.Y+58, 400, col)
	}
	turn := p.g.Players[p.g.Turn]
	who := "Your turn"
	if turn.CPU {
		who = turn.Name + "'s turn"
	}
	pn.Color(p.playerColor(p.g.Turn))
	pn.Circle(m.X-62, m.Y+118, 6)
	pn.Fill()
	pn.Text(who, "IBM Plex Sans Medium 12", m.X-50, m.Y+118, 0, 0.5, c.t0)
	pn.Text(fmt.Sprintf("round %d of %d", p.g.Round, games.FortuneDays), "Geist Mono, IBM Plex Mono 10", m.X+80, m.Y+118, 0.5, 0.5, c.t2)
}
