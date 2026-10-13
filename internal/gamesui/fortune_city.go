package gamesui

import (
	"image/color"
	"math"
	"math/rand/v2"

	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/i18n"
)

// A city's look on the washi board: the words on its corners, the signs on
// Chance and the taxes, what is printed in its middle, and the pieces that
// stand on it. They follow games.FortuneCities, in order.
type cityArt struct {
	corners              map[int]string // Start, Jail, Fountain, Go to Jail, in the city's script
	cornerFont           string
	chance, tax          string
	middle               func(pn pen, r *rand.Rand, tr *track) // inside the track
	outside              func(pn pen, r *rand.Rand, tr *track) // round it: water, banks, fields
	slip, dice           games.Vec                             // where the news and the dice lie
	house                func(pn pen, u, v, s float64, along bool, owner color.NRGBA)
	hotel                func(pn pen, u, v, su, sv float64, owner color.NRGBA)
	tree                 func(pn pen, u, v, s float64, r *rand.Rand)
	pawn                 func(pn pen, u, v, lift float64, col color.NRGBA, turn bool)
	spout                bool // the Fountain is a basin fed by a spout; else a jet
	trees                [][3]float64
	houseWord, hotelWord string // what the setup calls them, in the plural
}

// cityTitles are where each city's board prints its title.
var cityTitles = []games.Vec{{X: 290, Y: 236}, {X: 84, Y: 48}, {X: 290, Y: 236}, {X: 302, Y: 196}}

var cityArts = []cityArt{
	{ // Tokyo
		corners: map[int]string{0: "出発", 10: "牢", 20: "泉", 30: "入牢"}, cornerFont: minchoBold + " 18",
		chance: "運", tax: "税", middle: tokyoMiddle, outside: tokyoOutside,
		slip: games.Vec{X: 450, Y: 478}, dice: games.Vec{X: 430, Y: 548},
		house: machiya, hotel: ryokan, tree: pine, pawn: kokeshi, spout: true, trees: pines,
		houseWord: i18n.M("machiya"), hotelWord: i18n.M("ryokan"),
	},
	{ // New York
		corners: map[int]string{0: "GO", 10: "JAIL", 20: "PARK", 30: "COPS"}, cornerFont: minchoBold + " 13",
		chance: "?", tax: "$", middle: newYorkMiddle, outside: newYorkOutside,
		slip: games.Vec{X: 616, Y: 674}, dice: games.Vec{X: 655, Y: 585},
		house: brownstone, hotel: decoTower, tree: roundTree, pawn: turnedPawn, trees: parkTrees,
		houseWord: i18n.M("brownstones"), hotelWord: i18n.M("towers"),
	},
	{ // Shanghai
		corners: map[int]string{0: "起点", 10: "监狱", 20: "喷泉", 30: "入狱"}, cornerFont: minchoBold + " 17",
		chance: "运", tax: "税", middle: shanghaiMiddle, outside: shanghaiOutside,
		slip: games.Vec{X: 450, Y: 522}, dice: games.Vec{X: 370, Y: 300},
		house: shikumen, hotel: bundHotel, tree: roundTree, pawn: turnedPawn, spout: true, trees: planeTrees,
		houseWord: i18n.M("shikumen"), hotelWord: i18n.M("Bund hotels"),
	},
	{ // Rome
		corners: map[int]string{0: "VIA", 10: "CARCERE", 20: "FONTANA", 30: "ARRESTO"}, cornerFont: minchoBold + " 11",
		chance: "?", tax: "%", middle: romaMiddle, outside: romaOutside,
		slip: games.Vec{X: 450, Y: 496}, dice: games.Vec{X: 420, Y: 560},
		house: villa, hotel: palazzo, tree: cypress, pawn: turnedPawn, trees: cypresses,
		houseWord: i18n.M("villas"), hotelWord: i18n.M("palazzi"),
	},
}

// art is the game's city's look.
func (p *fortune) art() cityArt { return cityArts[p.city()] }

// city is the board's city: the game's, or before one the settings'.
func (p *fortune) city() int {
	if p.g != nil {
		return p.g.City
	}
	return p.settings.City
}

var (
	parkTrees  = [][3]float64{{360, 330, 18}, {392, 290, 15}, {330, 372, 14}, {420, 262, 13}}
	planeTrees = [][3]float64{{176, 466, 24}, {200, 500, 18}}
	cypresses  = [][3]float64{{34, 650, 50}, {54, 672, 42}, {672, 52, 48}, {690, 74, 40}}
)

// --- pieces ---

