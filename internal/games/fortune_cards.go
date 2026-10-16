package games

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/lyricat/inua/internal/i18n"
)

// Small Fortune's cards: a player gets them from Chance and buys them at
// the card shops, holds up to HandMax, and plays any number of them in a
// turn of theirs, before the roll or after the move. A card that needs a
// target takes one: a player, a square, or a number of steps.

type CardKind uint8

const (
	CardDice        CardKind = iota // move the steps chosen instead of rolling
	CardFreeRent                    // the next rent is not paid
	CardRequisition                 // buy the rival's property one stands on
	CardRob                         // take a card from a player
	CardDemolish                    // pull down a building of a rival's
	CardPermit                      // put up a building of one's own, free
	CardTurtle                      // a player walks one square a roll, three times
	CardSwap                        // change places with a player
	CardJailPass                    // out of jail, without bail
	CardAudit                       // a player pays a tenth of their cash into the fountain
	CardEqual                       // everyone's cash shared out equally
	CardSleep                       // a player misses their next turn
	cardKinds
)

// CardTarget is what a card is played on.
type CardTarget uint8

const (
	TargetNone   CardTarget = iota
	TargetPlayer            // a player, by index
	TargetSquare            // a square of the board
	TargetSteps             // how many squares to walk
)

// CardInfo is a card's name, what it does, its price in the shops, and
// what it is played on.
type CardInfo struct {
	Name, Text string
	Price      int
	Target     CardTarget
}

var Cards = [cardKinds]CardInfo{
	CardDice:        {i18n.M("Loaded Dice"), i18n.M("Before you roll: walk the number of squares you choose instead. At a fork, chance still picks the road."), 80, TargetSteps},
	CardFreeRent:    {i18n.M("Free Rent"), i18n.M("The next rent you owe is not paid."), 160, TargetNone},
	CardRequisition: {i18n.M("Requisition"), i18n.M("Buy the rival's property you stand on, buildings and all, paying its owner what it cost."), 200, TargetNone},
	CardRob:         {i18n.M("Pickpocket"), i18n.M("Take a card, at random, from a rival's hand."), 100, TargetPlayer},
	CardDemolish:    {i18n.M("Demolition"), i18n.M("Pull down a building on a rival's street: pick the street on the board first."), 150, TargetSquare},
	CardPermit:      {i18n.M("Building Permit"), i18n.M("Put up a building on a street of yours, free: pick the street on the board first."), 150, TargetSquare},
	CardTurtle:      {i18n.M("Turtle"), i18n.M("A player of your choice walks one square a roll for their next three rolls."), 80, TargetPlayer},
	CardSwap:        {i18n.M("Swap Places"), i18n.M("Change places with a rival; neither of you lands anywhere."), 100, TargetPlayer},
	CardJailPass:    {i18n.M("Jail Pass"), i18n.M("Walk out of jail before you roll, without paying bail."), 50, TargetNone},
	CardAudit:       {i18n.M("Tax Audit"), i18n.M("A rival pays a tenth of their cash into the fountain."), 120, TargetPlayer},
	CardEqual:       {i18n.M("Fair Shares"), i18n.M("Everyone's cash is pooled and shared out equally."), 200, TargetNone},
	CardSleep:       {i18n.M("Sleeping Pill"), i18n.M("A rival misses their next turn."), 100, TargetPlayer},
}

const (
	HandMax    = 10 // cards a player may hold
	shopOffers = 3  // cards a shop offers
	turtleRuns = 3  // rolls a turtle walks one square
)

// RequisitionCost is what a Requisition of sq pays its owner: its price
// and its buildings'.
func RequisitionCost(sq int, houses int) int { return Board[sq].Price + houses*HouseCost(sq) }

// MaxSteps is the most a Loaded Dice card walks: what the dice could show.
func (g *Fortune) MaxSteps() int {
	if g.OneDie {
		return 6
	}
	return 12
}

