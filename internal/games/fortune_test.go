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
	g.Owner[3] = 0 // the whole group: double the bare rent
	if g.Rent(1) != 12 || g.Rent(3) != 12 {
		t.Fatalf("group rents %d %d", g.Rent(1), g.Rent(3))
	}
}

// Each street of yours passed or landed on in a turn may go up one level,
// even alone: four houses, then a hotel.
func TestFortuneRaiseWhatYouPass(t *testing.T) {
	g := ringFortune(FortuneSettings{Rivals: 1}, 1)
	g.Owner[1], g.Owner[3] = 0, 0
	if g.CanBuild(1) {
		t.Fatal("built on a street not passed")
	}
	for n := range Hotel {
		g.Turn, g.Phase, g.Players[0].At = 0, PhaseRoll, 0
		g.RollWith(3, 1) // passes Willow Lane and Birch Row
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
	g.RollWith(3, 3) // Mill Street, for sale
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
	g.Owner[5], g.Owner[15] = 0, 0
	if g.Rent(5) != 50 {
		t.Fatalf("two piers' rent %d", g.Rent(5))
	}
	g.Owner[12] = 0
	g.Dice = [2]int{3, 4}
	if g.Rent(12) != 28 {
		t.Fatalf("one utility, a 7: %d", g.Rent(12))
	}
	g.Owner[28] = 0
	if g.Rent(28) != 70 {
		t.Fatalf("both utilities, a 7: %d", g.Rent(28))
	}
	// The computer lands on a pier of ours.
	g.Turn, g.Phase = 1, PhaseRoll
	g.Players[1].At = 1
	cash := g.Players[0].Cash
	g.RollWith(2, 2) // North Pier
	if g.Players[0].Cash != cash+50 {
		t.Fatalf("pier rent paid %d", g.Players[0].Cash-cash)
	}
}

func TestFortuneDoublesAndJail(t *testing.T) {
	g := ringFortune(FortuneSettings{Rivals: 1}, 1)
	g.RollWith(3, 3) // Mill Street, then roll again
	g.Pass()
	if g.Phase != PhaseRoll {
		t.Fatalf("no second roll after a double: %v", g.Phase)
	}
	g.RollWith(2, 2) // Jail, visiting
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
	if !g.PayBail() || g.Players[0].InJail || g.Pot != bail {
		t.Fatalf("bail: %+v pot %d", g.Players[0], g.Pot)
	}
}

func TestFortuneFountainAndTax(t *testing.T) {
	g := ringFortune(FortuneSettings{Rivals: 1}, 1)
	g.RollWith(3, 1) // Income Tax
	if g.Pot != 150 {
		t.Fatalf("pot %d", g.Pot)
	}
	g.Turn, g.Phase = 0, PhaseRoll
	g.Players[0].At = 17
	g.RollWith(2, 1) // Fountain
	if g.Pot != 0 || g.Players[0].Cash != DefaultFortune.Cash {
		t.Fatalf("pot %d cash %d", g.Pot, g.Players[0].Cash)
	}
}

func TestFortuneDebtSellsThenBankrupts(t *testing.T) {
	g := ringFortune(FortuneSettings{Rivals: 2}, 1)
	g.Owner[37], g.Owner[39], g.Houses[39] = 1, 1, Hotel // Summit Plaza: $2,700
	g.Owner[1], g.Houses[1] = 0, 1
	g.Players[0].Cash = 100
	g.Players[0].At = 36
	g.RollWith(2, 1)
	if !g.Players[0].Out || g.Phase != PhaseOver || g.Result != Lost {
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
					t.Fatalf("seed %d: %d buildings on %s", seed, h, Board[sq].Name)
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
	if g.SquareName(39) != "The Bund" || g.SquareName(0) != "Start" || g.SquareName(4) != "Income Tax" {
		t.Errorf("names %q %q %q", g.SquareName(39), g.SquareName(0), g.SquareName(4))
	}
	g.RollWith(3, 3) // Mill Street's square: the Old City in Shanghai
	g.Buy()
	bought := false
	for _, e := range g.Take() {
		bought = bought || strings.Contains(e.Text, "bought Old City")
	}
	if !bought {
		t.Error("the purchase did not name the Old City")
	}
	for _, c := range FortuneCities {
		for sq, s := range Board {
			_, named := c.Names[sq]
			if property := s.Kind == Street || s.Kind == Pier || s.Kind == Utility; property != named {
				t.Errorf("%s: square %d (%s) named %v", c.ID, sq, s.Name, named)
			}
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
	g.RollWith(3, 3)                                                // over North Pier's fork, onto the branch
	if g.Players[0].At != 40 {
		t.Fatalf("at %d, not the branch's first square", g.Players[0].At)
	}
	took := false
	for _, e := range g.Take() {
		if e.Kind == EvWalk && fmt.Sprint(e.Path) != "[1 2 3 4 5 40]" {
			t.Errorf("path %v", e.Path)
		}
		took = took || strings.Contains(e.Text, "took the road through")
	}
	if !took {
		t.Error("no word of the branch taken")
	}
	g.Turn, g.Phase = 0, PhaseRoll
	g.RollWith(5, 4) // to the branch's end and back onto the ring
	if g.Players[0].At != 16 {
		t.Errorf("at %d after the branch, not 16", g.Players[0].At)
	}
	if Prev(40) != 5 || Prev(41) != 40 || Prev(15) != 14 || fmt.Sprint(Next(5)) != "[6 40]" || fmt.Sprint(Next(47)) != "[15]" {
		t.Errorf("roads: prev %d %d %d, next %v %v", Prev(40), Prev(41), Prev(15), Next(5), Next(47))
	}
	// At random, both roads are taken.
	seen := map[int]bool{}
	for seed := range 40 {
		g := NewFortune(FortuneSettings{Rivals: 1}, uint64(seed))
		g.Players[0].At = 4
		g.RollWith(1, 1)
		seen[g.Players[0].At] = true
	}
	if !seen[6] || !seen[40] {
		t.Errorf("roads taken from the fork: %v", seen)
	}
	if n := 0; true {
		for _, s := range Board {
			if s.Kind == Street || s.Kind == Pier || s.Kind == Utility {
				n++
			}
		}
		if n != 40 {
			t.Errorf("%d properties to buy", n)
		}
	}
}