// brownstone draws a New York row house: brick, a cornice, two storeys of
// windows, a stoop up to a door painted in its owner's colour.
func brownstone(pn pen, u, v, s float64, _ bool, owner color.NRGBA) {
	h := s * 1.55
	u0, v0, u1, v1 := u-s/2, v-s*0.45, u+s/2, v+s*0.45
	brick, stone := color.NRGBA{0x8E, 0x4B, 0x37, 255}, color.NRGBA{0xA8, 0x78, 0x60, 255}
	shadow(pn, u0, v0, s, v1-v0, h)
	inked(pn, color.NRGBA{0x6A, 0x5A, 0x52, 255}, project(u0, v0, h), project(u1, v0, h), project(u1, v1, h), project(u0, v1, h))
	inked(pn, brick, project(u0, v1, 0), project(u1, v1, 0), project(u1, v1, h), project(u0, v1, h))
	pn.Width(0.35)
	pn.Alpha(white, 0.12)
	for k := 1; k < 9; k++ {
		a, b := project(u0, v1, h*float64(k)/9), project(u1, v1, h*float64(k)/9)
		pn.Line(a.X, a.Y, b.X, b.Y)
	}
	poly(pn, shade(brick, 0.6), project(u0-0.6, v1+0.8, h-1.4), project(u1+0.6, v1+0.8, h-1.4), project(u1+0.6, v1+0.8, h+0.6), project(u0-0.6, v1+0.8, h+0.6))
	for _, z := range []float64{h * 0.5, h * 0.76} {
		for _, f := range []float64{0.27, 0.73} {
			c := u0 + s*f
			win(pn, c-s*0.12, c+s*0.12, v1, z-h*0.09, z+h*0.07, color.NRGBA{0xF2, 0xD7, 0x8A, 255})
		}
	}
	// The stoop and the door.
	inked(pn, stone, project(u-s*0.22, v1+2.6, 2.4), project(u+s*0.22, v1+2.6, 2.4), project(u+s*0.22, v1, 2.4), project(u-s*0.22, v1, 2.4))
	inked(pn, shade(stone, 0.8), project(u-s*0.22, v1+2.6, 0), project(u+s*0.22, v1+2.6, 0), project(u+s*0.22, v1+2.6, 2.4), project(u-s*0.22, v1+2.6, 2.4))
	poly(pn, owner, project(u-s*0.13, v1, 2.4), project(u+s*0.13, v1, 2.4), project(u+s*0.13, v1, h*0.33), project(u-s*0.13, v1, h*0.33))
}

// win draws a lit window from u0 to u1 on the plane v, z0 to z1 high, in
// a pale frame.
func win(pn pen, u0, u1, v, z0, z1 float64, glass color.NRGBA) {
	poly(pn, color.NRGBA{0xE9, 0xE0, 0xCC, 255}, project(u0-0.4, v, z0-0.4), project(u1+0.4, v, z0-0.4), project(u1+0.4, v, z1+0.4), project(u0-0.4, v, z1+0.4))
	poly(pn, glass, project(u0, v, z0), project(u1, v, z0), project(u1, v, z1), project(u0, v, z1))
}

// decoTower draws a hotel in New York: an Art Deco tower in setbacks, lit,
// a band of its owner's colour under a spire.
func decoTower(pn pen, u, v, su, sv float64, owner color.NRGBA) {
	stone := color.NRGBA{0xD9, 0xCE, 0xB8, 255}
	glass := color.NRGBA{0xF2, 0xD7, 0x8A, 255}
	shadow(pn, u-su/2, v-sv/2, su, sv, 40)
	tiers := [][3]float64{{0, 0, 20}, {2.2, 20, 34}, {4.6, 34, 44}}
	for i, t := range tiers {
		in, z0, z1 := t[0], t[1], t[2]
		a0, a1 := u-su/2+in, u+su/2-in
		b0, b1 := v-sv/2+in*0.6, v+sv/2-in*0.6
		inked(pn, shade(stone, 0.88-0.04*float64(i)), project(a0, b1, z0), project(a1, b1, z0), project(a1, b1, z1), project(a0, b1, z1))
		inked(pn, mix(stone, white, 0.25), project(a0, b0, z1), project(a1, b0, z1), project(a1, b1, z1), project(a0, b1, z1))
		cols := int((a1 - a0) / 3.4)
		for row := z0 + 2; row < z1-2; row += 3.2 {
			for k := range cols {
				c := a0 + 1.4 + float64(k)*(a1-a0-2.8)/float64(cols)
				poly(pn, glass, project(c, b1, row), project(c+1.2, b1, row), project(c+1.2, b1, row+1.8), project(c, b1, row+1.8))
			}
		}
	}
	a0, a1, b1 := u-su/2+4.6, u+su/2-4.6, v+sv/2-2.8
	poly(pn, owner, project(a0, b1, 41), project(a1, b1, 41), project(a1, b1, 43.6), project(a0, b1, 43.6))
	top, tip := project(u, v, 44), project(u, v, 56)
	pn.Width(1.2)
	pn.Color(shade(stone, 0.6))
	pn.Line(top.X, top.Y, tip.X, tip.Y)
	pn.Color(groupColors[3])
	pn.Circle(tip.X, tip.Y, 1.3)
	pn.Fill()
}