// give puts a random card in a player's hand; none when it is full.
func (g *Fortune) give(pi int, why string) {
	p := &g.Players[pi]
	if len(p.Cards) >= HandMax {
		g.note(pi, 0, 0, i18n.T("%s's hand is full: no card."), p.Name)
		return
	}
	c := CardKind(g.rng.IntN(int(cardKinds)))
	p.Cards = append(p.Cards, c)
	g.emit(FortuneEvent{Kind: EvCard, Player: pi, Card: c, Sees: Seat(pi), Target: -1})
	g.emit(FortuneEvent{Kind: EvNote, Player: pi, Text: fmt.Sprintf(i18n.T("%s takes a card."), p.Name),
		Secret: fmt.Sprintf(why, p.Name, i18n.T(Cards[c].Name)), Sees: Seat(pi), Helps: Seat(pi)})
}

// openShop sets out a shop's cards for the player on it: a computer player
// buys what it wants at once.
func (g *Fortune) openShop(pi int) {
	p := &g.Players[pi]
	if len(p.Cards) >= HandMax {
		g.note(pi, 0, 0, i18n.T("%s's hand is full: nothing to buy."), p.Name)
		g.afterMove()
		return
	}
	g.Offer = g.Offer[:0]
	for range shopOffers {
		g.Offer = append(g.Offer, CardKind(g.rng.IntN(int(cardKinds))))
	}
	g.Phase = PhaseShop
	if p.Computer() {
		g.cpuShop(pi)
		g.LeaveShop()
	}
}

// CanBuyCard reports whether the player at the shop may buy its offer i.
func (g *Fortune) CanBuyCard(i int) bool {
	p := g.Players[g.Turn]
	return g.Phase == PhaseShop && i >= 0 && i < len(g.Offer) && len(p.Cards) < HandMax && p.Cash >= Cards[g.Offer[i]].Price
}

// BuyCard buys the shop's offer i: none, one, or all of them may be bought.
func (g *Fortune) BuyCard(i int) bool {
	if !g.CanBuyCard(i) {
		return false
	}
	p := &g.Players[g.Turn]
	c := g.Offer[i]
	g.Offer = slices.Delete(g.Offer, i, i+1)
	p.Cash -= Cards[c].Price
	p.Cards = append(p.Cards, c)
	g.emit(FortuneEvent{Kind: EvCard, Player: g.Turn, Card: c, Target: -1})
	g.note(g.Turn, 0, 0, i18n.T("%s bought %s for $%d."), p.Name, i18n.T(Cards[c].Name), Cards[c].Price)
	return true
}

// LeaveShop ends the visit to the shop, and the move.
func (g *Fortune) LeaveShop() bool {
	if g.Phase != PhaseShop {
		return false
	}
	g.Offer = g.Offer[:0]
	g.afterMove()
	return true
}

// CanPlay reports whether the player whose turn it is may play their card
// i now on target: a player, a square or steps, as the card takes; a card
// that takes none ignores it.
func (g *Fortune) CanPlay(i, target int) bool {
	pi := g.Turn
	p := g.Players[pi]
	if g.Phase != PhaseRoll && g.Phase != PhaseEnd || p.Out || i < 0 || i >= len(p.Cards) {
		return false
	}
	rival := func(t int) bool { return t >= 0 && t < len(g.Players) && t != pi && !g.Players[t].Out }
	switch p.Cards[i] {
	case CardDice:
		return g.Phase == PhaseRoll && !p.InJail && target >= 1 && target <= g.MaxSteps()
	case CardFreeRent:
		return !p.RentFree
	case CardRequisition:
		o := g.Owner[p.At]
		return IsProperty(p.At) && o >= 0 && o != pi && p.Cash >= RequisitionCost(p.At, g.Houses[p.At])
	case CardRob:
		return rival(target) && len(g.Players[target].Cards) > 0
	case CardDemolish:
		return target >= 0 && target < len(Board) && Board[target].Kind == Street && g.Owner[target] >= 0 && g.Owner[target] != pi && g.Houses[target] > 0
	case CardPermit:
		return target >= 0 && target < len(Board) && Board[target].Kind == Street && g.Owner[target] == pi && g.Houses[target] < Hotel
	case CardTurtle:
		return target >= 0 && target < len(g.Players) && !g.Players[target].Out && g.Players[target].Turtle == 0
	case CardSwap:
		return rival(target) && !p.InJail && !g.Players[target].InJail
	case CardJailPass:
		return g.Phase == PhaseRoll && p.InJail
	case CardAudit:
		return rival(target) && g.Players[target].Cash >= 10
	case CardEqual:
		return true
	case CardSleep:
		return rival(target) && !g.Players[target].Asleep
	}
	return false
}

