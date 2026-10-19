// Package layout is a session's tiling: a binary split tree whose leaves
// are panes, as in Ghostty and tmux. Each split divides its area right or
// down at a ratio. The operations return the tree to keep; a nil tree has
// no panes.
package layout

import "math"

// Split is how a node divides its area.
type Split string

const (
	Right Split = "right" // A on the left, B on the right
	Down  Split = "down"  // A above, B below
)

// Dir is a direction from a pane, for focus, swapping and resizing.
type Dir int

const (
	Left Dir = iota
	RightOf
	Up
	Below
)

// Node is a pane (a leaf, Pane set) or a split of two nodes.
type Node struct {
	Pane  int     `json:"pane,omitempty"`
	Split Split   `json:"split,omitempty"`
	Ratio float64 `json:"ratio,omitempty"` // A's share, 0 to 1
	A     *Node   `json:"a,omitempty"`
	B     *Node   `json:"b,omitempty"`
}

// MinRatio bounds a split, so no pane is resized away.
const MinRatio = 0.1

// Leaf is a tree of one pane.
func Leaf(pane int) *Node { return &Node{Pane: pane} }

func (n *Node) leaf() bool { return n.A == nil }

// Panes are the tree's panes, left to right and top to bottom.
func (n *Node) Panes() []int {
	if n == nil {
		return nil
	}
	if n.leaf() {
		return []int{n.Pane}
	}
	return append(n.A.Panes(), n.B.Panes()...)
}

// Has says pane is in the tree.
func (n *Node) Has(pane int) bool { return n.find(pane) != nil }

func (n *Node) find(pane int) *Node {
	if n == nil {
		return nil
	}
	if n.leaf() {
		if n.Pane == pane {
			return n
		}
		return nil
	}
	if f := n.A.find(pane); f != nil {
		return f
	}
	return n.B.find(pane)
}

// parent is the split holding child, nil for the root.
func (n *Node) parent(child *Node) *Node {
	if n == nil || n.leaf() {
		return nil
	}
	if n.A == child || n.B == child {
		return n
	}
	if p := n.A.parent(child); p != nil {
		return p
	}
	return n.B.parent(child)
}

// Clone is a deep copy.
func (n *Node) Clone() *Node {
	if n == nil {
		return nil
	}
	c := *n
	c.A, c.B = n.A.Clone(), n.B.Clone()
	return &c
}

// SplitPane puts pane beside target, right of it or below, sharing its
// area evenly. A target not in the tree leaves it as it is.
func SplitPane(root *Node, target, pane int, s Split) *Node {
	if root == nil {
		return Leaf(pane)
	}
	t := root.find(target)
	if t == nil {
		return root
	}
	old := *t
	*t = Node{Split: s, Ratio: 0.5, A: &old, B: Leaf(pane)}
	return root
}

// Remove takes pane out; its sibling takes the split's place.
func Remove(root *Node, pane int) *Node {
	t := root.find(pane)
	if t == nil {
		return root
	}
	p := root.parent(t)
	if p == nil {
		return nil
	}
	sib := p.A
	if sib == t {
		sib = p.B
	}
	*p = *sib
	return root
}

// Swap exchanges two panes' places.
func Swap(root *Node, a, b int) *Node {
	na, nb := root.find(a), root.find(b)
	if na != nil && nb != nil {
		na.Pane, nb.Pane = b, a
	}
	return root
}

// Toward is the direction from pane to the sibling that takes its place
// when it closes, and whether it has one.
func Toward(root *Node, pane int) (Dir, bool) {
	t := root.find(pane)
	p := root.parent(t)
	if p == nil {
		return 0, false
	}
	switch {
	case p.Split == Down && p.B == t:
		return Up, true
	case p.Split == Down:
		return Below, true
	case p.B == t:
		return Left, true
	}
	return RightOf, true
}

// Rect is an area in pixels, or any unit.
type Rect struct{ X, Y, W, H float64 }

// Rects lays the tree out in r, gap apart: where each pane goes.
func Rects(root *Node, r Rect, gap float64) map[int]Rect {
	out := map[int]Rect{}
	root.rects(r, gap, out)
	return out
}

func (n *Node) rects(r Rect, gap float64, out map[int]Rect) {
	if n == nil {
		return
	}
	if n.leaf() {
		out[n.Pane] = r
		return
	}
	a, b := n.halves(r, gap)
	n.A.rects(a, gap, out)
	n.B.rects(b, gap, out)
}

// halves is a split's two areas, gap between them.
func (n *Node) halves(r Rect, gap float64) (Rect, Rect) {
	ratio := clamp(n.Ratio)
	if n.Split == Down {
		h := math.Round((r.H - gap) * ratio)
		return Rect{r.X, r.Y, r.W, h}, Rect{r.X, r.Y + h + gap, r.W, r.H - h - gap}
	}
	w := math.Round((r.W - gap) * ratio)
	return Rect{r.X, r.Y, w, r.H}, Rect{r.X + w + gap, r.Y, r.W - w - gap, r.H}
}

// clamp is a ratio as used: an unset one (0) is half, and none goes past
// MinRatio of either side.
func clamp(r float64) float64 {
	if r == 0 {
		return 0.5
	}
	return bound(r)
}

func bound(r float64) float64 { return min(max(r, MinRatio), 1-MinRatio) }

