package games

import (
	"fmt"
	"math/rand/v2"
	"slices"
)

// Small Fortune is a property game for one player against one to three
// computer players, on a board of its own: 40 squares, 22 streets in eight
// colour groups, four ferry piers and two utilities. Each street of
// theirs a player's pawn passes or lands on in a turn may go up one level
// that turn: house by house to four, then a hotel; owning a whole group
// doubles its bare rent and adds half to its built rents.
// Piers pay more the more of them one owns; utilities charge by the dice.
// Taxes and fines go into the fountain's pot, which the next player to
// land there takes. A player who cannot pay sells buildings, then
// property, back to the bank at half price; one who still cannot is out.
// The game ends when one player is left, or after its last round, with
// the most worth (cash, property and buildings).

type SquareKind uint8

const (
	Street SquareKind = iota
	StartSquare
	JailSquare
	Fountain
	GoToJail
	ChanceSquare
	TaxSquare
	Pier    // a ferry pier: rent by how many piers the owner has
	Utility // rent by the dice
)

type Square struct {
	Name  string
	Kind  SquareKind
	Group int // a street's colour group, 0–7
	Price int // a property's price, or a tax
}

var Board = [40]Square{
	{"Start", StartSquare, -1, 0},
	{"Willow Lane", Street, 0, 60},
	{"Chance", ChanceSquare, -1, 0},
	{"Birch Row", Street, 0, 60},
	{"Income Tax", TaxSquare, -1, 150},
	{"North Pier", Pier, -1, 200},
	{"Mill Street", Street, 1, 100},
	{"Chance", ChanceSquare, -1, 0},
	{"Ferry Road", Street, 1, 100},
	{"Weaver's Yard", Street, 1, 120},
	{"Jail", JailSquare, -1, 0},
	{"Lantern Alley", Street, 2, 140},
	{"Power Station", Utility, -1, 150},
	{"Paper Market", Street, 2, 140},
	{"Ink Court", Street, 2, 160},
	{"East Pier", Pier, -1, 200},
	{"Tea House Row", Street, 3, 180},
	{"Chance", ChanceSquare, -1, 0},
	{"Bell Tower", Street, 3, 180},
	{"Clock Square", Street, 3, 200},
	{"Fountain", Fountain, -1, 0},
	{"Garden Gate", Street, 4, 220},
	{"Chance", ChanceSquare, -1, 0},
	{"Orchard Hill", Street, 4, 220},
	{"Vineyard Way", Street, 4, 240},
	{"South Pier", Pier, -1, 200},
	{"Harbor Front", Street, 5, 260},
	{"Lighthouse", Street, 5, 260},
	{"Water Works", Utility, -1, 150},
	{"Coral Bay", Street, 5, 280},
	{"Go to Jail", GoToJail, -1, 0},
	{"Observatory", Street, 6, 300},
	{"Museum Mile", Street, 6, 300},
	{"Chance", ChanceSquare, -1, 0},
	{"Opera House", Street, 6, 320},
	{"West Pier", Pier, -1, 200},
	{"Chance", ChanceSquare, -1, 0},
	{"Sky Terrace", Street, 7, 350},
	{"Luxury Tax", TaxSquare, -1, 100},
	{"Summit Plaza", Street, 7, 400},
}

const (
	JailAt      = 10
	startPay    = 200
	startCash   = 1500
	bail        = 50
	Hotel       = 5 // buildings on a street: four houses, then a hotel
	cpuReserve  = 150
	FortuneDays = 30 // rounds in a game
)

// HouseCost is the price of one building on a street: a house, or the
// hotel that replaces the four.
func HouseCost(sq int) int { return Board[sq].Price / 2 / 10 * 10 }

// Rent multipliers of a street's tenth of its price: bare, then one to
// four houses, then a hotel.
var rentTimes = [...]int{1, 5, 15, 40, 60, 80}

// pierRent is a pier's rent by how many piers its owner has.
var pierRent = [...]int{0, 25, 50, 100, 200}

