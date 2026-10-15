package gamesui

import (
	"fmt"
	"image/color"
	"math"
	"math/rand/v2"

	"github.com/lyricat/inua/internal/games"
)

// A city's card on the setup page: its board's middle, on washi.
const (
	cityCardW   = 220.0
	cityCardH   = 150.0
	cityPiecesH = 64.0
)

// paintCard lays a city's card, drawn once at the screen's scale.
func (p *fortune) paintCard(pn pen, city int) {
	scale := pn.DeviceScale()
	if math.Abs(scale-p.cardAt) > 0.01 {
		for _, im := range p.cards {
			im.Free()
		}
		p.cards, p.cardAt = map[int]*image{}, scale
	}
	im := p.cards[city]
	if im == nil {
		im = newImage(int(math.Ceil(cityCardW*scale)), int(math.Ceil(cityCardH*scale)))
		im.Draw(func(q pen) {
			q.Scale(scale, scale)
			q.Round()
			paintCityCard(q, city)
		})
		p.cards[city] = im
	}
	pn.Blit(im, 1/scale, false)
}

func paintCityCard(pn pen, city int) {
	pn.Save()
	pn.RoundRect(0, 0, cityCardW, cityCardH, 4)
	pn.Clip()
	pn.Color(color.NRGBA{0xE4, 0xDA, 0xC4, 255})
	pn.Paint()
	// The whole board, its track's shape and its print, in small.
	k := cityCardH / fBoard
	pn.Translate((cityCardW-fBoard*k)/2, 0)
	pn.Scale(k, k)
	paintPrint(pn, city)
	pn.Restore()
	pn.Width(1)
	pn.Alpha(washiInk, 0.25)
	pn.RoundRect(0.5, 0.5, cityCardW-1, cityCardH-1, 4)
	pn.Stroke()
}

// moneyFX is money a player won or lost, shown at their pawn: won, coins
// burst up and the sum rises in gold; lost, ink splashes, bills flutter
// off and the sum sinks in red.
type moneyFX struct {
	player, amount int
	t              float64
	seed           uint64
}

const fxTime = 1.8

// moneyFrom starts the effects of cash changing between two views.
func (p *fortune) moneyFrom(old, now games.FortuneView) {
	for i := range now.Cash {
		if i < len(old.Cash) && now.Cash[i] != old.Cash[i] {
			p.fx = append(p.fx, moneyFX{player: i, amount: now.Cash[i] - old.Cash[i], seed: rand.Uint64()})
		}
	}
}

// stepFX moves the effects on; true when one ended.
func (p *fortune) stepFX(dt float64) bool {
	ended := false
	live := p.fx[:0]
	for _, e := range p.fx {
		e.t += dt
		if e.t < fxTime {
			live = append(live, e)
		} else {
			ended = true
		}
	}
	p.fx = live
	return ended
}

// moneyTone is how a player's cash is changing now: 1 up, -1 down, 0 not.
func (p *fortune) moneyTone(pi int) int {
	for _, e := range p.fx {
		if e.player == pi {
			if e.amount > 0 {
				return 1
			}
			return -1
		}
	}
	return 0
}

// pawnJolt is how a pawn moves with the money it gets: a hop up when it
// wins, a shake when it loses.
func (p *fortune) pawnJolt(pi int) (dx, lift float64) {
	for _, e := range p.fx {
		if e.player != pi {
			continue
		}
		if e.amount > 0 && e.t < 0.4 {
			lift += math.Sin(math.Pi*e.t/0.4) * 7
		}
		if e.amount < 0 && e.t < 0.5 {
			dx += math.Sin(e.t*55) * 1.8 * (1 - e.t/0.5)
		}
	}
	return dx, lift
}

var (
	gold      = color.NRGBA{0xF2, 0xC2, 0x4E, 255}
	goldDeep  = color.NRGBA{0xB0, 0x7A, 0x12, 255}
	billInk   = color.NRGBA{0x6E, 0x8C, 0x6A, 255}
	billPaper = color.NRGBA{0xC9, 0xD9, 0xB8, 255}
)

