package games

import (
	"slices"
	"strings"
	"testing"
)

// hand gives the human player a hand of cards, at the start of their turn.
func hand(g *Fortune, cards ...CardKind) {
	g.Players[0].Cards = slices.Clone(cards)
	g.Turn, g.Phase = 0, PhaseRoll
}

// A shop offers three cards: any of them may be bought, none or all,
// while there is cash and room in the hand; leaving ends the move.
func TestFortuneCardShop(t *testing.T) {
	g := ringFortune(FortuneSettings{Rivals: 1}, 1)
	g.RollWith(4, 3) // the first card shop
	if g.Phase != PhaseShop || len(g.Offer) != shopOffers {
		t.Fatalf("phase %v, offer %v", g.Phase, g.Offer)
	}
	cash := g.Players[0].Cash
	first, third := g.Offer[0], g.Offer[2]
	if !g.BuyCard(2) || !g.BuyCard(0) || len(g.Offer) != 1 {
		t.Fatalf("bought two: offer left %v", g.Offer)
	}
	if got := g.Players[0].Cards; len(got) != 2 || got[0] != third || got[1] != first {
		t.Errorf("hand %v", got)
	}
	if g.Players[0].Cash != cash-Cards[first].Price-Cards[third].Price {
		t.Errorf("paid %d", cash-g.Players[0].Cash)
	}
	if g.EndTurn() || !g.LeaveShop() || g.Phase != PhaseEnd || len(g.Offer) != 0 {
		t.Fatalf("leaving the shop: phase %v", g.Phase)
	}

	// A full hand buys nothing, and the shop does not open.
	g = ringFortune(FortuneSettings{Rivals: 1}, 1)
	hand(g, slices.Repeat([]CardKind{CardTurtle}, HandMax)...)
	g.RollWith(4, 3)
	if g.Phase != PhaseEnd || len(g.Players[0].Cards) != HandMax {
		t.Fatalf("a full hand at the shop: phase %v, %d cards", g.Phase, len(g.Players[0].Cards))
	}
	// Nor does the lack of cash buy.
	g = ringFortune(FortuneSettings{Rivals: 1}, 1)
	g.Players[0].Cash = 10
	g.RollWith(4, 3)
	if g.CanBuyCard(0) || g.BuyCard(0) {
		t.Fatal("bought a card without the cash")
	}
}

// Cards are played in the player's own turn, before the roll or after the
// move, not while buying; each is spent.
func TestFortuneCardTiming(t *testing.T) {
	g := ringFortune(FortuneSettings{Rivals: 1}, 1)
	hand(g, CardFreeRent, CardDice)
	if !g.Play(0, 0) || len(g.Players[0].Cards) != 1 || !g.Players[0].RentFree {
		t.Fatalf("free rent before the roll: %+v", g.Players[0])
	}
	if g.CanPlay(0, 0) || g.CanPlay(0, 13) || !g.CanPlay(0, 12) {
		t.Error("loaded dice's steps")
	}
	g.RollWith(1, 0) // a street for sale
	if g.Phase != PhaseBuy || g.CanPlay(0, 2) {
		t.Fatal("played a card while buying")
	}
	g.Pass()
	if g.CanPlay(0, 2) {
		t.Fatal("loaded dice after the roll")
	}
	g.EndTurn()
	if g.Play(0, 2) {
		t.Fatal("played in a rival's turn")
	}
}

func TestFortuneLoadedDiceAndFreeRent(t *testing.T) {
	g := ringFortune(FortuneSettings{Rivals: 1}, 1)
	g.Owner[9], g.Houses[9] = 1, 3
	hand(g, CardFreeRent, CardDice)
	g.Play(0, 0)
	cash := g.Players[0].Cash
	if !g.Play(0, 9) || g.Players[0].At != 9 || g.Phase != PhaseEnd {
		t.Fatalf("walked to %d, phase %v", g.Players[0].At, g.Phase)
	}
	if g.Players[0].Cash != cash || g.Players[0].RentFree {
		t.Fatalf("rent paid %d, still free %v", cash-g.Players[0].Cash, g.Players[0].RentFree)
	}
	// The next rent is paid.
	g.Turn, g.Phase, g.Players[0].At = 0, PhaseRoll, 0
	g.RollWith(5, 4)
	if g.Players[0].Cash != cash-g.Rent(9) {
		t.Errorf("second rent: paid %d", cash-g.Players[0].Cash)
	}
}

func TestFortuneRequisition(t *testing.T) {
	g := ringFortune(FortuneSettings{Rivals: 1}, 1)
	g.Owner[3], g.Houses[3] = 1, 2
	hand(g, CardRequisition)
	if g.Playable(0) {
		t.Fatal("requisitioned from Start")
	}
	g.RollWith(2, 1) // the rival's street: rent, then the card
	cost := RequisitionCost(3, 2)
	mine, theirs := g.Players[0].Cash, g.Players[1].Cash
	if !g.Play(0, 0) || g.Owner[3] != 0 || g.Houses[3] != 2 {
		t.Fatalf("owner %d, houses %d", g.Owner[3], g.Houses[3])
	}
	if g.Players[0].Cash != mine-cost || g.Players[1].Cash != theirs+cost || cost != 60+2*30 {
		t.Errorf("paid %d, got %d, cost %d", mine-g.Players[0].Cash, g.Players[1].Cash-theirs, cost)
	}
}

