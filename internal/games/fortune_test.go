package games

import (
	"fmt"
	"strings"
	"testing"
)

func TestFortuneBuyRentAndGroups(t *testing.T) {
	g := ringFortune(FortuneSettings{Rivals: 1}, 1)
	g.RollWith(1, 0) // Willow Lane
	if g.Phase != PhaseBuy || !g.Buy() || g.Owner[1] != 0 || g.Players[0].Cash != DefaultFortune.Cash-60 {
		t.Fatalf("phase %v owner %d cash %d", g.Phase, g.Owner[1], g.Players[0].Cash)
	}
	if !g.EndTurn() || g.Phase != PhaseCPU || g.Turn != 1 {
		t.Fatalf("phase %v turn %d", g.Phase, g.Turn)
	}
	// The computer lands on Willow Lane and pays the bare rent.
	g.Phase = PhaseRoll
	before := g.Players[0].Cash
	g.RollWith(1, 0)
	if g.Players[0].Cash != before+6 {
		t.Fatalf("rent paid %d", g.Players[0].Cash-before)
	}
	g.Owner[3], g.Owner[4] = 0, 0 // the whole group: double the bare rent
	if g.Rent(1) != 12 || g.Rent(3) != 12 {
		t.Fatalf("group rents %d %d", g.Rent(1), g.Rent(3))
	}
}

// Each street of yours passed or landed on in a turn may go up one level,
// even alone: four houses, then a hotel.
func TestFortuneRaiseWhatYouPass(t *testing.T) {
	g := ringFortune(FortuneSettings{Rivals: 1}, 1)
	g.Owner[1], g.Owner[3], g.Owner[4] = 0, 0, 0
	if g.CanBuild(1) {
		t.Fatal("built on a street not passed")
	}
	for n := range Hotel {
		g.Turn, g.Phase, g.Players[0].At = 0, PhaseRoll, 0
		g.RollWith(2, 1) // passes the first street, lands on the second
		if !g.Build(1) || !g.Build(3) {
			t.Fatalf("turn %d: could not raise both streets passed", n+1)
		}
		if g.Build(1) || g.Build(3) {
			t.Fatalf("turn %d: raised a street twice", n+1)
		}
		g.Phase = PhaseEnd
		g.EndTurn()
	}
	if g.Houses[1] != Hotel || g.Houses[3] != Hotel {
		t.Fatalf("buildings %d %d", g.Houses[1], g.Houses[3])
	}
	if g.Rent(1) != 6*80*3/2 { // a hotel, with the whole group
		t.Fatalf("hotel rent %d", g.Rent(1))
	}
	g.Owner[3] = -1
	if g.Rent(1) != 6*80 {
		t.Fatalf("hotel rent alone %d", g.Rent(1))
	}
	g.Turn, g.Phase = 0, PhaseEnd
	cash := g.Players[0].Cash
	if !g.Sell(1) || g.Players[0].Cash != cash+15 || g.Houses[1] != 4 {
		t.Fatalf("sold for %d", g.Players[0].Cash-cash)
	}
	// Buying a street is not passing one of yours.
	g.Phase, g.Players[0].At = PhaseRoll, 0
	g.RollWith(3, 3) // the second group's first street, for sale
	g.Buy()
	if g.CanBuild(6) {
		t.Fatal("built on the street just bought")
	}
	// What the last player passed is not the next one's.
	g.Phase = PhaseEnd
	g.EndTurn()
	g.Owner[1] = 1
	if g.CanBuild(1) {
		t.Fatal("built on the last player's way")
	}
}

func TestFortunePiersAndUtilities(t *testing.T) {
	g := ringFortune(FortuneSettings{Rivals: 1}, 1)
	g.Owner[10], g.Owner[30] = 0, 0
	if g.Rent(10) != 50 {
		t.Fatalf("two piers' rent %d", g.Rent(10))
	}
	g.Owner[25] = 0
	g.Dice = [2]int{3, 4}
	if g.Rent(25) != 28 {
		t.Fatalf("one utility, a 7: %d", g.Rent(25))
	}
	g.Owner[55] = 0
	if g.Rent(55) != 70 {
		t.Fatalf("both utilities, a 7: %d", g.Rent(55))
	}
	// The computer lands on a pier of ours.
	g.Turn, g.Phase = 1, PhaseRoll
	g.Players[1].At = 6
	cash := g.Players[0].Cash
	g.RollWith(2, 2) // the north pier
	if g.Players[0].Cash != cash+50 {
		t.Fatalf("pier rent paid %d", g.Players[0].Cash-cash)
	}
}