// paintFX draws the money effects over the board.
func (p *fortune) paintFX(pn pen) {
	stack := map[int]int{} // effects already drawn for a player: their sums rise above each other
	for _, e := range p.fx {
		at, lift := p.token(e.player)
		n := stack[e.player]
		stack[e.player]++
		r := rand.New(rand.NewPCG(e.seed, 3))
		if e.amount > 0 {
			paintGain(pn, at.X, at.Y, lift, e.t, r)
		} else {
			paintLoss(pn, at.X, at.Y, lift, e.t, r)
		}
		paintSum(pn, at.X, at.Y, lift+float64(n)*14, e)
	}
}

// paintGain: a gold pulse on the board, coins thrown up spinning, sparkles.
func paintGain(pn pen, u, v, lift, t float64, r *rand.Rand) {
	if t < 0.9 {
		c := project(u, v, 0)
		k := t / 0.9
		pn.Save()
		pn.Translate(c.X, c.Y)
		pn.Scale(boardK*(8+k*30), boardK*(8+k*30)*elevSin)
		pn.Circle(0, 0, 1)
		pn.Restore()
		pn.Width(2.5 * (1 - k))
		pn.Alpha(gold, 1-k)
		pn.Stroke()
	}
	fade := 1.0
	if t > 1.2 {
		fade = max(0, 1-(t-1.2)/(fxTime-1.2))
	}
	for range 11 {
		a := r.Float64() * 2 * math.Pi
		sp := 10 + r.Float64()*22
		up := 60 + r.Float64()*40
		spin := 8 + r.Float64()*10
		delay := r.Float64() * 0.15
		tt := max(0, t-delay)
		z := lift + 26 + up*tt - 95*tt*tt
		bounce := false
		if z < 0 {
			z, bounce = 0, true
		}
		cu, cv := u+math.Cos(a)*sp*tt, v+math.Sin(a)*sp*tt*0.7
		c := project(cu, cv, z)
		w := math.Abs(math.Cos(tt*spin)) * 3.6 * boardK
		if bounce {
			w = 3.6 * boardK
		}
		h := 3.6 * boardK
		pn.Save()
		pn.Translate(c.X, c.Y)
		pn.Scale(max(w, 0.6), h)
		pn.Circle(0, 0, 1)
		pn.Restore()
		pn.Gradient(c.X-w, c.Y-h, c.X+w, c.Y+h, color.NRGBA{0xFF, 0xE7, 0x9A, uint8(255 * fade)}, color.NRGBA{goldDeep.R, goldDeep.G, goldDeep.B, uint8(255 * fade)})
		pn.FillKeep()
		pn.Width(0.7)
		pn.Alpha(goldDeep, fade)
		pn.Stroke()
	}
	for range 7 {
		a := r.Float64() * 2 * math.Pi
		d := 6 + r.Float64()*18
		ph := r.Float64() * math.Pi
		tw := math.Sin(t*9 + ph)
		if tw < 0 {
			continue
		}
		c := project(u+math.Cos(a)*d, v+math.Sin(a)*d*0.6, lift+18+r.Float64()*24)
		star(pn, c.X, c.Y, 3.2*tw*boardK, gold, fade)
	}
}

// star draws a four-pointed sparkle.
func star(pn pen, x, y, s float64, col color.NRGBA, a float64) {
	pn.MoveTo(x, y-s)
	pn.Curve(x, y, x, y, x+s, y)
	pn.Curve(x, y, x, y, x, y+s)
	pn.Curve(x, y, x, y, x-s, y)
	pn.Curve(x, y, x, y, x, y-s)
	pn.Alpha(white, a)
	pn.FillKeep()
	pn.Width(0.6)
	pn.Alpha(col, a)
	pn.Stroke()
}

