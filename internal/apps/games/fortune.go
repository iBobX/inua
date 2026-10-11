package games

import (
	"fmt"
	"math/rand/v2"
)

var StreetNames = [20]string{"START", "Willow", "Chance", "Maple", "Cedar", "Tax", "Market", "Harbor", "Chance", "Garden", "Park", "Museum", "Library", "Chance", "Station", "Jail", "Canal", "Orchard", "Chance", "Sunset"}

type Player struct{ Position, Cash, Skip int }
type Property struct{ Owner, Level int } // owner: 0 bank, 1 human, 2 computer

// Fortune is a twelve-round property game, with cash and purchase value
// counting towards the final score. No loans or mortgages: a negative cash
// balance ends the game immediately, before another action can be taken.
type Fortune struct {
	Players    [2]Player
	Properties [20]Property
	Round, Die int
	Phase      string // roll, buy, upgrade, end
	Result     Outcome
	Winner     int // -1 tie, 0 human, 1 computer
	Log        []string
}

func NewFortune() *Fortune {
	return &Fortune{Players: [2]Player{{Cash: 600}, {Cash: 600}}, Round: 1, Phase: "roll", Winner: -1}
}

func PropertyPrice(i int) int {
	if i < 0 || i >= 20 || i%5 == 0 || i == 2 || i == 8 || i == 13 || i == 18 {
		return 0
	}
	return 80 + (i/5)*40
}

func (g *Fortune) Worth(player int) int {
	v := g.Players[player].Cash
	for i, p := range g.Properties {
		if p.Owner == player+1 {
			v += PropertyPrice(i) * (2 + p.Level) / 2
		}
	}
	return v
}

func (g *Fortune) note(s string) {
	g.Log = append(g.Log, s)
	if len(g.Log) > 5 {
		g.Log = g.Log[len(g.Log)-5:]
	}
}

func (g *Fortune) check() {
	for i, p := range g.Players {
		if p.Cash < 0 {
			g.Winner = 1 - i
			g.Result = Lost
			if g.Winner == 0 {
				g.Result = Won
			}
			g.Phase = "done"
			return
		}
	}
}

func (g *Fortune) land(player, die int) {
	p := &g.Players[player]
	name := "You"
	if player == 1 {
		name = "Computer"
	}
	if p.Skip > 0 {
		p.Skip--
		g.note(name + " sit out a turn in jail.")
		return
	}
	if p.Position+die >= 20 {
		p.Cash += 120
		g.note(name + " pass Start: +$120.")
	}
	p.Position = (p.Position + die) % 20
	i := p.Position
	g.note(fmt.Sprintf("%s roll %d → %s.", name, die, StreetNames[i]))
	switch {
	case i == 5:
		p.Cash -= 80
		g.note(name + " pay $80 tax.")
	case i == 15:
		p.Skip = 1
		g.note(name + " will miss the next turn.")
	case i == 2 || i == 8 || i == 13 || i == 18:
		events := []struct {
			amount int
			text   string
		}{{90, "collect a $90 dividend."}, {-60, "pay $60 for repairs."}, {50, "receive a $50 refund."}, {-40, "pay a $40 fine."}}
		e := events[rand.IntN(len(events))]
		p.Cash += e.amount
		g.note(name + " " + e.text)
	case PropertyPrice(i) > 0:
		property := g.Properties[i]
		if property.Owner != 0 && property.Owner != player+1 {
			rent := PropertyPrice(i) / 4 * (1 + property.Level)
			p.Cash -= rent
			g.Players[1-player].Cash += rent
			g.note(fmt.Sprintf("%s pay $%d rent.", name, rent))
		}
	}
	g.check()
}

func (g *Fortune) Roll(die int) {
	if g.Result != Playing || g.Phase != "roll" || die < 1 || die > 6 {
		return
	}
	skipped := g.Players[0].Skip > 0
	g.Die = die
	g.land(0, die)
	if g.Result != Playing {
		return
	}
	g.Phase = "end"
	if skipped {
		return
	}
	i := g.Players[0].Position
	p := g.Properties[i]
	price := PropertyPrice(i)
	if price > 0 && p.Owner == 0 && g.Players[0].Cash >= price {
		g.Phase = "buy"
	}
	if price > 0 && p.Owner == 1 && p.Level < 2 && g.Players[0].Cash >= price/2 {
		g.Phase = "upgrade"
	}
}

func (g *Fortune) Buy() {
	if g.Result != Playing || (g.Phase != "buy" && g.Phase != "upgrade") {
		return
	}
	i := g.Players[0].Position
	cost := PropertyPrice(i)
	if g.Phase == "upgrade" {
		cost /= 2
	}
	if g.Players[0].Cash < cost {
		return
	}
	g.Players[0].Cash -= cost
	if g.Phase == "buy" {
		g.Properties[i].Owner = 1
	} else {
		g.Properties[i].Level++
	}
	g.note(fmt.Sprintf("You invest $%d in %s.", cost, StreetNames[i]))
	g.Phase = "end"
}

// EndTurn declines any offer, plays one computer turn, then settles the
// round. Buying and declining cannot accidentally grant an extra roll.
func (g *Fortune) EndTurn() {
	if g.Result != Playing || g.Phase == "roll" {
		return
	}
	skipped := g.Players[1].Skip > 0
	g.land(1, rand.IntN(6)+1)
	if g.Result != Playing {
		return
	}
	i := g.Players[1].Position
	price := PropertyPrice(i)
	p := &g.Properties[i]
	if !skipped && price > 0 {
		cost := price
		if p.Owner == 2 {
			cost /= 2
		}
		if g.Players[1].Cash-cost >= 80 && (p.Owner == 0 || p.Owner == 2 && p.Level < 2) {
			g.Players[1].Cash -= cost
			if p.Owner == 0 {
				p.Owner = 2
			} else {
				p.Level++
			}
			g.note(fmt.Sprintf("Computer invests $%d in %s.", cost, StreetNames[i]))
		}
	}
	if g.Round == 12 {
		a, b := g.Worth(0), g.Worth(1)
		g.Result, g.Phase = Won, "done"
		if a > b {
			g.Winner = 0
		} else if a < b {
			g.Winner = 1
			g.Result = Lost
		} else {
			g.Winner = -1
		}
		return
	}
	g.Round++
	g.Phase = "roll"
}
