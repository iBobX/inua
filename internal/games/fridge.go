package games

// Full Fridge: pack every grocery into the fridge. Frozen food goes in
// the freezer, the top rows; the rest on the shelves below. Each fridge
// has exactly the room its groceries need, so every square ends up full.

type Food struct {
	Name   string
	Shape  []Point
	Frozen bool
}

type FridgeLevel struct {
	W, H, Cold int // Cold: the freezer's rows, at the top
	Foods      []Food
}

var (
	shapeI2 = []Point{{0, 0}, {1, 0}}
	shapeI3 = []Point{{0, 0}, {1, 0}, {2, 0}}
	shapeI4 = []Point{{0, 0}, {1, 0}, {2, 0}, {3, 0}}
	shapeL3 = []Point{{0, 0}, {1, 0}, {0, 1}}
	shapeO4 = []Point{{0, 0}, {1, 0}, {0, 1}, {1, 1}}
	shapeL4 = []Point{{0, 0}, {0, 1}, {1, 1}, {2, 1}}
	shapeJ4 = []Point{{2, 0}, {0, 1}, {1, 1}, {2, 1}}
	shapeT4 = []Point{{0, 0}, {1, 0}, {2, 0}, {1, 1}}
	shapeS4 = []Point{{1, 0}, {2, 0}, {0, 1}, {1, 1}}
	shapeZ4 = []Point{{0, 0}, {1, 0}, {1, 1}, {2, 1}}
	shapeR6 = []Point{{0, 0}, {1, 0}, {2, 0}, {0, 1}, {1, 1}, {2, 1}}
)

var FridgeLevels = []FridgeLevel{
	{6, 5, 2, []Food{
		{"Ice cream", shapeL3, true}, {"Peas", shapeL3, true}, {"Fish", shapeI3, true}, {"Berries", shapeI3, true},
		{"Milk", shapeO4, false}, {"Eggs", shapeO4, false}, {"Tofu", shapeO4, false},
		{"Carrots", shapeI3, false}, {"Greens", shapeI3, false},
	}},
	{6, 6, 2, []Food{
		{"Pizza", shapeL4, true}, {"Fish fingers", shapeI4, true}, {"Dumplings", shapeJ4, true},
		{"Cheese", shapeL4, false}, {"Celery", shapeI4, false}, {"Milk", shapeO4, false}, {"Grapes", shapeS4, false},
		{"Yogurt", shapeO4, false}, {"Butter", shapeI2, false}, {"Lemons", shapeI2, false},
	}},
	{7, 6, 2, []Food{
		{"Ice cream", shapeO4, true}, {"Peas", shapeL3, true}, {"Berries", shapeL3, true}, {"Waffles", shapeT4, true},
		{"Cake", shapeR6, false}, {"Salad", shapeZ4, false}, {"Juice", shapeI4, false}, {"Eggs", shapeO4, false},
		{"Ham", shapeL4, false}, {"Carrots", shapeI3, false}, {"Herbs", shapeL3, false},
	}},
}

type Fridge struct {
	Level int
	FridgeLevel
	Cells  []int // 0 empty, else the food's index + 1
	Placed []bool
	Result Outcome
}

func NewFridge(level int) *Fridge {
	level %= len(FridgeLevels)
	l := FridgeLevels[level]
	return &Fridge{Level: level, FridgeLevel: l, Cells: make([]int, l.W*l.H), Placed: make([]bool, len(l.Foods))}
}

// Rotate turns a shape r quarter turns clockwise, back against the top
// left corner.
func Rotate(shape []Point, r int) []Point {
	out := append([]Point(nil), shape...)
	for range (r%4 + 4) % 4 {
		minX, minY := 1<<30, 1<<30
		for i, p := range out {
			out[i] = Point{-p.Y, p.X}
			minX, minY = min(minX, -p.Y), min(minY, p.X)
		}
		for i := range out {
			out[i].X -= minX
			out[i].Y -= minY
		}
	}
	return out
}

// Fits reports whether food, turned rot times, fits with its shape's
// top left corner at at.
func (g *Fridge) Fits(food, rot int, at Point) bool {
	if g.Result != Playing || food < 0 || food >= len(g.Foods) || g.Placed[food] {
		return false
	}
	for _, d := range Rotate(g.Foods[food].Shape, rot) {
		p := at.Add(d)
		if !p.Inside(g.W, g.H) || g.Cells[p.Y*g.W+p.X] != 0 || (p.Y < g.Cold) != g.Foods[food].Frozen {
			return false
		}
	}
	return true
}

// Place puts food in, if it fits.
func (g *Fridge) Place(food, rot int, at Point) bool {
	if !g.Fits(food, rot, at) {
		return false
	}
	for _, d := range Rotate(g.Foods[food].Shape, rot) {
		p := at.Add(d)
		g.Cells[p.Y*g.W+p.X] = food + 1
	}
	g.Placed[food] = true
	for _, p := range g.Placed {
		if !p {
			return true
		}
	}
	g.Result = Won
	return true
}

// Remove takes out the food on at, and gives its index; -1 for none.
func (g *Fridge) Remove(at Point) int {
	if !at.Inside(g.W, g.H) {
		return -1
	}
	id := g.Cells[at.Y*g.W+at.X]
	if id == 0 {
		return -1
	}
	for i, v := range g.Cells {
		if v == id {
			g.Cells[i] = 0
		}
	}
	g.Placed[id-1] = false
	g.Result = Playing
	return id - 1
}
