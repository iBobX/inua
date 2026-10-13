package gamesui

import (
	"fmt"
	"image/color"
	"math"
	"math/rand/v2"
	"slices"
	"sort"
	"time"

	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/i18n"
)

// Small Fortune's board: a printed washi board seen square-on from above
// and in front, at fElev, its track shaped like the city (fortune_track.go).
// The board is laid out flat in units of its own (u to the right, v down,
// fBoard square). The print, which does not change (paper, the city round
// the track, tiles, names, the middle's art), is drawn once into an image
// and laid on the board; what changes (owners, rents, the square in focus)
// is printed over it each frame; what stands on the board (houses, inns,
// pawns, dice, trees, the fountain) is drawn as solids from projected
// corners, back to front.

const (
	fBoard = 706.0 // the paper's side
	fSlab  = 14.0  // the board's thickness

	boardW = 900.0
	boardH = 830.0
	boardK = 1.2 // canvas units per board unit

	fElev = 62 * math.Pi / 180 // the camera's angle above the table
)

var (
	elevSin, elevCos = math.Sin(fElev), math.Cos(fElev)
	boardOX          = (boardW - fBoard*boardK) / 2
	boardOY          = 40.0
)

// project is the canvas point of board point u, v at height z.
func project(u, v, z float64) games.Vec {
	return games.Vec{X: boardOX + u*boardK, Y: boardOY + (v*elevSin-z*elevCos)*boardK}
}

// unproject is the board point under canvas point p, on the board.
func unproject(p games.Vec) (u, v float64) {
	return (p.X - boardOX) / boardK, (p.Y - boardOY) / boardK / elevSin
}

// flat makes pn draw in board units on the board's surface, raised by z.
func flat(pn pen, z float64) {
	pn.Translate(boardOX, boardOY-z*elevCos*boardK)
	pn.Scale(boardK, boardK*elevSin)
}

// The board's own colours: it is a printed object on the table, the same
// whatever the theme.
var (
	washiPaper  = color.NRGBA{0xF1, 0xE9, 0xD6, 255}
	washiTile   = color.NRGBA{0xF6, 0xF0, 0xE0, 255}
	washiInk    = color.NRGBA{0x1F, 0x1B, 0x17, 255}
	washiIndigo = color.NRGBA{0x22, 0x2F, 0x4C, 255}
	washiShu    = color.NRGBA{0xBF, 0x44, 0x30, 255}
	washiWood   = color.NRGBA{0xA8, 0x74, 0x4A, 255}
	washiTiles  = color.NRGBA{0x4A, 0x4F, 0x5A, 255} // roof tiles
	washiPine   = color.NRGBA{0x3E, 0x5B, 0x3A, 255}
	washiStone  = color.NRGBA{0x9A, 0x96, 0x8C, 255}
	washiWater  = color.NRGBA{0x6C, 0x9C, 0xB8, 255}
	washiIvory  = color.NRGBA{0xF7, 0xF1, 0xE3, 255}
	washiSkin   = color.NRGBA{0xF4, 0xE2, 0xC8, 255}
	white       = color.NRGBA{255, 255, 255, 255}
)

// The type: a mincho, as printed on washi.
const (
	mincho     = "Shippori Mincho, Zen Old Mincho, Noto Serif CJK JP, serif"
	minchoBold = "Shippori Mincho, Zen Old Mincho, Noto Serif CJK JP, serif Heavy"
)

// squareAt is the game's city's square at board point u, v; -1 for none.
func squareAt(city int, u, v float64) int {
	for i, t := range trackOf(city).tiles {
		if inside(t.outline, games.Vec{X: u, Y: v}) {
			return i
		}
	}
	return -1
}

// squareAtScreen is the square under a canvas point, -1 for none.
func squareAtScreen(city int, p games.Vec) int {
	u, v := unproject(p)
	return squareAt(city, u, v)
}

// tileFrame turns pn so that square t's print faces the middle of the
// loop: its centre at 0, 0, its inner side up. It returns the square's
// size so turned: along the track, and across.
func tileFrame(pn pen, t tile) (along, across float64) {
	pn.Translate(t.center.X, t.center.Y)
	pn.Rotate(t.turn())
	return t.along, t.across
}

// tokenAt is where player pi's pawn stands on square sq of the city's
// track: each player has a place of its own there, clear of the print. In
// jail, it is behind the bars.
func tokenAt(city, sq, pi int, jailed bool) games.Vec {
	t := trackOf(city).tiles[sq]
	if sq%10 == 0 { // a corner: its words inside, the pawns, then the cell or the basin outside
		if jailed {
			return t.at(-6+12*float64(pi%2), 18+10*float64(pi/2))
		}
		return t.at(-15+30*float64(pi%2), -4+12*float64(pi/2))
	}
	return t.at((float64(pi)-1.5)*t.along*0.17, 10+8*float64(pi%2))
}

// token is where a player's pawn is now, and how high it hops.
func (p *fortune) token(pi int) (games.Vec, float64) {
	e := p.cur
	if e != nil && e.Player == pi {
		switch e.Kind {
		case games.EvWalk:
			s := min(p.t/hopTime, float64(len(e.Path)))
			n := int(s)
			a := e.From // the square it hops from, along the road it took
			if n > 0 {
				a = e.Path[n-1]
			}
			b := a
			if n < len(e.Path) {
				b = e.Path[n]
			}
			frac := s - float64(n)
			return tokenAt(p.city(), a, pi, false).Lerp(tokenAt(p.city(), b, pi, false), frac), math.Sin(frac*math.Pi) * 16
		case games.EvJump:
			frac := min(1, p.t/jumpTime)
			frac = frac * frac * (3 - 2*frac)
			return tokenAt(p.city(), e.From, pi, false).Lerp(tokenAt(p.city(), e.To, pi, e.View.InJail[pi]), frac), math.Sin(frac*math.Pi) * 90
		}
	}
	return tokenAt(p.city(), p.shown.At[pi], pi, p.shown.InJail[pi]), 0
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
	path(pn, pts...)
	pn.Color(col)
	pn.Fill()
}

