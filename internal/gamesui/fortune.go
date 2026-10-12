package gamesui

import (
	"fmt"
	"html"
	"image/color"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/gtkx"
)

// fortune is Small Fortune: the board on a canvas, where the dice roll
// and the tokens walk, and a panel with the players, the square in
// focus, what can be done, and what happened.
type fortune struct {
	f      *frame
	g      *games.Fortune
	cv     *canvas
	rivals int

	// The board shows events one after another: queue waits, cur plays.
	queue   []games.FortuneEvent
	cur     *games.FortuneEvent
	t, wait float64
	shown   games.FortuneView
	dice    [2]int
	log     []logLine

	selected, hover int

	round, pot                      *gtk.Label
	players, logBox, cardButtons    gtk.Box
	rows                            []*gtk.Box
	names, cashes                   []*gtk.Label
	cardTitle, cardBody             *gtk.Label
	primary, secondary, build, sell *gtk.Button
	rivalButtons                    []*gtk.Button

	onPrimary, onSecondary, onBuild, onSell, onRivals func(gtk.Button)
	onClick                                           func(gtk.GestureClick, int32, float64, float64)
	onMotion                                          func(gtk.EventControllerMotion, float64, float64)
	onLeave                                           func(gtk.EventControllerMotion)
}

type logLine struct {
	text      string
	good, bad bool
}

// The board: 700 units square, corners of 96, five squares between.
const (
	fBoard  = 700.0
	fCorner = 96.0
	fEdge   = (fBoard - 2*fCorner) / 5
	fBand   = 15.0
)

// The streets' pairs, in calm versions of their usual colours.
var groupColors = [8]color.NRGBA{
	{0xB0, 0x7A, 0x5A, 255}, {0x7F, 0xA6, 0xC9, 255}, {0xC9, 0x8F, 0xB0, 255}, {0xD9, 0xA4, 0x41, 255},
	{0x8D, 0xB8, 0x6B, 255}, {0x4E, 0x9C, 0x9A, 255}, {0x8C, 0x7F, 0xC9, 255}, {0xD9, 0x6A, 0x55, 255},
}

func (p *fortune) playerColor(i int) color.NRGBA {
	c := p.f.col
	return [...]color.NRGBA{c.accent, c.warn, c.danger, {0x9B, 0x8C, 0xE0, 255}}[i]
}

