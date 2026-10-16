package games

import (
	"cmp"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"

	"github.com/lyricat/inua/internal/i18n"
)

// Small Fortune is a property game for one to four players, any of them
// played by the computer (two to four in all), on a board of its own: a ring of 80 squares and two
// branch roads across it, a pawn at a fork taking either road as chance
// has it; 72 streets in 24 colour groups, four ferry piers and two
// utilities. Each street of theirs a player's pawn passes or lands on in a
// turn may go up one level that turn: house by house to four, then a
// hotel; owning a whole group doubles its bare rent and adds half to its
// built rents. Piers pay more the more of them one owns; utilities charge
// by the dice. Taxes and fines go into the fountain's pot, which the next
// player to land there takes. Cards, from Chance and the card shops, are
// played in a player's turn, before or after the roll (fortune_cards.go).
// A player who cannot pay sells buildings, then property, back to the bank
// at half price; one who still cannot is out. The game ends when one
// player is left, or after its last round, with the most worth (cash,
// property and buildings).

type SquareKind uint8

const (
	Street SquareKind = iota
	StartSquare
	JailSquare
	Fountain
	GoToJail
	ChanceSquare
	TaxSquare
	Pier     // a ferry pier: rent by how many piers the owner has
	Utility  // rent by the dice
	CardShop // cards for sale
)

// Square is a square of the board. A property's name is its city's
// (FortuneCity.Names); Name is the others'.
type Square struct {
	Name  string
	Kind  SquareKind
	Group int // a street's colour group, 0–23
	Price int // a property's price, or a tax
}

var (
	chanceSq = Square{i18n.M("Chance"), ChanceSquare, -1, 0}
	shopSq   = Square{i18n.M("Card Shop"), CardShop, -1, 0}
)

// street is a street of group at price.
func street(group, price int) Square { return Square{"", Street, group, price} }

// Board is the ring, from Start, one side of it to a line, each side's
// streets in four groups of three round its pier; then the branch roads,
// in four groups each.
var Board = [...]Square{
	{i18n.M("Start"), StartSquare, -1, 0},
	street(0, 60), chanceSq, street(0, 60), street(0, 80), {i18n.M("Income Tax"), TaxSquare, -1, 150},
	street(1, 80), shopSq, street(1, 80), street(1, 100), {"", Pier, -1, 200},
	street(2, 100), chanceSq, street(2, 100), street(2, 120), shopSq,
	street(3, 120), street(3, 120), chanceSq, street(3, 140),
	{i18n.M("Jail"), JailSquare, -1, 0},
	street(4, 140), chanceSq, street(4, 140), street(4, 160), {"", Utility, -1, 150},
	street(5, 160), shopSq, street(5, 160), street(5, 180), {"", Pier, -1, 200},
	street(6, 180), chanceSq, street(6, 180), street(6, 200), shopSq,
	street(7, 200), street(7, 200), chanceSq, street(7, 220),
	{i18n.M("Fountain"), Fountain, -1, 0},
	street(8, 220), chanceSq, street(8, 220), street(8, 240), shopSq,
	street(9, 240), chanceSq, street(9, 240), street(9, 260), {"", Pier, -1, 200},
	street(10, 260), shopSq, street(10, 260), street(10, 280), {"", Utility, -1, 150},
	street(11, 280), street(11, 280), chanceSq, street(11, 300),
	{i18n.M("Go to Jail"), GoToJail, -1, 0},
	street(12, 300), chanceSq, street(12, 300), street(12, 320), shopSq,
	street(13, 320), chanceSq, street(13, 320), street(13, 340), {"", Pier, -1, 200},
	street(14, 350), shopSq, street(14, 350), street(14, 370), chanceSq,
	street(15, 380), street(15, 380), {i18n.M("Luxury Tax"), TaxSquare, -1, 100}, street(15, 400),
	// The first branch road, from the north pier across to the east one.
	street(16, 110), street(16, 110), chanceSq, street(16, 130),
	street(17, 130), street(17, 130), shopSq, street(17, 150),
	street(18, 150), street(18, 150), chanceSq, street(18, 170),
	street(19, 170), shopSq, street(19, 170), street(19, 190),
	// The second, from the south pier across to the west one.
	street(20, 270), street(20, 270), chanceSq, street(20, 290),
	street(21, 290), street(21, 290), shopSq, street(21, 310),
	street(22, 310), street(22, 310), chanceSq, street(22, 330),
	street(23, 330), shopSq, street(23, 330), street(23, 350),
}