// path adds a closed polygon of projected points to the path.
func path(pn pen, pts ...games.Vec) {
	pn.MoveTo(pts[0].X, pts[0].Y)
	for _, q := range pts[1:] {
		pn.LineTo(q.X, q.Y)
	}
	pn.Close()
}

// inked fills a polygon and draws its outline in thin ink.
func inked(pn pen, col color.NRGBA, pts ...games.Vec) {
	path(pn, pts...)
	pn.Color(col)
	pn.FillKeep()
	pn.Width(0.7)
	pn.Alpha(washiInk, 0.75)
	pn.Stroke()
}

// ellipse fills the projection of a circle of radius r on the board at
// height z.
func ellipse(pn pen, u, v, z, r float64, col color.NRGBA, a float64) {
	c := project(u, v, z)
	pn.Save()
	pn.Translate(c.X, c.Y)
	pn.Scale(boardK*r, boardK*r*elevSin)
	pn.Circle(0, 0, 1)
	pn.Restore()
	pn.Alpha(col, a)
	pn.Fill()
}

// shadow lays a soft shadow of a w by d footprint at u, v on the board,
// cast down and to the right by a light from the upper left.
func shadow(pn pen, u, v, w, d, h float64) {
	l := h * 0.22
	pn.Save()
	flat(pn, 0)
	for k := 3; k >= 1; k-- {
		g := float64(k) * 0.9
		pn.RoundRect(u+l*0.6-g, v+l*0.5-g, w+l*0.5+2*g, d+l*0.4+2*g, 1.5+g)
		pn.Alpha(washiInk, 0.06)
		pn.Fill()
	}
	pn.Restore()
}

// machiya draws a town house of side s at u, v: dark wood with a lattice
// front, a tiled roof, and a noren in its owner's colour over the door.
// along says the street runs left to right, so the ridge does too.
func machiya(pn pen, u, v, s float64, along bool, owner color.NRGBA) {
	gabled(pn, u, v, s, along, 0.4, washiTiles, plaster, func(u0, u1, v, h float64) { front(pn, u0, u1, v, h, washiWood, owner) })
}

// gabled draws a house of side s at u, v under a pitched roof of roof,
// the roof's height pitch times s; front paints its front wall from u0 to
// u1 on the plane v, h high, and gable is the colour of the gable above
// it. along says the street runs left to right, so the ridge does too.
func gabled(pn pen, u, v, s float64, along bool, pitch float64, roof, gable color.NRGBA, front func(u0, u1, v, h float64)) {
	h, r, o := s*1.05, s*pitch, s*0.07
	u0, v0, u1, v1 := u-s/2, v-s/2, u+s/2, v+s/2
	shadow(pn, u0, v0, s, s, h+r)
	if along { // the ridge along the row: a slope behind, the front wall, a slope in front
		inked(pn, shade(roof, 1.25), project(u0-o, v0-o, h), project(u1+o, v0-o, h), project(u1+o, v, h+r), project(u0-o, v, h+r))
		front(u0, u1, v1, h)
		inked(pn, roof, project(u0-o, v, h+r), project(u1+o, v, h+r), project(u1+o, v1+o, h), project(u0-o, v1+o, h))
		tileRows(pn, u0-o, u1+o, v, v1+o, h+r, h)
		return
	}
	// The ridge from back to front: the gable faces the front.
	inked(pn, gable, project(u0, v1, h*0.55), project(u1, v1, h*0.55), project(u1, v1, h), project(u, v1, h+r), project(u0, v1, h))
	front(u0, u1, v1, h)
	inked(pn, shade(roof, 1.3), project(u0-o, v0-o, h), project(u, v0-o, h+r), project(u, v1+o, h+r), project(u0-o, v1+o, h))
	inked(pn, roof, project(u, v0-o, h+r), project(u1+o, v0-o, h), project(u1+o, v1+o, h), project(u, v1+o, h+r))
}

// plaster is a machiya's upper storey.
var plaster = color.NRGBA{0xEE, 0xE6, 0xD4, 255}

// front draws a machiya's front from u0 to u1 on the plane v, h high: a
// slatted wooden ground floor with its door and noren, a plaster storey
// with a slit window above.
func front(pn pen, u0, u1, v, h float64, wall, owner color.NRGBA) {
	g := h * 0.55
	inked(pn, shade(wall, 0.9), project(u0, v, 0), project(u1, v, 0), project(u1, v, g), project(u0, v, g))
	inked(pn, plaster, project(u0, v, g), project(u1, v, g), project(u1, v, h), project(u0, v, h))
	lattice(pn, u0, u1, v, g)
	door(pn, (u0+u1)/2, v, u1-u0, g, owner)
	w := (u1 - u0) * 0.28
	m := (u0 + u1) / 2
	poly(pn, shade(washiWood, 0.5), project(m-w, v, g+(h-g)*0.35), project(m+w, v, g+(h-g)*0.35), project(m+w, v, g+(h-g)*0.7), project(m-w, v, g+(h-g)*0.7))
}

// lattice draws a machiya's slatted front, from u0 to u1 on the plane v,
// h high.
func lattice(pn pen, u0, u1, v, h float64) {
	pn.Width(0.5)
	pn.Alpha(washiInk, 0.45)
	n := 7
	for k := 1; k < n; k++ {
		a := project(u0+(u1-u0)*float64(k)/float64(n), v, h*0.92)
		b := project(u0+(u1-u0)*float64(k)/float64(n), v, h*0.1)
		pn.Line(a.X, a.Y, b.X, b.Y)
	}
}

// door draws a dark doorway with a noren of col hanging over it.
func door(pn pen, u, v, s, h float64, col color.NRGBA) {
	d := s * 0.17
	poly(pn, shade(washiWood, 0.35), project(u-d, v, 0), project(u+d, v, 0), project(u+d, v, h*0.85), project(u-d, v, h*0.85))
	// The noren: two panels with a slit.
	for _, side := range []float64{-1, 1} {
		a, b := u+side*0.4, u+side*(d+1.2)
		poly(pn, col, project(min(a, b), v+0.2, h*0.92), project(max(a, b), v+0.2, h*0.92), project(max(a, b), v+0.2, h*0.5), project(min(a, b), v+0.2, h*0.5))
	}
}