// shikumen draws a Shanghai lane house: grey brick under a tiled roof, a
// stone gate with a carved lintel round a black door, its couplets in the
// owner's colour.
func shikumen(pn pen, u, v, s float64, along bool, owner color.NRGBA) {
	grey := color.NRGBA{0x8D, 0x8F, 0x90, 255}
	gabled(pn, u, v, s, along, 0.36, color.NRGBA{0x50, 0x54, 0x5C, 255}, grey, func(u0, u1, v, h float64) {
		inked(pn, grey, project(u0, v, 0), project(u1, v, 0), project(u1, v, h), project(u0, v, h))
		pn.Width(0.35)
		pn.Alpha(white, 0.16)
		for k := 1; k < 8; k++ {
			a, b := project(u0, v, h*float64(k)/8), project(u1, v, h*float64(k)/8)
			pn.Line(a.X, a.Y, b.X, b.Y)
		}
		m, d := (u0+u1)/2, (u1-u0)*0.22
		stone := color.NRGBA{0xDD, 0xD5, 0xC4, 255}
		poly(pn, stone, project(m-d-1, v, 0), project(m+d+1, v, 0), project(m+d+1, v, h*0.7), project(m-d-1, v, h*0.7))
		// The lintel, an arch over the gate.
		c := project(m, v, h*0.7)
		pn.Save()
		pn.Translate(c.X, c.Y)
		pn.Scale((d+1.6)*boardK, 2.4*boardK*elevCos)
		pn.Arc(0, 0, 1, math.Pi, 2*math.Pi)
		pn.Restore()
		pn.Color(stone)
		pn.Fill()
		poly(pn, color.NRGBA{0x22, 0x1E, 0x1C, 255}, project(m-d, v, 0), project(m+d, v, 0), project(m+d, v, h*0.62), project(m-d, v, h*0.62))
		for _, side := range []float64{-1, 1} {
			x := m + side*(d+2)
			poly(pn, owner, project(x-0.7, v, h*0.12), project(x+0.7, v, h*0.12), project(x+0.7, v, h*0.6), project(x-0.7, v, h*0.6))
		}
	})
}

// bundHotel draws a hotel in Shanghai: a stone building on the Bund under a
// green pyramid roof, its owner's colour in a band below the roof.
func bundHotel(pn pen, u, v, su, sv float64, owner color.NRGBA) {
	stone := color.NRGBA{0xD6, 0xC8, 0xAA, 255}
	green := color.NRGBA{0x4E, 0x86, 0x6C, 255}
	h := 30.0
	u0, v0, u1, v1 := u-su/2, v-sv/2, u+su/2, v+sv/2
	shadow(pn, u0, v0, su, sv, h+10)
	inked(pn, shade(stone, 0.88), project(u0, v1, 0), project(u1, v1, 0), project(u1, v1, h), project(u0, v1, h))
	for row := 3.0; row < h-3; row += 4 {
		cols := int(su / 3.6)
		for k := range cols {
			c := u0 + 1.6 + float64(k)*(su-3.2)/float64(cols)
			poly(pn, color.NRGBA{0xF2, 0xD7, 0x8A, 255}, project(c, v1, row), project(c+1.3, v1, row), project(c+1.3, v1, row+2.2), project(c, v1, row+2.2))
		}
	}
	poly(pn, owner, project(u0, v1, h-2.4), project(u1, v1, h-2.4), project(u1, v1, h), project(u0, v1, h))
	// The pyramid: a square on top, its four faces up to the apex.
	q := min(su, sv) * 0.48
	a0, a1, b0, b1 := u-q, u+q, v-q*0.8, v+q*0.8
	inked(pn, mix(stone, white, 0.2), project(u0, v0, h), project(u1, v0, h), project(u1, v1, h), project(u0, v1, h))
	apex := project(u, v, h+q*1.4)
	inked(pn, shade(green, 1.2), project(a0, b0, h), project(a1, b0, h), apex)
	inked(pn, shade(green, 1.1), project(a0, b0, h), project(a0, b1, h), apex)
	inked(pn, shade(green, 0.8), project(a1, b0, h), project(a1, b1, h), apex)
	inked(pn, green, project(a0, b1, h), project(a1, b1, h), apex)
}

// villa draws a Roman house: warm stucco under a low terracotta roof,
// green shutters, a door in its owner's colour.
func villa(pn pen, u, v, s float64, along bool, owner color.NRGBA) {
	stucco := color.NRGBA{0xDA, 0xA0, 0x68, 255}
	gabled(pn, u, v, s, along, 0.26, color.NRGBA{0xB4, 0x52, 0x2E, 255}, stucco, func(u0, u1, v, h float64) {
		inked(pn, stucco, project(u0, v, 0), project(u1, v, 0), project(u1, v, h), project(u0, v, h))
		m := (u0 + u1) / 2
		w := (u1 - u0) * 0.17
		poly(pn, owner, project(m-w, v, 0), project(m+w, v, 0), project(m+w, v, h*0.5), project(m-w, v, h*0.5))
		for _, f := range []float64{0.2, 0.8} {
			c := u0 + (u1-u0)*f
			poly(pn, color.NRGBA{0x4F, 0x7A, 0x4E, 255}, project(c-w*0.9, v, h*0.6), project(c+w*0.9, v, h*0.6), project(c+w*0.9, v, h*0.9), project(c-w*0.9, v, h*0.9))
			pn.Width(0.4)
			pn.Alpha(washiInk, 0.5)
			a, b := project(c, v, h*0.6), project(c, v, h*0.9)
			pn.Line(a.X, a.Y, b.X, b.Y)
		}
	})
}