// The board's roads: a ring of LoopLen squares from Start, a corner every
// Side squares (Start, Jail, the Fountain, Go to Jail), and branch roads
// off it, each from its fork on the ring through its own squares First to
// Last and back onto the ring at Rejoin. At a fork a pawn takes either
// road, as chance has it.
const (
	LoopLen = 80
	Side    = LoopLen / 4
)

var Branches = []struct{ Fork, First, Last, Rejoin int }{
	{10, 80, 95, 30},
	{50, 96, 111, 70},
}

// IsCorner reports whether sq is one of the ring's four corners.
func IsCorner(sq int) bool { return sq < LoopLen && sq%Side == 0 }

// Next are the squares a pawn may step on from sq: the ring's next first,
// and at a fork the branch's first square too.
func Next(sq int) []int {
	if sq < LoopLen {
		next := []int{(sq + 1) % LoopLen}
		for _, b := range Branches {
			if b.Fork == sq {
				next = append(next, b.First)
			}
		}
		return next
	}
	for _, b := range Branches {
		if sq == b.Last {
			return []int{b.Rejoin}
		}
	}
	return []int{sq + 1}
}

// Prev is the square behind sq, going back: on the ring the one before,
// on a branch the one before or its fork.
func Prev(sq int) int {
	if sq < LoopLen {
		return (sq + LoopLen - 1) % LoopLen
	}
	for _, b := range Branches {
		if sq == b.First {
			return b.Fork
		}
	}
	return sq - 1
}

const (
	JailAt     = Side
	FountainAt = 2 * Side
	StartPay   = 400 // for passing Start: a lap is long
	Bail       = 50  // the way out of jail
	Hotel      = 5   // buildings on a street: four houses, then a hotel
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
	Name string
	// CPU is a computer player; Away a person's seat the computer plays
	// while they are gone (SetAway); AI a seat the user's AI plays, moving
	// as a person does, from outside (fortuneai).
	CPU, Away, AI bool
	Out           bool
	Cash, At      int
	InJail        bool
	JailTries     int
	Cards         []CardKind // the hand, at most HandMax
	RentFree      bool       // a Free Rent card waits for the next rent
	Turtle        int        // rolls left that walk one square
	Asleep        bool       // misses the next turn
	Rival         int        // a computer player's character: its index in Rivals
}

// Computer reports whether the computer plays this player's turns.
func (p FortunePlayer) Computer() bool { return p.CPU || p.Away }

// FortunePhase is what the player whose turn it is may do.
type FortunePhase uint8

const (
	PhaseRoll FortunePhase = iota // roll (or pay bail, in jail)
	PhaseBuy                      // buy the street landed on, or pass
	PhaseShop                     // buy cards at the shop landed on, then leave
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
	EvCard                  // a card was got, or Played on Target
)

// FortuneView is what the board shows: the state after an event.
type FortuneView struct {
	Cash   []int
	At     []int
	InJail []bool
	Out    []bool          // who is bankrupt
	Hands  []int           // how many cards each player holds
	Owner  [len(Board)]int // -1 for the bank
	Houses [len(Board)]int
	Pot    int
}

type FortuneEvent struct {
	Kind     EventKind
	Player   int
	Dice     [2]int
	From, To int
	Steps    int
	Path     []int // the squares an EvWalk stepped on, in order
	Text     string
	// Who a note is good or bad news for, and who sees an EvCard's face
	// or reads Secret in place of Text: a bit a player (Seat), Everyone;
	// Sees 0 is everyone too. For turns them into one player's event.
	Helps, Hurts, Sees uint8
	Secret             string
	// A note's tone for the one who watches, and whether an EvCard is
	// face down to them: For sets them.
	Good, Bad bool
	// An EvCard's card; played, what on: a player or a square, as the
	// card takes, else -1.
	Card         CardKind
	Played, Back bool
	Target       int
	View         FortuneView
}

