package games

import "testing"

func TestFortuneBuyRentAndGroups(t *testing.T) {
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
	g.Owner[3] = 0 // the whole group: double the bare rent
	if g.Rent(1) != 12 || g.Rent(3) != 12 {
		t.Fatalf("group rents %d %d", g.Rent(1), g.Rent(3))
	}
}

// Each street of yours passed or landed on in a turn may go up one level,
// even alone: four houses, then a hotel.
func TestFortuneRaiseWhatYouPass(t *testing.T) {
	g := NewFortune(1, 1)
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
	g := NewFortune(1, 1)
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
	g := NewFortune(1, 1)
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
	g := NewFortune(1, 1)
	g.RollWith(3, 1) // Income Tax
	if g.Pot != 150 {
		t.Fatalf("pot %d", g.Pot)
	}
	g.Turn, g.Phase = 0, PhaseRoll
	g.Players[0].At = 17
	g.RollWith(2, 1) // Fountain
	if g.Pot != 0 || g.Players[0].Cash != startCash {
		t.Fatalf("pot %d cash %d", g.Pot, g.Players[0].Cash)
	}
}

func TestFortuneDebtSellsThenBankrupts(t *testing.T) {
	g := NewFortune(2, 1)
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
		g := NewFortune(3, uint64(seed))
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
		if r < FortuneDays {
			early += n
		}
	}
	t.Logf("games decided before the last round: %d / 200; hotels standing at the end: %d", early, hotels)
}