// palazzo draws a hotel in Rome: a pale palazzo with arches below, shuttered
// windows above, a hipped terracotta roof and its owner's banners.
func palazzo(pn pen, u, v, su, sv float64, owner color.NRGBA) {
	stone := color.NRGBA{0xE6, 0xCD, 0xA4, 255}
	roof := color.NRGBA{0xB4, 0x52, 0x2E, 255}
	h := 22.0
	u0, v0, u1, v1 := u-su/2, v-sv/2, u+su/2, v+sv/2
	shadow(pn, u0, v0, su, sv, h+6)
	inked(pn, shade(stone, 0.9), project(u0, v1, 0), project(u1, v1, 0), project(u1, v1, h), project(u0, v1, h))
	n := max(3, int(su/7))
	for k := range n {
		c := u0 + (float64(k)+0.5)*su/float64(n)
		w := su / float64(n) * 0.3
		// An arch below.
		a := project(c, v1, 7)
		poly(pn, shade(stone, 0.45), project(c-w, v1, 0), project(c+w, v1, 0), project(c+w, v1, 7), project(c-w, v1, 7))
		pn.Save()
		pn.Translate(a.X, a.Y)
		pn.Scale(w*boardK, w*boardK*elevCos)
		pn.Arc(0, 0, 1, math.Pi, 2*math.Pi)
		pn.Restore()
		pn.Color(shade(stone, 0.45))
		pn.Fill()
		// A window above, its shutters.
		poly(pn, color.NRGBA{0x4F, 0x7A, 0x4E, 255}, project(c-w*0.8, v1, 12), project(c+w*0.8, v1, 12), project(c+w*0.8, v1, 18), project(c-w*0.8, v1, 18))
	}
	for _, f := range []float64{1.0 / 3, 2.0 / 3} {
		c := u0 + su*f
		poly(pn, owner, project(c-1, v1+0.3, h-1), project(c+1, v1+0.3, h-1), project(c+1, v1+0.3, h-9), project(c, v1+0.3, h-11), project(c-1, v1+0.3, h-9))
	}
	// The hipped roof: back, sides, front up to a ridge.
	o, r := 1.4, 7.0
	ra, rb := u0+sv*0.4, u1-sv*0.4
	inked(pn, shade(roof, 1.25), project(u0-o, v0-o, h), project(u1+o, v0-o, h), project(rb, v, h+r), project(ra, v, h+r))
	inked(pn, shade(roof, 1.1), project(u0-o, v0-o, h), project(ra, v, h+r), project(u0-o, v1+o, h))
	inked(pn, shade(roof, 0.85), project(u1+o, v0-o, h), project(rb, v, h+r), project(u1+o, v1+o, h))
	inked(pn, roof, project(u0-o, v1+o, h), project(u1+o, v1+o, h), project(rb, v, h+r), project(ra, v, h+r))
	tileRows(pn, u0-o, u1+o, v, v1+o, h+r, h)
}

// turnedPawn draws a player's pawn standing at u, v, lifted by lift: a
// lacquered wooden pawn, a base, a waist, a ball.
func turnedPawn(pn pen, u, v, lift float64, col color.NRGBA, turn bool) {
	r := 6.6
	ellipse(pn, u+1.5, v+1.5, 0, r*1.15-lift*0.02, washiInk, 0.22)
	if turn {
		ring(pn, u, v, r*1.9)
	}
	base := project(u, v, lift)
	k, ky := r*boardK, r*boardK*elevSin
	lit, dark := mix(col, white, 0.3), shade(col, 0.6)
	// The base, a disc with its edge.
	pn.Gradient(base.X-k, 0, base.X+k, 0, lit, dark)
	pn.Save()
	pn.Translate(base.X, base.Y)
	pn.Scale(k, ky)
	pn.Circle(0, 0, 1)
	pn.Restore()
	pn.Fill()
	b2 := project(u, v, lift+3)
	pn.Gradient(b2.X-k, 0, b2.X+k, 0, mix(col, white, 0.45), col)
	pn.Save()
	pn.Translate(b2.X, b2.Y)
	pn.Scale(k*0.92, ky*0.92)
	pn.Circle(0, 0, 1)
	pn.Restore()
	pn.Fill()
	// The body narrowing to the neck.
	neck := project(u, v, lift+15)
	pn.Gradient(b2.X-k, 0, b2.X+k, 0, lit, dark)
	pn.MoveTo(b2.X-k*0.62, b2.Y)
	pn.Curve(b2.X-k*0.5, b2.Y-(b2.Y-neck.Y)*0.5, neck.X-k*0.22, neck.Y+ky*0.6, neck.X-k*0.24, neck.Y)
	pn.LineTo(neck.X+k*0.24, neck.Y)
	pn.Curve(neck.X+k*0.22, neck.Y+ky*0.6, b2.X+k*0.5, b2.Y-(b2.Y-neck.Y)*0.5, b2.X+k*0.62, b2.Y)
	pn.Close()
	pn.Fill()
	// The ball.
	hd := project(u, v, lift+15+4.6)
	hr := 4.8 * boardK
	pn.Color(col)
	pn.Circle(hd.X, hd.Y, hr)
	pn.Fill()
	pn.Gradient(hd.X-hr, hd.Y-hr, hd.X+hr, hd.Y+hr, color.NRGBA{255, 255, 255, 110}, color.NRGBA{0, 0, 0, 70})
	pn.Circle(hd.X, hd.Y, hr)
	pn.Fill()
	pn.Alpha(white, 0.7)
	pn.Circle(hd.X-hr*0.38, hd.Y-hr*0.42, hr*0.24)
	pn.Fill()
}

