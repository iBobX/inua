// Package fortuneai has the user's AI play seats of Small Fortune: at each
// choice of an AI player's (games.FortunePlayer.AI) it has the model pick
// one of the moves it may make, told the rules and judging the game as it
// stands, and makes it, as a person would through the game's own moves. When the
// model does not answer, or answers with no move, the computer finishes
// that turn; after a few such turns, it plays the seat for the rest of the
// game. The model is asked elsewhere (inua-assistant --answer, a choice on
// Mister Morph's decision route): choose is given.
package fortuneai

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/i18n"
)

// MoveKind is what a move does.
type MoveKind uint8

const (
	Roll MoveKind = iota
	Bail
	Buy
	Pass
	BuyCard // the shop's offer I
	Leave   // the shop
	Play    // card I of the hand, on Target
	Build   // on square I
	End     // the turn
)

// Move is one of the moves the player whose turn it is may make, with
// what it is, in words for the model.
type Move struct {
	Do        MoveKind
	I, Target int
	Text      string
}

// Moves are the moves the player whose turn it is may make now; none
// when it is no one's to make (the computer's turn, the game over).
func Moves(g *games.Fortune) []Move {
	pi := g.Turn
	p := g.Players[pi]
	city := games.FortuneCities[g.City]
	name := func(sq int) string { return city.SquareName(sq) }
	var out []Move
	add := func(m Move) { out = append(out, m) }
	cards := func() {
		var seen []games.CardKind
		for i, c := range p.Cards {
			if slices.Contains(seen, c) {
				continue
			}
			seen = append(seen, c)
			info := games.Cards[c]
			for _, t := range g.Targets(i) {
				text := "Play " + info.Name
				switch info.Target {
				case games.TargetPlayer:
					text += " on " + g.Players[t].Name
				case games.TargetSquare:
					text += fmt.Sprintf(" on %s (%s, %d buildings)", name(t), g.Players[g.Owner[t]].Name, g.Houses[t])
				case games.TargetSteps:
					text += fmt.Sprintf(": walk %d squares%s", t, ahead(g, p.At, t))
				}
				add(Move{Do: Play, I: i, Target: t, Text: text})
			}
		}
	}
	builds := func() {
		for sq := range games.Board {
			if g.CanBuild(sq) {
				h := g.Houses[sq] + 1
				what := fmt.Sprintf("house %d", h)
				if h == games.Hotel {
					what = "the hotel"
				}
				add(Move{Do: Build, I: sq, Text: fmt.Sprintf("Build %s on %s for $%d: its rent becomes $%d",
					what, name(sq), games.HouseCost(sq), games.RentAt(sq, h, g.HasGroup(pi, games.Board[sq].Group)))})
			}
		}
	}
	switch g.Phase {
	case games.PhaseRoll:
		if p.InJail {
			add(Move{Do: Roll, Text: "Roll: a double leaves jail"})
			if p.Cash >= games.Bail {
				add(Move{Do: Bail, Text: fmt.Sprintf("Pay $%d bail, then roll", games.Bail)})
			}
		} else {
			add(Move{Do: Roll, Text: "Roll the dice"})
		}
		cards()
		builds()
	case games.PhaseBuy:
		sq := p.At
		add(Move{Do: Buy, Text: fmt.Sprintf("Buy %s for $%d, leaving $%d", name(sq), games.Board[sq].Price, p.Cash-games.Board[sq].Price)})
		add(Move{Do: Pass, Text: "Leave it to the bank"})
	case games.PhaseShop:
		for i, c := range g.Offer {
			if g.CanBuyCard(i) {
				info := games.Cards[c]
				add(Move{Do: BuyCard, I: i, Text: fmt.Sprintf("Buy the card %s for $%d: %s", info.Name, info.Price, info.Text)})
			}
		}
		add(Move{Do: Leave, Text: "Leave the shop"})
	case games.PhaseEnd:
		cards()
		builds()
		add(Move{Do: End, Text: "End the turn"})
	}
	return out
}

// ahead says where walking steps from sq ends, when no fork is on the way.
func ahead(g *games.Fortune, sq, steps int) string {
	for range steps {
		next := games.Next(sq)
		if len(next) > 1 {
			return ", past a fork"
		}
		sq = next[0]
	}
	return " to " + square(g, sq)
}

// Apply makes move m; false when it cannot be made now.
func Apply(g *games.Fortune, m Move) bool {
	switch m.Do {
	case Roll:
		return g.Roll()
	case Bail:
		return g.PayBail()
	case Buy:
		return g.Buy()
	case Pass:
		return g.Pass()
	case BuyCard:
		return g.BuyCard(m.I)
	case Leave:
		return g.LeaveShop()
	case Play:
		return g.Play(m.I, m.Target)
	case Build:
		return g.Build(m.I)
	case End:
		return g.EndTurn()
	}
	return false
}