// Playable reports whether the card i may be played now on anything: for
// the panel, before a target is picked.
func (g *Fortune) Playable(i int) bool {
	for _, t := range g.Targets(i) {
		if g.CanPlay(i, t) {
			return true
		}
	}
	return false
}

// Targets are what card i of the player whose turn it is may be played on
// now: players, squares or steps; for a card that takes none, a 0.
func (g *Fortune) Targets(i int) []int {
	p := g.Players[g.Turn]
	if i < 0 || i >= len(p.Cards) {
		return nil
	}
	var all []int
	switch Cards[p.Cards[i]].Target {
	case TargetNone:
		all = []int{0}
	case TargetPlayer:
		for t := range g.Players {
			all = append(all, t)
		}
	case TargetSquare:
		for t := range Board {
			all = append(all, t)
		}
	case TargetSteps:
		for t := 1; t <= g.MaxSteps(); t++ {
			all = append(all, t)
		}
	}
	return slices.DeleteFunc(all, func(t int) bool { return !g.CanPlay(i, t) })
}

// Play plays card i of the person whose turn it is on target.
func (g *Fortune) Play(i, target int) bool {
	if g.Players[g.Turn].Computer() || !g.CanPlay(i, target) {
		return false
	}
	g.play(i, target)
	return true
}

// play plays card i of the player whose turn it is on target, CanPlay
// having said it may.
func (g *Fortune) play(i, target int) {
	pi := g.Turn
	p := &g.Players[pi]
	c := p.Cards[i]
	p.Cards = slices.Delete(p.Cards, i, i+1)
	name := i18n.T(Cards[c].Name)
	who := func(t int) string { return g.Players[t].Name }
	on := -1
	if Cards[c].Target == TargetPlayer || Cards[c].Target == TargetSquare {
		on = target
	}
	g.emit(FortuneEvent{Kind: EvCard, Player: pi, Card: c, Played: true, Target: on})
	switch c {
	case CardDice:
		g.note(pi, 0, 0, i18n.T("%s played %s: %d squares."), p.Name, name, target)
		g.Doubles = 0
		g.walk(pi, target)
	case CardFreeRent:
		p.RentFree = true
		g.note(pi, 0, 0, i18n.T("%s played %s: no rent next time."), p.Name, name)
	case CardRequisition:
		sq, o := p.At, g.Owner[p.At]
		cost := RequisitionCost(sq, g.Houses[sq])
		p.Cash -= cost
		g.Players[o].Cash += cost
		g.Owner[sq] = pi
		g.note(pi, Seat(pi), Seat(o), i18n.T("%s played %s and took %s from %s for $%d."), p.Name, name, g.SquareName(sq), who(o), cost)
	case CardRob:
		hand := &g.Players[target].Cards
		k := g.rng.IntN(len(*hand))
		got := (*hand)[k]
		*hand = slices.Delete(*hand, k, k+1)
		p.Cards = append(p.Cards, got)
		both := Seat(pi) | Seat(target)
		g.emit(FortuneEvent{Kind: EvCard, Player: pi, Card: got, Sees: both, Target: -1})
		took := i18n.T("%s played %s and took %s from %s.")
		g.emit(FortuneEvent{Kind: EvNote, Player: pi, Helps: Seat(pi), Hurts: Seat(target), Sees: both,
			Text:   fmt.Sprintf(took, p.Name, name, i18n.T("a card"), who(target)),
			Secret: fmt.Sprintf(took, p.Name, name, i18n.T(Cards[got].Name), who(target))})
	case CardDemolish:
		o := g.Owner[target]
		g.Houses[target]--
		g.note(pi, 0, Seat(o), i18n.T("%s played %s: a building on %s, %s's, is pulled down."), p.Name, name, g.SquareName(target), who(o))
	case CardPermit:
		g.Houses[target]++
		g.note(pi, Seat(pi), 0, i18n.T("%s played %s: a building goes up on %s."), p.Name, name, g.SquareName(target))
	case CardTurtle:
		g.Players[target].Turtle = turtleRuns
		g.note(pi, 0, Seat(target)&^Seat(pi), i18n.T("%s played %s: %s walks one square a roll for three rolls."), p.Name, name, who(target))
	case CardSwap:
		a, b := p.At, g.Players[target].At
		g.note(pi, 0, Seat(target), i18n.T("%s played %s and changed places with %s."), p.Name, name, who(target))
		g.jump(pi, b)
		g.jump(target, a)
	case CardJailPass:
		p.InJail, p.JailTries = false, 0
		g.note(pi, Everyone, 0, i18n.T("%s played %s and walked out of jail."), p.Name, name)
	case CardAudit:
		due := g.Players[target].Cash / 10 / 10 * 10
		g.payPot(target, due)
		g.note(pi, 0, Seat(target), i18n.T("%s played %s: %s pays $%d into the fountain."), p.Name, name, who(target), due)
	case CardEqual:
		total, n := 0, 0
		for _, q := range g.Players {
			if !q.Out {
				total, n = total+q.Cash, n+1
			}
		}
		share := total / n
		for t := range g.Players {
			if !g.Players[t].Out {
				g.Players[t].Cash = share
			}
		}
		g.Pot += total - share*n
		g.note(pi, 0, 0, i18n.T("%s played %s: everyone now has $%d."), p.Name, name, share)
	case CardSleep:
		g.Players[target].Asleep = true
		g.note(pi, 0, Seat(target), i18n.T("%s played %s: %s sits out the next turn."), p.Name, name, who(target))
	}
}