// paintLoss: red ink splashed on the board, bills fluttering away.
func paintLoss(pn pen, u, v, lift, t float64, r *rand.Rand) {
	fade := 1.0
	if t > 0.9 {
		fade = max(0, 1-(t-0.9)/(fxTime-0.9))
	}
	grow := 1 - math.Pow(1-min(1, t/0.25), 3)
	pn.Save()
	flat(pn, 0)
	for range 9 {
		a, d := r.Float64()*2*math.Pi, r.Float64()*14*grow
		rr := (2 + r.Float64()*6) * grow
		pn.Alpha(washiShu, 0.38*fade)
		pn.Circle(u+math.Cos(a)*d, v+math.Sin(a)*d, rr)
		pn.Fill()
	}
	for range 6 { // flecks
		a, d := r.Float64()*2*math.Pi, (14+r.Float64()*12)*grow
		pn.Alpha(washiShu, 0.5*fade)
		pn.Circle(u+math.Cos(a)*d, v+math.Sin(a)*d, 0.8+r.Float64()*1.2)
		pn.Fill()
	}
	pn.Restore()
	for range 7 {
		a := r.Float64() * 2 * math.Pi
		sp := 12 + r.Float64()*20
		delay := r.Float64() * 0.2
		tt := max(0, t-delay)
		z := lift + 28 + 28*tt - 34*tt*tt
		if z < 1 {
			z = 1
		}
		c := project(u+math.Cos(a)*sp*tt, v+math.Sin(a)*sp*tt*0.6, z)
		rot := a + tt*(4+r.Float64()*4)
		flutter := math.Cos(tt * (10 + r.Float64()*6))
		pn.Save()
		pn.Translate(c.X, c.Y)
		pn.Rotate(rot)
		pn.Scale(1, 0.35+0.65*math.Abs(flutter))
		bw, bh := 7*boardK, 3.6*boardK
		pn.Rect(-bw/2, -bh/2, bw, bh)
		pn.Alpha(billPaper, fade)
		pn.FillKeep()
		pn.Width(0.6)
		pn.Alpha(billInk, fade)
		pn.Stroke()
		pn.Alpha(billInk, fade)
		pn.Circle(0, 0, bh*0.28)
		pn.Fill()
		pn.Restore()
	}
}

// paintSum draws the money won or lost over the pawn: rising in gold, or
// sinking in red with a shudder.
func paintSum(pn pen, u, v, lift float64, e moneyFX) {
	t := e.t
	a := 1.0
	if t > 1.25 {
		a = max(0, 1-(t-1.25)/(fxTime-1.25))
	}
	pop := 1 + 0.6*max(0, 1-t/0.22)
	text := fmt.Sprintf("+$%s", thousands(e.amount))
	col := color.NRGBA{0xC8, 0x8A, 0x1E, 255}
	z := lift + 44 + 22*(1-math.Pow(1-min(1, t/fxTime), 2))
	dx := 0.0
	if e.amount < 0 {
		text = fmt.Sprintf("−$%s", thousands(-e.amount))
		col = washiShu
		z = lift + 54 - 18*min(1, t/fxTime)
		dx = math.Sin(t*42) * 3 * max(0, 1-t/0.5)
	}
	c := project(u, v, z)
	font := fmt.Sprintf("%s %.1f", minchoBold, 15*pop)
	pn.Text(text, font, c.X+dx+1, c.Y+1.2, 0.5, 0.5, color.NRGBA{0x1F, 0x1B, 0x17, uint8(140 * a)})
	pn.Text(text, font, c.X+dx, c.Y, 0.5, 0.5, color.NRGBA{col.R, col.G, col.B, uint8(255 * a)})
}

// How long a card's effect plays: one got, one played.
const (
	cardGotTime  = 1.1
	cardPlayTime = 1.6
)