// square is a square for the model: its name and kind, its price, owner,
// buildings and rent.
func square(g *games.Fortune, sq int) string {
	s := games.Board[sq]
	name := games.FortuneCities[g.City].SquareName(sq)
	switch s.Kind {
	case games.Street, games.Pier, games.Utility:
		kind := fmt.Sprintf("street of group %d", s.Group+1)
		switch s.Kind {
		case games.Pier:
			kind = "pier"
		case games.Utility:
			kind = "utility"
		}
		o := g.Owner[sq]
		if o < 0 {
			return fmt.Sprintf("%s (%s, $%d, for sale)", name, kind, s.Price)
		}
		rent := fmt.Sprintf("rent $%d", g.Rent(sq))
		if s.Kind == games.Utility {
			rent = fmt.Sprintf("rent %d times the dice", games.UtilityTimes(countKind(g, o, games.Utility)))
		}
		built := ""
		if g.Houses[sq] == games.Hotel {
			built = ", a hotel"
		} else if g.Houses[sq] > 0 {
			built = fmt.Sprintf(", %d houses", g.Houses[sq])
		}
		return fmt.Sprintf("%s (%s, %s's%s, %s)", name, kind, g.Players[o].Name, built, rent)
	case games.TaxSquare:
		return fmt.Sprintf("%s (pay $%d into the fountain)", name, s.Price)
	case games.CardShop:
		return name + " (a card shop)"
	case games.ChanceSquare:
		return name + " (draw a Chance card)"
	case games.Fountain:
		return fmt.Sprintf("%s (take the pot, $%d)", name, g.Pot)
	case games.GoToJail:
		return name + " (go to jail)"
	}
	return name
}

func countKind(g *games.Fortune, player int, kind games.SquareKind) int {
	n := 0
	for sq, o := range g.Owner {
		if o == player && games.Board[sq].Kind == kind {
			n++
		}
	}
	return n
}

// rules are the game's rules, in short, for the model.
const rules = `You play Small Fortune, a property board game like Monopoly, for up to four players. The board is a ring of 80 squares with two branch roads across it; at a fork chance picks the road. Passing Start pays $%d. A street's rent is a tenth of its price; owning its whole colour group doubles the bare rent and adds half to built rents. Each turn you may build one level (a house, up to four, then a hotel) on each street of yours you passed or landed on that turn; buildings raise rents steeply (5, 15, 40, 60 and 80 times the bare rent). Piers pay more the more piers the owner has. Taxes and fines go into the fountain; whoever lands there takes the pot. Cards come from Chance and the card shops, at most %d in hand, played in your turn before the roll or after the move. Who cannot pay sells buildings, then property, to the bank at half price; who still cannot is out. The game ends when one player is left, or after the last round: the most worth (cash, property at its price, buildings at their cost) wins.`

// Question is what the model is asked: one of the moves (Options), as
// Instructions say, the rules and who it is, judging State, the game as its
// player sees it (its own cards, not the others').
type Question struct {
	Instructions, State string
	Options             []string
}

// Ask is the question for the player whose turn it is, choosing one of
// moves.
func Ask(g *games.Fortune, moves []Move) Question {
	q := Question{
		Instructions: fmt.Sprintf(rules, games.StartPay, games.HandMax) + fmt.Sprintf("\n\nYou are %s. Pick the move that gives you the best chance to win.", g.Players[g.Turn].Name),
		State:        state(g),
	}
	for _, m := range moves {
		q.Options = append(q.Options, m.Text)
	}
	return q
}

