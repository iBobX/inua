package gamesui

import (
	"image/color"
	"math"

	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/i18n"
)

// Small Fortune's cards as printed: washi with a band of the card's own
// pigment, its picture in ink and colour, its name below; the back
// indigo, with waves and the seal. paintCard draws one in units of its
// own, cardW × cardH from 0, 0, for the panel and for the board's effects.

const (
	cardW = 70.0
	cardH = 100.0
)

// cardInk is each card's pigment, for its band and its picture.
var cardInk = map[games.CardKind]color.NRGBA{
	games.CardDice:        {0xBF, 0x44, 0x30, 255}, // shu
	games.CardFreeRent:    {0x5E, 0x84, 0x3E, 255}, // matcha
	games.CardRequisition: {0x8E, 0x35, 0x4A, 255}, // suou
	games.CardRob:         {0x4E, 0x5D, 0x6C, 255}, // aonibi
	games.CardDemolish:    {0x8A, 0x5A, 0x3A, 255}, // kogecha
	games.CardPermit:      {0xD4, 0x9B, 0x39, 255}, // yamabuki
	games.CardTurtle:      {0x2F, 0x6C, 0x68, 255}, // seiji
	games.CardSwap:        {0x2D, 0x5D, 0x9F, 255}, // ruri
	games.CardJailPass:    {0x7D, 0x7A, 0x3A, 255}, // uguisu
	games.CardAudit:       {0x5C, 0x58, 0x8C, 255}, // fuji
	games.CardEqual:       {0x4A, 0x9B, 0xA8, 255}, // asagi
	games.CardSleep:       {0x2E, 0x3A, 0x5C, 255}, // kon
}

var washiGold = color.NRGBA{0xD9, 0xA4, 0x3A, 255}

// paintCard draws card c, its back if back; with a price, a tag for it.
func paintCard(pn pen, c games.CardKind, back bool, price int) {
	// Its shadow, the paper, an inked edge.
	for k := 3; k >= 1; k-- {
		g := float64(k) * 0.9
		pn.RoundRect(-g+1, -g+2, cardW+2*g, cardH+2*g, 5+g)
		pn.Alpha(washiInk, 0.07)
		pn.Fill()
	}
	if back {
		paintCardBack(pn)
		return
	}
	ink := cardInk[c]
	pn.RoundRect(0, 0, cardW, cardH, 5)
	pn.Color(washiIvory)
	pn.FillKeep()
	pn.Save()
	pn.Clip()
	pn.Color(ink)
	pn.Rect(0, 0, cardW, 7)
	pn.Fill()
	// A wash behind the picture.
	pn.Alpha(ink, 0.1)
	pn.Circle(cardW/2, 40, 25)
	pn.Fill()
	pn.Restore()
	pn.RoundRect(0.5, 0.5, cardW-1, cardH-1, 5)
	pn.Width(1)
	pn.Alpha(washiInk, 0.55)
	pn.Stroke()
	pn.RoundRect(3.5, 10, cardW-7, cardH-13.5, 3)
	pn.Width(0.6)
	pn.Alpha(ink, 0.45)
	pn.Stroke()

	pn.Save()
	pn.Translate(cardW/2, 40)
	cardPicture(pn, c, ink)
	pn.Restore()

	pn.Width(0.7)
	pn.Alpha(ink, 0.6)
	pn.Line(16, 68, cardW-16, 68)
	// The name, on one line or two, in the middle of what is left.
	name, font := i18n.T(games.Cards[c].Name), minchoBold+" 7.2"
	h := pn.TextBox(name, font, cardW/2, 0, cardW-8, color.NRGBA{})
	pn.TextBox(name, font, cardW/2, 70+(cardH-4-70-h)/2, cardW-8, washiInk)
	if price > 0 {
		pn.Color(washiShu)
		pn.RoundRect(cardW-31, 12, 27, 13, 6.5)
		pn.Fill()
		pn.Text(fmtPrice(price), minchoBold+" 7", cardW-17.5, 18.6, 0.5, 0.5, washiIvory)
	}
}

func fmtPrice(n int) string { return "$" + thousands(n) }