func (p *fortune) setup(f *frame) {
	p.f, p.rivals, p.selected, p.hover = f, 2, -1, -1
	f.win.SetDefaultSize(1080, 820)
	p.round = f.stat("ROUND")
	p.pot = f.stat("FOUNTAIN")

	row := gtk.NewBox(gtk.OrientationHorizontalValue, 18)
	p.cv = newCanvas(fBoard, fBoard, p.paint)
	gtkx.Append(row, &p.cv.area.Widget)
	panel := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	panel.AddCssClass("fortune-panel")
	panel.SetSizeRequest(300, -1)
	panel.SetHexpand(false)
	gtkx.Append(row, &panel.Widget)
	gtkx.Append(&f.body, &row.Widget)

	section := func(name string) {
		k := gtk.NewLabel(name)
		k.AddCssClass("kicker")
		k.AddCssClass("fortune-kicker")
		k.SetXalign(0)
		gtkx.Append(panel, &k.Widget)
	}
	section("PLAYERS")
	p.players = *gtk.NewBox(gtk.OrientationVerticalValue, 0)
	gtkx.Append(panel, &p.players.Widget)

	section("SQUARE")
	p.cardTitle = gtk.NewLabel("")
	p.cardTitle.AddCssClass("fortune-card-title")
	p.cardTitle.SetXalign(0)
	gtkx.Append(panel, &p.cardTitle.Widget)
	p.cardBody = gtk.NewLabel("")
	p.cardBody.AddCssClass("fortune-card")
	p.cardBody.SetXalign(0)
	p.cardBody.SetWrap(true)
	p.cardBody.SetMaxWidthChars(36)
	gtkx.Append(panel, &p.cardBody.Widget)
	p.cardButtons = *gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	p.cardButtons.SetHomogeneous(true)
	p.cardButtons.AddCssClass("fortune-buttons")
	p.onBuild = func(gtk.Button) { p.act(func() bool { return p.g.Build(p.focus()) }) }
	p.onSell = func(gtk.Button) { p.act(func() bool { return p.g.Sell(p.focus()) }) }
	p.build = button("Build a house", &p.onBuild)
	p.sell = button("Sell a house", &p.onSell)
	gtkx.Append(&p.cardButtons, &p.build.Widget)
	gtkx.Append(&p.cardButtons, &p.sell.Widget)
	gtkx.Append(panel, &p.cardButtons.Widget)

	section("YOUR TURN")
	acts := gtk.NewBox(gtk.OrientationVerticalValue, 8)
	p.onPrimary = func(gtk.Button) { p.main() }
	p.onSecondary = func(gtk.Button) { p.other() }
	p.primary = button("Roll the dice", &p.onPrimary)
	p.primary.RemoveCssClass("outlined")
	p.primary.AddCssClass("primary")
	p.primary.AddCssClass("fortune-main")
	p.secondary = button("", &p.onSecondary)
	gtkx.Append(acts, &p.primary.Widget)
	gtkx.Append(acts, &p.secondary.Widget)
	gtkx.Append(panel, &acts.Widget)

	section("WHAT HAPPENED")
	p.logBox = *gtk.NewBox(gtk.OrientationVerticalValue, 4)
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	scroll.SetVexpand(true)
	scroll.SetChild(&p.logBox.Widget)
	p.logBox.Unref()
	gtkx.Append(panel, &scroll.Widget)

	rivals := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
	rivals.AddCssClass("fortune-rivals")
	l := gtk.NewLabel("New game with")
	l.AddCssClass("dim")
	l.SetHexpand(true)
	l.SetXalign(0)
	gtkx.Append(rivals, &l.Widget)
	p.onRivals = func(b gtk.Button) {
		p.rivals = int(b.GetName()[0] - '0')
		p.reset()
	}
	for n := 1; n <= 3; n++ {
		b := button(fmt.Sprint(n), &p.onRivals)
		b.SetName(fmt.Sprint(n))
		b.AddCssClass("fortune-rival")
		b.SetTooltipText(fmt.Sprintf("Play against %d computer players", n))
		gtkx.Append(rivals, &b.Widget)
		p.rivalButtons = append(p.rivalButtons, b)
	}
	r := gtk.NewLabel("rivals")
	r.AddCssClass("dim")
	gtkx.Append(rivals, &r.Widget)
	gtkx.Append(panel, &rivals.Widget)

	f.keyDown = p.key
	p.onClick = func(_ gtk.GestureClick, _ int32, x, y float64) {
		if sq := squareAt(p.cv.at(x, y)); sq >= 0 {
			p.selected = sq
			p.refresh()
		}
	}
	click := gtk.NewGestureClick()
	click.ConnectPressed(&p.onClick)
	p.cv.area.AddController(&click.EventController)
	p.onMotion = func(_ gtk.EventControllerMotion, x, y float64) {
		if sq := squareAt(p.cv.at(x, y)); sq != p.hover {
			p.hover = sq
			p.cv.redraw()
		}
	}
	p.onLeave = func(gtk.EventControllerMotion) {
		p.hover = -1
		p.cv.redraw()
	}
	motion := gtk.NewEventControllerMotion()
	motion.ConnectMotion(&p.onMotion)
	motion.ConnectLeave(&p.onLeave)
	p.cv.area.AddController(&motion.EventController)
}

func (p *fortune) reset() {
	p.g = games.NewFortune(p.rivals, uint64(time.Now().UnixNano()))
	p.queue, p.cur, p.t, p.wait = nil, nil, 0, 0
	p.shown = p.g.View()
	p.dice = [2]int{}
	p.log = nil
	p.selected = -1
	gtkx.Clear(&p.players)
	p.rows, p.names, p.cashes = nil, nil, nil
	for range p.g.Players {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		row.AddCssClass("fortune-player")
		name := gtk.NewLabel("")
		name.SetHexpand(true)
		name.SetXalign(0)
		gtkx.Append(row, &name.Widget)
		cash := gtk.NewLabel("")
		cash.AddCssClass("fortune-cash")
		cash.SetValign(gtk.AlignStartValue)
		gtkx.Append(row, &cash.Widget)
		gtkx.Append(&p.players, &row.Widget)
		p.rows, p.names, p.cashes = append(p.rows, row), append(p.names, name), append(p.cashes, cash)
	}
	for i, b := range p.rivalButtons {
		if i+1 == p.rivals {
			b.AddCssClass("on")
		} else {
			b.RemoveCssClass("on")
		}
	}
	p.refresh()
	p.cv.redraw()
}