// state is the game as the player whose turn it is sees it.
func state(g *games.Fortune) string {
	pi := g.Turn
	var b strings.Builder
	fmt.Fprintf(&b, "It is round %d of %d. The fountain's pot is $%d.\n\nPlayers:\n", g.Round, g.Rounds, g.Pot)
	for i, p := range g.Players {
		if p.Out {
			fmt.Fprintf(&b, "- %s: bankrupt, out of the game\n", p.Name)
			continue
		}
		who := p.Name
		if i == pi {
			who += " (you)"
		}
		var states []string
		if p.InJail {
			states = append(states, "in jail")
		}
		if p.Asleep {
			states = append(states, "misses the next turn")
		}
		if p.Turtle > 0 {
			states = append(states, fmt.Sprintf("walks one square for %d more rolls", p.Turtle))
		}
		if p.RentFree {
			states = append(states, "the next rent is free")
		}
		state := ""
		if len(states) > 0 {
			state = ", " + strings.Join(states, ", ")
		}
		fmt.Fprintf(&b, "- %s: cash $%d, worth $%d, %d cards, on %s%s\n", who, p.Cash, g.Worth(i), len(p.Cards), square(g, p.At), state)
	}
	for i, p := range g.Players {
		var own []string
		for sq, o := range g.Owner {
			if o == i {
				own = append(own, square(g, sq))
			}
		}
		if len(own) > 0 && !p.Out {
			fmt.Fprintf(&b, "\n%s owns: %s\n", p.Name, strings.Join(own, "; "))
		}
	}
	groups := map[int]bool{}
	for sq, o := range g.Owner {
		if o == pi && games.Board[sq].Kind == games.Street {
			groups[games.Board[sq].Group] = true
		}
	}
	for gr := range 24 {
		if !groups[gr] || g.HasGroup(pi, gr) {
			continue
		}
		var rest []string
		for _, sq := range games.Group(gr) {
			if g.Owner[sq] != pi {
				rest = append(rest, square(g, sq))
			}
		}
		fmt.Fprintf(&b, "\nGroup %d, which you have begun, also has: %s\n", gr+1, strings.Join(rest, "; "))
	}
	if hand := g.Players[pi].Cards; len(hand) > 0 {
		b.WriteString("\nYour cards:\n")
		for _, c := range hand {
			fmt.Fprintf(&b, "- %s: %s\n", games.Cards[c].Name, games.Cards[c].Text)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// GiveUp is how many turns in a row the model may fail before the
// computer plays its seats for the rest of the game.
const GiveUp = 3

// Timeout is how long the model has for a move.
var Timeout = 90 * time.Second

// Driver plays the AI players of one game after another. Its methods are
// called where the game lives, and Run brings answers back there.
type Driver struct {
	choose   func(ctx context.Context, profile, instructions, state string, options []string) (int, error)
	run      func(func())
	profiles []string // each AI player's, in their order; none, the default

	game   *games.Fortune
	busy   bool
	fails  int  // turns in a row the model failed
	off    bool // given up on the model for this game
	ctx    context.Context
	cancel context.CancelFunc
}

// New is a driver asking profiles with choose (the index of the option
// picked), run bringing its answers back to where the game lives.
func New(choose func(ctx context.Context, profile, instructions, state string, options []string) (int, error), run func(func())) *Driver {
	return &Driver{choose: choose, run: run}
}

// SetProfiles names the profile each AI player asks, in their order in the
// game; one left out asks the default.
func (d *Driver) SetProfiles(profiles []string) { d.profiles = slices.Clone(profiles) }

// profile is the profile player pi of g asks: as the AI players before it.
func (d *Driver) profile(g *games.Fortune, pi int) string {
	k := 0
	for _, p := range g.Players[:pi] {
		if p.AI {
			k++
		}
	}
	if k < len(d.profiles) {
		return d.profiles[k]
	}
	return ""
}

// Stop stops asking: the game is over, or closed.
func (d *Driver) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
	d.game, d.busy, d.cancel = nil, false, nil
}

// Next makes the next move of g's player whose turn it is, when that is
// an AI player, after wait (for the board to show what happened); then
// done, where the game lives, for it to show the move. Nothing while a
// move is being chosen.
func (d *Driver) Next(g *games.Fortune, wait time.Duration, done func()) {
	if g != d.game {
		d.Stop()
		d.game, d.fails, d.off = g, 0, false
		d.ctx, d.cancel = context.WithCancel(context.Background())
	}
	if d.busy || g.Phase == games.PhaseOver || !g.Players[g.Turn].AI || g.Players[g.Turn].Out {
		return
	}
	moves := Moves(g)
	if len(moves) == 0 {
		return
	}
	d.busy = true
	ctx, profile := d.ctx, d.profile(g, g.Turn)
	var q *Question
	if len(moves) > 1 && !d.off {
		a := Ask(g, moves)
		q = &a
	}
	go func() {
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		i, err := 0, error(nil)
		if q != nil {
			actx, cancel := context.WithTimeout(ctx, Timeout)
			i, err = d.choose(actx, profile, q.Instructions, q.State, q.Options)
			cancel()
		}
		d.run(func() {
			if ctx.Err() != nil || d.game != g {
				return
			}
			d.busy = false
			d.move(g, moves, q, i, err)
			done()
		})
	}()
}

// move makes move i of those asked about in q, or has the computer finish
// the turn.
func (d *Driver) move(g *games.Fortune, moves []Move, q *Question, i int, err error) {
	if now := Moves(g); !slices.Equal(now, moves) || q != nil && state(g) != q.State {
		return // the game moved on meanwhile
	}
	name := g.Players[g.Turn].Name
	if d.off {
		g.PlayOn("")
		return
	}
	if q != nil {
		if err != nil || i < 0 || i >= len(moves) {
			d.fails++
			if d.fails >= GiveUp {
				d.off = true
				g.PlayOn(i18n.Tf("The AI keeps not answering: the computer plays for %s from now on.", name))
			} else {
				g.PlayOn(i18n.Tf("No answer from the AI: the computer plays this turn for %s.", name))
			}
			return
		}
		d.fails = 0
	}
	if !Apply(g, moves[i]) {
		g.PlayOn("")
	}
}