// paintCardFX draws the card the board is showing being got or played.
// Got by a rival: it rises off their pawn face down, shines, and settles
// back into the pawn's hand. Got by the player: it rises over their hand
// at the view's foot turning face up, shines, and drops into it. Played: it
// rises to the middle of the view, large, rays bursting behind it, then
// flies to what it was played on, a player's pawn or a square, and
// ripples there; played on nothing, it bursts where it is.
func (p *fortune) paintCardFX(pn pen) {
	e := p.cur
	if e == nil || e.Kind != games.EvCard {
		return
	}
	at, _ := p.token(e.Player)
	from := project(at.X, at.Y, 26)
	ease := func(f float64) float64 { f = max(0, min(1, f)); return f * f * (3 - 2*f) }
	ink := cardInk[e.Card]
	if !e.Played {
		t := p.t
		lift, k0, k1, sink := 70.0, 0.3, 0.85, 0.0
		if e.Player == p.me { // over the hand, clear of it, then down into it
			h := p.cam.half()
			from = games.Vec{X: p.cam.view.X, Y: p.cam.view.Y + h.Y - handRoom - 20}
			lift, k0, k1, sink = 140, 0.6, 1.3, handRoom
		}
		rise := ease(t / 0.35)
		y := from.Y - lift*rise
		k := k0 + (k1-k0)*rise
		turn := math.Pi * (1 - rise) // from its back to its face
		if t > 0.8 {
			f := ease((t - 0.8) / 0.3)
			y = from.Y - lift + (lift+sink)*f
			k = k1 - (k1-k0)*f
		}
		if t > 0.3 && t < 0.9 {
			pn.Glow(from.X, y, 70, washiGold, 0.55*math.Sin((t-0.3)/0.6*math.Pi))
			r := rand.New(rand.NewPCG(uint64(e.Card), 7))
			for range 7 {
				a, d := r.Float64()*2*math.Pi, 40+r.Float64()*40*(t-0.3)
				star(pn, from.X+math.Cos(a)*d, y+math.Sin(a)*d*0.8, 3+r.Float64()*3, washiGold, 1-(t-0.3)/0.6)
			}
		}
		back := e.Back || math.Cos(turn) < 0
		paintCardAt(pn, e.Card, back, from.X, y, k, 0, math.Cos(turn))
		return
	}
	stage := games.Vec{X: p.cam.view.X, Y: p.cam.view.Y - 40}
	to, ripple := stage, false
	switch {
	case e.Target >= 0 && games.Cards[e.Card].Target == games.TargetPlayer:
		a, _ := p.token(e.Target)
		to, ripple = project(a.X, a.Y, 20), true
	case e.Target >= 0 && games.Cards[e.Card].Target == games.TargetSquare:
		c := trackOf(p.city()).tiles[e.Target].center
		to, ripple = project(c.X, c.Y, 0), true
	}
	t := p.t
	switch {
	case t < 0.4: // up to the stage, growing
		f := ease(t / 0.4)
		paintCardAt(pn, e.Card, false, from.X+(stage.X-from.X)*f, from.Y+(stage.Y-from.Y)*f, 0.3+1.3*f, -0.3*(1-f), 1)
	case t < 1.0: // shown, rays behind
		f := (t - 0.4) / 0.6
		pn.Glow(stage.X, stage.Y, 150, ink, 0.45*math.Sin(f*math.Pi))
		pn.Width(5)
		for i := range 16 {
			a := float64(i)*math.Pi/8 + t*0.8
			r0, r1 := 70+30*f, 120+80*f
			pn.Alpha(mix(ink, washiGold, float64(i%2)), 0.5*(1-f))
			pn.Line(stage.X+math.Cos(a)*r0, stage.Y+math.Sin(a)*r0, stage.X+math.Cos(a)*r1, stage.Y+math.Sin(a)*r1)
		}
		paintCardAt(pn, e.Card, false, stage.X, stage.Y, 1.6+0.06*math.Sin(f*math.Pi*2), 0, 1)
	default: // off to the target, or a burst
		f := ease((t - 1.0) / 0.45)
		if !ripple {
			for i := range 12 {
				a := float64(i) * math.Pi / 6
				d := 40 + 120*f
				star(pn, stage.X+math.Cos(a)*d, stage.Y+math.Sin(a)*d, 6*(1-f)+1, mix(ink, washiGold, 0.5), 1-f)
			}
			if f < 1 {
				paintCardAt(pn, e.Card, false, stage.X, stage.Y, 1.6*(1-f)+0.01, 0, 1)
			}
			return
		}
		if f < 1 {
			paintCardAt(pn, e.Card, false, stage.X+(to.X-stage.X)*f, stage.Y+(to.Y-stage.Y)*f, 1.6-1.4*f, f*0.6, 1)
		}
		if g := (t - 1.3) / 0.3; g > 0 {
			for k := range 3 {
				r := (g + float64(k)*0.25) * 60
				pn.Save()
				pn.Translate(to.X, to.Y)
				pn.Scale(1, elevSin)
				pn.Circle(0, 0, r)
				pn.Restore()
				pn.Width(3)
				pn.Alpha(ink, max(0, 0.8-g*0.6-float64(k)*0.2))
				pn.Stroke()
			}
		}
	}
}
