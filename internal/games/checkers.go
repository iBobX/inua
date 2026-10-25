package games

import (
	"cmp"
	"slices"
)

// Chinese checkers (Stern-Halma): a six-pointed star of 121 holes, two to
// six players each starting with ten marbles in a point of the star and
// racing them to the point across. A marble steps to a neighbouring hole,
// or hops over a neighbouring marble (anyone's) into the hole beyond, and
// on from there as long as it can. Players may play in teams: a team wins
// when all its players are home, and a player home moves a teammate's
// marbles on its turns.

// Hole is a hole of the star in cube coordinates: X+Y+Z == 0. Rows run
// across the screen, Z down them; the star's top point is where Z < -4.
type Hole struct{ X, Y, Z int }

// StarHoles are the board's 121 holes, row by row from the top point, left
// to right: a hole's number is its place here.
var StarHoles = func() []Hole {
	var out []Hole
	for z := -8; z <= 8; z++ {
		for x := -8; x <= 8; x++ {
			h := Hole{x, -x - z, z}
			if starHole(h) {
				out = append(out, h)
			}
		}
	}
	return out
}()

// starHole: the star is the union of two triangles, one with every
// coordinate at most 4, the other with every coordinate at least -4.
func starHole(h Hole) bool {
	return (h.X <= 4 && h.Y <= 4 && h.Z <= 4) || (h.X >= -4 && h.Y >= -4 && h.Z >= -4)
}

var holeIndex = func() map[Hole]int {
	m := make(map[Hole]int, len(StarHoles))
	for i, h := range StarHoles {
		m[h] = i
	}
	return m
}()

// HoleAt is the number of the hole at h, -1 off the board.
func HoleAt(h Hole) int {
	if i, ok := holeIndex[h]; ok {
		return i
	}
	return -1
}

// starDirs are the six ways to a neighbouring hole, clockwise from the
// right as the board is drawn.
var starDirs = [6]Hole{{1, -1, 0}, {0, -1, 1}, {-1, 0, 1}, {-1, 1, 0}, {0, 1, -1}, {1, 0, -1}}

func (h Hole) add(d Hole) Hole { return Hole{h.X + d.X, h.Y + d.Y, h.Z + d.Z} }

// starNeighbors are each hole's neighbours by way (starDirs), -1 where the
// board ends.
var starNeighbors = func() [][6]int {
	out := make([][6]int, len(StarHoles))
	for i, h := range StarHoles {
		for d, dir := range starDirs {
			out[i][d] = HoleAt(h.add(dir))
		}
	}
	return out
}()

// StarXY is where a hole is drawn, in units of the gap between holes, the
// board's middle at 0, 0, y down.
func StarXY(i int) (x, y float64) {
	h := StarHoles[i]
	return float64(h.X) + float64(h.Z)/2, float64(h.Z) * 0.8660254037844386
}

// Corners are the star's six points, clockwise from the top: a corner's
// opposite is three on.
const Corners = 6

// cornerAxis is how far towards corner k a hole lies: 8 at its tip, -8 at
// the opposite tip.
func cornerAxis(k int, h Hole) int {
	switch k {
	case 0:
		return -h.Z
	case 1:
		return h.X
	case 2:
		return -h.Y
	case 3:
		return h.Z
	case 4:
		return -h.X
	}
	return h.Y
}

// CornerOf is the point of the star a hole lies in, -1 in the middle.
func CornerOf(i int) int {
	h := StarHoles[i]
	for k := range Corners {
		if cornerAxis(k, h) > 4 {
			return k
		}
	}
	return -1
}

// CornerHoles are the ten holes of point k, its tip first, then row by row
// towards the middle.
func CornerHoles(k int) []int {
	var out []int
	for i := range StarHoles {
		if CornerOf(i) == k {
			out = append(out, i)
		}
	}
	slices.SortStableFunc(out, func(a, b int) int {
		return cmp.Compare(cornerAxis(k, StarHoles[b]), cornerAxis(k, StarHoles[a]))
	})
	return out
}

// Opposite is the point across the star from k.
func Opposite(k int) int { return (k + 3) % Corners }

// SeatCorners are the points the players start in, by how many play, in
// their order of play (clockwise): two face each other, three take every
// other point, four two facing pairs.
var SeatCorners = map[int][]int{
	2: {0, 3},
	3: {0, 2, 4},
	4: {1, 2, 4, 5},
	6: {0, 1, 2, 3, 4, 5},
}

// CheckersSeat is a player as the game is set up: a name and a team (the
// same number for partners; each its own otherwise).
type CheckersSeat struct {
	Name string
	Team int
}