func TestFortuneDoublesAndJail(t *testing.T) {
	g := ringFortune(FortuneSettings{Rivals: 1}, 1)
	g.RollWith(3, 3) // a street for sale, then roll again
	g.Pass()
	if g.Phase != PhaseRoll {
		t.Fatalf("no second roll after a double: %v", g.Phase)
	}
	g.Owner[8] = 0
	g.RollWith(1, 1) // a street of ours
	g.RollWith(1, 1) // the third double
	if !g.Players[0].InJail || g.Players[0].At != JailAt || g.Phase != PhaseEnd {
		t.Fatalf("not jailed: %+v %v", g.Players[0], g.Phase)
	}
	g.Turn, g.Phase = 0, PhaseRoll
	g.RollWith(1, 2)
	if !g.Players[0].InJail || g.Players[0].JailTries != 1 {
		t.Fatalf("left jail without a double: %+v", g.Players[0])
	}
	g.Phase = PhaseRoll
	if !g.PayBail() || g.Players[0].InJail || g.Pot != Bail {
		t.Fatalf("bail: %+v pot %d", g.Players[0], g.Pot)
	}
}

func TestFortuneFountainAndTax(t *testing.T) {
	g := ringFortune(FortuneSettings{Rivals: 1}, 1)
	g.RollWith(3, 2) // Income Tax
	if g.Pot != 150 {
		t.Fatalf("pot %d", g.Pot)
	}
	g.Turn, g.Phase = 0, PhaseRoll
	g.Players[0].At = FountainAt - 3
	g.RollWith(2, 1) // Fountain
	if g.Pot != 0 || g.Players[0].Cash != DefaultFortune.Cash {
		t.Fatalf("pot %d cash %d", g.Pot, g.Players[0].Cash)
	}
}

func TestFortuneDebtSellsThenBankrupts(t *testing.T) {
	g := ringFortune(FortuneSettings{Rivals: 2}, 1)
	g.Owner[76], g.Owner[77], g.Owner[79], g.Houses[79] = 1, 1, 1, Hotel // the dearest street: $4,800
	g.Owner[1], g.Houses[1] = 0, 1
	g.Players[0].Cash = 100
	g.Players[0].At = 76
	g.RollWith(2, 1)
	if !g.Players[0].Out || g.Phase != PhaseOver || g.Outcome(0) != Lost {
		t.Fatalf("player %+v phase %v", g.Players[0], g.Phase)
	}
	if g.Owner[1] != -1 || g.Houses[1] != 0 {
		t.Fatal("streets not back with the bank")
	}
}

// Whole games between computer players end, in time, without anyone in
// debt or buildings off a street its owner holds.
func TestFortuneComputerGames(t *testing.T) {
	ended := map[int]int{}
	hotels := 0
	for seed := range 200 {
		g := NewFortune(FortuneSettings{Rivals: 3}, uint64(seed)) // forks at random
		g.Players[0].CPU = true
		g.Phase = PhaseCPU
		for turns := 0; g.Phase != PhaseOver; turns++ {
			if turns > 600 || !g.PlayCPU() {
				t.Fatalf("seed %d: stuck in phase %v", seed, g.Phase)
			}
			for i, p := range g.Players {
				if p.Cash < 0 || p.Out && g.Worth(i) != 0 {
					t.Fatalf("seed %d: %+v", seed, p)
				}
			}
			for sq, h := range g.Houses {
				if h > 0 && (Board[sq].Kind != Street || g.Owner[sq] < 0 || h > Hotel) {
					t.Fatalf("seed %d: %d buildings on square %d", seed, h, sq)
				}
			}
			g.Take()
		}
		for _, h := range g.Houses {
			if h == Hotel {
				hotels++
			}
		}
		ended[g.Round]++
	}
	early := 0
	for r, n := range ended {
		if r < DefaultFortune.Rounds {
			early += n
		}
	}
	t.Logf("games decided before the last round: %d / 200; hotels standing at the end: %d", early, hotels)
}