type FortunePlayer struct {
	Name      string
	CPU, Out  bool
	Cash, At  int
	InJail    bool
	JailTries int
}

// FortunePhase is what the player whose turn it is may do.
type FortunePhase uint8

const (
	PhaseRoll FortunePhase = iota // roll (or pay bail, in jail)
	PhaseBuy                      // buy the street landed on, or pass
	PhaseEnd                      // build or sell, then end the turn
	PhaseCPU                      // a computer player is next
	PhaseOver
)

// EventKind is what happened, for the board to show in order.
type EventKind uint8

const (
	EvDice EventKind = iota // Dice were rolled
	EvWalk                  // a token walked forward Steps squares
	EvJump                  // a token was moved straight to To
	EvNote                  // something to tell, in Text
)

// FortuneView is what the board shows: the state after an event.
type FortuneView struct {
	Cash   []int
	At     []int
	InJail []bool
	Owner  [len(Board)]int // -1 for the bank
	Houses [len(Board)]int
	Pot    int
}

type FortuneEvent struct {
	Kind      EventKind
	Player    int
	Dice      [2]int
	From, To  int
	Steps     int
	Text      string
	Good, Bad bool // a note's tone, for the log
	View      FortuneView
}

type Fortune struct {
	Players []FortunePlayer
	Owner   [len(Board)]int
	Houses  [len(Board)]int
	Turn    int // whose turn it is
	Round   int
	Phase   FortunePhase
	Dice    [2]int
	Pot     int
	Doubles int
	Winner  int
	Result  Outcome // for the human player, once the game is over
	// CanRaise are the streets of theirs the player whose turn it is
	// passed or landed on this turn, each of which may go up a level.
	CanRaise [len(Board)]bool

	// Events since Take; the board replays them.
	events []FortuneEvent
	deck   []int
	rng    *rand.Rand
}

var cpuNames = []string{"Mika", "Ren", "Sol"}

// NewFortune starts a game against rivals computer players (1–3).
func NewFortune(rivals int, seed uint64) *Fortune {
	rivals = max(1, min(3, rivals))
	g := &Fortune{Round: 1, Winner: -1, rng: rand.New(rand.NewPCG(seed, seed^0x5f)), Phase: PhaseRoll}
	g.Players = append(g.Players, FortunePlayer{Name: "You", Cash: startCash})
	for i := range rivals {
		g.Players = append(g.Players, FortunePlayer{Name: cpuNames[i], CPU: true, Cash: startCash})
	}
	for i := range g.Owner {
		g.Owner[i] = -1
	}
	g.shuffle()
	return g
}

// Take hands over the events since the last Take.
func (g *Fortune) Take() []FortuneEvent {
	e := g.events
	g.events = nil
	return e
}

// View is the board as it is now.
func (g *Fortune) View() FortuneView {
	v := FortuneView{Owner: g.Owner, Houses: g.Houses, Pot: g.Pot}
	for _, p := range g.Players {
		v.Cash = append(v.Cash, p.Cash)
		v.At = append(v.At, p.At)
		v.InJail = append(v.InJail, p.InJail)
	}
	return v
}

// Worth is a player's worth as the view shows it.
func (v FortuneView) Worth(player int) int {
	w := v.Cash[player]
	for sq, o := range v.Owner {
		if o == player {
			w += Board[sq].Price + v.Houses[sq]*HouseCost(sq)
		}
	}
	return w
}

func (g *Fortune) emit(e FortuneEvent) {
	e.View = g.View()
	g.events = append(g.events, e)
}

func (g *Fortune) note(p int, good, bad bool, format string, args ...any) {
	g.emit(FortuneEvent{Kind: EvNote, Player: p, Text: fmt.Sprintf(format, args...), Good: good, Bad: bad})
}

