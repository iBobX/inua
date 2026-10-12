package gamesui

import (
	"fmt"
	"html"
	"image/color"
	"math/rand/v2"
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

// The streets' colour groups, in calm versions of their usual colours.
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
	f.win.SetDefaultSize(1440, 900)
	p.round = f.stat("ROUND")
	p.pot = f.stat("FOUNTAIN")

	row := gtk.NewBox(gtk.OrientationHorizontalValue, 18)
	p.cv = newCanvas(isoW, isoH, p.paint)
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
	p.cardTitle.SetEllipsize(3) // end
	p.cardTitle.SetMaxWidthChars(28)
	gtkx.Append(panel, &p.cardTitle.Widget)
	p.cardBody = gtk.NewLabel("")
	p.cardBody.AddCssClass("fortune-card")
	p.cardBody.SetXalign(0)
	p.cardBody.SetWrap(true)
	p.cardBody.SetMaxWidthChars(36)
	gtkx.Append(panel, &p.cardBody.Widget)
	// Stacked, so that a long label never widens the panel (and shrinks
	// the board).
	p.cardButtons = *gtk.NewBox(gtk.OrientationVerticalValue, 6)
	p.cardButtons.AddCssClass("fortune-buttons")
	p.onBuild = func(gtk.Button) { p.act(func() bool { return p.g.Build(p.focus()) }) }
	p.onSell = func(gtk.Button) { p.act(func() bool { return p.g.Sell(p.focus()) }) }
	p.build = button("Build a house", &p.onBuild)
	p.sell = button("Sell", &p.onSell)
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
		if sq := squareAtScreen(p.cv.at(x, y)); sq >= 0 {
			p.selected = sq
			p.refresh()
		}
	}
	click := gtk.NewGestureClick()
	click.ConnectPressed(&p.onClick)
	p.cv.area.AddController(&click.EventController)
	p.onMotion = func(_ gtk.EventControllerMotion, x, y float64) {
		if sq := squareAtScreen(p.cv.at(x, y)); sq != p.hover {
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

// raisable counts the streets the player may raise a level now.
func (p *fortune) raisable() int {
	n := 0
	if p.g.Turn != 0 || p.busy() {
		return 0
	}
	for sq := range games.Board {
		if p.g.CanBuild(sq) {
			n++
		}
	}
	return n
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
		var group []string
		for _, o := range games.Group(s.Group) {
			if o != sq {
				group = append(group, games.Board[o].Name)
			}
		}
		fmt.Fprintf(&b, "Price $%d · each building $%d\nGroup: %s\n", s.Price, games.HouseCost(sq), strings.Join(group, ", "))
		fmt.Fprintf(&b, "Rent $%d (whole group $%d)\n1–4 houses: $%d, $%d, $%d, $%d\nHotel: $%d (+50%% with the group)",
			games.RentAt(sq, 0, false), games.RentAt(sq, 0, true), games.RentAt(sq, 1, false), games.RentAt(sq, 2, false),
			games.RentAt(sq, 3, false), games.RentAt(sq, 4, false), games.RentAt(sq, games.Hotel, false))
		if o := v.Owner[sq]; o >= 0 {
			fmt.Fprintf(&b, "\nOwned by %s", g.Players[o].Name)
			switch h := v.Houses[sq]; {
			case h == games.Hotel:
				b.WriteString(", with a hotel")
			case h > 0:
				fmt.Fprintf(&b, ", %d house%s", h, map[bool]string{true: "s"}[h > 1])
			}
			fmt.Fprintf(&b, ": rent now $%d", rentOf(v, sq))
		} else {
			b.WriteString("\nFor sale")
		}
	case games.Pier:
		fmt.Fprintf(&b, "Price $%d. Rent $%d, $%d, $%d or $%d as its owner has 1 to 4 piers.", s.Price,
			games.PierRent(1), games.PierRent(2), games.PierRent(3), games.PierRent(4))
		if o := v.Owner[sq]; o >= 0 {
			fmt.Fprintf(&b, "\nOwned by %s: rent now $%d", g.Players[o].Name, rentOf(v, sq))
		} else {
			b.WriteString("\nFor sale")
		}
	case games.Utility:
		fmt.Fprintf(&b, "Price $%d. Rent is the dice times %d, or times %d with both utilities.", s.Price,
			games.UtilityTimes(1), games.UtilityTimes(2))
		if o := v.Owner[sq]; o >= 0 {
			fmt.Fprintf(&b, "\nOwned by %s", g.Players[o].Name)
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
	switch h := v.Houses[sq]; {
	case h == games.Hotel:
		p.build.SetLabel("Hotel built")
	case h == games.Hotel-1:
		p.build.SetLabel(fmt.Sprintf("Build a hotel ($%d)", games.HouseCost(sq)))
	default:
		p.build.SetLabel(fmt.Sprintf("Build a house ($%d)", games.HouseCost(sq)))
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
		p.primary.SetLabel(fmt.Sprintf("Buy it for $%d", games.Board[at].Price)) // short: the panel keeps its width
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
	case p.raisable() > 0:
		p.f.say(fmt.Sprintf("You passed %d street%s of yours (outlined): click one to build a level on it, one a turn each, or end your turn.",
			p.raisable(), map[bool]string{true: "s"}[p.raisable() > 1]), "good")
	case g.Phase == games.PhaseEnd:
		p.f.say("End your turn. Streets of yours you pass go up a level each, if you pay.", "")
	case g.Players[0].InJail:
		p.f.say("You are in jail: roll for a double, or pay $50 and roll.", "")
	default:
		p.f.say("Your turn: roll the dice. Click any square to see its rents.", "")
	}
	p.cv.redraw()
}