// A game's settings: its rivals, rounds, cash and city's names.
func TestFortuneSettings(t *testing.T) {
	g := ringFortune(FortuneSettings{Rivals: 3, Rounds: 20, Cash: 1000, City: 2}, 1)
	if len(g.Players) != 4 || g.Players[0].Cash != 1000 || g.Players[3].Cash != 1000 || g.Rounds != 20 {
		t.Fatalf("players %d, cash %d, rounds %d", len(g.Players), g.Players[0].Cash, g.Rounds)
	}
	if g.SquareName(79) != "The Bund" || g.SquareName(0) != "Start" || g.SquareName(5) != "Income Tax" || g.SquareName(10) != "Railway Station" {
		t.Errorf("names %q %q %q %q", g.SquareName(79), g.SquareName(0), g.SquareName(5), g.SquareName(10))
	}
	g.RollWith(3, 3) // the second group's first street: Tilanqiao in Shanghai
	g.Buy()
	bought := false
	for _, e := range g.Take() {
		bought = bought || strings.Contains(e.Text, "bought Tilanqiao")
	}
	if !bought {
		t.Error("the purchase did not name Tilanqiao")
	}
	properties := 0
	for sq := range Board {
		if IsProperty(sq) {
			properties++
		}
	}
	for _, c := range FortuneCities {
		seen := map[string]bool{}
		for _, n := range c.Names {
			if seen[n] {
				t.Errorf("%s: %s named twice", c.ID, n)
			}
			seen[n] = true
		}
		if len(c.Names) != properties {
			t.Errorf("%s: %d names for %d properties", c.ID, len(c.Names), properties)
		}
	}
	// A game of 20 rounds ends after its 20th.
	g = ringFortune(FortuneSettings{Rivals: 2, Rounds: 20}, 7)
	g.Players[0].CPU, g.Phase = true, PhaseCPU
	for g.Phase != PhaseOver {
		g.PlayCPU()
		g.Take()
	}
	if g.Round > 20 {
		t.Errorf("ended in round %d", g.Round)
	}
}

// The rules a game may choose: one die, and building the turn a street is
// bought.
func TestFortuneRules(t *testing.T) {
	g := ringFortune(FortuneSettings{Rivals: 1, BuildOnBuy: true}, 1)
	g.RollWith(1, 0) // Willow Lane
	g.Buy()
	if !g.CanBuild(1) || !g.Build(1) || g.Houses[1] != 1 {
		t.Fatalf("could not build on the street just bought: houses %d", g.Houses[1])
	}
	g = ringFortune(FortuneSettings{Rivals: 1}, 1)
	g.RollWith(1, 0)
	g.Buy()
	if g.CanBuild(1) {
		t.Fatal("built on the street just bought, the rule off")
	}

	g = ringFortune(FortuneSettings{Rivals: 1, Dice: 1}, 1)
	for range 50 { // one die: no second roll, ever
		g.Turn, g.Phase, g.Players[0].InJail = 0, PhaseRoll, false
		g.Roll()
		if g.Dice[1] != 0 || g.Dice[0] < 1 || g.Dice[0] > 6 || g.Phase == PhaseRoll && g.Turn == 0 {
			t.Fatalf("dice %v, phase %v", g.Dice, g.Phase)
		}
		g.Phase = PhaseEnd
		g.EndTurn()
		g.Take()
	}
	g.Turn, g.Phase = 0, PhaseRoll
	g.Players[0].InJail, g.Players[0].At = true, JailAt
	g.RollWith(5, 0)
	if !g.Players[0].InJail {
		t.Fatal("left jail on a five")
	}
	g.Phase = PhaseRoll
	g.RollWith(6, 0)
	if g.Players[0].InJail || g.Players[0].At != JailAt+6 {
		t.Fatalf("a six: in jail %v, at %d", g.Players[0].InJail, g.Players[0].At)
	}
}

// ringFortune is a game whose pawns keep to the ring at the forks.
func ringFortune(s FortuneSettings, seed uint64) *Fortune {
	g := NewFortune(s, seed)
	g.choose = func(roads []int) int { return roads[0] }
	return g
}