// Neighbor is the pane next to pane in direction d, 0 for none: of the
// panes beyond its edge that face it, the nearest, then the one facing
// it most.
func Neighbor(root *Node, pane int, d Dir) int {
	rs := Rects(root, Rect{0, 0, 1000, 1000}, 0)
	from, ok := rs[pane]
	if !ok {
		return 0
	}
	best, bestDist, bestOver := 0, math.Inf(1), 0.0
	for p, r := range rs {
		if p == pane {
			continue
		}
		var dist, over float64
		switch d {
		case Left:
			dist, over = from.X-(r.X+r.W), overlap(from.Y, from.H, r.Y, r.H)
		case RightOf:
			dist, over = r.X-(from.X+from.W), overlap(from.Y, from.H, r.Y, r.H)
		case Up:
			dist, over = from.Y-(r.Y+r.H), overlap(from.X, from.W, r.X, r.W)
		case Below:
			dist, over = r.Y-(from.Y+from.H), overlap(from.X, from.W, r.X, r.W)
		}
		if dist < -0.5 || over <= 0 {
			continue
		}
		if dist < bestDist-0.5 || (math.Abs(dist-bestDist) <= 0.5 && (over > bestOver || over == bestOver && p < best)) {
			best, bestDist, bestOver = p, dist, over
		}
	}
	return best
}

func overlap(a, al, b, bl float64) float64 {
	return min(a+al, b+bl) - max(a, b)
}

// Resize moves the edge of pane's area that faces d by step (a share of
// the split's area): the nearest split across that direction holding the
// pane. Right and down grow the share on the left and top.
func Resize(root *Node, pane int, d Dir, step float64) *Node {
	want := Right
	if d == Up || d == Below {
		want = Down
	}
	for n := root.find(pane); n != nil; {
		p := root.parent(n)
		if p == nil {
			break
		}
		if p.Split == want {
			if d == RightOf || d == Below {
				p.Ratio = bound(clamp(p.Ratio) + step)
			} else {
				p.Ratio = bound(clamp(p.Ratio) - step)
			}
			break
		}
		n = p
	}
	return root
}

// Equalize gives the panes of each run of same-way splits equal shares.
func Equalize(root *Node) *Node {
	root.equalize()
	return root
}

func (n *Node) equalize() {
	if n == nil || n.leaf() {
		return
	}
	a, b := n.A.along(n.Split), n.B.along(n.Split)
	n.Ratio = float64(a) / float64(a+b)
	n.A.equalize()
	n.B.equalize()
}

// along is how many cells the node has in a row of splits of s.
func (n *Node) along(s Split) int {
	if n.leaf() || n.Split != s {
		return 1
	}
	return n.A.along(s) + n.B.along(s)
}

// Presets are the layouts Ctrl+Shift+L goes through, in order.
var Presets = []string{"columns", "rows", "grid", "main"}

// Preset lays panes out anew: side by side ("columns"), one above another
// ("rows"), in a grid, or one large on the left and the rest stacked
// beside it ("main").
func Preset(panes []int, kind string) *Node {
	switch {
	case len(panes) == 0:
		return nil
	case len(panes) == 1:
		return Leaf(panes[0])
	}
	switch kind {
	case "rows":
		return chain(panes, Down)
	case "grid":
		cols := int(math.Ceil(math.Sqrt(float64(len(panes)))))
		var rows []*Node
		for i := 0; i < len(panes); i += cols {
			rows = append(rows, chain(panes[i:min(i+cols, len(panes))], Right))
		}
		return chainNodes(rows, Down)
	case "main":
		return &Node{Split: Right, Ratio: 0.6, A: Leaf(panes[0]), B: chain(panes[1:], Down)}
	}
	return chain(panes, Right)
}

// chain is panes in a row of splits of s, with equal shares.
func chain(panes []int, s Split) *Node {
	nodes := make([]*Node, len(panes))
	for i, p := range panes {
		nodes[i] = Leaf(p)
	}
	return chainNodes(nodes, s)
}

func chainNodes(nodes []*Node, s Split) *Node {
	if len(nodes) == 1 {
		return nodes[0]
	}
	return &Node{Split: s, Ratio: 1 / float64(len(nodes)), A: nodes[0], B: chainNodes(nodes[1:], s)}
}

// Divider is the gap of one split, for dragging: its line in the layout
// and the area the split divides.
type Divider struct {
	Node  *Node
	Path  string // the split's place: "" the root, then "a" and "b" down
	Line  Rect   // the gap
	Total Rect   // the split's area
}

// Dividers are the tree's gaps laid out in r, gap wide.
func Dividers(root *Node, r Rect, gap float64) []Divider {
	var out []Divider
	root.dividers("", r, gap, &out)
	return out
}

func (n *Node) dividers(path string, r Rect, gap float64, out *[]Divider) {
	if n == nil || n.leaf() {
		return
	}
	a, b := n.halves(r, gap)
	line := Rect{a.X + a.W, r.Y, gap, r.H}
	if n.Split == Down {
		line = Rect{r.X, a.Y + a.H, r.W, gap}
	}
	*out = append(*out, Divider{Node: n, Path: path, Line: line, Total: r})
	n.A.dividers(path+"a", a, gap, out)
	n.B.dividers(path+"b", b, gap, out)
}

// SetRatioAt sets the ratio of the split at path, as Divider.Path names
// it; false when there is none there.
func SetRatioAt(root *Node, path string, ratio float64) bool {
	n := root
	for _, c := range path {
		if n == nil || n.leaf() {
			return false
		}
		if c == 'a' {
			n = n.A
		} else {
			n = n.B
		}
	}
	if n == nil || n.leaf() {
		return false
	}
	n.Ratio = bound(ratio)
	return true
}

// SetRatio puts a divider at pos (the x or y of its middle, in the
// layout's units).
func (d Divider) SetRatio(pos, gap float64) {
	if d.Node.Split == Down {
		d.Node.Ratio = bound((pos - d.Total.Y - gap/2) / (d.Total.H - gap))
	} else {
		d.Node.Ratio = bound((pos - d.Total.X - gap/2) / (d.Total.W - gap))
	}
}