func TestFortunePlayerCards(t *testing.T) {
	g := ringFortune(FortuneSettings{Rivals: 2}, 1)
	g.Players[1].Cards = []CardKind{CardJailPass}
	hand(g, CardRob, CardSleep, CardTurtle, CardAudit, CardSwap)
	if slices.Contains(g.Targets(0), 0) || slices.Contains(g.Targets(0), 2) || !slices.Contains(g.Targets(0), 1) {
		t.Fatalf("pickpocket targets %v", g.Targets(0))
	}
	if !g.Play(0, 1) || len(g.Players[1].Cards) != 0 || g.Players[0].Cards[4] != CardJailPass {
		t.Fatalf("pickpocket: %v %v", g.Players[0].Cards, g.Players[1].Cards)
	}
	g.Players[2].At = 33
	g.Players[2].Cash = 1234
	if !g.Play(0, 2) || !g.Players[2].Asleep { // the sleeping pill
		t.Fatal("not asleep")
	}
	if !g.Play(0, 1) || g.Players[1].Turtle != turtleRuns { // the turtle
		t.Fatal("no turtle")
	}
	pot := g.Pot
	if !g.Play(0, 2) || g.Players[2].Cash != 1234-120 || g.Pot != pot+120 { // the audit
		t.Fatalf("audit: cash %d, pot %d", g.Players[2].Cash, g.Pot)
	}
	if !g.Play(0, 2) || g.Players[0].At != 33 || g.Players[2].At != 0 { // the swap
		t.Fatalf("swap: %d %d", g.Players[0].At, g.Players[2].At)
	}
	// The turtle walks one square, without a second roll on a double.
	g.Phase = PhaseEnd
	g.EndTurn()
	g.Phase = PhaseRoll
	g.RollWith(4, 4)
	if g.Players[1].At != 1 || g.Players[1].Turtle != turtleRuns-1 || g.Phase == PhaseRoll {
		t.Fatalf("turtle at %d, %d left, phase %v", g.Players[1].At, g.Players[1].Turtle, g.Phase)
	}
	// The sleeper's turn passes to the human player.
	g.Phase = PhaseEnd
	g.EndTurn()
	if g.Turn != 0 || g.Players[2].Asleep || g.Round != 2 {
		t.Fatalf("turn %d, asleep %v, round %d", g.Turn, g.Players[2].Asleep, g.Round)
	}
}

func TestFortuneSquareCards(t *testing.T) {
	g := ringFortune(FortuneSettings{Rivals: 1}, 1)
	g.Owner[1], g.Houses[1] = 1, Hotel
	g.Owner[3], g.Houses[3] = 0, Hotel-1
	hand(g, CardDemolish, CardPermit)
	if g.CanPlay(0, 3) || g.CanPlay(1, 1) {
		t.Fatal("demolished our own or built on theirs")
	}
	if !g.Play(0, 1) || g.Houses[1] != Hotel-1 || g.Owner[1] != 1 {
		t.Fatalf("demolition: %d", g.Houses[1])
	}
	cash := g.Players[0].Cash
	if !g.Play(0, 3) || g.Houses[3] != Hotel || g.Players[0].Cash != cash {
		t.Fatalf("permit: %d, paid %d", g.Houses[3], cash-g.Players[0].Cash)
	}
}

func TestFortuneJailPassAndFairShares(t *testing.T) {
	g := ringFortune(FortuneSettings{Rivals: 2}, 1)
	hand(g, CardJailPass, CardEqual)
	if g.CanPlay(0, 0) {
		t.Fatal("a jail pass out of jail")
	}
	g.Players[0].InJail, g.Players[0].At = true, JailAt
	cash := g.Players[0].Cash
	if !g.Play(0, 0) || g.Players[0].InJail || g.Players[0].Cash != cash {
		t.Fatalf("jail pass: %+v", g.Players[0])
	}
	g.Players[0].Cash, g.Players[1].Cash, g.Players[2].Cash = 100, 1000, 2001
	if !g.Play(0, 0) || g.Players[0].Cash != 1033 || g.Players[2].Cash != 1033 || g.Pot != 2 {
		t.Fatalf("fair shares: %d %d %d, pot %d", g.Players[0].Cash, g.Players[1].Cash, g.Players[2].Cash, g.Pot)
	}
}

// Computer players buy cards and play them.
func TestFortuneComputerCards(t *testing.T) {
	played := 0
	for seed := range 20 {
		g := NewFortune(FortuneSettings{Rivals: 3}, uint64(seed))
		g.Players[0].CPU = true
		g.Phase = PhaseCPU
		for g.Phase != PhaseOver {
			g.PlayCPU()
			for _, p := range g.Players {
				if len(p.Cards) > HandMax {
					t.Fatalf("seed %d: %d cards", seed, len(p.Cards))
				}
			}
			for _, e := range g.Take() {
				for _, c := range Cards {
					if strings.Contains(e.Text, " played "+c.Name) {
						played++
					}
				}
			}
		}
	}
	if played == 0 {
		t.Error("no card played")
	}
}

// Getting and playing a card tell the board which card, face down when
// the human player may not see it.
func TestFortuneCardEvents(t *testing.T) {
	g := ringFortune(FortuneSettings{Rivals: 1}, 1)
	g.Players[1].Cards = []CardKind{CardJailPass}
	hand(g, CardRob)
	g.Take()
	g.Play(0, 1)
	var cards []FortuneEvent
	for _, e := range g.Take() {
		if e.Kind == EvCard {
			cards = append(cards, e)
		}
	}
	if len(cards) != 2 || !cards[0].Played || cards[0].Card != CardRob || cards[0].Target != 1 ||
		cards[1].Played || cards[1].Card != CardJailPass || cards[1].Back {
		t.Fatalf("events %+v", cards)
	}
	g.give(1, "%s %s")
	for _, e := range g.Take() {
		if e.Kind == EvCard && (!e.Back || e.Player != 1) {
			t.Errorf("a rival's card shown: %+v", e)
		}
	}
}