// paintCardBack draws a card's back: indigo, waves, the seal.
func paintCardBack(pn pen) {
	pn.RoundRect(0, 0, cardW, cardH, 5)
	pn.Color(washiIndigo)
	pn.FillKeep()
	pn.Save()
	pn.Clip()
	pn.Width(0.8)
	pn.Alpha(washiPaper, 0.22)
	for row := 0; row < 9; row++ {
		for col := -1; col < 6; col++ {
			x := float64(col)*16 + float64(row%2)*8
			y := float64(row) * 12
			for r := 7.0; r > 1; r -= 2.4 {
				pn.Arc(x, y+8, r, math.Pi, 2*math.Pi)
				pn.Stroke()
			}
		}
	}
	pn.Restore()
	pn.RoundRect(4, 4, cardW-8, cardH-8, 3)
	pn.Width(1)
	pn.Alpha(washiGold, 0.8)
	pn.Stroke()
	pn.Color(washiShu)
	pn.RoundRect(cardW/2-13, cardH/2-13, 26, 26, 3)
	pn.Fill()
	pn.Text("福", minchoBold+" 15", cardW/2, cardH/2, 0.5, 0.5, washiIvory)
}

// cardPicture draws card c's picture about 0, 0, some 46 units across.
func cardPicture(pn pen, c games.CardKind, ink color.NRGBA) {
	stroke := func(w float64) {
		pn.Width(w)
		pn.Color(washiInk)
		pn.Stroke()
	}
	fill := func(col color.NRGBA) {
		pn.Color(col)
		pn.FillKeep()
		stroke(1.1)
	}
	house := func(x, y, s float64, col color.NRGBA) {
		pn.MoveTo(x-s, y)
		pn.LineTo(x-s, y-s*1.1)
		pn.LineTo(x, y-s*2)
		pn.LineTo(x+s, y-s*1.1)
		pn.LineTo(x+s, y)
		pn.Close()
		fill(col)
		pn.Rect(x-s*0.3, y-s*0.75, s*0.6, s*0.75)
		pn.Color(washiInk)
		pn.Fill()
	}
	coin := func(x, y, r float64) {
		pn.Circle(x, y, r)
		fill(washiGold)
		pn.Rect(x-r*0.25, y-r*0.25, r*0.5, r*0.5)
		pn.Color(washiInk)
		pn.Fill()
	}
	switch c {
	case games.CardDice:
		for i, d := range [][3]float64{{-9, 4, -0.25}, {9, -4, 0.3}} {
			pn.Save()
			pn.Translate(d[0], d[1])
			pn.Rotate(d[2])
			pn.RoundRect(-10, -10, 20, 20, 3.5)
			fill(washiIvory)
			for _, pip := range pips[[]int{6, 5}[i]] {
				pn.Circle(pip[0]*5.2, pip[1]*5.2, 1.7)
			}
			pn.Color(ink)
			pn.Fill()
			pn.Restore()
		}
	case games.CardFreeRent:
		house(-4, 14, 13, washiTile)
		coin(11, 6, 8)
		pn.Width(2.4)
		pn.Color(washiShu)
		pn.Line(3, 14, 19, -2)
	case games.CardRequisition:
		house(-5, 15, 13, washiTile)
		pn.Save()
		pn.Translate(10, -2)
		pn.Rotate(0.2)
		pn.RoundRect(-9, -9, 18, 18, 2)
		pn.Color(washiShu)
		pn.Fill()
		pn.Text("収", minchoBold+" 11", 0, 0, 0.5, 0.5, washiIvory)
		pn.Restore()
	case games.CardRob:
		pn.Save()
		pn.Rotate(-0.25)
		pn.RoundRect(-14, -8, 16, 22, 2)
		fill(washiIvory)
		pn.Restore()
		pn.MoveTo(16, -18) // a hook on a line
		pn.LineTo(16, 0)
		pn.ArcBack(10, 0, 6, 0, math.Pi*0.9)
		stroke(1.8)
		pn.Color(ink)
		pn.Circle(4.5, -1, 1.6)
		pn.Fill()
	case games.CardDemolish:
		pn.MoveTo(-16, -18)
		pn.LineTo(4, -2)
		stroke(1.3)
		pn.Circle(6, 0, 7)
		fill(shade(washiStone, 0.6))
		pn.MoveTo(2, 18) // a house broken in two
		pn.LineTo(2, 8)
		pn.LineTo(9, 4)
		pn.LineTo(12, 9)
		pn.LineTo(18, 7)
		pn.LineTo(18, 18)
		pn.Close()
		fill(washiTile)
		for _, b := range [][2]float64{{-6, 16}, {-11, 14}, {-3, 12}} {
			pn.Rect(b[0], b[1], 3, 2.4)
			pn.Color(ink)
			pn.Fill()
		}
	case games.CardPermit:
		pn.MoveTo(-14, 20) // a crane, lifting a house
		pn.LineTo(-14, -18)
		pn.LineTo(16, -18)
		pn.MoveTo(-14, -10)
		pn.LineTo(-6, -18)
		stroke(1.6)
		pn.MoveTo(10, -18)
		pn.LineTo(10, -4)
		stroke(0.8)
		house(10, 12, 8, ink)
	case games.CardTurtle:
		pn.Save()
		pn.Scale(1, 0.7)
		pn.Circle(-15, 4, 5) // the head
		fill(mix(ink, white, 0.4))
		for _, l := range [][2]float64{{-9, 10}, {9, 10}} {
			pn.Circle(l[0], l[1], 4)
			fill(mix(ink, white, 0.4))
		}
		pn.Arc(0, 4, 14, math.Pi, 2*math.Pi)
		pn.Close()
		fill(ink)
		pn.Restore()
		pn.Width(0.8)
		pn.Alpha(washiIvory, 0.8)
		pn.Line(-7, -3, 7, -3)
		pn.Line(-3.5, -8, -3.5, 2.5)
		pn.Line(3.5, -8, 3.5, 2.5)
	case games.CardSwap:
		for _, s := range []float64{1, -1} {
			pn.Save()
			pn.Scale(s, s)
			pn.Arc(0, 0, 15, math.Pi*1.1, math.Pi*1.85)
			pn.Width(3)
			pn.Color(ink)
			pn.Stroke()
			a := games.Vec{X: 15 * math.Cos(math.Pi*1.85), Y: 15 * math.Sin(math.Pi*1.85)}
			pn.MoveTo(a.X+5, a.Y-1)
			pn.LineTo(a.X-2, a.Y-5)
			pn.LineTo(a.X+1, a.Y+5)
			pn.Close()
			pn.Fill()
			pn.Restore()
		}
		pn.Circle(-8, 8, 4)
		fill(washiShu)
		pn.Circle(8, -8, 4)
		fill(washiIndigo)
	case games.CardJailPass:
		pn.Circle(-9, 0, 8) // a key
		fill(washiGold)
		pn.Circle(-9, 0, 3)
		pn.Color(washiIvory)
		pn.Fill()
		pn.Rect(-1, -2, 20, 4)
		fill(washiGold)
		pn.Rect(12, 2, 3, 5)
		fill(washiGold)
		pn.Rect(16, 2, 3, 4)
		fill(washiGold)
	case games.CardAudit:
		coin(-6, 6, 9)
		pn.Circle(4, -4, 10) // a magnifying glass
		pn.Alpha(white, 0.5)
		pn.FillKeep()
		stroke(2)
		pn.Width(3.4)
		pn.Color(ink)
		pn.Line(11, 3, 19, 11)
	case games.CardEqual:
		pn.MoveTo(0, -18) // a balance
		pn.LineTo(0, 16)
		pn.MoveTo(-8, 16)
		pn.LineTo(8, 16)
		pn.MoveTo(-17, -12)
		pn.LineTo(17, -12)
		stroke(1.6)
		for _, x := range []float64{-14, 14} {
			pn.MoveTo(x, -12)
			pn.LineTo(x-6, 0)
			pn.MoveTo(x, -12)
			pn.LineTo(x+6, 0)
			stroke(0.6)
			pn.Arc(x, 0, 6.5, 0, math.Pi)
			pn.Close()
			fill(washiGold)
		}
	case games.CardSleep:
		pn.Arc(-4, 2, 14, math.Pi*0.35, math.Pi*1.65) // a crescent moon
		pn.ArcBack(2, -2, 11, math.Pi*1.5, math.Pi*0.5)
		pn.Close()
		fill(washiGold)
		for i, z := range [][2]float64{{9, -6}, {15, -13}, {19, -19}} {
			pn.Text("z", minchoBold+" "+[]string{"9", "7", "5.5"}[i], z[0], z[1], 0.5, 0.5, ink)
		}
	}
}

// paintCardAt draws card c centred at x, y, k times its size, turned by a
// and squeezed across by flip (1 flat; 0 edge on).
func paintCardAt(pn pen, c games.CardKind, back bool, x, y, k, a, flip float64) {
	pn.Save()
	pn.Translate(x, y)
	pn.Rotate(a)
	pn.Scale(k*max(0.02, math.Abs(flip)), k)
	pn.Translate(-cardW/2, -cardH/2)
	paintCard(pn, c, back, 0)
	pn.Restore()
}
