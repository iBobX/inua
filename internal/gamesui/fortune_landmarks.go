package gamesui

import (
	"image/color"
	"math"
	"math/rand/v2"

	"github.com/lyricat/inua/internal/games"
)

// A city's landmarks, standing on its map as solids: where, and how drawn.
type landmark struct {
	at   games.Vec
	draw func(pn pen, u, v float64)
}

var cityLandmarks = [][]landmark{
	{{games.Vec{X: 584, Y: 320}, tokyoTower}, {games.Vec{X: 176, Y: 462}, skytree}},
	{{games.Vec{X: 290, Y: 470}, empireState}, {games.Vec{X: 62, Y: 684}, liberty}},
	{{games.Vec{}, pearlTower}, {games.Vec{}, shanghaiTower}}, // in the Huangpu's bend: placed from the track
	{{games.Vec{X: 52, Y: 58}, stPeters}, {games.Vec{X: 170, Y: 440}, pantheon}},
}

// landmarksOf are a city's landmarks, those placed by its track placed.
func landmarksOf(city int) []landmark {
	ls := append([]landmark(nil), cityLandmarks[city]...)
	if city == 2 {
		t := trackOf(city).tiles[34]
		ls[0].at = t.at(-6, t.across/2+64)
		ls[1].at = t.at(26, t.across/2+56)
	}
	return ls
}

var (
	stoneLight = color.NRGBA{0xE4, 0xDA, 0xC4, 255}
	steelGrey  = color.NRGBA{0xB8, 0xBE, 0xC2, 255}
	glassBlue  = color.NRGBA{0x8F, 0xA9, 0xB8, 255}
	verdigris  = color.NRGBA{0x6E, 0x9C, 0x8C, 255}
)

// block draws a box from u0, v0 to u1, v1, z0 to z1 high: its front and
// its top.
func block(pn pen, u0, v0, u1, v1, z0, z1 float64, col color.NRGBA) {
	inked(pn, shade(col, 0.86), project(u0, v1, z0), project(u1, v1, z0), project(u1, v1, z1), project(u0, v1, z1))
	inked(pn, mix(col, white, 0.2), project(u0, v0, z1), project(u1, v0, z1), project(u1, v1, z1), project(u0, v1, z1))
}

// tokyoTower draws Tokyo Tower: a red and white lattice narrowing to its
// mast, two decks.
func tokyoTower(pn pen, u, v float64) {
	shadow(pn, u-12, v-6, 24, 12, 30)
	const h = 84.0
	w := func(z float64) float64 { return 12 * math.Pow(1-z/h, 1.6) }
	bands := 8
	for k := range bands {
		z0, z1 := h*float64(k)/float64(bands), h*float64(k+1)/float64(bands)
		col := washiShu
		if k%2 == 1 {
			col = color.NRGBA{0xF4, 0xEE, 0xE2, 255}
		}
		poly(pn, col, project(u-w(z0), v, z0), project(u+w(z0), v, z0), project(u+w(z1)+0.4, v, z1), project(u-w(z1)-0.4, v, z1))
	}
	// The lattice, the legs' arch.
	pn.Width(0.5)
	pn.Alpha(washiInk, 0.5)
	for k := range 12 {
		z0, z1 := h*float64(k)/12*0.85, h*float64(k+1)/12*0.85
		a, b := project(u-w(z0), v, z0), project(u+w(z1), v, z1)
		c, d := project(u+w(z0), v, z0), project(u-w(z1), v, z1)
		pn.Line(a.X, a.Y, b.X, b.Y)
		pn.Line(c.X, c.Y, d.X, d.Y)
	}
	base := project(u, v, 0)
	pn.Color(washiPaper)
	pn.Save()
	pn.Translate(base.X, base.Y)
	pn.Scale(w(0)*0.55*boardK, 9*boardK*elevCos)
	pn.Arc(0, 0, 1, math.Pi, 2*math.Pi)
	pn.Restore()
	pn.Fill()
	for _, z := range []float64{30, 54} {
		block(pn, u-w(z)-2.5, v-2, u+w(z)+2.5, v+1, z, z+4, color.NRGBA{0xEE, 0xE6, 0xD6, 255})
	}
	top, tip := project(u, v, h), project(u, v, h+16)
	pn.Width(1.2)
	pn.Color(washiShu)
	pn.Line(top.X, top.Y, tip.X, tip.Y)
}