// ring draws an ink ring on the board round u, v, radius r: whose turn.
func ring(pn pen, u, v, r float64) {
	c := project(u, v, 0)
	pn.Save()
	pn.Translate(c.X, c.Y)
	pn.Scale(boardK*r, boardK*r*elevSin)
	pn.Arc(0, 0, 1, -0.3, 2*math.Pi-0.9)
	pn.Restore()
	pn.Width(2)
	pn.Round()
	pn.Color(washiInk)
	pn.Stroke()
}

// roundTree draws a broadleaf tree at u, v: a trunk, a round crown.
func roundTree(pn pen, u, v, s float64, r *rand.Rand) {
	shadow(pn, u-s*0.3, v-s*0.1, s*0.6, s*0.25, s*0.5)
	a, b := project(u, v, 0), project(u, v, s*0.55)
	pn.Width(s * 0.11 * boardK)
	pn.Round()
	pn.Color(color.NRGBA{0x5A, 0x40, 0x2C, 255})
	pn.Line(a.X, a.Y, b.X, b.Y)
	leaf := color.NRGBA{0x5B, 0x7F, 0x4A, 255}
	for k := range 4 {
		c := project(u+(r.Float64()-0.5)*s*0.4, v, s*(0.8+0.12*float64(k%2)))
		w := s * (0.34 - 0.03*float64(k)) * boardK
		pn.Color(shade(leaf, 0.85+0.07*float64(k)))
		pn.Circle(c.X, c.Y-float64(k)*s*0.06*boardK, w)
		pn.FillKeep()
		pn.Width(0.6)
		pn.Alpha(washiInk, 0.5)
		pn.Stroke()
	}
	c := project(u-s*0.1, v, s*1.0)
	pn.Alpha(color.NRGBA{0xA8, 0xC2, 0x7E, 255}, 0.5)
	pn.Circle(c.X, c.Y, s*0.14*boardK)
	pn.Fill()
}

// cypress draws an Italian cypress at u, v: tall, narrow, dark.
func cypress(pn pen, u, v, s float64, _ *rand.Rand) {
	shadow(pn, u-s*0.12, v-s*0.06, s*0.24, s*0.12, s*0.4)
	base, top := project(u, v, s*0.08), project(u, v, s*1.25)
	w := s * 0.13 * boardK
	pn.Gradient(base.X-w, 0, base.X+w, 0, color.NRGBA{0x4D, 0x6B, 0x3E, 255}, color.NRGBA{0x24, 0x3A, 0x22, 255})
	pn.MoveTo(base.X, base.Y)
	pn.Curve(base.X-w*1.4, base.Y-(base.Y-top.Y)*0.3, top.X-w*0.6, top.Y+(base.Y-top.Y)*0.2, top.X, top.Y)
	pn.Curve(top.X+w*0.6, top.Y+(base.Y-top.Y)*0.2, base.X+w*1.4, base.Y-(base.Y-top.Y)*0.3, base.X, base.Y)
	pn.FillKeep()
	pn.Width(0.6)
	pn.Alpha(washiInk, 0.55)
	pn.Stroke()
}

// jetFountain draws the Fountain corner's round fountain: a stone basin,
// its water, jets falling back into it, and the pot above.
func jetFountain(pn pen, u, v float64) {
	const rb = 16.0
	ellipse(pn, u+3, v+3, 0, rb+2, washiInk, 0.18)
	ellipse(pn, u, v, 0, rb, shade(washiStone, 0.72), 1)
	ellipse(pn, u, v, 5, rb, mix(washiStone, white, 0.2), 1)
	ellipse(pn, u, v, 5.3, rb-3.5, washiWater, 0.95)
	ellipse(pn, u, v, 5.5, 4, washiStone, 1)
	jet, top := project(u, v, 6), project(u, v, 26)
	pn.Width(2.2)
	pn.Round()
	pn.Alpha(washiWater, 0.85)
	pn.Line(jet.X, jet.Y, top.X, top.Y)
	for k := range 6 {
		a := float64(k) * math.Pi / 3
		end := project(u+math.Cos(a)*10, v+math.Sin(a)*10, 6)
		pn.Width(1.1)
		pn.Alpha(washiWater, 0.7)
		pn.MoveTo(top.X, top.Y)
		pn.Curve(top.X, top.Y-6, end.X, end.Y-10, end.X, end.Y)
		pn.Stroke()
	}
}

// --- the middles ---

// tokyoMiddle prints the title before a red sun.
func tokyoMiddle(pn pen, r *rand.Rand, tr *track) {
	sun(pn, 360, 178, 46, washiShu, r)
	title(pn, cityTitles[0].X, cityTitles[0].Y, "小さな福", minchoBold+" 40", "S M A L L    F O R T U N E", "福", r)
}

// tokyoOutside prints the city's lanes, and Tokyo Bay's waves to the
// south-east.
func tokyoOutside(pn pen, r *rand.Rand, tr *track) {
	lanes(pn, r)
	pn.Save()
	pn.Circle(fBoard, fBoard, 210)
	pn.Clip()
	pn.Alpha(washiIndigo, 0.06)
	pn.Paint()
	seigaiha(pn, 380, 520, 14)
	pn.Restore()
	pn.Text("東京湾", minchoBold+" 10", 660, 690, 0.5, 0.5, shade(washiIndigo, 1.4))
}

