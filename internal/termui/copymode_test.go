package termui

import "testing"

func TestWords(t *testing.T) {
	row := []rune("git rebase -i  origin/main")
	for _, c := range []struct{ from, next, prev int }{
		{0, 4, 0},
		{4, 11, 0},
		{11, 12, 4},
		{12, 15, 11},
		{15, 21, 12},
	} {
		if got := nextWord(row, c.from); got != c.next {
			t.Errorf("w from %d: %d, want %d", c.from, got, c.next)
		}
		if got := prevWord(row, c.from); got != c.prev {
			t.Errorf("b from %d: %d, want %d", c.from, got, c.prev)
		}
	}
}