// skytree draws the Skytree: a pale steel column rising to a mast, two
// rings of decks.
func skytree(pn pen, u, v float64) {
	shadow(pn, u-8, v-4, 16, 8, 30)
	const h = 112.0
	w := func(z float64) float64 { return 7.5 - 4.2*z/h }
	poly(pn, steelGrey, project(u-w(0), v, 0), project(u+w(0), v, 0), project(u+w(h), v, h), project(u-w(h), v, h))
	pn.Width(0.4)
	pn.Alpha(washiInk, 0.4)
	for z := 4.0; z < h; z += 6 {
		a, b := project(u-w(z), v, z), project(u+w(z), v, z+6)
		pn.Line(a.X, a.Y, b.X, b.Y)
	}
	for _, z := range []float64{62, 84} {
		block(pn, u-w(z)-2.2, v-2, u+w(z)+2.2, v+1, z, z+4.5, color.NRGBA{0xC9, 0xD2, 0xD8, 255})
	}
	top, tip := project(u, v, h), project(u, v, h+20)
	pn.Width(1.2)
	pn.Color(steelGrey)
	pn.Line(top.X, top.Y, tip.X, tip.Y)
}

// empireState draws the Empire State Building in its setbacks, lit, its
// mast.
func empireState(pn pen, u, v float64) {
	shadow(pn, u-12, v-8, 24, 16, 60)
	for _, t := range [][3]float64{{13, 0, 26}, {9, 26, 58}, {5.5, 58, 72}, {3, 72, 78}} {
		block(pn, u-t[0], v-t[0]*0.6, u+t[0], v+t[0]*0.6, t[1], t[2], stoneLight)
		for z := t[1] + 2; z < t[2]-1; z += 3 {
			for x := -t[0] + 1.5; x < t[0]-1; x += 2.4 {
				poly(pn, color.NRGBA{0xF2, 0xD7, 0x8A, 255}, project(u+x, v+t[0]*0.6, z), project(u+x+1, v+t[0]*0.6, z), project(u+x+1, v+t[0]*0.6, z+1.6), project(u+x, v+t[0]*0.6, z+1.6))
			}
		}
	}
	top, tip := project(u, v, 78), project(u, v, 96)
	pn.Width(1.2)
	pn.Color(shade(stoneLight, 0.6))
	pn.Line(top.X, top.Y, tip.X, tip.Y)
}

// liberty draws the Statue of Liberty on her pedestal, her torch lit.
func liberty(pn pen, u, v float64) {
	ellipse(pn, u, v, 0, 16, mix(washiPaper, color.NRGBA{0xC9, 0xBE, 0x9C, 255}, 0.4), 1)
	block(pn, u-5, v-4, u+5, v+4, 0, 14, stoneLight)
	b, t := project(u, v, 14), project(u, v, 40)
	pn.Gradient(b.X-4, 0, b.X+4, 0, mix(verdigris, white, 0.25), shade(verdigris, 0.7))
	pn.MoveTo(b.X-3.6, b.Y)
	pn.LineTo(t.X-2, t.Y+4)
	pn.LineTo(t.X+2, t.Y+4)
	pn.LineTo(b.X+3.6, b.Y)
	pn.Close()
	pn.Fill()
	pn.Color(verdigris)
	pn.Circle(t.X, t.Y+1, 2.4)
	pn.Fill()
	arm, torch := project(u+1.5, v, 36), project(u+4, v, 50)
	pn.Width(1.6)
	pn.Line(arm.X, arm.Y, torch.X, torch.Y)
	pn.Glow(torch.X, torch.Y, 7, color.NRGBA{0xFF, 0xC8, 0x6A, 255}, 0.7)
	pn.Color(groupColors[3])
	pn.Circle(torch.X, torch.Y, 1.6)
	pn.Fill()
}