// Rent is what landing on a property costs, as it stands; a utility's
// depends on the dice, the last roll here.
func (g *Fortune) Rent(sq int) int {
	o := g.Owner[sq]
	if o < 0 {
		return 0
	}
	switch Board[sq].Kind {
	case Street:
		return RentAt(sq, g.Houses[sq], g.HasGroup(o, Board[sq].Group))
	case Pier:
		return pierRent[g.count(o, Pier)]
	case Utility:
		return UtilityTimes(g.count(o, Utility)) * (g.Dice[0] + g.Dice[1])
	}
	return 0
}

// RentAt is a street's rent with so many buildings (Hotel for a hotel),
// and with its whole group or not.
func RentAt(sq, buildings int, group bool) int {
	r := Board[sq].Price / 10 * rentTimes[buildings]
	switch {
	case group && buildings == 0:
		r *= 2
	case group:
		r += r / 2
	}
	return r
}

// PierRent is a pier's rent when its owner has n piers.
func PierRent(n int) int { return pierRent[n] }

// UtilityTimes is what a utility charges per point of the dice when its
// owner has n utilities.
func UtilityTimes(n int) int { return map[int]int{1: 4, 2: 10}[n] }

func (g *Fortune) count(player int, kind SquareKind) int {
	n := 0
	for sq, o := range g.Owner {
		if o == player && Board[sq].Kind == kind {
			n++
		}
	}
	return n
}

// Group is the streets of a colour group.
func Group(group int) []int {
	var out []int
	for i, s := range Board {
		if s.Kind == Street && s.Group == group {
			out = append(out, i)
		}
	}
	return out
}

// HasGroup reports whether player owns every street of group.
func (g *Fortune) HasGroup(player, group int) bool {
	if player < 0 {
		return false
	}
	for _, sq := range Group(group) {
		if g.Owner[sq] != player {
			return false
		}
	}
	return true
}

// IsProperty reports whether a square can be bought.
func IsProperty(sq int) bool {
	k := Board[sq].Kind
	return k == Street || k == Pier || k == Utility
}

// Worth is a player's cash, streets and houses at their prices.
func (g *Fortune) Worth(player int) int {
	w := g.Players[player].Cash
	for sq, o := range g.Owner {
		if o == player {
			w += Board[sq].Price + g.Houses[sq]*HouseCost(sq)
		}
	}
	return w
}

// CanBuild reports whether the player whose turn it is may build on sq
// now: a street of theirs they passed or landed on this turn, not yet
// raised, short of a hotel.
func (g *Fortune) CanBuild(sq int) bool {
	p := g.Turn
	return (g.Phase == PhaseRoll || g.Phase == PhaseEnd) && g.CanRaise[sq] && Board[sq].Kind == Street &&
		g.Owner[sq] == p && g.Houses[sq] < Hotel && g.Players[p].Cash >= HouseCost(sq)
}

// CanSell reports whether the player whose turn it is may sell a house
// on sq now.
func (g *Fortune) CanSell(sq int) bool {
	return (g.Phase == PhaseRoll || g.Phase == PhaseEnd) && g.Owner[sq] == g.Turn && g.Houses[sq] > 0
}

// Build puts a house on sq, or the hotel on its fourth house.
func (g *Fortune) Build(sq int) bool {
	if !g.CanBuild(sq) {
		return false
	}
	g.Players[g.Turn].Cash -= HouseCost(sq)
	g.Houses[sq]++
	g.CanRaise[sq] = false // one level a turn
	what := "a house"
	if g.Houses[sq] == Hotel {
		what = "a hotel"
	}
	g.note(g.Turn, false, false, "%s built %s on %s ($%d).", g.Players[g.Turn].Name, what, Board[sq].Name, HouseCost(sq))
	return true
}

// Sell sells a house on sq back to the bank for half its cost.
func (g *Fortune) Sell(sq int) bool {
	if !g.CanSell(sq) {
		return false
	}
	g.Houses[sq]--
	g.Players[g.Turn].Cash += HouseCost(sq) / 2
	g.note(g.Turn, false, false, "%s sold a building on %s for $%d.", g.Players[g.Turn].Name, Board[sq].Name, HouseCost(sq)/2)
	return true
}