// tileRows draws the courses of tiles on a roof slope.
func tileRows(pn pen, u0, u1, v0, v1, z0, z1 float64) {
	pn.Width(0.5)
	pn.Alpha(white, 0.18)
	for k := 1; k < 4; k++ {
		f := float64(k) / 4
		a := project(u0, v0+(v1-v0)*f, z0+(z1-z0)*f)
		b := project(u1, v0+(v1-v0)*f, z0+(z1-z0)*f)
		pn.Line(a.X, a.Y, b.X, b.Y)
	}
}

// ryokan draws an inn: two storeys under two tiers of tiled roof, lanterns
// lit by the door, a noren in its owner's colour.
func ryokan(pn pen, u, v, su, sv float64, owner color.NRGBA) {
	h1, h2, r, o := 17.0, 13.0, 5.0, 1.8
	u0, v0, u1, v1 := u-su/2, v-sv/2, u+su/2, v+sv/2
	shadow(pn, u0, v0, su, sv, h1+h2+r)
	wall := shade(washiWood, 1.05)
	// The ground floor, its eaves, the upper floor set back, its roof.
	inked(pn, shade(wall, 0.85), project(u0, v1, 0), project(u1, v1, 0), project(u1, v1, h1), project(u0, v1, h1))
	lattice(pn, u0, u1, v1, h1)
	door(pn, u, v1, min(su, sv)*1.4, h1*0.8, owner)
	inked(pn, shade(washiTiles, 1.3), project(u0-o, v0-o, h1), project(u1+o, v0-o, h1), project(u1+o, v1+o, h1), project(u0-o, v1+o, h1))
	inked(pn, washiTiles, project(u0-o, v1+o, h1), project(u1+o, v1+o, h1), project(u1+o, v1+o, h1-2), project(u0-o, v1+o, h1-2))
	in := 2.5
	inked(pn, plaster, project(u0+in, v1-in, h1), project(u1-in, v1-in, h1), project(u1-in, v1-in, h1+h2), project(u0+in, v1-in, h1+h2))
	pn.Width(0.5)
	pn.Alpha(color.NRGBA{0xF7, 0xD9, 0x8A, 255}, 0.9) // lit paper windows
	for k := range 3 {
		a := u0 + in + 2 + float64(k)*(su-2*in-4)/3
		path(pn, project(a+0.6, v1-in, h1+2), project(a+(su-2*in-4)/3-0.6, v1-in, h1+2), project(a+(su-2*in-4)/3-0.6, v1-in, h1+h2-2), project(a+0.6, v1-in, h1+h2-2))
		pn.Fill()
	}
	inked(pn, shade(washiTiles, 1.25), project(u0+in-o, v0+in-o, h1+h2), project(u1-in+o, v0+in-o, h1+h2), project(u1-in+o, v, h1+h2+r), project(u0+in-o, v, h1+h2+r))
	inked(pn, washiTiles, project(u0+in-o, v, h1+h2+r), project(u1-in+o, v, h1+h2+r), project(u1-in+o, v1-in+o, h1+h2), project(u0+in-o, v1-in+o, h1+h2))
	// Lanterns either side of the door.
	for _, side := range []float64{-1, 1} {
		c := project(u+side*su*0.32, v1+0.5, h1*0.55)
		pn.Glow(c.X, c.Y, 5*boardK, color.NRGBA{0xFF, 0xC8, 0x6A, 255}, 0.5)
		pn.Save()
		pn.Translate(c.X, c.Y)
		pn.Scale(1, 1.35)
		pn.Circle(0, 0, 1.7*boardK)
		pn.Restore()
		pn.Color(washiShu)
		pn.Fill()
	}
}

// kokeshi draws a player's pawn standing at u, v, lifted by lift: a
// turned wooden doll, its body in the player's colour.
func kokeshi(pn pen, u, v, lift float64, col color.NRGBA, turn bool) {
	r := 7.2
	ellipse(pn, u+1.5, v+1.5, 0, r*1.2-lift*0.02, washiInk, 0.22)
	if turn { // an ink ring on the board under it
		ring(pn, u, v, r*1.9)
	}
	base := project(u, v, lift)
	top := project(u, v, lift+18)
	k := r * boardK
	ky := k * elevSin
	// The body: a rounded column, lit from the left.
	pn.Gradient(base.X-k, 0, base.X+k, 0, mix(col, white, 0.25), shade(col, 0.62))
	pn.MoveTo(base.X-k, base.Y)
	pn.Curve(base.X-k*1.05, base.Y-(base.Y-top.Y)*0.5, top.X-k*0.8, top.Y+ky, top.X-k*0.78, top.Y)
	pn.LineTo(top.X+k*0.78, top.Y)
	pn.Curve(top.X+k*0.8, top.Y+ky, base.X+k*1.05, base.Y-(base.Y-top.Y)*0.5, base.X+k, base.Y)
	pn.Save()
	pn.Translate(base.X, base.Y)
	pn.Scale(k, ky)
	pn.ArcOn(0, 0, 1, 0, math.Pi)
	pn.Restore()
	pn.Close()
	pn.Fill()
	// Painted flowers: two bands and a blossom.
	for _, f := range []float64{0.3, 0.72} {
		y := base.Y - (base.Y-top.Y)*f
		w := k * (1 - 0.18*f)
		pn.Width(0.8)
		pn.Alpha(white, 0.55)
		pn.Save()
		pn.Translate(base.X, y)
		pn.Scale(w, ky*(1-0.18*f))
		pn.Arc(0, 0, 1, 0.15, math.Pi-0.15)
		pn.Restore()
		pn.Stroke()
	}
	bl := project(u-1.6, v, lift+9)
	pn.Alpha(white, 0.75)
	for a := range 5 {
		t := float64(a) * 2 * math.Pi / 5
		pn.Circle(bl.X+math.Cos(t)*1.3*boardK*0.5, bl.Y+math.Sin(t)*1.3*boardK*0.5, 0.6*boardK)
	}
	pn.Fill()
	// The head, its hair.
	hd := project(u, v, lift+18+5.8)
	hr := 5.8 * boardK
	pn.Color(washiSkin)
	pn.Circle(hd.X, hd.Y, hr)
	pn.Fill()
	pn.Color(washiInk)
	pn.MoveTo(hd.X-hr, hd.Y)
	pn.ArcOn(hd.X, hd.Y, hr, math.Pi, 2*math.Pi)
	pn.Curve(hd.X+hr*0.5, hd.Y-hr*0.35, hd.X-hr*0.5, hd.Y-hr*0.35, hd.X-hr, hd.Y)
	pn.Fill()
	pn.Alpha(white, 0.35)
	pn.Circle(hd.X-hr*0.4, hd.Y-hr*0.55, hr*0.22)
	pn.Fill()
	pn.Alpha(washiShu, 0.6) // cheeks
	pn.Circle(hd.X-hr*0.45, hd.Y+hr*0.35, hr*0.13)
	pn.Circle(hd.X+hr*0.45, hd.Y+hr*0.35, hr*0.13)
	pn.Fill()
}