func (p *fortune) redraw() {
	p.refresh()
	p.cv.redraw()
}

// busy: the board is still showing what happened, or a computer player
// is about to play.
func (p *fortune) busy() bool {
	return p.cur != nil || len(p.queue) > 0 || p.g.Phase == games.PhaseCPU
}

// focus is the square the panel shows: the one picked, else the one
// the player stands on.
func (p *fortune) focus() int {
	if p.selected >= 0 {
		return p.selected
	}
	return p.shown.At[0]
}

// act does a move of the player's and plays out what follows.
func (p *fortune) act(do func() bool) {
	if p.busy() || !do() {
		return
	}
	p.queue = append(p.queue, p.g.Take()...)
	p.play()
}

func (p *fortune) main() {
	switch p.g.Phase {
	case games.PhaseRoll:
		p.selected = -1
		p.act(p.g.Roll)
	case games.PhaseBuy:
		p.act(p.g.Buy)
	case games.PhaseEnd:
		p.act(p.g.EndTurn)
	case games.PhaseOver:
		p.reset()
	}
}

func (p *fortune) other() {
	switch p.g.Phase {
	case games.PhaseRoll:
		p.act(p.g.PayBail)
	case games.PhaseBuy:
		p.act(p.g.Pass)
	}
}

func (p *fortune) key(k uint32) bool {
	switch k {
	case ' ', keyEnter:
		p.main()
	case 'r':
		if p.g.Phase == games.PhaseRoll {
			p.main()
		}
	case 'b':
		if p.g.Phase == games.PhaseBuy {
			p.main()
		}
	case 'e':
		if p.g.Phase == games.PhaseEnd {
			p.main()
		}
	case 'x', 'j':
		p.other()
	case 'h':
		p.act(func() bool { return p.g.Build(p.focus()) })
	default:
		return false
	}
	return true
}

// How long each kind of event shows, in seconds.
const (
	hopTime  = 0.14
	jumpTime = 0.45
	diceTime = 0.55
)

func (p *fortune) play() {
	p.refresh()
	p.cv.animate(p.step)
}

func (p *fortune) step(dt float64) bool {
	if p.cur == nil {
		switch {
		case len(p.queue) > 0:
			p.cur, p.queue, p.t = &p.queue[0], p.queue[1:], 0
			if p.cur.Kind == games.EvNote {
				p.log = append([]logLine{{p.cur.Text, p.cur.Good, p.cur.Bad}}, p.log...)
				if len(p.log) > 7 {
					p.log = p.log[:7]
				}
				p.refresh()
			}
		case p.g.Phase == games.PhaseCPU:
			if p.wait += dt; p.wait > 0.5 {
				p.wait = 0
				p.g.PlayCPU()
				p.queue = append(p.queue, p.g.Take()...)
			}
			return true
		default:
			p.shown = p.g.View()
			p.refresh()
			return false
		}
	}
	p.t += dt
	e := p.cur
	var length float64
	switch e.Kind {
	case games.EvDice:
		length = diceTime
		if p.t < diceTime-0.1 {
			p.dice = [2]int{rand.IntN(6) + 1, rand.IntN(6) + 1}
		} else {
			p.dice = e.Dice
		}
	case games.EvWalk:
		length = hopTime*float64(e.Steps) + 0.08
	case games.EvJump:
		length = jumpTime
	case games.EvNote:
		length = 0.3
		if e.Player != 0 || p.g.Players[p.g.Turn].CPU {
			length = 0.75
		}
	}
	if p.t >= length {
		p.shown = e.View
		p.cur = nil
		p.refresh()
	}
	return true
}