// newYorkMiddle prints Central Park down the middle of the island, its
// reservoir.
func newYorkMiddle(pn pen, r *rand.Rand, tr *track) {
	// The park lies along the island: from the island's axis, w wide.
	ax, ay := 540.0-186, 92.0-624
	l := math.Hypot(ax, ay)
	ux, uy := ax/l, ay/l
	nx, ny := -uy, ux
	c := games.Vec{X: 186 + ax*0.62, Y: 624 + ay*0.62}
	corner := func(a, b float64) (float64, float64) { return c.X + ux*a + nx*b, c.Y + uy*a + ny*b }
	pn.MoveTo(corner(-104, -36))
	pn.LineTo(corner(104, -36))
	pn.LineTo(corner(104, 36))
	pn.LineTo(corner(-104, 36))
	pn.Close()
	pn.Alpha(color.NRGBA{0x9F, 0xB8, 0x7E, 255}, 0.7)
	pn.FillKeep()
	pn.Width(1)
	pn.Alpha(washiInk, 0.5)
	pn.Stroke()
	x, y := corner(30, 0)
	pn.Save()
	pn.Translate(x, y)
	pn.Rotate(math.Atan2(uy, ux))
	pn.Scale(34, 18)
	pn.Circle(0, 0, 1)
	pn.Restore()
	pn.Alpha(washiWater, 0.75)
	pn.Fill()
	// Avenues across the island's south.
	pn.Width(0.6)
	pn.Alpha(washiInk, 0.18)
	for k := range 9 {
		x0, y0 := 186+ax*(0.08+float64(k)*0.05), 624+ay*(0.08+float64(k)*0.05)
		pn.Line(x0-nx*90, y0-ny*90, x0+nx*90, y0+ny*90)
	}
}

// newYorkOutside prints the harbour and the rivers round the island, New
// Jersey and Brooklyn on their banks, Liberty Island, the title.
func newYorkOutside(pn pen, r *rand.Rand, tr *track) {
	pn.Color(mix(washiPaper, washiWater, 0.22))
	pn.Paint()
	ripples(pn, 0, fBoard, 0, fBoard, r)
	land := mix(washiPaper, color.NRGBA{0xC9, 0xBE, 0x9C, 255}, 0.25)
	for _, shore := range [][]games.Vec{
		{{X: 0, Y: 0}, {X: 300, Y: 0}, {X: 262, Y: 52}, {X: 130, Y: 196}, {X: 60, Y: 380}, {X: 0, Y: 430}},
		{{X: 706, Y: 360}, {X: 706, Y: 706}, {X: 448, Y: 706}, {X: 516, Y: 640}, {X: 600, Y: 548}, {X: 664, Y: 430}},
	} {
		polyPath(pn, shore)
		pn.Color(land)
		pn.FillKeep()
		pn.Width(1.2)
		pn.Alpha(washiInk, 0.55)
		pn.Stroke()
	}
	pn.Text("NEW JERSEY", mincho+" Medium 7", 46, 300, 0.5, 0.5, shade(washiInk, 2))
	pn.Text("BROOKLYN", mincho+" Medium 7", 650, 520, 0.5, 0.5, shade(washiInk, 2))
	sun(pn, 640, 300, 36, groupColors[3], r)
	title(pn, cityTitles[1].X, cityTitles[1].Y, "NEW YORK", minchoBold+" 18", "S M A L L    F O R T U N E", "NY", r)
}

// shanghaiMiddle prints the title, a red sun and clouds.
func shanghaiMiddle(pn pen, r *rand.Rand, tr *track) {
	lanes(pn, r)
	pn.Color(washiPaper)
	sun(pn, 470, 200, 50, washiShu, r)
	for _, c := range [][3]float64{{176, 346, 1}, {360, 176, 0.8}, {420, 520, 0.9}} {
		cloud(pn, c[0], c[1], c[2])
	}
	title(pn, cityTitles[2].X, cityTitles[2].Y, "上海", minchoBold+" 44", "S H A N G H A I", "福", r)
}

// shanghaiOutside prints the lanes, the Huangpu along the east of the
// track, and Pudong beyond it.
func shanghaiOutside(pn pen, r *rand.Rand, tr *track) {
	lanes(pn, r)
	var course []games.Vec
	for _, i := range []int{28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 0, 1} {
		t := tr.tiles[i]
		course = append(course, t.at(0, t.across/2+34))
	}
	pn.Round()
	pn.Width(56)
	pn.Alpha(mix(washiPaper, washiWater, 0.4), 1)
	strokeCurve(pn, course)
	pn.Width(1)
	for _, d := range []float64{-14, 0, 14} {
		var line []games.Vec
		for k, i := range []int{28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 0, 1} {
			t := tr.tiles[i]
			line = append(line, t.at(float64(k%3)*4, t.across/2+34+d))
		}
		pn.Alpha(washiIndigo, 0.3)
		strokeCurve(pn, line)
	}
	label := tr.tiles[31].at(0, tr.tiles[31].across/2+30)
	pn.Text("黄浦江", minchoBold+" 9", min(label.X, fBoard-24), label.Y, 0.5, 0.5, shade(washiIndigo, 1.5))
}