// pine draws a pine at u, v: a leaning trunk, tiers of needles.
func pine(pn pen, u, v, s float64, r *rand.Rand) {
	shadow(pn, u-s*0.3, v-s*0.1, s*0.6, s*0.25, s*0.5)
	lean := (r.Float64() - 0.5) * s * 0.3
	a, b := project(u, v, 0), project(u+lean, v, s*0.9)
	pn.Width(s * 0.13 * boardK)
	pn.Round()
	pn.Color(color.NRGBA{0x5A, 0x40, 0x2C, 255})
	pn.MoveTo(a.X, a.Y)
	pn.Curve(a.X+lean*boardK, a.Y-(a.Y-b.Y)*0.4, b.X-lean*boardK*0.5, b.Y+(a.Y-b.Y)*0.3, b.X, b.Y)
	pn.Stroke()
	for t, tier := range [][3]float64{{-0.25, 0.55, 0.55}, {0.3, 0.75, 0.48}, {0, 1.0, 0.4}} {
		c := project(u+lean*tier[1]+tier[0]*s, v, s*tier[1])
		w, h := tier[2]*s*boardK, tier[2]*s*boardK*0.42
		pn.Save()
		pn.Translate(c.X, c.Y)
		pn.Scale(w, h)
		pn.Circle(0, 0, 1)
		pn.Restore()
		pn.Color(shade(washiPine, 0.85+0.1*float64(t)))
		pn.FillKeep()
		pn.Width(0.6)
		pn.Alpha(washiInk, 0.6)
		pn.Stroke()
		pn.Save()
		pn.Translate(c.X-w*0.15, c.Y-h*0.3)
		pn.Scale(w*0.6, h*0.45)
		pn.Circle(0, 0, 1)
		pn.Restore()
		pn.Alpha(color.NRGBA{0x8F, 0xA8, 0x6E, 255}, 0.55)
		pn.Fill()
	}
}