// The branch roads: a pawn at a fork may take one, walks it to its end and
// rejoins the ring; going back leaves it by its fork; the dice choose.
func TestFortuneBranches(t *testing.T) {
	g := NewFortune(FortuneSettings{Rivals: 1}, 1)
	g.choose = func(roads []int) int { return roads[len(roads)-1] } // the branch
	g.Players[0].At = 4
	g.RollWith(4, 3) // over the north pier's fork, onto the branch
	if g.Players[0].At != 80 {
		t.Fatalf("at %d, not the branch's first square", g.Players[0].At)
	}
	took := false
	for _, e := range g.Take() {
		if e.Kind == EvWalk && fmt.Sprint(e.Path) != "[5 6 7 8 9 10 80]" {
			t.Errorf("path %v", e.Path)
		}
		took = took || strings.Contains(e.Text, "took the road through")
	}
	if !took {
		t.Error("no word of the branch taken")
	}
	g.Turn, g.Phase = 0, PhaseRoll
	g.Players[0].At = 90
	g.RollWith(5, 4) // to the branch's end and back onto the ring
	if g.Players[0].At != 33 {
		t.Errorf("at %d after the branch, not 33", g.Players[0].At)
	}
	if Prev(80) != 10 || Prev(81) != 80 || Prev(30) != 29 || fmt.Sprint(Next(10)) != "[11 80]" || fmt.Sprint(Next(95)) != "[30]" {
		t.Errorf("roads: prev %d %d %d, next %v %v", Prev(80), Prev(81), Prev(30), Next(10), Next(95))
	}
	// At random, both roads are taken.
	seen := map[int]bool{}
	for seed := range 40 {
		g := NewFortune(FortuneSettings{Rivals: 1}, uint64(seed))
		g.Players[0].At = 9
		g.RollWith(1, 1)
		seen[g.Players[0].At] = true
	}
	if !seen[11] || !seen[80] {
		t.Errorf("roads taken from the fork: %v", seen)
	}
	if n := 0; true {
		for _, s := range Board {
			if s.Kind == Street || s.Kind == Pier || s.Kind == Utility {
				n++
			}
		}
		if n != 78 {
			t.Errorf("%d properties to buy", n)
		}
	}
}

// People named play first, then the computer players: two to four in
// all, with none of the computer's when the people are enough.
func TestFortunePeople(t *testing.T) {
	for _, c := range []struct {
		humans       []string
		rivals       int
		people, cpus int
	}{
		{nil, 0, 1, DefaultFortune.Rivals},
		{nil, 3, 1, 3},
		{[]string{"Ann", ""}, 0, 2, 0},
		{[]string{"Ann"}, 0, 1, 1},
		{[]string{"A", "B", "C"}, 3, 3, 1},
		{[]string{"A", "B", "C", "D", "E"}, 2, 4, 0},
	} {
		g := NewFortune(FortuneSettings{Humans: c.humans, Rivals: c.rivals}, 1)
		people, cpus := 0, 0
		for i, p := range g.Players {
			if p.CPU {
				cpus++
			} else if people++; i >= c.people {
				t.Errorf("%v: a person after the computer's", c.humans)
			}
		}
		if people != c.people || cpus != c.cpus {
			t.Errorf("%v + %d: %d people, %d computer players", c.humans, c.rivals, people, cpus)
		}
	}
	g := NewFortune(FortuneSettings{Humans: []string{"Ann", ""}}, 1)
	if g.Players[0].Name != "Ann" || g.Players[1].Name != "Player 2" {
		t.Errorf("names %q, %q", g.Players[0].Name, g.Players[1].Name)
	}
}

// Someone gone has the computer finish their turn and play the next ones
// until they are back.
func TestFortuneAway(t *testing.T) {
	g := ringFortune(FortuneSettings{Humans: []string{"Ann", "Ben"}}, 1)
	g.RollWith(1, 0) // a street for sale
	if g.Phase != PhaseBuy {
		t.Fatalf("phase %v", g.Phase)
	}
	g.SetAway(0, true)
	if g.Turn != 1 || g.Phase != PhaseRoll {
		t.Fatalf("turn %d phase %v: Ann's turn not finished", g.Turn, g.Phase)
	}
	g.RollWith(2, 1)
	g.Pass()
	g.LeaveShop()
	g.EndTurn()
	if g.Turn != 0 || g.Phase != PhaseCPU || g.Roll() {
		t.Fatalf("turn %d phase %v: Ann's turn not the computer's", g.Turn, g.Phase)
	}
	g.SetAway(0, false)
	if g.Phase != PhaseRoll || !g.Roll() {
		t.Fatalf("phase %v: Ann's turn not back", g.Phase)
	}
	if g.SetAway(2, true); len(g.Players) != 2 {
		t.Fatal("players")
	}
}