// Roll rolls the dice for the human player.
func (g *Fortune) Roll() bool {
	if g.Phase != PhaseRoll || g.Players[g.Turn].CPU {
		return false
	}
	g.roll(g.rng.IntN(6)+1, g.rng.IntN(6)+1)
	return true
}

// RollWith rolls given dice: for tests.
func (g *Fortune) RollWith(a, b int) bool {
	if g.Phase != PhaseRoll {
		return false
	}
	g.roll(a, b)
	return true
}

// PayBail pays the way out of jail before rolling.
func (g *Fortune) PayBail() bool {
	p := &g.Players[g.Turn]
	if g.Phase != PhaseRoll || !p.InJail || p.Cash < bail {
		return false
	}
	g.payPot(g.Turn, bail)
	p.InJail, p.JailTries = false, 0
	g.note(g.Turn, false, false, "%s paid $%d bail.", p.Name, bail)
	return true
}

// Buy buys the street the human player is on.
func (g *Fortune) Buy() bool {
	if g.Phase != PhaseBuy {
		return false
	}
	g.buy(g.Turn)
	g.afterMove()
	return true
}

// Pass leaves the street to the bank.
func (g *Fortune) Pass() bool {
	if g.Phase != PhaseBuy {
		return false
	}
	g.afterMove()
	return true
}

// EndTurn hands the dice to the next player.
func (g *Fortune) EndTurn() bool {
	if g.Phase != PhaseEnd {
		return false
	}
	g.next()
	return true
}

func (g *Fortune) buy(p int) {
	sq := g.Players[p].At
	g.Players[p].Cash -= Board[sq].Price
	g.Owner[sq] = p
	g.note(p, false, false, "%s bought %s for $%d.", g.Players[p].Name, Board[sq].Name, Board[sq].Price)
}

func (g *Fortune) roll(a, b int) {
	p := &g.Players[g.Turn]
	g.Dice = [2]int{a, b}
	g.emit(FortuneEvent{Kind: EvDice, Player: g.Turn, Dice: g.Dice})
	double := a == b
	if p.InJail {
		if double {
			p.InJail, p.JailTries = false, 0
			g.note(g.Turn, true, false, "%s rolled a double and walked out of jail.", p.Name)
			double = false // no second roll after leaving jail
		} else if p.JailTries++; p.JailTries < 3 {
			g.note(g.Turn, false, false, "%s stays in jail.", p.Name)
			g.Phase = PhaseEnd
			return
		} else {
			g.note(g.Turn, false, true, "%s paid $%d bail after three tries.", p.Name, bail)
			p.InJail, p.JailTries = false, 0
			if !g.charge(g.Turn, bail, -1) {
				return
			}
		}
	} else if double {
		if g.Doubles++; g.Doubles == 3 {
			g.note(g.Turn, false, true, "Three doubles in a row: %s goes to jail.", p.Name)
			g.jail(g.Turn)
			return
		}
	}
	if !double {
		g.Doubles = 0
	}
	g.walk(g.Turn, a+b)
}

// walk moves a player forward, paying them as they pass Start, and
// settles the square they land on.
func (g *Fortune) walk(pi, steps int) {
	p := &g.Players[pi]
	from := p.At
	for k := 1; k <= steps; k++ { // the streets of theirs it passes, and the one it lands on
		if sq := (from + k) % len(Board); g.Owner[sq] == pi && Board[sq].Kind == Street {
			g.CanRaise[sq] = true
		}
	}
	p.At = (p.At + steps) % len(Board)
	if p.At < from || steps >= len(Board) {
		p.Cash += startPay
	}
	g.emit(FortuneEvent{Kind: EvWalk, Player: pi, From: from, To: p.At, Steps: steps})
	if p.At < from {
		g.note(pi, true, false, "%s passed Start: +$%d.", p.Name, startPay)
	}
	g.land(pi)
}