// Everyone is every player's bit, for FortuneEvent's Helps and Hurts.
const Everyone uint8 = 0xff

// Seat is a player's bit, for FortuneEvent's Helps, Hurts and Sees; none
// for -1, no player.
func Seat(player int) uint8 {
	if player < 0 {
		return 0
	}
	return 1 << player
}

// For is the event as the player viewer sees it (-1 for someone who
// watches without playing): its tone for them, and the card face down,
// with no Card, and the Text without the Secret, where they may not see it.
func (e FortuneEvent) For(viewer int) FortuneEvent {
	in := func(m uint8) bool { return m&Seat(viewer) != 0 }
	e.Good, e.Bad = e.Helps == Everyone || in(e.Helps), e.Hurts == Everyone || in(e.Hurts)
	sees := e.Sees == 0 || in(e.Sees)
	if e.Kind == EvCard && !sees {
		e.Back, e.Card = true, 0
	}
	if e.Secret != "" && sees {
		e.Text = e.Secret
	}
	e.Helps, e.Hurts, e.Sees, e.Secret = 0, 0, 0, ""
	return e
}

type Fortune struct {
	Players []FortunePlayer
	Owner   [len(Board)]int
	Houses  [len(Board)]int
	Turn    int // whose turn it is
	Round   int
	Rounds  int // the last round
	City    int // the board's names: FortuneCities
	// OneDie plays with one die: no doubles; a six leaves jail.
	OneDie bool
	// BuildOnBuy lets a street go up a level the turn it is bought.
	BuildOnBuy bool
	Phase      FortunePhase
	Dice       [2]int
	Pot        int
	Doubles    int
	Winner     int
	// CanRaise are the streets of theirs the player whose turn it is
	// passed or landed on this turn, each of which may go up a level.
	CanRaise [len(Board)]bool
	// Offer is the card shop's cards, while the player is at one.
	Offer []CardKind

	// choose picks a road at a fork, from Next's choices: at random.
	choose func(roads []int) int

	// Events since Take; the board replays them.
	events []FortuneEvent
	deck   []int
	rng    *rand.Rand
}