// pearlTower draws the Oriental Pearl Tower: three legs and a column
// threading pink pearls.
func pearlTower(pn pen, u, v float64) {
	shadow(pn, u-10, v-5, 20, 10, 30)
	const h = 100.0
	pn.Width(2)
	pn.Color(shade(steelGrey, 0.75))
	for _, d := range []float64{-7, 7} {
		a, b := project(u+d, v, 0), project(u, v, 40)
		pn.Line(a.X, a.Y, b.X, b.Y)
	}
	a, b := project(u, v, 0), project(u, v, h)
	pn.Width(2.6)
	pn.Line(a.X, a.Y, b.X, b.Y)
	for _, ball := range [][2]float64{{22, 8}, {72, 6}, {94, 3}} {
		c := project(u, v, ball[0])
		r := ball[1] * boardK
		pn.Color(color.NRGBA{0xC9, 0x46, 0x5E, 255})
		pn.Circle(c.X, c.Y, r)
		pn.Fill()
		pn.Gradient(c.X-r, c.Y-r, c.X+r, c.Y+r, color.NRGBA{255, 255, 255, 120}, color.NRGBA{0, 0, 0, 60})
		pn.Circle(c.X, c.Y, r)
		pn.Fill()
	}
	tip := project(u, v, h+20)
	pn.Width(1)
	pn.Color(shade(steelGrey, 0.75))
	pn.Line(b.X, b.Y, tip.X, tip.Y)
}

// shanghaiTower draws the Shanghai Tower: a tapering glass spire, its
// twist drawn up it.
func shanghaiTower(pn pen, u, v float64) {
	shadow(pn, u-10, v-6, 20, 12, 50)
	const h = 120.0
	w := func(z float64) float64 { return 9 - 4*z/h }
	pn.Gradient(project(u-9, v, 0).X, 0, project(u+9, v, 0).X, 0, mix(glassBlue, white, 0.35), shade(glassBlue, 0.7))
	pn.MoveTo(project(u-w(0), v, 0).X, project(u-w(0), v, 0).Y)
	for z := 0.0; z <= h; z += 8 {
		p := project(u-w(z)+math.Sin(z/h*math.Pi)*1.2, v, z)
		pn.LineTo(p.X, p.Y)
	}
	top := project(u+w(h)-1.5, v, h+6)
	pn.LineTo(top.X, top.Y)
	for z := h; z >= 0; z -= 8 {
		p := project(u+w(z)+math.Sin(z/h*math.Pi)*1.2, v, z)
		pn.LineTo(p.X, p.Y)
	}
	pn.Close()
	pn.FillKeep()
	pn.Width(0.6)
	pn.Alpha(washiInk, 0.6)
	pn.Stroke()
	pn.Width(0.5)
	pn.Alpha(white, 0.5)
	for z := 6.0; z < h; z += 12 {
		a, b := project(u-w(z), v, z), project(u+w(z+10), v, z+10)
		pn.Line(a.X, a.Y, b.X, b.Y)
	}
}