// squareRect is a square's place on the board, and the side of it that
// faces the middle: 0 up, 1 right, 2 down, 3 left; -1 for a corner.
func squareRect(i int) (x, y, w, h float64, side int) {
	switch {
	case i == 0:
		return fBoard - fCorner, fBoard - fCorner, fCorner, fCorner, -1
	case i < 6:
		return fBoard - fCorner - float64(i)*fEdge, fBoard - fCorner, fEdge, fCorner, 0
	case i == 6:
		return 0, fBoard - fCorner, fCorner, fCorner, -1
	case i < 12:
		return 0, fBoard - fCorner - float64(i-6)*fEdge, fCorner, fEdge, 1
	case i == 12:
		return 0, 0, fCorner, fCorner, -1
	case i < 18:
		return fCorner + float64(i-13)*fEdge, 0, fEdge, fCorner, 2
	case i == 18:
		return fBoard - fCorner, 0, fCorner, fCorner, -1
	default:
		return fBoard - fCorner, fCorner + float64(i-19)*fEdge, fCorner, fEdge, 3
	}
}

func squareAt(v games.Vec) int {
	for i := range games.Board {
		x, y, w, h, _ := squareRect(i)
		if v.X >= x && v.X < x+w && v.Y >= y && v.Y < y+h {
			return i
		}
	}
	return -1
}

// tokenAt is where player pi's token stands on square sq: each player
// has a place of its own there. In jail, it is behind the bars.
func tokenAt(sq, pi int, jailed bool) games.Vec {
	x, y, w, h, side := squareRect(sq)
	off := [4]games.Vec{{X: -12, Y: -12}, {X: 12, Y: -12}, {X: -12, Y: 12}, {X: 12, Y: 12}}[pi]
	if side < 0 {
		if jailed {
			return games.Vec{X: x + w - 23 + off.X*0.7, Y: y + 23 + off.Y*0.7}
		}
		return games.Vec{X: x + w/2 + off.X, Y: y + 64 + off.Y}
	}
	cx, cy := x+w/2, y+h/2
	switch side { // clear of the colour band
	case 0:
		cy += fBand / 2
	case 1:
		cx -= fBand / 2
	case 2:
		cy -= fBand / 2
	case 3:
		cx += fBand / 2
	}
	return games.Vec{X: cx + (float64(pi)-1.5)*19, Y: cy + 4}
}

// token is where a player's token is drawn now, and how high it hops.
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
			return tokenAt(a, pi, false).Lerp(tokenAt(b, pi, false), frac), math.Sin(frac*math.Pi) * 10
		case games.EvJump:
			frac := min(1, p.t/jumpTime)
			frac = frac * frac * (3 - 2*frac)
			return tokenAt(e.From, pi, false).Lerp(tokenAt(e.To, pi, e.View.InJail[pi]), frac), math.Sin(frac*math.Pi) * 40
		}
	}
	return tokenAt(p.shown.At[pi], pi, p.shown.InJail[pi]), 0
}

