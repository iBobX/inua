package games

// Clock Out: cross the office to the exit before the turns run out,
// without meeting a colleague. Colleagues walk their rounds one square
// for each of the player's moves or waits; stepping onto their next
// square, or swapping squares with one, is a meeting.

type officeMap struct {
	W, H        int
	Walls       []Point
	Routes      [][]Point // each a loop: the last square leads to the first
	Start, Exit Point
	Offset      int // where in their rounds the colleagues begin
	Turns       int
}

// The first three are drawn by hand; the last three were found by a
// search for offices whose quickest way out needs the most waiting and
// going round.
var officeMaps = []officeMap{
	{6, 6, []Point{{1, 1}, {1, 2}, {3, 3}, {4, 3}, {3, 0}},
		[][]Point{
			{{0, 3}, {1, 3}, {2, 3}, {2, 2}, {2, 1}, {2, 2}, {2, 3}, {1, 3}},
			{{3, 1}, {4, 1}, {5, 1}, {5, 2}, {4, 2}, {3, 2}},
		}, Point{0, 5}, Point{5, 0}, 0, 24},
	{6, 6, []Point{{1, 1}, {1, 2}, {3, 3}, {4, 3}, {3, 0}},
		[][]Point{
			{{0, 3}, {1, 3}, {2, 3}, {2, 2}, {2, 1}, {2, 2}, {2, 3}, {1, 3}},
			{{3, 1}, {4, 1}, {5, 1}, {5, 2}, {4, 2}, {3, 2}},
		}, Point{0, 5}, Point{5, 0}, 5, 24},
	{7, 6, []Point{{2, 1}, {2, 2}, {4, 3}, {4, 4}, {5, 1}},
		[][]Point{
			{{0, 3}, {1, 3}, {2, 3}, {3, 3}, {2, 3}, {1, 3}},
			{{6, 1}, {6, 2}, {6, 3}, {6, 4}, {6, 3}, {6, 2}},
			{{3, 0}, {4, 0}, {5, 0}, {4, 0}},
		}, Point{0, 5}, Point{6, 0}, 8, 26},
	{7, 7, []Point{{3, 2}, {0, 0}, {1, 5}, {5, 2}, {4, 1}, {4, 0}},
		[][]Point{
			{{5, 4}, {6, 4}, {6, 3}, {5, 3}, {6, 3}, {6, 4}},
			{{5, 3}, {6, 3}, {6, 4}, {6, 3}},
			{{0, 3}, {0, 2}, {0, 1}, {0, 2}},
		}, Point{0, 6}, Point{6, 0}, 0, 30},
	{7, 7, []Point{{1, 0}, {3, 0}, {4, 2}, {2, 4}, {0, 3}, {4, 0}, {2, 6}, {1, 4}},
		[][]Point{
			{{0, 4}, {0, 5}, {1, 5}, {0, 5}},
			{{0, 5}, {1, 5}, {2, 5}, {3, 5}, {2, 5}, {1, 5}},
			{{5, 0}, {5, 1}, {4, 1}, {5, 1}},
		}, Point{0, 6}, Point{6, 0}, 0, 30},
	{7, 7, []Point{{6, 4}, {5, 5}, {6, 1}, {4, 1}, {4, 3}, {4, 4}, {5, 3}, {5, 4}, {3, 0}, {0, 1}},
		[][]Point{
			{{6, 2}, {5, 2}, {4, 2}, {5, 2}},
			{{2, 3}, {2, 2}, {3, 2}, {3, 3}, {3, 2}, {2, 2}},
			{{2, 4}, {2, 5}, {3, 5}, {2, 5}},
		}, Point{0, 6}, Point{6, 0}, 0, 32},
}

// OfficeLevels is how many offices there are.
var OfficeLevels = len(officeMaps)

type ClockOut struct {
	Level, Steps int
	officeMap
	Player Point
	Result Outcome
}

func NewClockOut(level int) *ClockOut {
	level %= len(officeMaps)
	m := officeMaps[level]
	return &ClockOut{Level: level, officeMap: m, Player: m.Start}
}

// Wall reports whether a desk stands on p.
func (g *ClockOut) Wall(p Point) bool {
	for _, w := range g.Walls {
		if w == p {
			return true
		}
	}
	return false
}

// Guards are where the colleagues are after step moves.
func (g *ClockOut) Guards(step int) []Point {
	out := make([]Point, len(g.Routes))
	for i, route := range g.Routes {
		out[i] = route[(step+g.Offset)%len(route)]
	}
	return out
}

// Move steps to a neighbouring square, or waits with p the player's own.
func (g *ClockOut) Move(p Point) bool {
	if g.Result != Playing || !p.Inside(g.W, g.H) || (p != g.Player && !adjacent(g.Player, p)) || g.Wall(p) {
		return false
	}
	before := g.Guards(g.Steps)
	old := g.Player
	g.Player = p
	g.Steps++
	for i, guard := range g.Guards(g.Steps) {
		if guard == p || (before[i] == p && guard == old) {
			g.Result = Lost
			return true
		}
	}
	if p == g.Exit {
		g.Result = Won
	} else if g.Steps >= g.Turns {
		g.Result = Lost
	}
	return true
}