// stPeters draws St Peter's: the basilica's front and Michelangelo's dome.
func stPeters(pn pen, u, v float64) {
	shadow(pn, u-20, v-10, 40, 20, 30)
	block(pn, u-20, v-10, u+20, v+8, 0, 14, stoneLight)
	pn.Width(0.5)
	pn.Alpha(washiInk, 0.5)
	for x := -18.0; x <= 18; x += 4.5 {
		a, b := project(u+x, v+8, 1), project(u+x, v+8, 12)
		pn.Line(a.X, a.Y, b.X, b.Y)
	}
	block(pn, u-9, v-8, u+9, v+2, 14, 22, stoneLight)
	c := project(u, v-3, 22)
	rx, ry := 9*boardK, 14*boardK*elevCos
	pn.Gradient(c.X-rx, 0, c.X+rx, 0, color.NRGBA{0xB8, 0xC6, 0xC4, 255}, color.NRGBA{0x6E, 0x80, 0x82, 255})
	pn.Save()
	pn.Translate(c.X, c.Y)
	pn.Scale(rx, ry)
	pn.Arc(0, 0, 1, math.Pi, 2*math.Pi)
	pn.Restore()
	pn.Close()
	pn.FillKeep()
	pn.Width(0.6)
	pn.Alpha(washiInk, 0.6)
	pn.Stroke()
	l := project(u, v-3, 36)
	pn.Color(stoneLight)
	pn.Rect(l.X-1.6, l.Y, 3.2, c.Y-ry-l.Y+1)
	pn.Fill()
	pn.Width(1)
	pn.Color(washiInk)
	pn.Line(l.X, l.Y, l.X, l.Y-6)
}

// pantheon draws the Pantheon: its portico of columns before the rotunda
// and its shallow dome.
func pantheon(pn pen, u, v float64) {
	shadow(pn, u-14, v-12, 28, 24, 20)
	c := project(u, v-4, 14)
	rx, ry := 12*boardK, 5*boardK*elevCos
	ellipse(pn, u, v-4, 0, 12, shade(stoneLight, 0.8), 1)
	a, b := project(u-12, v-4, 0), project(u+12, v-4, 14)
	pn.Gradient(a.X, 0, b.X, 0, stoneLight, shade(stoneLight, 0.75))
	pn.Rect(a.X, b.Y, b.X-a.X, a.Y-b.Y)
	pn.Fill()
	pn.Color(color.NRGBA{0xA8, 0xA2, 0x96, 255})
	pn.Save()
	pn.Translate(c.X, c.Y)
	pn.Scale(rx, ry)
	pn.Arc(0, 0, 1, math.Pi, 2*math.Pi)
	pn.Restore()
	pn.Close()
	pn.Fill()
	// The portico.
	block(pn, u-9, v+2, u+9, v+10, 10, 13, stoneLight)
	poly(pn, mix(stoneLight, white, 0.2), project(u-9, v+10, 13), project(u+9, v+10, 13), project(u, v+10, 17))
	pn.Width(1.2)
	pn.Color(shade(stoneLight, 0.7))
	for x := -8.0; x <= 8; x += 4 {
		p, q := project(u+x, v+10, 0), project(u+x, v+10, 10)
		pn.Line(p.X, p.Y, q.X, q.Y)
	}
}

// paintCityPieces draws a city's buildings in a row, for its card: houses
// as they grow on a street, its hotel, a tree.
func paintCityPieces(pn pen, city int) {
	art := cityArts[city]
	pn.Save()
	pn.RoundRect(0, 0, cityCardW, cityPiecesH, 4)
	pn.Clip()
	pn.Color(washiPaper)
	pn.Paint()
	k := 1.55
	o := project(100, 100, 0)
	pn.Translate(18, 50)
	pn.Scale(k, k)
	pn.Translate(-o.X, -o.Y)
	owners := []color.NRGBA{{0xC2, 0x3B, 0x22, 255}, {0x2B, 0x4C, 0x7E, 255}, {0xC8, 0x8A, 0x1E, 255}}
	for i := range 3 {
		art.house(pn, 100+float64(i)*13, 100, 11, true, owners[i])
	}
	art.hotel(pn, 160, 98, 30, 14, owners[0])
	art.tree(pn, 196, 104, 22, rand.New(rand.NewPCG(1, 2)))
	pn.Restore()
}
