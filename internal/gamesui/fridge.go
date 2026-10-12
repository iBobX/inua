package gamesui

import (
	"fmt"
	"image/color"
	"math"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/games"
)

// fullFridge is Full Fridge: pick a grocery from the bag, turn it, and
// put it in the fridge where its shadow shows it fits.
type fullFridge struct {
	f       *frame
	g       *games.Fridge
	cv      *canvas
	level   int
	fridges *choices
	packed  *gtk.Label
	hand    int // the food held, -1 none
	rot     int
	pointer games.Vec
	note    string
}

const (
	fCell   = 62.0
	fWall   = 14.0
	fDivide = 10.0 // between the freezer and the shelves
	bagCell = 18.0
	bagW    = 128.0
	bagH    = 82.0
)

var foodColors = []color.NRGBA{
	{0xD9, 0x8F, 0x8F, 255}, {0x8D, 0xB8, 0x6B, 255}, {0x7F, 0xA6, 0xC9, 255}, {0xC9, 0x8F, 0xB0, 255},
	{0xE6, 0xE1, 0xD3, 255}, {0xD9, 0xA4, 0x41, 255}, {0xB8, 0xA7, 0x8A, 255}, {0xD9, 0x6A, 0x55, 255},
	{0x6B, 0xA8, 0x7A, 255}, {0x8C, 0x7F, 0xC9, 255}, {0x4E, 0x9C, 0x9A, 255},
}

func (r *fullFridge) setup(f *frame) {
	r.f = f
	f.win.SetDefaultSize(860, 760)
	r.packed = f.stat("PACKED")
	r.cv = newCanvas(1, 1, r.paint)
	f.add(r.cv)
	var names []string
	for i := range games.FridgeLevels {
		names = append(names, fmt.Sprintf("Fridge %d", i+1))
	}
	r.fridges = f.choices(names, func(i int) {
		r.level = i
		r.reset()
	})
	r.cv.clicks(r.click)
	r.cv.hovers(func(at games.Vec) {
		r.pointer = at
		if r.hand >= 0 {
			r.cv.redraw()
		}
	})
	f.keyDown = func(k uint32) bool {
		switch k {
		case 'r':
			r.turn()
		case 0xff1b: // Escape
			r.hand = -1
			r.update()
		default:
			return false
		}
		return true
	}
}

func (r *fullFridge) reset() {
	r.g = games.NewFridge(r.level)
	r.hand, r.rot, r.note = -1, 0, ""
	r.cv.w = r.fridgeW() + 28 + 2*bagW
	r.cv.h = max(r.fridgeH(), float64((len(r.g.Foods)+1)/2)*bagH+30)
	r.fridges.set(r.level)
	r.update()
}

func (r *fullFridge) redraw() { r.cv.redraw() }

func (r *fullFridge) fridgeW() float64 { return float64(r.g.W)*fCell + 2*fWall }
func (r *fullFridge) fridgeH() float64 { return float64(r.g.H)*fCell + 2*fWall + fDivide }

// cellPos is the top left of a fridge square.
func (r *fullFridge) cellPos(p games.Point) (float64, float64) {
	y := fWall + float64(p.Y)*fCell
	if p.Y >= r.g.Cold {
		y += fDivide
	}
	return fWall + float64(p.X)*fCell, y
}

// cellAt is the fridge square under a point; false outside.
func (r *fullFridge) cellAt(v games.Vec) (games.Point, bool) {
	y := v.Y - fWall
	if y >= float64(r.g.Cold)*fCell+fDivide {
		y -= fDivide
	}
	p := games.Point{X: int(math.Floor((v.X - fWall) / fCell)), Y: int(math.Floor(y / fCell))}
	return p, p.Inside(r.g.W, r.g.H)
}

// corner is where the held food's top left goes for the pointer at p:
// the food is held by its middle.
func (r *fullFridge) corner(p games.Point) games.Point {
	w, h := 0, 0
	for _, q := range games.Rotate(r.g.Foods[r.hand].Shape, r.rot) {
		w, h = max(w, q.X+1), max(h, q.Y+1)
	}
	return games.Point{X: p.X - (w-1)/2, Y: p.Y - (h-1)/2}
}

// bagSlot is the top left of food i's place in the bag.
func (r *fullFridge) bagSlot(i int) (float64, float64) {
	return r.fridgeW() + 28 + float64(i%2)*bagW, 30 + float64(i/2)*bagH
}