func (g *Fortune) jump(pi, to int) {
	p := &g.Players[pi]
	from := p.At
	p.At = to
	g.emit(FortuneEvent{Kind: EvJump, Player: pi, From: from, To: to})
}

func (g *Fortune) jail(pi int) {
	p := &g.Players[pi]
	p.InJail, p.JailTries = true, 0
	g.Doubles = 0
	g.jump(pi, JailAt)
	g.Phase = PhaseEnd
}

func (g *Fortune) payPot(pi, amount int) {
	g.Players[pi].Cash -= amount
	g.Pot += amount
}

func (g *Fortune) land(pi int) {
	p := &g.Players[pi]
	sq := Board[p.At]
	g.Phase = PhaseEnd
	switch sq.Kind {
	case Street, Pier, Utility:
		owner := g.Owner[p.At]
		switch {
		case owner < 0 && p.Cash >= sq.Price:
			g.Phase = PhaseBuy
			if p.CPU {
				if g.cpuWants(pi, p.At) {
					g.buy(pi)
				}
				g.afterMove()
			}
			return
		case owner < 0:
			g.note(pi, false, false, "%s cannot afford %s.", p.Name, sq.Name)

		case owner != pi:
			rent := g.Rent(p.At)
			if !g.charge(pi, rent, owner) {
				return
			}
			g.note(pi, pi == owner, pi == 0, "%s paid %s $%d rent for %s.", p.Name, g.Players[owner].Name, rent, sq.Name)
		}
	case TaxSquare:
		if !g.charge(pi, sq.Price, -1) {
			return
		}
		g.note(pi, false, pi == 0, "%s paid $%d %s into the fountain.", p.Name, sq.Price, sq.Name)
	case Fountain:
		if g.Pot > 0 {
			g.note(pi, true, false, "%s took $%d from the fountain.", p.Name, g.Pot)
			p.Cash += g.Pot
			g.Pot = 0
		} else {
			g.note(pi, false, false, "The fountain is empty.")
		}
	case GoToJail:
		g.note(pi, false, pi == 0, "%s goes to jail.", p.Name)
		g.jail(pi)
		return
	case ChanceSquare:
		if !g.chance(pi) {
			return
		}
	}
	g.afterMove()
}

// afterMove ends a move: another roll after a double, or the end of the
// turn. A computer player plays its whole turn at once.
func (g *Fortune) afterMove() {
	if g.Phase == PhaseOver || g.Players[g.Turn].Out {
		return
	}
	g.Phase = PhaseEnd
	if g.Doubles > 0 && !g.Players[g.Turn].InJail {
		g.Phase = PhaseRoll
		if !g.Players[g.Turn].CPU {
			g.note(g.Turn, false, false, "A double: roll again.")
		}
	}
}

// Chance cards: an amount (or a move) and a line.
var chanceCards = []struct {
	text   string
	amount int
	move   int // squares forward, negative back; 0 none
	to     int // a square to go to, -1 none
	each   int // from (positive) or to (negative) every other player
	house  int // per house owned
}{
	{"A street fair pays out: collect $60.", 60, 0, -1, 0, 0},
	{"Advance to Start.", 0, 0, 0, 0, 0},
	{"Your bicycle needs a new chain: pay $40.", -40, 0, -1, 0, 0},
	{"Go back three squares.", 0, -3, -1, 0, 0},
	{"It is your birthday: every player gives you $25.", 0, 0, -1, 25, 0},
	{"Roof repairs: pay $25 for each building.", 0, 0, -1, 0, -25},
	{"A refund from the tax office: collect $80.", 80, 0, -1, 0, 0},
	{"Take the ferry to the next pier.", 0, 0, nextPier, 0, 0},
	{"An evening at the opera: go to Opera House.", 0, 0, 34, 0, 0},
	{"Your shares paid a dividend: collect $100.", 100, 0, -1, 0, 0},
	{"Hotel inspection: pay $40 for each building.", 0, 0, -1, 0, -40},
	{"A parking fine: pay $30.", -30, 0, -1, 0, 0},
	{"You won a crossword: collect $50.", 50, 0, -1, 0, 0},
	{"Buy everyone lunch: pay each player $15.", 0, 0, -1, -15, 0},
	{"Caught jaywalking: go to jail.", 0, 0, JailAt, 0, 0},
}