// cpuShop buys what the computer player wants from the shop, keeping a
// reserve: the cards it plays best first, two at most.
func (g *Fortune) cpuShop(pi int) {
	rv := g.rival(pi)
	rank := rv.Likes
	for bought := 0; bought < rv.Cards; {
		best := -1
		for i, c := range g.Offer {
			r := slices.Index(rank, c)
			if r >= 0 && g.Players[pi].Cash-Cards[c].Price >= rv.CardReserve && (best < 0 || r < slices.Index(rank, g.Offer[best])) {
				best = i
			}
		}
		if best < 0 || !g.BuyCard(best) {
			return
		}
		bought++
	}
}

// cpuCards plays the computer player's cards it has a use for: before its
// roll, or after its move.
func (g *Fortune) cpuCards(pi int, before bool) {
	for played := true; played && g.Turn == pi && (g.Phase == PhaseRoll || g.Phase == PhaseEnd); {
		played = false
		p := g.Players[pi]
		for i, c := range p.Cards {
			t, ok := g.cpuTarget(pi, c, before)
			if ok && g.CanPlay(i, t) {
				g.play(i, t)
				played = true
				break
			}
		}
	}
}

// cpuTarget is what the computer player would play a card on now, and
// whether it would.
func (g *Fortune) cpuTarget(pi int, c CardKind, before bool) (int, bool) {
	p, rv := g.Players[pi], g.rival(pi)
	// The leading rival, by worth, and the one with the most cash.
	lead, rich := -1, -1
	for t, q := range g.Players {
		if t == pi || q.Out {
			continue
		}
		if lead < 0 || g.Worth(t) > g.Worth(lead) {
			lead = t
		}
		if rich < 0 || q.Cash > g.Players[rich].Cash {
			rich = t
		}
	}
	if lead < 0 {
		return 0, false
	}
	switch c {
	case CardJailPass:
		return 0, before && p.InJail
	case CardFreeRent:
		return 0, !p.RentFree
	case CardDice:
		if !before || p.InJail {
			return 0, false
		}
		steps, gain := g.cpuBestSteps(pi)
		return steps, gain >= rv.DiceGain
	case CardRequisition:
		o := g.Owner[p.At]
		if !IsProperty(p.At) || o < 0 || o == pi {
			return 0, false
		}
		cost := RequisitionCost(p.At, g.Houses[p.At])
		mine := false
		if Board[p.At].Kind == Street {
			for _, sq := range Group(Board[p.At].Group) {
				mine = mine || g.Owner[sq] == pi
			}
		}
		return 0, p.Cash-cost >= rv.Reserve && (mine || cost*3 <= p.Cash)
	case CardRob:
		most := -1
		for t, q := range g.Players {
			if t != pi && !q.Out && len(q.Cards) > 0 && (most < 0 || len(q.Cards) > len(g.Players[most].Cards)) {
				most = t
			}
		}
		return most, most >= 0
	case CardDemolish:
		best := -1
		for sq, o := range g.Owner {
			if o >= 0 && o != pi && g.Houses[sq] > 0 && (best < 0 || g.Rent(sq) > g.Rent(best)) {
				best = sq
			}
		}
		return best, best >= 0 && g.Houses[best] >= 2
	case CardPermit:
		best, score := -1, 0
		for sq, o := range g.Owner {
			if o != pi || Board[sq].Kind != Street || g.Houses[sq] >= Hotel {
				continue
			}
			s := RentAt(sq, g.Houses[sq]+1, g.HasGroup(pi, Board[sq].Group)) - g.Rent(sq)
			if s > score {
				best, score = sq, s
			}
		}
		return best, best >= 0
	case CardTurtle:
		return lead, before && g.Players[lead].Turtle == 0 && (rv.Mean || g.Worth(lead) > g.Worth(pi))
	case CardSleep:
		if rv.Mean { // the leader, as soon as there is one
			return lead, g.Worth(lead) >= g.Worth(pi)
		}
		return lead, g.Worth(lead) > g.Worth(pi)*11/10
	case CardAudit:
		if rv.Mean {
			return rich, g.Players[rich].Cash >= 300
		}
		return rich, g.Players[rich].Cash >= 600
	case CardEqual:
		total, n := 0, 0
		for _, q := range g.Players {
			if !q.Out {
				total, n = total+q.Cash, n+1
			}
		}
		return 0, p.Cash*10 < total/n*6
	}
	return 0, false // a swap: the computer players keep their places
}