func (p *fortune) paint(pn pen) {
	c := p.f.col
	v := p.shown
	// The board's middle.
	pn.Color(c.surface)
	pn.Rect(0, 0, fBoard, fBoard)
	pn.Fill()

	for i, sq := range games.Board {
		x, y, w, h, side := squareRect(i)
		bg := c.surface
		if o := v.Owner[i]; o >= 0 {
			bg = mix(c.surface, p.playerColor(o), 0.13)
		}
		if i == p.hover || i == p.focus() && p.selected >= 0 {
			bg = mix(bg, c.t0, 0.06)
		}
		pn.Color(bg)
		pn.Rect(x, y, w, h)
		pn.Fill()

		// Inside the colour band.
		ix, iy, iw, ih := x, y, w, h
		if sq.Kind == games.Street {
			bx, by, bw, bh := x, y, w, h
			switch side {
			case 0:
				bh = fBand
				iy, ih = y+fBand, h-fBand
			case 1:
				bx, bw = x+w-fBand, fBand
				iw = w - fBand
			case 2:
				by, bh = y+h-fBand, fBand
				ih = h - fBand
			case 3:
				bw = fBand
				ix, iw = x+fBand, w-fBand
			}
			pn.Color(mix(c.surface, groupColors[sq.Group], 0.85))
			pn.Rect(bx, by, bw, bh)
			pn.Fill()
			for n := range v.Houses[i] {
				hx, hy := bx+bw/2, by+bh/2
				d := (float64(n) - float64(v.Houses[i]-1)/2) * 14
				if side == 0 || side == 2 {
					hx += d
				} else {
					hy += d
				}
				pn.Color(c.app)
				pn.RoundRect(hx-5, hy-5, 10, 10, 2)
				pn.Fill()
				pn.Color(c.t0)
				pn.RoundRect(hx-3.5, hy-3.5, 7, 7, 1.5)
				pn.Fill()
			}
		}
		cx := ix + iw/2
		switch sq.Kind {
		case games.Street:
			pn.TextBox(sq.Name, "IBM Plex Sans Medium 9", cx, iy+6, iw-8, c.t0)
			if o := v.Owner[i]; o >= 0 {
				pn.Color(p.playerColor(o))
				pn.Circle(cx-24, iy+ih-12, 3.5)
				pn.Fill()
				pn.Text(fmt.Sprintf("rent $%d", rentOf(v, i)), "Geist Mono, IBM Plex Mono 8.5", cx+4, iy+ih-6, 0.5, 1, c.t1)
			} else {
				pn.Text(fmt.Sprintf("$%d", sq.Price), "Geist Mono, IBM Plex Mono 9", cx, iy+ih-6, 0.5, 1, c.t2)
			}
		case games.ChanceSquare:
			pn.Text("?", "IBM Plex Sans Bold 24", cx, iy+24, 0.5, 0.5, c.accent)
			pn.Text("Chance", "IBM Plex Sans 9", cx, iy+ih-6, 0.5, 1, c.t1)
		case games.TaxSquare:
			pn.TextBox(sq.Name, "IBM Plex Sans Medium 9.5", cx, iy+10, iw-10, c.t0)
			pn.Text(fmt.Sprintf("pay $%d", sq.Price), "Geist Mono, IBM Plex Mono 9", cx, iy+ih-6, 0.5, 1, c.danger)
		case games.StartSquare:
			pn.Text("← START", "IBM Plex Sans Bold 13", cx, y+18, 0.5, 0.5, c.ok)
			pn.Text("collect $200", "IBM Plex Sans 8.5", cx, y+34, 0.5, 0.5, c.t1)
		case games.JailSquare:
			pn.Color(mix(c.surface, c.t2, 0.25))
			pn.Rect(x+w-46, y, 46, 46)
			pn.Fill()
			pn.Width(2)
			pn.Color(c.t2)
			for b := range 4 {
				bx := x + w - 40 + float64(b)*11
				pn.Line(bx, y+4, bx, y+42)
			}
			pn.Text("JAIL", "IBM Plex Sans Bold 10", x+8, y+10, 0, 0, c.t1)
			pn.Text("just visiting", "IBM Plex Sans 8", x+w/2, y+h-4, 0.5, 1, c.t2)
		case games.Fountain:
			pn.Text("FOUNTAIN", "IBM Plex Sans Bold 9.5", cx, y+16, 0.5, 0.5, c.t0)
			pn.Text(fmt.Sprintf("$%d", v.Pot), "Geist Mono, IBM Plex Mono Bold 10", cx, y+31, 0.5, 0.5, c.accent)
			pn.Width(1.5)
			pn.Alpha(c.accent, 0.5)
			pn.Arc(cx, y+64, 26, 0.15*math.Pi, 0.85*math.Pi)
			pn.Stroke()
		case games.GoToJail:
			pn.Text("GO TO", "IBM Plex Sans Bold 11", cx, y+16, 0.5, 0.5, c.danger)
			pn.Text("JAIL ↙", "IBM Plex Sans Bold 11", cx, y+32, 0.5, 0.5, c.danger)
		}
		pn.Width(1)
		pn.Color(c.line)
		pn.Rect(x+0.5, y+0.5, w-1, h-1)
		pn.Stroke()
	}
	if f := p.focus(); p.selected >= 0 {
		x, y, w, h, _ := squareRect(f)
		pn.Width(2.5)
		pn.Color(c.accent)
		pn.Rect(x+1.5, y+1.5, w-3, h-3)
		pn.Stroke()
	}

	p.paintMiddle(pn)

	// Tokens, the one whose turn it is on top.
	order := []int{}
	for i := range p.g.Players {
		if i != p.g.Turn {
			order = append(order, i)
		}
	}
	order = append(order, p.g.Turn)
	for _, i := range order {
		if p.g.Players[i].Out && (p.cur == nil || p.cur.Player != i) {
			continue
		}
		at, lift := p.token(i)
		pn.Alpha(c.app, 0.35)
		pn.Circle(at.X+1.5, at.Y+2.5, 9)
		pn.Fill()
		at.Y -= lift
		if i == p.g.Turn && p.g.Phase != games.PhaseOver {
			pn.Color(c.t0)
			pn.Circle(at.X, at.Y, 11)
			pn.Fill()
		}
		pn.Color(p.playerColor(i))
		pn.Circle(at.X, at.Y, 9)
		pn.Fill()
		pn.Text(p.g.Players[i].Name[:1], "IBM Plex Sans Bold 8.5", at.X, at.Y, 0.5, 0.5, c.app)
	}
}