// CheckersSettings set up a game: the players in their order of play (2,
// 3, 4 or 6), and the rules.
type CheckersSettings struct {
	Players []CheckersSeat
	// NoParking: a marble may cross another player's point but not stop
	// in it, only in its own start or goal; so no one can wall a goal in.
	NoParking bool
	// MaxRounds ends a game no one can finish; 0 for DefaultRounds.
	MaxRounds int
}

// DefaultRounds is how many rounds a game lasts at most.
const DefaultRounds = 300

// CheckersPlayer is a player of a game.
type CheckersPlayer struct {
	Name        string
	Team        int
	Start, Goal int  // the points they start in and race to
	Home        bool // every hole of the goal filled, some with their marbles
	Place       int  // 1 for the first home, …; 0 while not
	Moves, Hops int  // moves made, and hops in them
}

// CheckersEventKind is what happened.
type CheckersEventKind uint8

const (
	CkMove CheckersEventKind = iota // Player moved Owner's marble along Path
	CkPass                          // Player could not move, and passed
	CkHome                          // Player has every marble home
	CkWin                           // Team won: Players
	CkDraw                          // the rounds ran out
)

// CheckersEvent is something that happened, for the board to show.
type CheckersEvent struct {
	Kind          CheckersEventKind
	Player, Owner int
	Path          []int // the holes the marble went through, from where it was
	Team          int
	Players       []int
}

// Checkers is a game.
type Checkers struct {
	Players   []CheckersPlayer
	Holes     []int // whose marble is in each hole, -1 none
	Turn      int   // the player whose turn it is
	Round     int   // from 1
	Over      bool
	WinTeam   int   // the winning team, when Over with a winner; -1 for none
	Last      []int // the last move's path, for the board to mark
	NoParking bool
	MaxRounds int
	homes     int // players home so far
	events    []CheckersEvent
}

// NewCheckers starts a game: two, three, four or six players, each with
// ten marbles in their point.
func NewCheckers(s CheckersSettings) *Checkers {
	n := len(s.Players)
	if _, ok := SeatCorners[n]; !ok {
		n = 2
		for len(s.Players) < 2 {
			s.Players = append(s.Players, CheckersSeat{Name: "Player", Team: len(s.Players)})
		}
		s.Players = s.Players[:2]
	}
	g := &Checkers{Holes: make([]int, len(StarHoles)), Round: 1, WinTeam: -1, NoParking: s.NoParking, MaxRounds: cmp.Or(s.MaxRounds, DefaultRounds)}
	for i := range g.Holes {
		g.Holes[i] = -1
	}
	for i, seat := range s.Players[:n] {
		k := SeatCorners[n][i]
		g.Players = append(g.Players, CheckersPlayer{Name: seat.Name, Team: seat.Team, Start: k, Goal: Opposite(k)})
		for _, h := range CornerHoles(k) {
			g.Holes[h] = i
		}
	}
	return g
}

// Take hands over what happened since it was last asked.
func (g *Checkers) Take() []CheckersEvent {
	e := g.events
	g.events = nil
	return e
}

// teamOf are the players of a team.
func (g *Checkers) teamOf(team int) []int {
	var out []int
	for i, p := range g.Players {
		if p.Team == team {
			out = append(out, i)
		}
	}
	return out
}

// Movers are the players whose marbles the player whose turn it is may
// move: their own, or once home their teammates' still racing.
func (g *Checkers) Movers() []int {
	if g.Over {
		return nil
	}
	p := g.Players[g.Turn]
	if !p.Home {
		return []int{g.Turn}
	}
	var out []int
	for _, i := range g.teamOf(p.Team) {
		if !g.Players[i].Home {
			out = append(out, i)
		}
	}
	return out
}

// Marbles are a player's marbles, the holes they are in.
func (g *Checkers) Marbles(player int) []int {
	var out []int
	for h, o := range g.Holes {
		if o == player {
			out = append(out, h)
		}
	}
	return out
}

// mayStop reports whether owner's marble may end a move in hole h.
func (g *Checkers) mayStop(owner, h int) bool {
	if !g.NoParking {
		return true
	}
	k := CornerOf(h)
	return k < 0 || k == g.Players[owner].Start || k == g.Players[owner].Goal
}

