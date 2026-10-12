package games

import "math/rand/v2"

// Mines defers placement until the first reveal and protects that cell
// and its neighbors. Flags do not start the board or reveal its contents.
type Mines struct {
	Width, Height, Count int
	Bomb, Open, Flag     []bool
	Started              bool
	Result               Outcome
}

func NewMines(w, h, count int) *Mines {
	w, h = max(4, min(w, 30)), max(4, min(h, 30))
	return &Mines{Width: w, Height: h, Count: max(1, min(count, w*h-9)), Bomb: make([]bool, w*h), Open: make([]bool, w*h), Flag: make([]bool, w*h)}
}

func (g *Mines) neighbors(i int) []int {
	var out []int
	x, y := i%g.Width, i/g.Width
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			p := Point{x + dx, y + dy}
			if (dx != 0 || dy != 0) && p.Inside(g.Width, g.Height) {
				out = append(out, p.Y*g.Width+p.X)
			}
		}
	}
	return out
}

func (g *Mines) Around(i int) int {
	n := 0
	for _, j := range g.neighbors(i) {
		if g.Bomb[j] {
			n++
		}
	}
	return n
}

func (g *Mines) ToggleFlag(i int) {
	if i >= 0 && i < len(g.Open) && g.Result == Playing && !g.Open[i] {
		g.Flag[i] = !g.Flag[i]
	}
}

func (g *Mines) Reveal(i int) {
	if i < 0 || i >= len(g.Open) || g.Result != Playing || g.Flag[i] {
		return
	}
	if !g.Started {
		protected := map[int]bool{i: true}
		for _, j := range g.neighbors(i) {
			protected[j] = true
		}
		var candidates []int
		for j := range g.Bomb {
			if !protected[j] {
				candidates = append(candidates, j)
			}
		}
		rand.Shuffle(len(candidates), func(a, b int) { candidates[a], candidates[b] = candidates[b], candidates[a] })
		for _, j := range candidates[:g.Count] {
			g.Bomb[j] = true
		}
		g.Started = true
	}
	if g.Open[i] {
		flags := 0
		for _, j := range g.neighbors(i) {
			if g.Flag[j] {
				flags++
			}
		}
		if flags != g.Around(i) {
			return
		}
		for _, j := range g.neighbors(i) {
			if !g.Open[j] && !g.Flag[j] {
				g.flood(j)
			}
		}
	} else {
		g.flood(i)
	}
	if g.Result == Lost {
		return
	}
	for j := range g.Open {
		if !g.Bomb[j] && !g.Open[j] {
			return
		}
	}
	g.Result = Won
}

func (g *Mines) flood(i int) {
	if g.Result == Lost {
		return
	}
	queue := []int{i}
	for len(queue) > 0 {
		j := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if g.Open[j] || g.Flag[j] {
			continue
		}
		g.Open[j] = true
		if g.Bomb[j] {
			g.Result = Lost
			return
		}
		if g.Around(j) == 0 {
			queue = append(queue, g.neighbors(j)...)
		}
	}
}
