package termui

import "testing"

func TestCellClusters(t *testing.T) {
	type c struct {
		text string
		x, w int
	}
	for _, tc := range []struct {
		text  string
		cells int
		want  []c
		ok    bool
	}{
		{"ab", 2, []c{{"a", 0, 1}, {"b", 1, 1}}, true},
		{"日本語", 6, []c{{"日", 0, 2}, {"本", 2, 2}, {"語", 4, 2}}, true},
		{"aあb", 4, []c{{"a", 0, 1}, {"あ", 1, 2}, {"b", 3, 1}}, true},
		{"ｱ", 1, []c{{"ｱ", 0, 1}}, true}, // half-width katakana
		{"éx", 2, []c{{"é", 0, 1}, {"x", 1, 1}}, true},
		{"❤️", 2, []c{{"❤️", 0, 2}}, true},
		{"👍🏽", 2, []c{{"👍🏽", 0, 2}}, true},
		{"👩‍💻", 2, []c{{"👩‍💻", 0, 2}}, true},
		{"🇯🇵", 2, []c{{"🇯🇵", 0, 2}}, true},
		{"日本", 3, nil, false}, // the engine counted otherwise: drawn whole
	} {
		got, ok := cellClusters(tc.text, tc.cells)
		if ok != tc.ok {
			t.Errorf("%q: ok %v, want %v", tc.text, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("%q: %d clusters %+v, want %d", tc.text, len(got), got, len(tc.want))
			continue
		}
		for i, w := range tc.want {
			if g := got[i]; g.text != w.text || g.x != w.x || g.w != w.w {
				t.Errorf("%q #%d: %q at %d, %d wide; want %q at %d, %d wide", tc.text, i, g.text, g.x, g.w, w.text, w.x, w.w)
			}
		}
	}
}

func TestPreeditCells(t *testing.T) {
	cs, _ := cellClusters("にほんご", 0)
	for _, tc := range []struct{ at, cursor int }{{0, 0}, {1, 2}, {3, 6}, {4, 8}, {9, 8}} {
		cells, cursor := preeditCells(cs, tc.at)
		if cells != 8 || cursor != tc.cursor {
			t.Errorf("at %d: %d cells, cursor %d; want 8, %d", tc.at, cells, cursor, tc.cursor)
		}
	}
	if cells, cursor := preeditCells(nil, 0); cells != 0 || cursor != 0 {
		t.Errorf("empty: %d, %d", cells, cursor)
	}
}