func (r *fullFridge) click(button int, at games.Vec) {
	g := r.g
	r.note = ""
	if g.Result != games.Playing {
		return
	}
	if p, ok := r.cellAt(at); ok {
		switch {
		case r.hand >= 0 && button == 3:
			r.turn()
			return
		case r.hand >= 0:
			if g.Place(r.hand, r.rot, r.corner(p)) {
				r.hand = -1
			} else {
				r.note = r.why(r.corner(p))
			}
		default:
			if f := g.Remove(p); f >= 0 {
				r.hand, r.rot = f, 0
			}
		}
		r.update()
		return
	}
	for i := range g.Foods {
		x, y := r.bagSlot(i)
		if g.Placed[i] || at.X < x-6 || at.X >= x+bagW-6 || at.Y < y-6 || at.Y >= y+bagH-6 {
			continue
		}
		if r.hand == i {
			r.turn()
			return
		}
		r.hand, r.rot = i, 0
		r.update()
		return
	}
	if button == 3 && r.hand >= 0 {
		r.turn()
	}
}

func (r *fullFridge) turn() {
	if r.hand >= 0 {
		r.rot = (r.rot + 1) % 4
		r.cv.redraw()
	}
}

// why says what stops the held food going in at corner.
func (r *fullFridge) why(corner games.Point) string {
	food := r.g.Foods[r.hand]
	for _, d := range games.Rotate(food.Shape, r.rot) {
		p := corner.Add(d)
		if !p.Inside(r.g.W, r.g.H) {
			return "It sticks out of the fridge there."
		}
		if r.g.Cells[p.Y*r.g.W+p.X] != 0 {
			return "Something is already there."
		}
		if (p.Y < r.g.Cold) != food.Frozen {
			if food.Frozen {
				return food.Name + " is frozen: it goes in the freezer, at the top."
			}
			return food.Name + " is fresh: it goes on the shelves, below the freezer."
		}
	}
	return ""
}

func (r *fullFridge) update() {
	g := r.g
	n := 0
	for _, p := range g.Placed {
		if p {
			n++
		}
	}
	r.packed.SetText(fmt.Sprintf("%d / %d", n, len(g.Foods)))
	switch {
	case g.Result == games.Won:
		r.f.say("Everything is in, and not a square to spare.", "good")
	case r.note != "":
		r.f.say(r.note, "bad")
	case r.hand >= 0:
		zone := "the shelves"
		if g.Foods[r.hand].Frozen {
			zone = "the freezer"
		}
		r.f.say(fmt.Sprintf("Holding %s, for %s. R or right-click turns it; Escape puts it back.", g.Foods[r.hand].Name, zone), "")
	default:
		r.f.say("Pick a grocery from the bag. Frozen food goes in the freezer at the top; click packed food to take it out.", "")
	}
	r.cv.redraw()
}

// block draws food i's squares at x, y, joined into one piece.
func block(pn pen, cells []games.Point, size float64, at func(games.Point) (float64, float64), col color.NRGBA, a float64) {
	has := map[games.Point]bool{}
	for _, p := range cells {
		has[p] = true
	}
	// Each square reaches over to its neighbours of the same food, so
	// that the piece is one block; a gap stays only between foods.
	in := size * 0.06
	pn.Alpha(col, a)
	for _, p := range cells {
		x, y := at(p)
		x0, y0, x1, y1 := x+in, y+in, x+size-in, y+size-in
		if has[p.Add(games.Point{X: -1})] {
			x0 = x
		}
		if has[p.Add(games.Point{Y: -1})] {
			y0 = y
		}
		if q := p.Add(games.Point{X: 1}); has[q] {
			x1, _ = at(q)
		}
		if q := p.Add(games.Point{Y: 1}); has[q] {
			_, y1 = at(q)
		}
		pn.Rect(x0, y0, x1-x0, y1-y0)
		pn.Fill()
	}
}

