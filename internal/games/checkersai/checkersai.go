// Package checkersai plays seats of Chinese checkers (games.Checkers): the
// computer, at three levels, and the user's AI, which picks one of the
// best moves the computer finds, told the rules and the board as it
// stands. When the model does not answer, the computer makes that move;
// after a few such moves it plays the seat for the rest of the game. The
// model is asked elsewhere (inua-assistant --answer, a choice on Mister
// Morph's decision route): choose is given.
package checkersai

import (
	"cmp"
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/games"
)

// Level is how well the computer plays.
type Level uint8

const (
	Easy   Level = iota // the better half of its moves, by chance
	Normal              // the move that gains the most
	Hard                // the move that gains the most with the next one after it
)

// Move is a move of the turn's player: the marble in From to To, along
// Path, and how much it is worth to the marble's owner.
type Move struct {
	From, To int
	Path     []int
	Owner    int
	Gain     float64
}

// cost is how far a player's marbles are from home, as the computer judges
// it: the rows left, a marble far behind counting more, and a marble off
// the goal's middle line a little.
func cost(g *games.Checkers, player int) float64 {
	goal := g.Players[player].Goal
	c := 0.0
	for _, h := range g.Marbles(player) {
		d := float64(games.Distance(goal, h))
		c += d + 0.06*d*d + 0.35*float64(lateral(goal, h))
	}
	return c
}

// lateral is how far hole h lies off the line through goal k's tip and
// the board's middle.
func lateral(k, h int) int {
	p := games.StarHoles[h]
	var a, b int
	switch k % 3 {
	case 0:
		a, b = p.X, p.Y
	case 1:
		a, b = p.Y, p.Z
	default:
		a, b = p.Z, p.X
	}
	return max(a-b, b-a)
}

// Moves are every move of the player whose turn it is, with their gain to
// the marble's owner, the best first.
func Moves(g *games.Checkers) []Move {
	var out []Move
	for _, owner := range g.Movers() {
		before := cost(g, owner)
		for _, from := range g.Marbles(owner) {
			for to, path := range g.Reach(from) {
				g.Holes[from], g.Holes[to] = -1, owner
				gain := before - cost(g, owner)
				g.Holes[from], g.Holes[to] = owner, -1
				out = append(out, Move{From: from, To: to, Path: path, Owner: owner, Gain: gain})
			}
		}
	}
	slices.SortStableFunc(out, func(a, b Move) int {
		if c := cmp.Compare(b.Gain, a.Gain); c != 0 {
			return c
		}
		return cmp.Or(cmp.Compare(a.From, b.From), cmp.Compare(a.To, b.To))
	})
	return out
}

// Best is the computer's move at level, for the player whose turn it is;
// false when there is none.
func Best(g *games.Checkers, level Level, rng *rand.Rand) (Move, bool) {
	moves := Moves(g)
	if len(moves) == 0 {
		return Move{}, false
	}
	switch level {
	case Easy:
		return moves[rng.IntN((len(moves)+1)/2)], true
	case Hard:
		return lookAhead(g, moves), true
	}
	// Normal: the best, one of those as good by chance.
	n := 1
	for n < len(moves) && moves[n].Gain > moves[0].Gain-0.01 {
		n++
	}
	return moves[rng.IntN(n)], true
}

// lookAhead is the move whose gain, with the owner's best move after it
// (the others' moves between left out), is the most: a hop that opens a
// ladder for the next turn beats a longer one that does not.
func lookAhead(g *games.Checkers, moves []Move) Move {
	best, bestScore := moves[0], -1e9
	for _, m := range moves[:min(len(moves), 40)] {
		owner := m.Owner
		g.Holes[m.From], g.Holes[m.To] = -1, owner
		before := cost(g, owner)
		follow := 0.0
		for _, from := range g.Marbles(owner) {
			for to := range g.Reach(from) {
				g.Holes[from], g.Holes[to] = -1, owner
				follow = max(follow, before-cost(g, owner))
				g.Holes[from], g.Holes[to] = owner, -1
			}
		}
		g.Holes[m.From], g.Holes[m.To] = owner, -1
		if score := m.Gain + 0.6*follow; score > bestScore {
			best, bestScore = m, score
		}
	}
	return best
}

// Play makes the computer's move at level; false when there is none.
func Play(g *games.Checkers, level Level, rng *rand.Rand) bool {
	m, ok := Best(g, level, rng)
	return ok && g.Move(m.From, m.To)
}

// Choices is how many of the best moves the model chooses from.
const Choices = 12

// rules are the game's rules, in short, for the model.
const rules = `You play Chinese checkers. The board is a six-pointed star of holes; each player starts with ten marbles in one point of the star and races them into the point across. In a turn you move one marble: one step into an empty neighbouring hole, or hops over a neighbouring marble (anyone's) into the empty hole just beyond, chaining as many hops as you can. Marbles hopped over stay. A player is home when every hole of their goal point is filled. Partners play as a team, and a player home moves a partner's marbles. The first player, or team, all home wins.`

// Question is what the model is asked: one of Options, as Instructions
// say, judging State.
type Question struct {
	Instructions, State string
	Options             []string
}