// paintMiddle draws the board's middle: the name, the dice, and the
// latest news; at the end, the standings.
func (p *fortune) paintMiddle(pn pen) {
	c := p.f.col
	m := fBoard / 2
	pn.Text("S M A L L   F O R T U N E", "IBM Plex Sans Bold 15", m, 150, 0.5, 0.5, mix(c.surface, c.t2, 0.7))

	if p.g.Phase == games.PhaseOver && !p.busy() {
		w := p.g.Winner
		title, tone := fmt.Sprintf("%s won.", p.g.Players[w].Name), c.danger
		if w == 0 {
			title, tone = "You won!", c.ok
		}
		pn.Text(title, "IBM Plex Sans Bold 30", m, 230, 0.5, 0.5, tone)
		idx := []int{}
		for i := range p.g.Players {
			idx = append(idx, i)
		}
		slices.SortFunc(idx, func(a, b int) int { return p.g.Worth(b) - p.g.Worth(a) })
		for n, i := range idx {
			y := 290 + float64(n)*34
			pn.Color(p.playerColor(i))
			pn.Circle(m-120, y, 7)
			pn.Fill()
			name := p.g.Players[i].Name
			if p.g.Players[i].Out {
				name += " (bankrupt)"
			}
			pn.Text(name, "IBM Plex Sans 13", m-104, y, 0, 0.5, c.t0)
			pn.Text("$"+thousands(p.g.Worth(i)), "Geist Mono, IBM Plex Mono 13", m+130, y, 1, 0.5, c.t0)
		}
		pn.Text("Press N or Space for a new game", "IBM Plex Sans 11", m, 450, 0.5, 0.5, c.t2)
		return
	}

	// Dice.
	if p.dice[0] > 0 {
		for i, d := range p.dice {
			x := m - 70 + float64(i)*84
			pn.Alpha(c.app, 0.4)
			pn.RoundRect(x+2, 248, 56, 56, 10)
			pn.Fill()
			pn.Color(c.t0)
			pn.RoundRect(x, 244, 56, 56, 10)
			pn.Fill()
			pn.Color(c.app)
			for _, pip := range pips[d] {
				pn.Circle(x+28+pip[0]*15, 272+pip[1]*15, 4.8)
				pn.Fill()
			}
		}
	}

	// The latest news, and whose turn it is.
	if len(p.log) > 0 {
		l := p.log[0]
		col := c.t1
		if l.good {
			col = c.ok
		} else if l.bad {
			col = c.danger
		}
		pn.TextBox(l.text, "IBM Plex Sans 13", m, 330, 380, col)
	}
	turn := p.g.Players[p.g.Turn]
	who := "Your turn"
	if turn.CPU {
		who = turn.Name + "'s turn"
	}
	pn.Color(p.playerColor(p.g.Turn))
	pn.Circle(m-50, 470, 6)
	pn.Fill()
	pn.Text(who, "IBM Plex Sans Medium 12", m-38, 470, 0, 0.5, c.t0)
	pn.Text(fmt.Sprintf("Round %d of %d", p.g.Round, games.FortuneDays), "Geist Mono, IBM Plex Mono 10", m, 496, 0.5, 0.5, c.t2)
}