// An event as each player sees it: its tone for them, a card face down
// and the secret line kept from the others.
func TestFortuneEventFor(t *testing.T) {
	e := FortuneEvent{Kind: EvNote, Helps: Seat(1), Hurts: Seat(0), Sees: Seat(1), Text: "a card", Secret: "Turtle"}
	if v := e.For(0); v.Good || !v.Bad || v.Text != "a card" || v.Secret != "" {
		t.Errorf("the one robbed: %+v", v)
	}
	if v := e.For(1); !v.Good || v.Bad || v.Text != "Turtle" {
		t.Errorf("the robber: %+v", v)
	}
	if v := e.For(-1); v.Good || v.Bad || v.Text != "a card" {
		t.Errorf("one watching: %+v", v)
	}
	if v := (FortuneEvent{Kind: EvNote, Helps: Everyone}).For(-1); !v.Good {
		t.Error("good news for everyone, not for one watching")
	}
	card := FortuneEvent{Kind: EvCard, Card: CardTurtle, Sees: Seat(2)}
	if v := card.For(0); !v.Back || v.Card != 0 {
		t.Errorf("someone else's card: %+v", v)
	}
	if v := card.For(2); v.Back || v.Card != CardTurtle {
		t.Errorf("one's own card: %+v", v)
	}
	if v := (FortuneEvent{Kind: EvCard, Card: CardTurtle, Played: true}).For(-1); v.Back || v.Card != CardTurtle {
		t.Errorf("a card played: %+v", v)
	}
}

// The game goes on while someone plays, and ends when the last person is
// out; the computer player left wins.
func TestFortuneOverWithoutPeople(t *testing.T) {
	g := ringFortune(FortuneSettings{Humans: []string{"Ann", "Ben"}, Rivals: 1}, 1)
	g.bankrupt(0)
	if g.Phase == PhaseOver || g.Outcome(1) != Playing {
		t.Fatalf("over with Ben still in: %v", g.Phase)
	}
	g.bankrupt(1)
	if g.Phase != PhaseOver || g.Outcome(2) != Won || g.Outcome(0) != Lost || g.Outcome(1) != Lost {
		t.Fatalf("phase %v winner %d", g.Phase, g.Winner)
	}
}

// The computer players take the names asked for, Mika, Ren and Sol
// for those not named.
func TestFortuneRivalNames(t *testing.T) {
	g := NewFortune(FortuneSettings{Humans: []string{"Ann"}, Rivals: 3, RivalNames: []string{"Sol", "Mika"}}, 1)
	var names []string
	for _, p := range g.Players[1:] {
		names = append(names, p.Name)
	}
	if strings.Join(names, " ") != "Sol Mika Sol" {
		t.Errorf("names %v", names)
	}
}

// The computer players play their characters: Mika keeps cash in hand and
// leaves a dear street of a group not begun, Ren buys it; with cash short
// of Mika's cushion for a lone street, Sol still builds there.
func TestFortuneRivalCharacters(t *testing.T) {
	g := NewFortune(FortuneSettings{Humans: []string{"Ann"}, Rivals: 3, RivalNames: []string{"Mika", "Ren", "Sol"}}, 1)
	mika, ren, sol := 1, 2, 3
	if g.Players[mika].Rival != 0 || g.Players[ren].Rival != 1 || g.Players[sol].Rival != 2 {
		t.Fatalf("characters %d %d %d", g.Players[mika].Rival, g.Players[ren].Rival, g.Players[sol].Rival)
	}
	dear := 79 // $400, its group not begun
	for _, pi := range []int{mika, ren} {
		g.Players[pi].Cash = 1000
	}
	if g.cpuWants(mika, dear) || !g.cpuWants(ren, dear) {
		t.Errorf("a $400 street: Mika %v, Ren %v", g.cpuWants(mika, dear), g.cpuWants(ren, dear))
	}
	for _, pi := range []int{mika, sol} {
		g.Owner[dear], g.Houses[dear] = pi, 0
		g.CanRaise = [len(Board)]bool{}
		g.CanRaise[dear] = true
		g.Turn, g.Phase = pi, PhaseEnd
		g.Players[pi].Cash = HouseCost(dear) + 300 // over Sol's cushion, under Mika's
		g.cpuBuild(pi)
		if built := g.Houses[dear] > 0; built != (pi == sol) {
			t.Errorf("player %d built on a lone street: %v", pi, built)
		}
	}
}