// die draws a die of side s at u, v, z showing n on top: ivory, the one
// pip red.
func die(pn pen, u, v, z, s float64, n int) {
	if z < 1 {
		shadow(pn, u-s/2, v-s/2, s, s, s)
	} else {
		ellipse(pn, u+2, v+2, 0, s*0.6, washiInk, 0.2)
	}
	u0, v0, u1, v1 := u-s/2, v-s/2, u+s/2, v+s/2
	inked(pn, shade(washiIvory, 0.84), project(u0, v1, z), project(u1, v1, z), project(u1, v1, z+s), project(u0, v1, z+s))
	inked(pn, washiIvory, project(u0, v0, z+s), project(u1, v0, z+s), project(u1, v1, z+s), project(u0, v1, z+s))
	pn.Save()
	flat(pn, z+s)
	for _, pip := range pips[n] {
		if n == 1 {
			pn.Color(washiShu)
			pn.Circle(u, v, s*0.17)
		} else {
			pn.Color(washiInk)
			pn.Circle(u+pip[0]*s*0.27, v+pip[1]*s*0.27, s*0.085)
		}
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

// The pines in the middle, and where they stand.
var pines = [][3]float64{{530, 214, 24}, {548, 250, 18}, {204, 410, 22}}

func (p *fortune) paint(pn pen) {
	if p.g == nil {
		return
	}
	// What stands still: the whole board drawn into an image at the
	// camera's zoom, again only when it or the zoom changes; the camera
	// shows its part, pixel for pixel, so panning costs nothing. Over it
	// what moves or marks: the square under the pointer, the one picked,
	// the streets that may be raised, the pawns, the dice, the money flying.
	p.canvasScale = pn.DeviceScale()
	scale := min(p.canvasScale*p.cam.zoom, 6)
	key := p.underKey(scale)
	// While the zoom is changing, the image there is stretched to it; it is
	// drawn again sharp once the zoom settles.
	settling := p.under != nil && time.Since(p.zoomedAt) < zoomSettle && p.underState == p.stateKey()
	if (p.under == nil || key != p.underFor) && !settling {
		if p.under != nil {
			p.under.Free()
		}
		p.under, p.underFor, p.underScale, p.underState = newImage(int(math.Ceil(boardW*scale)), int(math.Ceil(boardH*scale))), key, scale, p.stateKey()
		p.under.Draw(func(q pen) {
			q.Scale(scale, scale)
			q.Round()
			p.paintUnder(q)
		})
	}
	pn.Save()
	p.cam.look(pn)
	pn.Blit(p.under, 1/p.underScale, math.Abs(p.underScale-pn.DeviceScale()) > 0.01) // pixel for pixel, but while zooming
	p.paintMarks(pn)
	p.paintOver(pn)
	p.paintFX(pn)
	pn.Restore()
	p.paintMiddle(pn) // on the screen, whatever the camera
}

// paintMarks draws over the board the square under the pointer, the one
// picked, and the streets the player may raise this turn.
func (p *fortune) paintMarks(pn pen) {
	tr := trackOf(p.city())
	pn.Save()
	flat(pn, 0)
	if p.hover >= 0 {
		polyPath(pn, tr.tiles[p.hover].outline)
		pn.Alpha(white, 0.32)
		pn.Fill()
	}
	if p.g.Turn == 0 && !p.busy() {
		for sq := range games.Board {
			if p.g.CanBuild(sq) {
				pn.Width(2.2)
				pn.Dash(5, 4)
				pn.Color(color.NRGBA{0x5E, 0x84, 0x3E, 255})
				polyPath(pn, tr.tiles[sq].outline)
				pn.Stroke()
				pn.Dash()
			}
		}
	}
	if f := p.focus(); p.selected >= 0 {
		pn.Width(3)
		pn.Round()
		pn.Color(washiShu)
		polyPath(pn, tr.tiles[f].outline)
		pn.Stroke()
	}
	pn.Restore()
}

// underKey names what the still layer shows: when it changes, the layer
// is drawn again.
func (p *fortune) underKey(scale float64) string {
	return fmt.Sprint(scale, " ", p.stateKey())
}

// stateKey names what the still layer shows, its scale aside.
func (p *fortune) stateKey() string {
	v := p.shown
	return fmt.Sprint(p.city(), v.Owner, v.Houses, v.Pot)
}

// paintOver draws what moves over the still layer: the pawns, the one
// whose turn it is in front, and the dice.
func (p *fortune) paintOver(pn pen) {
	art := p.art()
	var solids []solid
	if p.dice[0] > 0 {
		bounce := 0.0
		if p.cur != nil && p.cur.Kind == games.EvDice {
			bounce = math.Abs(math.Sin(p.t*14)) * 18 * max(0, 1-p.t/diceTime)
		}
		for i, d := range p.dice {
			if d == 0 { // one die
				continue
			}
			u, w := art.dice.X-22+float64(i)*44, art.dice.Y-float64(i)*12
			solids = append(solids, solid{w, func() { die(pn, u, w, bounce*(1-0.3*float64(i)), 24, d) }})
		}
	}
	for i := range p.g.Players {
		if p.g.Players[i].Out && (p.cur == nil || p.cur.Player != i) {
			continue
		}
		at, lift := p.token(i)
		dx, jump := p.pawnJolt(i)
		at.X += dx
		lift += jump
		turn := i == p.g.Turn && p.g.Phase != games.PhaseOver
		col := p.playerColor(i)
		depth := at.Y + 0.5
		if turn {
			depth += 0.25
		}
		solids = append(solids, solid{depth, func() { art.pawn(pn, at.X, at.Y, lift, col, turn) }})
	}
	sort.SliceStable(solids, func(a, b int) bool { return solids[a].depth < solids[b].depth })
	for _, s := range solids {
		s.draw()
	}
}

// paintUnder draws what stands still, in the board's own projection: the
// board, its print and who owns what on it, the buildings, trees,
// landmarks and the fountain.
func (p *fortune) paintUnder(pn pen) {
	pn.Save()
	v := p.shown
	art := p.art()
	tr := trackOf(p.city())

	// The board's shadow on the table, and its front edge.
	tl, br := project(0, 0, 0), project(fBoard, fBoard, -fSlab)
	for k := 6; k >= 1; k-- {
		g := float64(k) * 4
		pn.RoundRect(tl.X-g*0.6, tl.Y+g*0.2, br.X-tl.X+g*1.2, br.Y-tl.Y+g*1.1, 6+g)
		pn.Alpha(washiInk, 0.07)
		pn.Fill()
	}
	front := project(0, fBoard, 0)
	pn.Gradient(0, front.Y, 0, br.Y, color.NRGBA{0xDC, 0xD0, 0xB4, 255}, color.NRGBA{0xA4, 0x96, 0x77, 255})
	pn.Rect(tl.X, front.Y, br.X-tl.X, br.Y-front.Y)
	pn.Fill()

	// The print, then what changes on it.
	pn.Save()
	flat(pn, 0)
	p.layPrint(pn)
	for i := range games.Board {
		p.paintTileState(pn, i, v)
	}
	pn.Restore()

	// What stands on the board, back to front.
	var solids []solid
	add := func(u, v float64, draw func()) { solids = append(solids, solid{v + u*0.001, draw}) }
	for i := range games.Board {
		n := v.Houses[i]
		if n == 0 || v.Owner[i] < 0 {
			continue
		}
		owner := p.playerColor(v.Owner[i])
		t := tr.tiles[i]
		mid := -t.across/2 + fBand/2 // the band's middle line
		along := math.Abs(t.tan.X) >= math.Abs(t.tan.Y)
		if n == games.Hotel {
			c := t.at(0, mid)
			su, sv := 38.0, 18.0
			if !along {
				su, sv = 18, 38
			}
			add(c.X, c.Y+sv/2, func() { art.hotel(pn, c.X, c.Y, su, sv, owner) })
			continue
		}
		for k := range n {
			c := t.at((float64(k)-float64(n-1)/2)*min(11.6, t.along/4.2), mid)
			add(c.X, c.Y+6, func() { art.house(pn, c.X, c.Y, 11, along, owner) })
		}
	}
	for _, t := range art.trees {
		add(t[0], t[1], func() { art.tree(pn, t[0], t[1], t[2], rand.New(rand.NewPCG(uint64(t[0]), uint64(t[1])))) })
	}
	for _, l := range landmarksOf(p.city()) {
		add(l.at.X, l.at.Y, func() {
			// Landmarks stand out: larger than the town round them.
			c := project(l.at.X, l.at.Y, 0)
			pn.Save()
			pn.Translate(c.X, c.Y)
			pn.Scale(1.7, 1.7)
			pn.Translate(-c.X, -c.Y)
			l.draw(pn, l.at.X, l.at.Y)
			pn.Restore()
		})
	}
	fc := tr.tiles[20].at(0, 24)
	add(fc.X, fc.Y, func() { p.fountain(pn, fc.X, fc.Y, v.Pot) })
	sort.SliceStable(solids, func(a, b int) bool { return solids[a].depth < solids[b].depth })
	for _, s := range solids {
		s.draw()
	}
	pn.Restore()
}

// layPrint lays the board's print on it, drawing it again when the scale
// on screen has changed.
func (p *fortune) layPrint(pn pen) {
	// At one of three steps of the zoom (1, 1.6, 2.5 times the canvas's):
	// zooming within a step lays the same print smaller or larger.
	scale := p.canvasScale * 2.5
	for _, k := range []float64{1.6, 1} {
		if p.canvasScale*k >= pn.DeviceScale()*0.95 {
			scale = p.canvasScale * k
		}
	}
	scale = min(scale, 5)
	if p.print == nil || math.Abs(scale/p.printScale-1) > 0.05 || p.printCity != p.city() {
		if p.print != nil {
			p.print.Free()
		}
		n := int(math.Ceil(fBoard * scale))
		p.print, p.printScale, p.printCity = newImage(n, n), scale, p.city()
		p.print.Draw(func(q pen) {
			q.Scale(scale, scale)
			paintPrint(q, p.printCity)
		})
		r := rand.New(rand.NewPCG(11, 3))
		p.print.Speckle(r, n*n/9, washiInk, 0.12)
		p.print.Speckle(r, n*n/14, white, 0.35)
	}
	pn.Image(p.print, 0, 0, 1/p.printScale)
}

// paintPrint draws what is printed on the board and never changes: the
// washi, its border, the tiles with their bands, names and signs, the
// corners, and the middle's art.
func paintPrint(pn pen, city int) {
	art, names, tr := cityArts[city], cityNames(city), trackOf(city)
	r := rand.New(rand.NewPCG(7, 1))
	pn.Color(washiIndigo)
	pn.Rect(0, 0, fBoard, fBoard)
	pn.Fill()
	pn.Color(washiPaper)
	pn.Rect(5, 5, fBoard-10, fBoard-10)
	pn.Fill()
	// Fibres in the paper.
	pn.Round()
	for range 900 {
		x, y := 5+r.Float64()*(fBoard-10), 5+r.Float64()*(fBoard-10)
		l, a := 6+r.Float64()*26, r.Float64()*2*math.Pi
		pn.Width(0.3 + r.Float64()*0.6)
		if r.IntN(2) == 0 {
			pn.Alpha(white, 0.5)
		} else {
			pn.Alpha(color.NRGBA{0x8A, 0x74, 0x52, 255}, 0.13)
		}
		pn.MoveTo(x, y)
		pn.Curve(x+math.Cos(a+1)*l*0.4, y+math.Sin(a+1)*l*0.4, x+math.Cos(a-0.6)*l*0.7, y+math.Sin(a-0.6)*l*0.7, x+math.Cos(a)*l, y+math.Sin(a)*l)
		pn.Stroke()
	}
	// Round the track the city, inside it the middle's art.
	art.outside(pn, r, tr)
	pn.Save()
	polyPath(pn, tr.inner)
	pn.Clip()
	pn.Color(washiPaper)
	pn.Paint()
	art.middle(pn, r, tr)
	pn.Restore()
	for i, sq := range games.Board {
		paintTile(pn, tr.tiles[i], i, sq, names[i], art, r)
	}
	// The track's edges, in ink; the branch roads'.
	sumiPath(pn, tr.inner, 1.6, r)
	sumiPath(pn, tr.outer, 1.6, r)
	for _, road := range tr.roads {
		sumiPath(pn, road, 1.4, r)
	}
	// Where the roads widen out of the ring: the road's ground over the
	// ring's edge, and curbs into the road's sides.
	for _, f := range tr.flares {
		polyPath(pn, f)
		pn.Color(washiPaper)
		pn.FillKeep()
		pn.Alpha(washiTile, 0.6)
		pn.Fill()
	}
	for _, c := range tr.curbs {
		pn.Round()
		pn.Width(1.4)
		pn.Alpha(washiInk, 0.85)
		pn.MoveTo(c[0].X, c[0].Y)
		for _, q := range c[1:] {
			pn.LineTo(q.X, q.Y)
		}
		pn.Stroke()
	}
}

// sumiPath draws a closed ink line through pts, its weight wavering a
// little as a brush's does.
func sumiPath(pn pen, pts []games.Vec, w float64, r *rand.Rand) {
	pn.Round()
	for k := range 2 {
		j := func() float64 { return (r.Float64() - 0.5) * 0.5 }
		pn.Width(w * (0.7 + 0.3*float64(k)))
		pn.Alpha(washiInk, 0.55+0.3*float64(k))
		pn.MoveTo(pts[0].X+j(), pts[0].Y+j())
		for _, q := range pts[1:] {
			pn.LineTo(q.X+j()*0.3, q.Y+j()*0.3)
		}
		pn.Close()
		pn.Stroke()
	}
}

// sumi draws an ink line from x0, y0 to x1, y1, w wide, its weight
// wavering a little as a brush's does.
func sumi(pn pen, x0, y0, x1, y1, w float64, r *rand.Rand) {
	pn.Round()
	for k := range 2 {
		j := func() float64 { return (r.Float64() - 0.5) * 0.5 }
		pn.Width(w * (0.7 + 0.3*float64(k)))
		pn.Alpha(washiInk, 0.55+0.3*float64(k))
		pn.MoveTo(x0+j(), y0+j())
		mx, my := (x0+x1)/2+j(), (y0+y1)/2+j()
		pn.Curve(mx, my, mx, my, x1+j(), y1+j())
		pn.Stroke()
	}
}

// paintTile prints a square: its ground, a street's band, its name and
// its sign, turned to face the middle; a corner's words.
func paintTile(pn pen, t tile, i int, sq games.Square, name string, art cityArt, r *rand.Rand) {
	polyPath(pn, t.outline)
	pn.Color(washiPaper) // over the city printed round the track
	pn.FillKeep()
	pn.Alpha(washiTile, 0.6)
	pn.Fill()
	if sq.Kind == games.Street {
		col := groupColors[sq.Group]
		polyPath(pn, t.band)
		pn.Color(col)
		pn.Fill()
		// A wash over it, as pigment settles unevenly.
		pn.Save()
		polyPath(pn, t.band)
		pn.Clip()
		for range 6 {
			c := t.at((r.Float64()-0.5)*t.along, -t.across/2+r.Float64()*fBand)
			pn.Alpha(shade(col, 0.82), 0.12)
			pn.Circle(c.X, c.Y, 4+r.Float64()*8)
			pn.Fill()
		}
		pn.Restore()
		a, b := t.at(-t.along/2, -t.across/2+fBand), t.at(t.along/2, -t.across/2+fBand)
		sumi(pn, a.X, a.Y, b.X, b.Y, 0.9, r)
	}
	// The square's ends, across the track.
	for _, x := range []float64{-t.along / 2, t.along / 2} {
		a, b := t.at(x, -t.across/2), t.at(x, t.across/2)
		sumi(pn, a.X, a.Y, b.X, b.Y, 1.1, r)
	}

	pn.Save()
	along, across := tileFrame(pn, t)
	top := -across / 2
	if sq.Kind == games.Street {
		top += fBand
	}
	label := func() float64 { return pn.TextBox(name, mincho+" Medium 6.6", 0, top+4, along-6, washiInk) }
	switch sq.Kind {
	case games.Street:
		label()
	case games.Pier:
		label()
		ship(pn, 0, 10, washiIndigo)
	case games.Utility:
		label()
		if i == 12 {
			bolt(pn, 0, 10, groupColors[3])
		} else {
			drop(pn, 0, 10, washiWater)
		}
	case games.ChanceSquare:
		pn.Width(1.6)
		pn.Color(washiShu)
		pn.Arc(0, -6, 13, 0.4, 2*math.Pi-0.2)
		pn.Stroke()
		pn.Text(art.chance, minchoBold+" 14", 0, -6, 0.5, 0.5, washiShu)
		pn.Text(i18n.T("Chance"), mincho+" 6.4", 0, across/2-12, 0.5, 0.5, washiInk)
	case games.TaxSquare:
		label()
		pn.Text(art.tax, minchoBold+" 13", 0, 8, 0.5, 0.5, washiInk)
	case games.StartSquare, games.JailSquare, games.Fountain, games.GoToJail:
		word := art.corners[i]
		sub := map[games.SquareKind]string{games.StartSquare: i18n.T("START\ncollect $200"), games.JailSquare: i18n.T("JAIL\njust visiting"), games.Fountain: i18n.T("FOUNTAIN\ntake the pot"), games.GoToJail: i18n.T("GO TO\nJAIL")}[sq.Kind]
		// Toward the middle the city's word and the English; then the
		// pawns; outside, Start's arrow, the cell, the basin.
		pn.Text(word, art.cornerFont, 0, -across/2+11, 0.5, 0.5, washiShu)
		pn.TextBox(sub, mincho+" Medium 5.6", 0, -across/2+20, along-6, washiInk)
		if sq.Kind == games.StartSquare {
			pn.Width(2.4)
			pn.Round()
			pn.Color(washiShu)
			pn.MoveTo(-20, 26)
			pn.LineTo(18, 26)
			pn.Stroke()
			pn.MoveTo(10, 20)
			pn.LineTo(19, 26)
			pn.LineTo(10, 32)
			pn.Stroke()
		}
	}
	if sq.Kind == games.JailSquare { // the cell, on the outer side
		pn.Alpha(washiWood, 0.25)
		pn.Rect(-15, 11, 30, across/2-13)
		pn.Fill()
		pn.Width(1.6)
		pn.Color(shade(washiWood, 0.7))
		for b := range 4 {
			x := -11 + float64(b)*7.3
			pn.Line(x, 12, x, across/2-3)
		}
		pn.Rect(-15, 11, 30, across/2-13)
		pn.Stroke()
	}
	pn.Restore()
}

// paintTileState prints over a square what changes: its owner's wash and
// seal, its price or rent, the pointer over it.
func (p *fortune) paintTileState(pn pen, i int, v games.FortuneView) {
	sq := games.Board[i]
	t := trackOf(p.city()).tiles[i]
	o := v.Owner[i]
	if o >= 0 {
		polyPath(pn, t.outline)
		pn.Alpha(p.playerColor(o), 0.13)
		pn.Fill()
	}
	if sq.Kind != games.Street && sq.Kind != games.Pier && sq.Kind != games.Utility && sq.Kind != games.TaxSquare {
		return
	}
	pn.Save()
	along, across := tileFrame(pn, t)
	price := func(s string, col color.NRGBA) {
		pn.Text(s, mincho+" Medium 6.2", 0, across/2-4, 0.5, 1, col)
	}
	switch {
	case sq.Kind == games.TaxSquare:
		price(fmt.Sprintf("pay $%d", sq.Price), washiShu)
	case o >= 0 && sq.Kind == games.Utility:
		price("dice ×", washiInk)
	case o >= 0:
		price(fmt.Sprintf("rent $%d", rentOf(v, i)), washiInk)
	default:
		price(fmt.Sprintf("$%d", sq.Price), shade(washiInk, 2.4))
	}
	if o >= 0 { // the owner's seal
		s := 9.0
		top := -across / 2
		if sq.Kind == games.Street {
			top += fBand
		}
		sx, sy := along/2-s-2.5, top+2.5
		pn.Color(p.playerColor(o))
		pn.RoundRect(sx, sy, s, s, 1.5)
		pn.Fill()
		pn.Text([]string{"一", "二", "三", "四"}[o], minchoBold+" 5.5", sx+s/2, sy+s/2, 0.5, 0.5, washiPaper)
	}
	pn.Restore()
}

func ship(pn pen, x, y float64, col color.NRGBA) {
	pn.MoveTo(x-12, y)
	pn.LineTo(x+12, y)
	pn.LineTo(x+8, y+6)
	pn.LineTo(x-8, y+6)
	pn.Close()
	pn.Color(col)
	pn.Fill()
	pn.MoveTo(x-2, y-2)
	pn.LineTo(x-2, y-14)
	pn.LineTo(x+8, y-2)
	pn.Close()
	pn.Alpha(col, 0.7)
	pn.Fill()
}

func bolt(pn pen, x, y float64, col color.NRGBA) {
	pn.MoveTo(x+3, y-12)
	pn.LineTo(x-6, y+2)
	pn.LineTo(x, y+2)
	pn.LineTo(x-3, y+12)
	pn.LineTo(x+6, y-3)
	pn.LineTo(x, y-3)
	pn.Close()
	pn.Color(col)
	pn.Fill()
}

func drop(pn pen, x, y float64, col color.NRGBA) {
	pn.MoveTo(x, y-11)
	pn.Curve(x+8, y-1, x+8, y+9, x, y+9)
	pn.Curve(x-8, y+9, x-8, y-1, x, y-11)
	pn.Color(col)
	pn.Fill()
}

// fountain draws the Fountain corner's stone basin, its water and spout,
// and the pot above it.
func (p *fortune) fountain(pn pen, u, v float64, pot int) {
	if !p.art().spout {
		jetFountain(pn, u, v)
		label := project(u+10, v+10, 30)
		pn.Text(fmt.Sprintf("$%d", pot), minchoBold+" 9.5", label.X, label.Y, 0.5, 0.5, washiShu)
		return
	}
	const rb = 16.0
	ellipse(pn, u+3, v+3, 0, rb+2, washiInk, 0.18)
	ellipse(pn, u, v, 0, rb, shade(washiStone, 0.72), 1)
	pn.Gradient(project(u-rb, v, 0).X, 0, project(u+rb, v, 0).X, 0, washiStone, shade(washiStone, 0.8))
	a, b := project(u-rb, v, 0), project(u+rb, v, 6)
	pn.Rect(a.X, b.Y, b.X-a.X, a.Y-b.Y)
	pn.Fill()
	ellipse(pn, u, v, 6, rb, mix(washiStone, white, 0.2), 1)
	ellipse(pn, u, v, 6.3, rb-4.5, washiWater, 0.95)
	ellipse(pn, u-4, v-3, 6.4, 5, white, 0.25)
	// A bamboo spout pouring in.
	s0, s1 := project(u+10, v-12, 20), project(u+3, v-5, 16)
	pn.Width(3.2)
	pn.Round()
	pn.Color(color.NRGBA{0x8C, 0xA0, 0x58, 255})
	pn.Line(s0.X, s0.Y, s1.X, s1.Y)
	w := project(u+2, v-4, 7)
	pn.Width(1.3)
	pn.Alpha(washiWater, 0.8)
	pn.MoveTo(s1.X, s1.Y)
	pn.Curve(s1.X-2, s1.Y+3, w.X+1, w.Y-4, w.X, w.Y)
	pn.Stroke()
	label := project(u+10, v+10, 30)
	pn.Text(fmt.Sprintf("$%d", pot), minchoBold+" 9.5", label.X, label.Y, 0.5, 0.5, washiShu)
}

// paintMiddle sets the latest news and whose turn it is on a slip of paper
// in the middle; at the end, the standings.
func (p *fortune) paintMiddle(pn pen) {
	// The slip where the city keeps it, as the camera sees it, kept on
	// the screen.
	s := project(p.art().slip.X, p.art().slip.Y, 0)
	m := games.Vec{X: (s.X-p.cam.view.X)*p.cam.zoom + boardMiddle.X, Y: (s.Y-p.cam.view.Y)*p.cam.zoom + boardMiddle.Y}
	m.X, m.Y = max(160, min(boardW-160, m.X)), max(50, min(boardH-60, m.Y))
	slip := func(x, y, w, h float64) {
		for k := 3; k >= 1; k-- {
			g := float64(k) * 1.5
			pn.RoundRect(x-g+2, y-g+4, w+2*g, h+2*g, 3+g)
			pn.Alpha(washiInk, 0.06)
			pn.Fill()
		}
		pn.Color(color.NRGBA{0xFB, 0xF7, 0xEC, 255})
		pn.RoundRect(x, y, w, h, 2)
		pn.Fill()
		pn.Color(washiShu)
		pn.Rect(x, y, w, 2.5)
		pn.Fill()
	}
	if p.g.Phase == games.PhaseOver && !p.busy() {
		m = boardMiddle // the standings, over the middle of the board
		slip(m.X-200, m.Y-150, 400, 260)
		w := p.g.Winner
		title := i18n.Tf("%s won.", p.g.Players[w].Name)
		if w == 0 {
			title = i18n.T("You won!")
		}
		pn.Text(title, minchoBold+" 26", m.X, m.Y-112, 0.5, 0.5, washiShu)
		idx := []int{}
		for i := range p.g.Players {
			idx = append(idx, i)
		}
		slices.SortFunc(idx, func(a, b int) int { return p.g.Worth(b) - p.g.Worth(a) })
		for n, i := range idx {
			y := m.Y - 62 + float64(n)*32
			pn.Color(p.playerColor(i))
			pn.RoundRect(m.X-156, y-7, 14, 14, 2)
			pn.Fill()
			name := p.g.Players[i].Name
			if p.g.Players[i].Out {
				name += " (bankrupt)"
			}
			pn.Text(name, mincho+" 13", m.X-134, y, 0, 0.5, washiInk)
			pn.Text("$"+thousands(p.g.Worth(i)), mincho+" Medium 13", m.X+160, y, 1, 0.5, washiInk)
		}
		pn.Text(i18n.T("Press N or Space for a new game"), mincho+" 11", m.X, m.Y+86, 0.5, 0.5, shade(washiInk, 2.4))
		return
	}
	turn := p.g.Players[p.g.Turn]
	who := i18n.T("Your turn")
	if turn.CPU {
		who = i18n.Tf("%s's turn", turn.Name)
	}
	h := 30.0
	if len(p.log) > 0 {
		h = 80
	}
	slip(m.X-110, m.Y-h/2, 220, h)
	y := m.Y - h/2 + 16
	if len(p.log) > 0 {
		l := p.log[0]
		col := washiInk
		if l.good {
			col = color.NRGBA{0x4E, 0x74, 0x34, 255}
		} else if l.bad {
			col = washiShu
		}
		pn.TextBox(l.text, mincho+" 9.5", m.X, y-6, 206, col)
		y += 40
	}
	pn.Color(p.playerColor(p.g.Turn))
	pn.RoundRect(m.X-96, y-5, 10, 10, 1.5)
	pn.Fill()
	pn.Text(who, mincho+" Medium 9.5", m.X-80, y, 0, 0.5, washiInk)
	pn.Text(i18n.Tf("round %d of %d", p.g.Round, p.g.Rounds), mincho+" 8.5", m.X+100, y, 1, 0.5, shade(washiInk, 2.4))
}
