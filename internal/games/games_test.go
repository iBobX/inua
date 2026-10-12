package games

import "testing"

func TestMinesFirstRevealAndWin(t *testing.T) {
	for _, first := range []int{0, 40, 80} {
		for range 30 {
			g := NewMines(9, 9, 10)
			g.ToggleFlag(first)
			g.Reveal(first)
			if g.Started {
				t.Fatal("flagged first square started the board")
			}
			g.ToggleFlag(first)
			g.Reveal(first)
			if g.Result == Lost || g.Around(first) != 0 {
				t.Fatal("first click not protected")
			}
			bombs := 0
			for i, b := range g.Bomb {
				if b {
					bombs++
				} else {
					g.Reveal(i)
				}
			}
			if bombs != 10 || g.Result != Won {
				t.Fatalf("bombs=%d result=%d", bombs, g.Result)
			}
		}
	}
}

func TestMinesWrongFlagChordLoses(t *testing.T) {
	g := NewMines(4, 4, 1)
	g.Started, g.Bomb[0], g.Open[5], g.Flag[1] = true, true, true, true
	g.Reveal(5)
	if g.Result != Lost {
		t.Fatal("chording with a wrong flag must expose the mine")
	}
	before := append([]bool(nil), g.Open...)
	g.Reveal(15)
	for i, v := range before {
		if g.Open[i] != v {
			t.Fatal("lost board changed")
		}
	}
}
