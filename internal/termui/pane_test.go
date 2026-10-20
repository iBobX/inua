package termui

import (
	"testing"

	"github.com/lyricat/inua/internal/term/vt"
)

// A line drawn is kept by what it draws: lines differing in a run's
// place, colours, attributes or text are drawn apart.
func TestLineKey(t *testing.T) {
	base := []vt.Run{{X: 0, Cells: 3, FG: 1, BG: vt.DefaultColor, Text: "abc"}, {X: 3, Cells: 2, FG: 2, Text: "de"}}
	vary := func(f func(r *vt.Run)) []vt.Run {
		out := append([]vt.Run(nil), base...)
		f(&out[1])
		return out
	}
	others := [][]vt.Run{
		vary(func(r *vt.Run) { r.X = 4 }),
		vary(func(r *vt.Run) { r.FG = 3 }),
		vary(func(r *vt.Run) { r.BG = 5 }),
		vary(func(r *vt.Run) { r.Attrs = vt.Selected }),
		vary(func(r *vt.Run) { r.Text = "dx" }),
		{{X: 0, Cells: 5, FG: 1, BG: vt.DefaultColor, Text: "abcde"}},
	}
	if lineKey(base) != lineKey(append([]vt.Run(nil), base...)) {
		t.Error("the same line, two keys")
	}
	for i, o := range others {
		if lineKey(o) == lineKey(base) {
			t.Errorf("line %d drawn as the first", i)
		}
	}
}