// Reach is where the marble in hole from may go this turn: each hole it
// may stop in, with the way there (from first). Steps go to a neighbouring
// hole; hops chain, never through a hole twice.
func (g *Checkers) Reach(from int) map[int][]int {
	out := map[int][]int{}
	if from < 0 || from >= len(g.Holes) || g.Holes[from] < 0 || !slices.Contains(g.Movers(), g.Holes[from]) {
		return out
	}
	owner := g.Holes[from]
	for _, n := range starNeighbors[from] {
		if n >= 0 && g.Holes[n] < 0 && g.mayStop(owner, n) {
			out[n] = []int{from, n}
		}
	}
	// Hops, breadth first: the shortest way to each hole.
	paths := map[int][]int{from: {from}}
	queue := []int{from}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		for d, over := range starNeighbors[at] {
			if over < 0 || g.Holes[over] < 0 {
				continue
			}
			land := starNeighbors[over][d]
			if land < 0 || g.Holes[land] >= 0 || land == from {
				continue
			}
			if _, seen := paths[land]; seen {
				continue
			}
			paths[land] = append(slices.Clone(paths[at]), land)
			queue = append(queue, land)
		}
	}
	for h, p := range paths {
		if h != from && g.mayStop(owner, h) {
			if _, step := out[h]; !step {
				out[h] = p
			}
		}
	}
	return out
}

// CanMove reports whether the player whose turn it is has a move.
func (g *Checkers) CanMove() bool {
	for _, m := range g.Movers() {
		for _, h := range g.Marbles(m) {
			if len(g.Reach(h)) > 0 {
				return true
			}
		}
	}
	return false
}

// Move moves the marble in hole from to hole to, for the player whose turn
// it is; false when it may not go there.
func (g *Checkers) Move(from, to int) bool {
	path, ok := g.Reach(from)[to]
	if !ok {
		return false
	}
	owner := g.Holes[from]
	g.Holes[from], g.Holes[to] = -1, owner
	g.Last = path
	p := &g.Players[g.Turn]
	p.Moves++
	if len(path) > 2 || !adjacent(path[0], path[1]) {
		p.Hops += len(path) - 1
	}
	g.events = append(g.events, CheckersEvent{Kind: CkMove, Player: g.Turn, Owner: owner, Path: path})
	g.checkHome()
	g.next()
	return true
}

func adjacent(a, b int) bool { return slices.Contains(starNeighbors[a][:], b) }

// goalFull reports whether every hole of a player's goal is filled, some
// with their own marbles: home, even where another's marble sits in it.
func (g *Checkers) goalFull(player int) bool {
	own := false
	for _, h := range CornerHoles(g.Players[player].Goal) {
		switch g.Holes[h] {
		case -1:
			return false
		case player:
			own = true
		}
	}
	return own
}

// checkHome marks players come home, and ends the game when a team is.
func (g *Checkers) checkHome() {
	for i := range g.Players {
		p := &g.Players[i]
		if !p.Home && g.goalFull(i) {
			g.homes++
			p.Home, p.Place = true, g.homes
			g.events = append(g.events, CheckersEvent{Kind: CkHome, Player: i})
		}
	}
	for _, p := range g.Players {
		team := g.teamOf(p.Team)
		if !slices.ContainsFunc(team, func(i int) bool { return !g.Players[i].Home }) {
			g.Over, g.WinTeam = true, p.Team
			g.events = append(g.events, CheckersEvent{Kind: CkWin, Team: p.Team, Players: team})
			return
		}
	}
}

// next gives the turn to the next player with something to move; one who
// cannot move passes. When no one can, or the rounds run out, it is a draw.
func (g *Checkers) next() {
	if g.Over {
		return
	}
	for range len(g.Players) {
		g.Turn = (g.Turn + 1) % len(g.Players)
		if g.Turn == 0 {
			g.Round++
			if g.Round > g.MaxRounds {
				g.draw()
				return
			}
		}
		if len(g.Movers()) == 0 {
			continue
		}
		if !g.CanMove() {
			g.events = append(g.events, CheckersEvent{Kind: CkPass, Player: g.Turn})
			continue
		}
		return
	}
	g.draw()
}

func (g *Checkers) draw() {
	g.Over = true
	g.events = append(g.events, CheckersEvent{Kind: CkDraw})
}

// Distance is how far the marble in hole h is from the tip of goal k, in
// rows: 0 at the tip.
func Distance(k, h int) int { return 8 - cornerAxis(k, StarHoles[h]) }

// Left is how far a player's marbles have still to go, all told: 20 when
// they fill their goal.
func (g *Checkers) Left(player int) int {
	n := 0
	for _, h := range g.Marbles(player) {
		n += Distance(g.Players[player].Goal, h)
	}
	return n
}

// Standings are the players best first: home in order, then by the
// least left to go.
func (g *Checkers) Standings() []int {
	out := make([]int, len(g.Players))
	for i := range out {
		out[i] = i
	}
	slices.SortStableFunc(out, func(a, b int) int {
		pa, pb := g.Players[a], g.Players[b]
		switch {
		case pa.Home && pb.Home:
			return cmp.Compare(pa.Place, pb.Place)
		case pa.Home != pb.Home:
			if pa.Home {
				return -1
			}
			return 1
		}
		return cmp.Compare(g.Left(a), g.Left(b))
	})
	return out
}