// nextPier is a chance card's destination: the next pier ahead.
const nextPier = -2

func (g *Fortune) shuffle() {
	g.deck = g.rng.Perm(len(chanceCards))
}

// chance draws a card; false when it ended the player's game or turn.
func (g *Fortune) chance(pi int) bool {
	if len(g.deck) == 0 {
		g.shuffle()
	}
	c := chanceCards[g.deck[0]]
	g.deck = g.deck[1:]
	p := &g.Players[pi]
	g.note(pi, c.amount > 0 || c.each > 0, (c.amount < 0 || c.each < 0 || c.house < 0) && pi == 0, "Chance for %s: %s", p.Name, c.text)
	switch {
	case c.to == JailAt:
		g.jail(pi)
		return false
	case c.to == nextPier:
		steps := 1
		for Board[(p.At+steps)%len(Board)].Kind != Pier {
			steps++
		}
		g.walk(pi, steps)
		return false
	case c.to >= 0:
		steps := (c.to - p.At + len(Board)) % len(Board)
		g.walk(pi, steps)
		return false
	case c.move < 0:
		g.jump(pi, (p.At+c.move+len(Board))%len(Board))
		g.land(pi)
		return false
	case c.amount > 0:
		p.Cash += c.amount
	case c.amount < 0:
		return g.charge(pi, -c.amount, -1)
	case c.each > 0:
		for o := range g.Players {
			if o == pi || g.Players[o].Out {
				continue
			}
			if g.charge(o, c.each, pi); g.Phase == PhaseOver {
				return false
			}
		}
	case c.each < 0:
		for o := range g.Players {
			if o != pi && !g.Players[o].Out {
				if !g.charge(pi, -c.each, o) {
					return false
				}
			}
		}
	case c.house < 0:
		n := 0
		for sq, o := range g.Owner {
			if o == pi {
				n += g.Houses[sq]
			}
		}
		if n > 0 {
			return g.charge(pi, -c.house*n, -1)
		}
	}
	return true
}

// charge makes player pi pay amount to creditor (-1 for the fountain),
// selling what it must. false when pi could not pay and is out.
func (g *Fortune) charge(pi, amount, creditor int) bool {
	p := &g.Players[pi]
	if p.Cash < amount {
		g.raise(pi, amount)
	}
	pay := min(amount, p.Cash)
	p.Cash -= pay
	if creditor >= 0 {
		g.Players[creditor].Cash += pay
	} else {
		g.Pot += pay
	}
	if pay == amount {
		return true
	}
	g.bankrupt(pi)
	return false
}

// raise sells houses, then streets, cheapest first, until the player has
// amount in cash or nothing left.
func (g *Fortune) raise(pi, amount int) {
	p := &g.Players[pi]
	for p.Cash < amount {
		best, house := -1, false
		for sq, o := range g.Owner {
			if o != pi {
				continue
			}
			if g.Houses[sq] > 0 && (!house || HouseCost(sq) < HouseCost(best)) {
				best, house = sq, true
			}
			if !house && (best < 0 || Board[sq].Price < Board[best].Price) {
				best = sq
			}
		}
		if best < 0 {
			return
		}
		if house {
			g.Houses[best]--
			p.Cash += HouseCost(best) / 2
			g.note(pi, false, pi == 0, "%s sold a house on %s for $%d.", p.Name, Board[best].Name, HouseCost(best)/2)
		} else {
			g.Owner[best] = -1
			p.Cash += Board[best].Price / 2
			g.note(pi, false, pi == 0, "%s sold %s to the bank for $%d.", p.Name, Board[best].Name, Board[best].Price/2)
		}
	}
}