// romaMiddle prints the Tiber through the walls, the Colosseum, the title
// under a warm sun.
func romaMiddle(pn pen, r *rand.Rand, tr *track) {
	var course []games.Vec
	for _, i := range []int{20, 19, 18, 17, 16, 15, 14, 13, 12, 11} {
		t := tr.tiles[i]
		course = append(course, t.at(0, -t.across/2-34))
	}
	pn.Round()
	pn.Width(26)
	pn.Alpha(mix(washiPaper, washiWater, 0.45), 1)
	strokeCurve(pn, course)
	pn.Width(0.8)
	pn.Alpha(washiIndigo, 0.35)
	strokeCurve(pn, course)
	pn.Text("Tevere", mincho+" Medium 7.5", course[4].X, course[4].Y, 0.5, 0.5, shade(washiIndigo, 1.5))
	sun(pn, 420, 158, 38, color.NRGBA{0xD9, 0x8A, 0x3C, 255}, r)
	colosseum(pn, 528, 262, 58, 15)
	title(pn, cityTitles[3].X, cityTitles[3].Y, "ROMA", minchoBold+" 36", "S M A L L    F O R T U N E", "SPQR", r)
}

// romaOutside prints the Campagna: fields in washes of green and ochre.
func romaOutside(pn pen, r *rand.Rand, tr *track) {
	for range 60 {
		x, y := r.Float64()*fBoard, r.Float64()*fBoard
		col := []color.NRGBA{{0x9F, 0xB8, 0x7E, 255}, {0xD9, 0xB8, 0x6A, 255}, {0xB8, 0xA0, 0x6E, 255}}[r.IntN(3)]
		pn.Save()
		pn.Translate(x, y)
		pn.Rotate(r.Float64() * math.Pi)
		pn.Rect(-30-r.Float64()*20, -12-r.Float64()*8, 60+r.Float64()*40, 24+r.Float64()*16)
		pn.Restore()
		pn.Alpha(col, 0.18)
		pn.Fill()
	}
}

// colosseum prints the Colosseum at cx, cy: its near wall in three tiers
// of arches, the far rim behind.
func colosseum(pn pen, cx, cy, rx, ry float64) {
	h := ry * 2.2
	ell := func(y float64) {
		pn.Save()
		pn.Translate(cx, y)
		pn.Scale(rx, ry)
		pn.Circle(0, 0, 1)
		pn.Restore()
	}
	ell(cy - h)
	pn.Alpha(color.NRGBA{0xD9, 0xB8, 0x86, 255}, 0.75)
	pn.Fill()
	pn.MoveTo(cx-rx, cy-h)
	pn.LineTo(cx-rx, cy)
	pn.Save()
	pn.Translate(cx, cy)
	pn.Scale(rx, ry)
	pn.ArcBack(0, 0, 1, math.Pi, 0)
	pn.Restore()
	pn.LineTo(cx+rx, cy-h)
	pn.Save()
	pn.Translate(cx, cy-h)
	pn.Scale(rx, ry)
	pn.ArcOn(0, 0, 1, 0, math.Pi)
	pn.Restore()
	pn.Close()
	pn.Alpha(color.NRGBA{0xE4, 0xC9, 0x9C, 255}, 1)
	pn.Fill()
	tier := h / 3
	for k := range 3 {
		z := float64(k) * tier
		for a := range 13 {
			t := math.Pi * (float64(a) + 0.5) / 13
			x := cx - math.Cos(t)*rx*0.95
			y := cy - z - tier*0.25 + math.Sin(t)*ry*0.95
			w := 5 * math.Sin(t)
			pn.Alpha(washiInk, 0.55)
			pn.Rect(x-w/2, y-tier*0.5, w, tier*0.36)
			pn.Arc(x, y-tier*0.5, w/2, math.Pi, 2*math.Pi)
			pn.Fill()
		}
		pn.Width(0.9)
		pn.Alpha(washiInk, 0.7)
		pn.Save()
		pn.Translate(cx, cy-z)
		pn.Scale(rx, ry)
		pn.Arc(0, 0, 1, 0, math.Pi)
		pn.Restore()
		pn.Stroke()
	}
	pn.Width(1.1)
	pn.Alpha(washiInk, 0.8)
	ell(cy - h)
	pn.Stroke()
}

// lanes prints a city's lanes, faint, in a loose grid.
func lanes(pn pen, r *rand.Rand) {
	pn.Width(0.5)
	pn.Alpha(washiInk, 0.08)
	for k := 0; k < 30; k++ {
		y := r.Float64() * fBoard
		pn.Line(0, y, fBoard, y+(r.Float64()-0.5)*60)
		x := r.Float64() * fBoard
		pn.Line(x, 0, x+(r.Float64()-0.5)*60, fBoard)
	}
}