func (r *fullFridge) paint(pn pen) {
	c, g := r.f.col, r.g
	fw, fh := r.fridgeW(), r.fridgeH()
	cold := mix(c.surface, c.accent, 0.12)
	// The fridge: its body, the freezer and the shelves.
	pn.Color(mix(c.surface2, c.t0, 0.1))
	pn.RoundRect(0, 0, fw, fh, 16)
	pn.Fill()
	coldH := float64(g.Cold) * fCell
	pn.Color(cold)
	pn.RoundRect(fWall-4, fWall-4, float64(g.W)*fCell+8, coldH+8, 6)
	pn.Fill()
	pn.Color(c.surface)
	pn.RoundRect(fWall-4, fWall+coldH+fDivide-4, float64(g.W)*fCell+8, float64(g.H-g.Cold)*fCell+8, 6)
	pn.Fill()
	pn.Text("FREEZER", "Geist Mono, IBM Plex Mono Bold 8", fw-fWall, fWall+coldH-4, 1, 1, mix(cold, c.accent, 0.6))
	for y := range g.H {
		for x := range g.W {
			px, py := r.cellPos(games.Point{X: x, Y: y})
			if y < g.Cold {
				pn.Width(1)
				pn.Alpha(c.accent, 0.25)
				cx, cy := px+fCell/2, py+fCell/2
				for a := 0.0; a < math.Pi; a += math.Pi / 3 {
					pn.Line(cx-5*math.Cos(a), cy-5*math.Sin(a), cx+5*math.Cos(a), cy+5*math.Sin(a))
				}
			} else {
				if y > g.Cold && x == 0 {
					pn.Width(2)
					pn.Color(mix(c.surface, c.t2, 0.3))
					pn.Line(fWall, py, fWall+float64(g.W)*fCell, py)
				}
				pn.Alpha(c.t2, 0.25)
				pn.Circle(px+fCell/2, py+fCell/2, 1.5)
				pn.Fill()
			}
		}
	}

	// Packed food.
	for f := range g.Foods {
		if !g.Placed[f] {
			continue
		}
		var cells []games.Point
		for i, v := range g.Cells {
			if v == f+1 {
				cells = append(cells, games.Point{X: i % g.W, Y: i / g.W})
			}
		}
		block(pn, cells, fCell, r.cellPos, foodColors[f%len(foodColors)], 1)
		mid := cells[len(cells)/2]
		x, y := r.cellPos(mid)
		pn.TextBox(g.Foods[f].Name, "IBM Plex Sans Medium 9", x+fCell/2, y+fCell/2-8, fCell-6, c.app)
	}

	// The held food's shadow, where it would go.
	if r.hand >= 0 {
		if p, ok := r.cellAt(r.pointer); ok {
			corner := r.corner(p)
			var cells []games.Point
			for _, d := range games.Rotate(g.Foods[r.hand].Shape, r.rot) {
				if q := corner.Add(d); q.Inside(g.W, g.H) {
					cells = append(cells, q)
				}
			}
			col := foodColors[r.hand%len(foodColors)]
			if !g.Fits(r.hand, r.rot, corner) {
				col = c.danger
			}
			block(pn, cells, fCell, r.cellPos, col, 0.55)
		}
	}

	// The bag: what is still to go in.
	bx := fw + 28
	pn.Text("IN THE BAG", "Geist Mono, IBM Plex Mono Bold 9", bx, 6, 0, 0, c.t2)
	for i, food := range g.Foods {
		if g.Placed[i] {
			continue
		}
		x, y := r.bagSlot(i)
		if r.hand == i {
			pn.Alpha(c.accent, 0.12)
			pn.RoundRect(x-6, y-6, bagW-8, bagH-6, 6)
			pn.Fill()
			pn.Width(1.5)
			pn.Color(c.accent)
			pn.RoundRect(x-6, y-6, bagW-8, bagH-6, 6)
			pn.Stroke()
		}
		rot := 0
		if r.hand == i {
			rot = r.rot
		}
		shape := games.Rotate(food.Shape, rot)
		w, h := 0, 0
		for _, q := range shape {
			w, h = max(w, q.X+1), max(h, q.Y+1)
		}
		size := min(bagCell, 40/float64(h), (bagW-24)/float64(w))
		block(pn, shape, size, func(p games.Point) (float64, float64) {
			return x + float64(p.X)*size, y + 22 + float64(p.Y)*size
		}, foodColors[i%len(foodColors)], 1)
		mark := "fresh"
		if food.Frozen {
			mark = "frozen"
		}
		nw, _ := pn.Text(food.Name, "IBM Plex Sans Medium 10", x, y, 0, 0, c.t0)
		pn.Text(mark, "Geist Mono, IBM Plex Mono 8", x+nw+6, y+3, 0, 0, c.t2)
	}
	if g.Result == games.Won {
		r.f.veil(pn, r.cv.w, r.cv.h, "All packed", "Pick another fridge above, or N to pack this one again", c.ok)
	}
}