func (g *Fortune) bankrupt(pi int) {
	p := &g.Players[pi]
	p.Out, p.Cash = true, 0
	for sq, o := range g.Owner {
		if o == pi {
			g.Owner[sq], g.Houses[sq] = -1, 0
		}
	}
	g.note(pi, false, pi == 0, "%s is bankrupt and out of the game.", p.Name)
	left := 0
	for _, q := range g.Players {
		if !q.Out {
			left++
		}
	}
	if pi == 0 || left == 1 {
		g.over()
	} else if pi == g.Turn {
		g.Phase = PhaseEnd
		g.Doubles = 0
	}
}

func (g *Fortune) next() {
	g.Doubles, g.CanRaise = 0, [len(Board)]bool{}
	for {
		g.Turn = (g.Turn + 1) % len(g.Players)
		if g.Turn == 0 {
			if g.Round == FortuneDays {
				g.over()
				return
			}
			g.Round++
		}
		if !g.Players[g.Turn].Out {
			break
		}
	}
	g.Phase = PhaseRoll
	if g.Players[g.Turn].CPU {
		g.Phase = PhaseCPU
	}
}

func (g *Fortune) over() {
	g.Phase = PhaseOver
	best := -1
	for i, p := range g.Players {
		if !p.Out && (best < 0 || g.Worth(i) > g.Worth(best)) {
			best = i
		}
	}
	g.Winner = best
	g.Result = Lost
	if best == 0 {
		g.Result = Won
	}
	g.note(best, best == 0, best != 0, "%s won with $%d in worth.", g.Players[best].Name, g.Worth(best))
}

// PlayCPU plays the computer player's whole turn.
func (g *Fortune) PlayCPU() bool {
	if g.Phase != PhaseCPU {
		return false
	}
	pi := g.Turn
	g.Phase = PhaseRoll
	for g.Phase == PhaseRoll && g.Turn == pi {
		p := &g.Players[pi]
		if p.InJail && p.Cash >= bail+400 {
			g.PayBail()
		}
		g.roll(g.rng.IntN(6)+1, g.rng.IntN(6)+1)
	}
	if g.Phase == PhaseOver {
		return true
	}
	if !g.Players[pi].Out {
		g.cpuBuild(pi)
	}
	g.next()
	return true
}

// cpuWants: buy when the cash after it stays above a reserve, or when
// the street adds to a group it has started.
func (g *Fortune) cpuWants(pi, sq int) bool {
	p := g.Players[pi]
	if Board[sq].Kind == Street {
		for _, o := range Group(Board[sq].Group) {
			if o != sq && g.Owner[o] == pi {
				return p.Cash >= Board[sq].Price
			}
		}
	}
	return p.Cash-Board[sq].Price >= cpuReserve
}

// cpuBuild raises the streets it passed this turn a level each, keeping
// a reserve: its whole groups first, then the least built and cheapest;
// lone streets only when it is well off.
func (g *Fortune) cpuBuild(pi int) {
	var sqs []int
	for sq := range g.CanRaise {
		if g.CanRaise[sq] && g.Owner[sq] == pi && g.Houses[sq] < Hotel {
			sqs = append(sqs, sq)
		}
	}
	score := func(sq int) int {
		s := g.Houses[sq]*1000 + HouseCost(sq)
		if !g.HasGroup(pi, Board[sq].Group) {
			s += 10000
		}
		return s
	}
	slices.SortFunc(sqs, func(a, b int) int { return score(a) - score(b) })
	g.Phase = PhaseEnd
	for _, sq := range sqs {
		reserve := cpuReserve + 100
		if !g.HasGroup(pi, Board[sq].Group) {
			reserve = cpuReserve + 300
		}
		if g.Players[pi].Cash-HouseCost(sq) < reserve {
			continue
		}
		g.Build(sq)
	}
}
