package layout

import (
	"encoding/json"
	"slices"
	"testing"
)

// three is 1 | (2 / 3): 1 on the left, 2 above 3 on the right.
func three() *Node {
	t := Leaf(1)
	t = SplitPane(t, 1, 2, Right)
	return SplitPane(t, 2, 3, Down)
}

func TestSplitAndRemove(t *testing.T) {
	tr := three()
	if got := tr.Panes(); !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("panes %v", got)
	}
	rs := Rects(tr, Rect{0, 0, 200, 100}, 0)
	if rs[1] != (Rect{0, 0, 100, 100}) || rs[2] != (Rect{100, 0, 100, 50}) || rs[3] != (Rect{100, 50, 100, 50}) {
		t.Errorf("rects %v", rs)
	}
	tr = Remove(tr, 2)
	if got := tr.Panes(); !slices.Equal(got, []int{1, 3}) {
		t.Errorf("after removing 2: %v", got)
	}
	if rs := Rects(tr, Rect{0, 0, 200, 100}, 0); rs[3] != (Rect{100, 0, 100, 100}) {
		t.Errorf("3 takes 2's place: %v", rs)
	}
	tr = Remove(tr, 1)
	tr = Remove(tr, 3)
	if tr != nil {
		t.Errorf("empty tree: %+v", tr)
	}
	if got := SplitPane(nil, 0, 7, Right); got.Pane != 7 {
		t.Errorf("first pane: %+v", got)
	}
}

func TestGap(t *testing.T) {
	rs := Rects(SplitPane(Leaf(1), 1, 2, Right), Rect{0, 0, 101, 50}, 1)
	if rs[1] != (Rect{0, 0, 50, 50}) || rs[2] != (Rect{51, 0, 50, 50}) {
		t.Errorf("rects with a gap %v", rs)
	}
}

func TestNeighbor(t *testing.T) {
	tr := three()
	for _, c := range []struct {
		from int
		d    Dir
		want int
	}{
		{1, RightOf, 2}, // both face it; 2 is first
		{2, Left, 1},
		{3, Left, 1},
		{2, Below, 3},
		{3, Up, 2},
		{1, Left, 0},
		{2, Up, 0},
	} {
		if got := Neighbor(tr, c.from, c.d); got != c.want {
			t.Errorf("Neighbor(%d, %d) = %d, want %d", c.from, c.d, got, c.want)
		}
	}
}

func TestSwapResizeEqualize(t *testing.T) {
	tr := Swap(three(), 1, 3)
	if got := tr.Panes(); !slices.Equal(got, []int{3, 2, 1}) {
		t.Errorf("swapped %v", got)
	}
	tr = three()
	tr = Resize(tr, 3, RightOf, 0.1) // the vertical edge: the root split
	if tr.Ratio != 0.6 {
		t.Errorf("root ratio %v", tr.Ratio)
	}
	tr = Resize(tr, 3, Up, 0.2) // the 2/3 split
	if r := tr.B.Ratio; r < 0.29 || r > 0.31 {
		t.Errorf("2/3 ratio %v", r)
	}
	for range 10 {
		tr = Resize(tr, 1, Left, 0.1)
	}
	if tr.Ratio != MinRatio {
		t.Errorf("clamped %v", tr.Ratio)
	}
	// Three in a row get a third each.
	row := SplitPane(SplitPane(Leaf(1), 1, 2, Right), 2, 3, Right)
	rs := Rects(Equalize(row), Rect{0, 0, 300, 10}, 0)
	if rs[1].W != 100 || rs[2].W != 100 || rs[3].W != 100 {
		t.Errorf("equalized %v", rs)
	}
}

func TestPresets(t *testing.T) {
	ps := []int{1, 2, 3, 4}
	r := Rect{0, 0, 400, 400}
	cols := Rects(Preset(ps, "columns"), r, 0)
	if cols[4] != (Rect{300, 0, 100, 400}) {
		t.Errorf("columns %v", cols)
	}
	rows := Rects(Preset(ps, "rows"), r, 0)
	if rows[2] != (Rect{0, 100, 400, 100}) {
		t.Errorf("rows %v", rows)
	}
	grid := Rects(Preset(ps, "grid"), r, 0)
	if grid[4] != (Rect{200, 200, 200, 200}) {
		t.Errorf("grid %v", grid)
	}
	main := Rects(Preset(ps, "main"), r, 0)
	if main[1] != (Rect{0, 0, 240, 400}) || main[4].X != 240 {
		t.Errorf("main %v", main)
	}
	if got := Preset([]int{9}, "grid"); got.Pane != 9 {
		t.Errorf("one pane %+v", got)
	}
}

func TestDividers(t *testing.T) {
	tr := three()
	ds := Dividers(tr, Rect{0, 0, 201, 101}, 1)
	if len(ds) != 2 || ds[0].Line != (Rect{100, 0, 1, 101}) {
		t.Fatalf("dividers %+v", ds)
	}
	ds[0].SetRatio(50.5, 1) // dragged to a quarter
	if r := tr.Ratio; r < 0.24 || r > 0.26 {
		t.Errorf("ratio after the drag %v", r)
	}
	if ds[1].Path != "b" || !SetRatioAt(tr, "b", 0.3) || tr.B.Ratio != 0.3 {
		t.Errorf("by path: %q %v", ds[1].Path, tr.B.Ratio)
	}
	if SetRatioAt(tr, "a", 0.3) {
		t.Error("a pane has no ratio")
	}
}

func TestJSON(t *testing.T) {
	b, err := json.Marshal(three())
	if err != nil {
		t.Fatal(err)
	}
	var back *Node
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if got := back.Panes(); !slices.Equal(got, []int{1, 2, 3}) || back.B.Split != Down {
		t.Errorf("round trip %s: %v", b, got)
	}
}
