package games

import "testing"

func TestFortuneBuyRentAndPairs(t *testing.T) {
	g := NewFortune(1, 1)
	g.RollWith(1, 0) // Willow Lane
	if g.Phase != PhaseBuy || !g.Buy() || g.Owner[1] != 0 || g.Players[0].Cash != startCash-60 {
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
	g.Owner[2] = 0
	if g.Rent(1) != 12 || g.Rent(2) != 14 {
		t.Fatalf("pair rents %d %d", g.Rent(1), g.Rent(2))
	}
}

func TestFortuneBuildAndSell(t *testing.T) {
	g := NewFortune(1, 1)
	g.Owner[1], g.Owner[2] = 0, 0
	if !g.CanBuild(1) || !g.Build(1) || !g.Build(1) || !g.Build(1) || g.Build(1) {
		t.Fatal("three houses, then no more")
	}
	if g.Rent(1) != 6*30 {
		t.Fatalf("rent with three houses %d", g.Rent(1))
	}
	cash := g.Players[0].Cash
	if !g.Sell(1) || g.Players[0].Cash != cash+15 || g.Houses[1] != 2 {
		t.Fatalf("sold for %d", g.Players[0].Cash-cash)
	}
	g.Owner[2] = 1
	if g.CanBuild(1) {
		t.Fatal("built without the pair")
	}
}

func TestFortuneDoublesAndJail(t *testing.T) {
	g := NewFortune(1, 1)
	g.RollWith(2, 2) // Mill Street, then roll again
	g.Pass()
	if g.Phase != PhaseRoll {
		t.Fatalf("no second roll after a double: %v", g.Phase)
	}
	g.RollWith(1, 1) // Jail, visiting
	g.RollWith(3, 3) // the third double
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
	g := NewFortune(1, 1)
	g.Players[0].At = 5
	g.RollWith(3, 1) // Income Tax
	if g.Pot != 100 {
		t.Fatalf("pot %d", g.Pot)
	}
	g.Turn, g.Phase = 0, PhaseRoll
	g.RollWith(2, 1) // Fountain
	if g.Pot != 0 || g.Players[0].Cash != startCash {
		t.Fatalf("pot %d cash %d", g.Pot, g.Players[0].Cash)
	}
}

func TestFortuneDebtSellsThenBankrupts(t *testing.T) {
	g := NewFortune(2, 1)
	g.Owner[22], g.Owner[23], g.Houses[23] = 1, 1, 3 // Summit Plaza: $900
	g.Owner[1], g.Owner[2], g.Houses[1] = 0, 0, 1
	g.Players[0].Cash = 100
	g.Players[0].At = 20
	g.RollWith(2, 1)
	if !g.Players[0].Out || g.Phase != PhaseOver || g.Result != Lost {
		t.Fatalf("player %+v phase %v", g.Players[0], g.Phase)
	}
	if g.Owner[1] != -1 || g.Houses[1] != 0 {
		t.Fatal("streets not back with the bank")
	}
}

// Whole games between computer players end, in time, without anyone in
// debt or houses off a pair.
func TestFortuneComputerGames(t *testing.T) {
	ended := map[int]int{}
	for seed := range 200 {
		g := NewFortune(3, uint64(seed))
		g.Players[0].CPU = true
		g.Phase = PhaseCPU
		for turns := 0; g.Phase != PhaseOver; turns++ {
			if turns > 400 || !g.PlayCPU() {
				t.Fatalf("seed %d: stuck in phase %v", seed, g.Phase)
			}
			for i, p := range g.Players {
				if p.Cash < 0 || p.Out && g.Worth(i) != 0 {
					t.Fatalf("seed %d: %+v", seed, p)
				}
			}
			for sq, h := range g.Houses {
				if h > 0 && !g.HasPair(g.Owner[sq], Board[sq].Group) {
					t.Fatalf("seed %d: houses on %s without the pair", seed, Board[sq].Name)
				}
			}
			g.Take()
		}
		ended[g.Round]++
	}
	early := 0
	for r, n := range ended {
		if r < FortuneDays {
			early += n
		}
	}
	t.Logf("games decided before the last round: %d / 200", early)
}