// Ask is the question for the player whose turn it is, choosing one of
// moves (the best Choices of Moves).
func Ask(g *games.Checkers, moves []Move) Question {
	p := g.Players[g.Turn]
	q := Question{Instructions: rules + fmt.Sprintf("\n\nYou are %s. Pick the move that brings your marbles home soonest; hops that open a ladder for your next turn are worth more than they look.", p.Name)}
	var b strings.Builder
	fmt.Fprintf(&b, "Round %d.\n\nPlayers (rows each still has to go, all marbles told; 20 is home):\n", g.Round)
	for i, pl := range g.Players {
		who := pl.Name
		if i == g.Turn {
			who += " (you)"
		}
		state := fmt.Sprintf("%d rows to go", g.Left(i))
		if pl.Home {
			state = "home"
		}
		fmt.Fprintf(&b, "- %s, team %d: %s\n", who, pl.Team+1, state)
	}
	for _, owner := range g.Movers() {
		var far []string
		for _, h := range g.Marbles(owner) {
			far = append(far, fmt.Sprint(games.Distance(g.Players[owner].Goal, h)))
		}
		fmt.Fprintf(&b, "\n%s's marbles are these rows from the goal's tip: %s\n", g.Players[owner].Name, strings.Join(far, ", "))
	}
	q.State = strings.TrimRight(b.String(), "\n")
	for _, m := range moves {
		goal := g.Players[m.Owner].Goal
		how := "a step"
		if hops := len(m.Path) - 1; hops > 1 || games.Distance(goal, m.From)-games.Distance(goal, m.To) > 1 {
			how = fmt.Sprintf("%d hops", hops)
		}
		whose := ""
		if m.Owner != g.Turn {
			whose = g.Players[m.Owner].Name + "'s "
		}
		q.Options = append(q.Options, fmt.Sprintf("Move %smarble %d rows from the goal's tip to %d rows (%s, %s it off the goal's middle line)",
			whose, games.Distance(goal, m.From), games.Distance(goal, m.To), how, offBy(goal, m.To)))
	}
	return q
}

func offBy(goal, h int) string {
	if d := lateral(goal, h); d > 0 {
		return fmt.Sprintf("%d holes", d)
	}
	return "on"
}

// GiveUp is how many moves in a row the model may fail before the
// computer plays its seats for the rest of the game.
const GiveUp = 3

// Timeout is how long the model has for a move.
var Timeout = 60 * time.Second

// Driver plays AI seats with the model. Its methods are called where the
// game lives; run brings answers back there.
type Driver struct {
	choose func(ctx context.Context, profile, instructions, state string, options []string) (int, error)
	run    func(func())
	rng    *rand.Rand

	game   *games.Checkers
	busy   bool
	fails  int
	off    bool
	ctx    context.Context
	cancel context.CancelFunc
}

// New is a driver asking with choose (the index of the option picked),
// run bringing its answers back to where the game lives.
func New(choose func(ctx context.Context, profile, instructions, state string, options []string) (int, error), run func(func()), seed uint64) *Driver {
	return &Driver{choose: choose, run: run, rng: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
}

// Stop stops asking: the game is over, or closed.
func (d *Driver) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
	d.game, d.busy, d.cancel = nil, false, nil
}

// Next makes the move of the player whose turn it is in g, an AI seat
// asking profile, after wait (for the board to show what happened); then
// done, where the game lives. Nothing while a move is being chosen.
func (d *Driver) Next(g *games.Checkers, profile string, wait time.Duration, done func()) {
	if g != d.game {
		d.Stop()
		d.game, d.fails, d.off = g, 0, false
		d.ctx, d.cancel = context.WithCancel(context.Background())
	}
	if d.busy || g.Over {
		return
	}
	moves := Moves(g)
	if len(moves) == 0 {
		return
	}
	moves = moves[:min(len(moves), Choices)]
	turn, round := g.Turn, g.Round
	d.busy = true
	if d.off {
		time.AfterFunc(wait, func() {
			d.run(func() { d.finish(g, turn, round, Move{}, false, done) })
		})
		return
	}
	q := Ask(g, moves)
	ctx := d.ctx
	go func() {
		start := time.Now()
		cctx, cancel := context.WithTimeout(ctx, Timeout)
		i, err := d.choose(cctx, profile, q.Instructions, q.State, q.Options)
		cancel()
		if rest := wait - time.Since(start); rest > 0 {
			time.Sleep(rest)
		}
		d.run(func() {
			ok := err == nil && i >= 0 && i < len(moves)
			var m Move
			if ok {
				m = moves[i]
			}
			d.finish(g, turn, round, m, ok, done)
		})
	}()
}

// finish makes the chosen move, or the computer's when the model gave
// none, if the game is still where it was asked.
func (d *Driver) finish(g *games.Checkers, turn, round int, m Move, ok bool, done func()) {
	if g != d.game || !d.busy {
		return
	}
	d.busy = false
	if g.Over || g.Turn != turn || g.Round != round {
		return
	}
	if ok {
		d.fails = 0
		ok = g.Move(m.From, m.To)
	} else if !d.off {
		d.fails++
		d.off = d.fails >= GiveUp
	}
	if !ok {
		Play(g, Normal, d.rng)
	}
	done()
}