var pips = [7][][2]float64{
	{}, {{0, 0}}, {{-1, -1}, {1, 1}}, {{-1, -1}, {0, 0}, {1, 1}},
	{{-1, -1}, {1, -1}, {-1, 1}, {1, 1}}, {{-1, -1}, {1, -1}, {0, 0}, {-1, 1}, {1, 1}},
	{{-1, -1}, {1, -1}, {-1, 0}, {1, 0}, {-1, 1}, {1, 1}},
}

// rentOf is a street's rent as the view shows it.
func rentOf(v games.FortuneView, sq int) int {
	level := 0
	if v.Houses[sq] > 0 {
		level = 1 + v.Houses[sq]
	} else {
		pair := games.Pair(games.Board[sq].Group)
		if v.Owner[pair[0]] == v.Owner[pair[1]] {
			level = 1
		}
	}
	return games.RentAt(sq, level)
}

// refresh sets the header and the panel from the board as shown.
func (p *fortune) refresh() {
	g, v := p.g, p.shown
	p.round.SetText(fmt.Sprintf("%d / %d", g.Round, games.FortuneDays))
	p.pot.SetText("$" + thousands(v.Pot))

	// Players.
	for i, pl := range g.Players {
		col := p.playerColor(i)
		status := fmt.Sprintf("worth $%s", thousands(v.Worth(i)))
		switch {
		case pl.Out:
			status = "bankrupt"
		case pl.InJail:
			status += " · in jail"
		}
		mark := ""
		if i == g.Turn && g.Phase != games.PhaseOver {
			mark = "  ◂"
		}
		p.names[i].SetMarkup(fmt.Sprintf(`<span foreground="#%02x%02x%02x">●</span>  %s%s
<span size="smaller" foreground="#%02x%02x%02x">%s</span>`, col.R, col.G, col.B, html.EscapeString(pl.Name), mark,
			p.f.col.t2.R, p.f.col.t2.G, p.f.col.t2.B, status))
		p.cashes[i].SetText("$" + thousands(v.Cash[i]))
		if pl.Out {
			p.rows[i].AddCssClass("out")
		} else {
			p.rows[i].RemoveCssClass("out")
		}
	}

	// The square in focus.
	sq := p.focus()
	s := games.Board[sq]
	p.cardTitle.SetText(s.Name)
	var b strings.Builder
	switch s.Kind {
	case games.Street:
		pair := games.Pair(s.Group)
		other := pair[0]
		if other == sq {
			other = pair[1]
		}
		fmt.Fprintf(&b, "Price $%d · house $%d · pairs with %s\n", s.Price, games.HouseCost(sq), games.Board[other].Name)
		fmt.Fprintf(&b, "Rent $%d, with the pair $%d\nWith 1, 2, 3 houses: $%d, $%d, $%d", games.RentAt(sq, 0), games.RentAt(sq, 1),
			games.RentAt(sq, 2), games.RentAt(sq, 3), games.RentAt(sq, 4))
		if o := v.Owner[sq]; o >= 0 {
			fmt.Fprintf(&b, "\nOwned by %s", g.Players[o].Name)
			if v.Houses[sq] > 0 {
				fmt.Fprintf(&b, ", %d house%s", v.Houses[sq], map[bool]string{true: "s"}[v.Houses[sq] > 1])
			}
			fmt.Fprintf(&b, ": rent now $%d", rentOf(v, sq))
		} else {
			b.WriteString("\nFor sale")
		}
	case games.StartSquare:
		b.WriteString("Collect $200 every time you pass.")
	case games.JailSquare:
		b.WriteString("Only visiting, unless sent here. In jail, roll a double to leave, or pay $50 before rolling; after three tries you pay and go.")
	case games.Fountain:
		fmt.Fprintf(&b, "Taxes and fines go into the fountain; the next player to land here takes it all: $%d now.", v.Pot)
	case games.GoToJail:
		b.WriteString("Straight to jail, without passing Start.")
	case games.ChanceSquare:
		b.WriteString("Draw a card: money, a fine, or a trip.")
	case games.TaxSquare:
		fmt.Fprintf(&b, "Pay $%d into the fountain.", s.Price)
	}
	p.cardBody.SetText(b.String())
	mine := s.Kind == games.Street && v.Owner[sq] == 0
	p.cardButtons.SetVisible(mine)
	idle := !p.busy() && g.Turn == 0
	p.build.SetSensitive(idle && g.CanBuild(sq))
	p.sell.SetSensitive(idle && g.CanSell(sq))
	if mine && !g.HasPair(0, s.Group) {
		p.build.SetTooltipText("Own both streets of the pair to build")
	} else {
		p.build.SetTooltipText("")
	}

	// What can be done.
	p.secondary.SetVisible(false)
	switch {
	case g.Phase == games.PhaseOver && !p.busy():
		p.primary.SetLabel("New game")
		p.primary.SetSensitive(true)
	case p.busy() || g.Players[g.Turn].CPU:
		p.primary.SetLabel(g.Players[g.Turn].Name + " is playing…")
		if g.Turn == 0 {
			p.primary.SetLabel("…")
		}
		p.primary.SetSensitive(false)
	case g.Phase == games.PhaseRoll:
		p.primary.SetLabel("Roll the dice")
		p.primary.SetSensitive(true)
		if g.Players[0].InJail {
			p.primary.SetLabel("Roll for a double")
			p.secondary.SetLabel("Pay $50 bail")
			p.secondary.SetVisible(true)
			p.secondary.SetSensitive(g.Players[0].Cash >= 50)
		}
	case g.Phase == games.PhaseBuy:
		at := g.Players[0].At
		p.primary.SetLabel(fmt.Sprintf("Buy %s for $%d", games.Board[at].Name, games.Board[at].Price))
		p.primary.SetSensitive(true)
		p.secondary.SetLabel("Pass")
		p.secondary.SetVisible(true)
		p.secondary.SetSensitive(true)
	case g.Phase == games.PhaseEnd:
		p.primary.SetLabel("End turn")
		p.primary.SetSensitive(true)
	}

	// What happened.
	gtkx.Clear(&p.logBox)
	for n, l := range p.log {
		lb := gtk.NewLabel(l.text)
		lb.AddCssClass("fortune-log")
		if n == 0 {
			lb.AddCssClass("latest")
		}
		if l.good {
			lb.AddCssClass("good")
		}
		if l.bad {
			lb.AddCssClass("bad")
		}
		lb.SetXalign(0)
		lb.SetWrap(true)
		lb.SetMaxWidthChars(36)
		gtkx.Append(&p.logBox, &lb.Widget)
	}

	// The header's line.
	switch {
	case g.Phase == games.PhaseOver && !p.busy():
		if g.Winner == 0 {
			p.f.say(fmt.Sprintf("You won after %d rounds, worth $%s.", g.Round, thousands(g.Worth(0))), "good")
		} else {
			p.f.say(fmt.Sprintf("%s won, worth $%s. You finished with $%s.", g.Players[g.Winner].Name, thousands(g.Worth(g.Winner)), thousands(g.Worth(0))), "bad")
		}
	case p.busy():
		p.f.say(g.Players[g.Turn].Name+" is playing.", "")
		if g.Turn == 0 {
			p.f.say("Moving…", "")
		}
	case g.Phase == games.PhaseBuy:
		p.f.say("Buy the street, or pass and leave it to the bank.", "")
	case g.Phase == games.PhaseEnd:
		p.f.say("Click a street of yours to build on it, or end your turn.", "")
	case g.Players[0].InJail:
		p.f.say("You are in jail: roll for a double, or pay $50 and roll.", "")
	default:
		p.f.say("Your turn: roll the dice. Click any square to see its rents.", "")
	}
	p.cv.redraw()
}