// cpuBestSteps is the walk a Loaded Dice card would best take the computer
// player on, keeping to the ring at the forks, and how much better it is
// than a roll's landing on average.
func (g *Fortune) cpuBestSteps(pi int) (int, int) {
	worth := func(sq int) int {
		p := g.Players[pi]
		switch Board[sq].Kind {
		case Street, Pier, Utility:
			switch o := g.Owner[sq]; {
			case o == pi:
				return 30
			case o >= 0 && p.RentFree:
				return 0
			case o >= 0:
				return -g.Rent(sq)
			case g.cpuWants(pi, sq):
				return Board[sq].Price / 2
			}
		case Fountain:
			return g.Pot
		case GoToJail:
			return -300
		case TaxSquare:
			return -Board[sq].Price
		case CardShop:
			return 40
		}
		return 0
	}
	ways := map[int]int{} // the dice's ways to each sum
	for a := 1; a <= 6; a++ {
		if g.OneDie {
			ways[a]++
			continue
		}
		for b := 1; b <= 6; b++ {
			ways[a+b]++
		}
	}
	best, bestAt, mean, n := 0, 1, 0, 0
	at := g.Players[pi].At
	for steps := 1; steps <= g.MaxSteps(); steps++ {
		at = Next(at)[0]
		w := worth(at)
		mean, n = mean+w*ways[steps], n+ways[steps]
		if steps == 1 || w > best {
			best, bestAt = w, steps
		}
	}
	return bestAt, best - mean/cmp.Or(n, 1)
}