// ripples prints water's ripples over a rectangle.
func ripples(pn pen, x0, x1, y0, y1 float64, r *rand.Rand) {
	for y := y0 + 6; y < y1; y += 9 {
		for x := x0 + r.Float64()*20; x < x1; {
			l := 10 + r.Float64()*26
			pn.Width(0.8)
			pn.Alpha(washiIndigo, 0.16+r.Float64()*0.14)
			pn.MoveTo(x, y)
			pn.Curve(x+l*0.3, y-1.4, x+l*0.7, y+1.4, x+l, y)
			pn.Stroke()
			x += l + 8 + r.Float64()*16
		}
	}
}

// strokeCurve strokes a smooth line through pts.
func strokeCurve(pn pen, pts []games.Vec) {
	pn.MoveTo(pts[0].X, pts[0].Y)
	for k := 1; k < len(pts)-1; k++ {
		m := games.Vec{X: (pts[k].X + pts[k+1].X) / 2, Y: (pts[k].Y + pts[k+1].Y) / 2}
		pn.Curve(pts[k].X, pts[k].Y, pts[k].X, pts[k].Y, m.X, m.Y)
	}
	last := pts[len(pts)-1]
	pn.LineTo(last.X, last.Y)
	pn.Stroke()
}

// seigaiha prints rows of wave scales from y down, n rows, fading in.
func seigaiha(pn pen, x0, y float64, n int) {
	for row := range n {
		cy := y + float64(row)*15
		fade := min(1, float64(row+2)/5)
		for col := -1; col < 15; col++ {
			cx := x0 + float64(col)*40 + float64(row%2)*20
			pn.Alpha(washiPaper, 0.85*fade) // each scale hides the one behind
			pn.Arc(cx, cy, 19.5, math.Pi, 2*math.Pi)
			pn.Close()
			pn.Fill()
			for k, rr := range []float64{19, 14.5, 10, 5.5} {
				pn.Width(1.1)
				pn.Alpha(washiIndigo, (0.22+0.06*float64(k))*fade)
				pn.Arc(cx, cy, rr, math.Pi, 2*math.Pi)
				pn.Stroke()
			}
		}
	}
}

// river prints a river's ripples from y down to the bottom.
func river(pn pen, x0, x1, y float64, r *rand.Rand) {
	pn.Alpha(washiIndigo, 0.08)
	pn.Rect(x0, y, x1-x0, 160)
	pn.Fill()
	for k := range 22 {
		yy := y + 6 + float64(k)*6.5
		for x := x0; x < x1; {
			l := 10 + r.Float64()*30
			pn.Width(1)
			pn.Alpha(washiIndigo, 0.25+r.Float64()*0.2)
			pn.MoveTo(x, yy)
			pn.Curve(x+l*0.3, yy-1.6, x+l*0.7, yy+1.6, x+l, yy)
			pn.Stroke()
			x += l + 6 + r.Float64()*14
		}
	}
}

// sun prints a stamped disc, a little uneven.
func sun(pn pen, x, y, rad float64, col color.NRGBA, r *rand.Rand) {
	pn.Alpha(col, 0.9)
	pn.Circle(x, y, rad)
	pn.Fill()
	for range 30 {
		a, d := r.Float64()*2*math.Pi, rad*0.64+r.Float64()*rad*0.36
		pn.Alpha(washiPaper, 0.08+r.Float64()*0.12)
		pn.Circle(x+math.Cos(a)*d, y+math.Sin(a)*d, 2+r.Float64()*5)
		pn.Fill()
	}
}

// cloud prints an auspicious cloud: curls in a row.
func cloud(pn pen, x, y, k float64) {
	pn.Width(1.6)
	pn.Alpha(washiIndigo, 0.55)
	for _, c := range [][3]float64{{0, 0, 14}, {22, -6, 18}, {48, 0, 13}} {
		cx, cy, rr := x+c[0]*k, y+c[1]*k, c[2]*k
		pn.Arc(cx, cy, rr, math.Pi*0.9, math.Pi*2.4)
		pn.Stroke()
		pn.Arc(cx+rr*0.3, cy+rr*0.1, rr*0.45, math.Pi, math.Pi*2.5)
		pn.Stroke()
	}
	pn.Line(x-18*k, y+12*k, x+66*k, y+12*k)
}

// title prints a board's title at x, y, its line in Latin, and a seal
// beside it.
func title(pn pen, x, y float64, name, font, line, seal string, r *rand.Rand) {
	w, _ := pn.Text(name, font, x, y, 0.5, 0.5, washiInk)
	pn.Text(line, mincho+" Medium 8", x, y+38, 0.5, 0.5, washiInk)
	half := max(76, w/2)
	sumi(pn, x-half, y+50, x+half-28, y+50, 1, r)
	sw := 26.0
	if len(seal) > 3 {
		sw = 36
	}
	pn.Width(2)
	pn.Color(washiShu)
	pn.RoundRect(x+half-18, y+40, sw, 26, 3)
	pn.Stroke()
	f := minchoBold + " 15"
	if seal[0] < 0x80 {
		f = minchoBold + " 8"
	}
	pn.Text(seal, f, x+half-18+sw/2, y+53, 0.5, 0.5, washiShu)
}

// cityNames are a city's names for the board's squares.
func cityNames(city int) [len(games.Board)]string {
	var n [len(games.Board)]string
	for i, s := range games.Board {
		n[i] = s.Name
		if c, ok := games.FortuneCities[city].Names[i]; ok {
			n[i] = c
		}
	}
	return n
}