// NewFortune starts a game as s says; what s leaves at zero is
// DefaultFortune's. The people play first, then the computer players.
func NewFortune(s FortuneSettings, seed uint64) *Fortune {
	humans := s.Humans[:min(len(s.Humans), 4)]
	rivals := cmp.Or(s.Rivals, DefaultFortune.Rivals)
	if len(humans) == 0 {
		humans = []string{i18n.T("You")}
	} else {
		rivals = s.Rivals // none is none, with people enough
	}
	ais := min(len(s.AIs), 4-len(humans))
	rivals = max(0, min(rivals, 4-len(humans)-ais, len(Rivals)))
	if len(humans)+ais+rivals < 2 {
		rivals = 1
	}
	cash := cmp.Or(s.Cash, DefaultFortune.Cash)
	g := &Fortune{Round: 1, Rounds: cmp.Or(s.Rounds, DefaultFortune.Rounds), City: max(0, min(len(FortuneCities)-1, s.City)),
		OneDie: s.Dice == 1, BuildOnBuy: s.BuildOnBuy,
		Winner: -1, rng: rand.New(rand.NewPCG(seed, seed^0x5f)), Phase: PhaseRoll}
	g.choose = func(roads []int) int { return roads[g.rng.IntN(len(roads))] }
	for i, name := range humans {
		g.Players = append(g.Players, FortunePlayer{Name: cmp.Or(name, i18n.Tf("Player %d", i+1)), Cash: cash})
	}
	for _, name := range s.AIs[:min(len(s.AIs), 4-len(g.Players))] {
		g.Players = append(g.Players, FortunePlayer{Name: name, AI: true, Cash: cash})
	}
	for i := range rivals {
		name := Rivals[i].Name
		if i < len(s.RivalNames) && s.RivalNames[i] != "" {
			name = s.RivalNames[i]
		}
		g.Players = append(g.Players, FortunePlayer{Name: name, CPU: true, Cash: cash, Rival: RivalNamed(name)})
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
		v.Out = append(v.Out, p.Out)
		v.Hands = append(v.Hands, len(p.Cards))
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

// note tells what happened to player p, good news for the players in
// helps and bad for those in hurts.
func (g *Fortune) note(p int, helps, hurts uint8, format string, args ...any) {
	g.emit(FortuneEvent{Kind: EvNote, Player: p, Text: fmt.Sprintf(format, args...), Helps: helps, Hurts: hurts})
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
	what := i18n.T("a house")
	if g.Houses[sq] == Hotel {
		what = i18n.T("a hotel")
	}
	g.note(g.Turn, 0, 0, i18n.T("%s built %s on %s ($%d)."), g.Players[g.Turn].Name, what, g.SquareName(sq), HouseCost(sq))
	return true
}

// Sell sells a house on sq back to the bank for half its cost.
func (g *Fortune) Sell(sq int) bool {
	if !g.CanSell(sq) {
		return false
	}
	g.Houses[sq]--
	g.Players[g.Turn].Cash += HouseCost(sq) / 2
	g.note(g.Turn, 0, 0, i18n.T("%s sold a building on %s for $%d."), g.Players[g.Turn].Name, g.SquareName(sq), HouseCost(sq)/2)
	return true
}

// Roll rolls the dice for the human player.
func (g *Fortune) Roll() bool {
	if g.Phase != PhaseRoll || g.Players[g.Turn].Computer() {
		return false
	}
	g.roll(g.throw())
	return true
}

// throw is a roll of the game's dice; the second is 0 with one die.
func (g *Fortune) throw() (int, int) {
	if g.OneDie {
		return g.rng.IntN(6) + 1, 0
	}
	return g.rng.IntN(6) + 1, g.rng.IntN(6) + 1
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
	if g.Phase != PhaseRoll || !p.InJail || p.Cash < Bail {
		return false
	}
	g.payPot(g.Turn, Bail)
	p.InJail, p.JailTries = false, 0
	g.note(g.Turn, 0, 0, i18n.T("%s paid $%d bail."), p.Name, Bail)
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
	g.note(p, 0, 0, i18n.T("%s bought %s for $%d."), g.Players[p].Name, g.SquareName(sq), Board[sq].Price)
	if g.BuildOnBuy && Board[sq].Kind == Street {
		g.CanRaise[sq] = true // it may go up a level this very turn
	}
}

func (g *Fortune) roll(a, b int) {
	p := &g.Players[g.Turn]
	g.Dice = [2]int{a, b}
	g.emit(FortuneEvent{Kind: EvDice, Player: g.Turn, Dice: g.Dice})
	double := a == b && !g.OneDie
	if p.InJail {
		if double || g.OneDie && a == 6 {
			p.InJail, p.JailTries = false, 0
			what := i18n.T("a double")
			if g.OneDie {
				what = i18n.T("a six")
			}
			g.note(g.Turn, Everyone, 0, i18n.T("%s rolled %s and walked out of jail."), p.Name, what)
			double = false // no second roll after leaving jail
		} else if p.JailTries++; p.JailTries < 3 {
			g.note(g.Turn, 0, 0, i18n.T("%s stays in jail."), p.Name)
			g.Phase = PhaseEnd
			return
		} else {
			g.note(g.Turn, 0, Everyone, i18n.T("%s paid $%d bail after three tries."), p.Name, Bail)
			p.InJail, p.JailTries = false, 0
			if !g.charge(g.Turn, Bail, -1) {
				return
			}
		}
	} else if p.Turtle > 0 { // a turtle: one square, and no roll again
		p.Turtle--
		g.Doubles = 0
		g.note(g.Turn, 0, Seat(g.Turn), i18n.T("%s is a turtle: one square."), p.Name)
		g.walk(g.Turn, 1)
		return
	} else if double {
		if g.Doubles++; g.Doubles == 3 {
			g.note(g.Turn, 0, Everyone, i18n.T("Three doubles in a row: %s goes to jail."), p.Name)
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
	path := make([]int, 0, steps)
	at := g.Players[pi].At
	for range steps {
		at = g.choose(Next(at))
		path = append(path, at)
	}
	g.walkPath(pi, path)
}

// walkTo walks a player along the ring to the first square that stop
// accepts.
func (g *Fortune) walkTo(pi int, stop func(sq int) bool) {
	var path []int
	at := g.Players[pi].At
	for range 2 * len(Board) {
		at = Next(at)[0]
		path = append(path, at)
		if stop(at) {
			break
		}
	}
	g.walkPath(pi, path)
}

// walkPath moves a player along path, paying them as they pass Start, and
// settles the square they land on.
func (g *Fortune) walkPath(pi int, path []int) {
	p := &g.Players[pi]
	from := p.At
	passed, via := false, -1
	for _, sq := range path { // the streets of theirs it passes, and the one it lands on
		if g.Owner[sq] == pi && Board[sq].Kind == Street {
			g.CanRaise[sq] = true
		}
		passed = passed || sq == 0
		for _, b := range Branches {
			if sq == b.First {
				via = sq
			}
		}
	}
	p.At = path[len(path)-1]
	if passed {
		p.Cash += StartPay
	}
	g.emit(FortuneEvent{Kind: EvWalk, Player: pi, From: from, To: p.At, Steps: len(path), Path: path})
	if via >= 0 {
		g.note(pi, 0, 0, i18n.T("%s took the road through %s."), p.Name, g.SquareName(via))
	}
	if passed {
		g.note(pi, Everyone, 0, i18n.T("%s passed Start: +$%d."), p.Name, StartPay)
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
			if p.Computer() {
				if g.cpuWants(pi, p.At) {
					g.buy(pi)
				}
				g.afterMove()
			}
			return
		case owner < 0:
			g.note(pi, 0, 0, i18n.T("%s cannot afford %s."), p.Name, g.SquareName(p.At))

		case owner != pi && p.RentFree:
			p.RentFree = false
			g.note(pi, Seat(pi), 0, i18n.T("%s owed %s rent for %s and paid nothing: Free Rent."), p.Name, g.Players[owner].Name, g.SquareName(p.At))
		case owner != pi:
			rent := g.Rent(p.At)
			if !g.charge(pi, rent, owner) {
				return
			}
			g.note(pi, Seat(owner), Seat(pi), i18n.T("%s paid %s $%d rent for %s."), p.Name, g.Players[owner].Name, rent, g.SquareName(p.At))
		}
	case TaxSquare:
		if !g.charge(pi, sq.Price, -1) {
			return
		}
		g.note(pi, 0, Seat(pi), i18n.T("%s paid $%d %s into the fountain."), p.Name, sq.Price, i18n.T(sq.Name))
	case Fountain:
		if g.Pot > 0 {
			g.note(pi, Everyone, 0, i18n.T("%s took $%d from the fountain."), p.Name, g.Pot)
			p.Cash += g.Pot
			g.Pot = 0
		} else {
			g.note(pi, 0, 0, "%s", i18n.T("The fountain is empty."))
		}
	case GoToJail:
		g.note(pi, 0, Seat(pi), i18n.T("%s goes to jail."), p.Name)
		g.jail(pi)
		return
	case ChanceSquare:
		if !g.chance(pi) {
			return
		}
	case CardShop:
		g.openShop(pi)
		return
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
		if !g.Players[g.Turn].Computer() {
			g.note(g.Turn, 0, 0, "%s", i18n.T("A double: roll again."))
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
	card   bool
}{
	{i18n.M("A street fair pays out: collect $60."), 60, 0, -1, 0, 0, false},
	{i18n.M("Advance to Start."), 0, 0, 0, 0, 0, false},
	{i18n.M("Your bicycle needs a new chain: pay $40."), -40, 0, -1, 0, 0, false},
	{i18n.M("Go back three squares."), 0, -3, -1, 0, 0, false},
	{i18n.M("It is your birthday: every player gives you $25."), 0, 0, -1, 25, 0, false},
	{i18n.M("Roof repairs: pay $25 for each building."), 0, 0, -1, 0, -25, false},
	{i18n.M("A refund from the tax office: collect $80."), 80, 0, -1, 0, 0, false},
	{i18n.M("Catch a ride to the next station or pier."), 0, 0, nextPier, 0, 0, false},
	{i18n.M("A night out: go to %s."), 0, 0, 69, 0, 0, false},
	{i18n.M("Your shares paid a dividend: collect $100."), 100, 0, -1, 0, 0, false},
	{i18n.M("Hotel inspection: pay $40 for each building."), 0, 0, -1, 0, -40, false},
	{i18n.M("A parking fine: pay $30."), -30, 0, -1, 0, 0, false},
	{i18n.M("You won a crossword: collect $50."), 50, 0, -1, 0, 0, false},
	{i18n.M("Buy everyone lunch: pay each player $15."), 0, 0, -1, -15, 0, false},
	{i18n.M("Caught jaywalking: go to jail."), 0, 0, JailAt, 0, 0, false},
	{i18n.M("A card left on a bench: take it."), 0, 0, -1, 0, 0, true},
	{i18n.M("The shopkeeper owes you a favour: take a card."), 0, 0, -1, 0, 0, true},
	{i18n.M("You won a raffle: take a card."), 0, 0, -1, 0, 0, true},
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
	text := i18n.T(c.text)
	if strings.Contains(text, "%s") { // a card that names a square
		text = fmt.Sprintf(text, g.SquareName(c.to))
	}
	helps, hurts := uint8(0), uint8(0)
	if c.amount > 0 || c.each > 0 {
		helps = Everyone
	}
	if c.amount < 0 || c.each < 0 || c.house < 0 {
		hurts = Seat(pi)
	}
	g.note(pi, helps, hurts, i18n.T("Chance for %s: %s"), p.Name, text)
	switch {
	case c.card:
		g.give(pi, i18n.T("%s takes a card: %s."))
	case c.to == JailAt:
		g.jail(pi)
		return false
	case c.to == nextPier:
		g.walkTo(pi, func(sq int) bool { return Board[sq].Kind == Pier })
		return false
	case c.to >= 0:
		g.walkTo(pi, func(sq int) bool { return sq == c.to })
		return false
	case c.move < 0:
		at := p.At
		for range -c.move {
			at = Prev(at)
		}
		g.jump(pi, at)
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
			g.note(pi, 0, Seat(pi), i18n.T("%s sold a house on %s for $%d."), p.Name, g.SquareName(best), HouseCost(best)/2)
		} else {
			g.Owner[best] = -1
			p.Cash += Board[best].Price / 2
			g.note(pi, 0, Seat(pi), i18n.T("%s sold %s to the bank for $%d."), p.Name, g.SquareName(best), Board[best].Price/2)
		}
	}
}

func (g *Fortune) bankrupt(pi int) {
	p := &g.Players[pi]
	p.Out, p.Cash = true, 0
	p.Cards, p.RentFree, p.Turtle, p.Asleep = nil, false, 0, false
	for sq, o := range g.Owner {
		if o == pi {
			g.Owner[sq], g.Houses[sq] = -1, 0
		}
	}
	g.note(pi, 0, Seat(pi), i18n.T("%s is bankrupt and out of the game."), p.Name)
	left, people := 0, 0
	for _, q := range g.Players {
		if !q.Out {
			left++
			if !q.CPU && !q.AI {
				people++
			}
		}
	}
	if people == 0 || left == 1 {
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
			if g.Round == g.Rounds {
				g.over()
				return
			}
			g.Round++
		}
		p := &g.Players[g.Turn]
		if p.Out {
			continue
		}
		if p.Asleep {
			p.Asleep = false
			g.note(g.Turn, 0, Seat(g.Turn), i18n.T("%s sleeps through their turn."), p.Name)
			continue
		}
		break
	}
	g.Phase = PhaseRoll
	if g.Players[g.Turn].Computer() {
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
	g.note(best, Seat(best), Everyone&^Seat(best), i18n.T("%s won with $%d in worth."), g.Players[best].Name, g.Worth(best))
}

// Outcome is how the game went for a player: Playing until it is over.
func (g *Fortune) Outcome(player int) Outcome {
	switch {
	case g.Phase != PhaseOver:
		return Playing
	case player == g.Winner:
		return Won
	}
	return Lost
}

// PlayCPU plays the computer player's whole turn.
func (g *Fortune) PlayCPU() bool {
	if g.Phase != PhaseCPU {
		return false
	}
	pi := g.Turn
	g.Phase = PhaseRoll
	for g.Phase == PhaseRoll && g.Turn == pi {
		if g.cpuCards(pi, true); g.Phase != PhaseRoll || g.Turn != pi {
			break // a card walked it
		}
		p := &g.Players[pi]
		if p.InJail && p.Cash >= Bail+g.rival(pi).BailAt {
			g.PayBail()
		}
		g.roll(g.throw())
	}
	if g.Phase == PhaseOver {
		return true
	}
	g.cpuEnd(pi)
	return true
}

// cpuEnd ends the computer player's turn: its cards, its buildings, then
// the next player.
func (g *Fortune) cpuEnd(pi int) {
	if !g.Players[pi].Out {
		g.cpuCards(pi, false)
		g.cpuBuild(pi)
	}
	g.next()
}

// SetAway has the computer play a person's turns while they are gone
// (away), or hands them back; no one else's, and nothing once the game
// is over. Gone in their own turn, the computer
// finishes it: what is left of it now, or PhaseCPU for PlayCPU.
func (g *Fortune) SetAway(player int, away bool) {
	if player < 0 || player >= len(g.Players) {
		return
	}
	p := &g.Players[player]
	if p.CPU || p.AI || p.Away == away || g.Phase == PhaseOver {
		return
	}
	p.Away = away
	if player != g.Turn {
		return
	}
	switch {
	case !away && g.Phase == PhaseCPU:
		g.Phase = PhaseRoll
	case !away:
	case g.Phase == PhaseRoll:
		g.Phase = PhaseCPU
	case g.Phase == PhaseBuy:
		if g.cpuWants(player, p.At) {
			g.buy(player)
		}
		g.afterMove()
	case g.Phase == PhaseShop:
		g.cpuShop(player)
		g.LeaveShop()
	}
	if away {
		switch g.Phase {
		case PhaseRoll:
			g.Phase = PhaseCPU // a double: it rolls again
		case PhaseEnd:
			g.cpuEnd(player)
		}
	}
}

// PlayOn has the computer finish the turn of the player whose turn it is,
// from where it stands, as for a person away; why, when not empty, is
// told first.
func (g *Fortune) PlayOn(why string) {
	pi := g.Turn
	if why != "" {
		g.note(pi, 0, 0, "%s", why)
	}
	for g.Turn == pi && g.Phase != PhaseOver {
		switch g.Phase {
		case PhaseRoll, PhaseCPU:
			g.Phase = PhaseCPU
			g.PlayCPU()
			return
		case PhaseBuy:
			if g.cpuWants(pi, g.Players[pi].At) {
				g.buy(pi)
			}
			g.afterMove()
		case PhaseShop:
			g.cpuShop(pi)
			g.LeaveShop()
		case PhaseEnd:
			g.cpuEnd(pi)
			return
		}
	}
}

// cpuWants: buy when the cash after it stays above a reserve, or when
// the street adds to a group it has started.
func (g *Fortune) cpuWants(pi, sq int) bool {
	p, r := g.Players[pi], g.rival(pi)
	if Board[sq].Kind == Street {
		for _, o := range Group(Board[sq].Group) {
			if o != sq && g.Owner[o] == pi {
				return p.Cash-Board[sq].Price >= r.Reserve/3 // a group begun: worth stretching for
			}
		}
		if r.BuyCheap > 0 && Board[sq].Price > r.BuyCheap {
			return false
		}
	}
	return p.Cash-Board[sq].Price >= r.Reserve
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
	r := g.rival(pi)
	for _, sq := range sqs {
		reserve := r.BuildReserve
		if !g.HasGroup(pi, Board[sq].Group) {
			reserve = r.LoneReserve
		}
		if reserve < 0 || g.Players[pi].Cash-HouseCost(sq) < reserve {
			continue
		}
		g.Build(sq)
	}
}
