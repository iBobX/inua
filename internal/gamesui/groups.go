package gamesui

import (
	"fmt"
	"strconv"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/gtkx"
)

// oddGroups is Odd Groups: twelve words, three groups of four to find,
// four mistakes allowed.
type oddGroups struct {
	f                         *frame
	g                         *games.Groups
	puzzle                    int
	puzzles                   *choices
	solvedBox, wordsBox, dots gtk.Box
	submit, deselect, shuffle *gtk.Button
	found, mistakes           *gtk.Label
	order                     []int // the groups in the order found
	onWord, onSubmit, onClear func(gtk.Button)
	onShuffle                 func(gtk.Button)
}

func (o *oddGroups) setup(f *frame) {
	o.f = f
	f.win.SetDefaultSize(640, 680)
	o.found = f.stat("FOUND")
	o.mistakes = f.stat("MISTAKES LEFT")
	var names []string
	for i := range games.GroupPuzzles {
		names = append(names, fmt.Sprint(i+1))
	}
	o.puzzles = f.choices(names, func(i int) {
		o.puzzle = i
		o.reset()
	})
	col := gtk.NewBox(gtk.OrientationVerticalValue, 8)
	col.AddCssClass("groups")
	col.SetHalign(gtk.AlignCenterValue)
	col.SetValign(gtk.AlignCenterValue)
	col.SetVexpand(true)
	o.solvedBox = *gtk.NewBox(gtk.OrientationVerticalValue, 8)
	gtkx.Append(col, &o.solvedBox.Widget)
	o.wordsBox = *gtk.NewBox(gtk.OrientationVerticalValue, 8)
	gtkx.Append(col, &o.wordsBox.Widget)

	o.dots = *gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	o.dots.SetHalign(gtk.AlignCenterValue)
	o.dots.AddCssClass("group-dots")
	gtkx.Append(col, &o.dots.Widget)

	row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	row.SetHalign(gtk.AlignCenterValue)
	row.AddCssClass("group-actions")
	o.onShuffle = func(gtk.Button) { o.g.Shuffle(); o.update() }
	o.onClear = func(gtk.Button) { o.g.Selected = [12]bool{}; o.update() }
	o.onSubmit = func(gtk.Button) { o.submitFour() }
	o.shuffle = button("Shuffle", &o.onShuffle)
	o.deselect = button("Deselect all", &o.onClear)
	o.submit = button("Submit", &o.onSubmit)
	o.submit.RemoveCssClass("outlined")
	o.submit.AddCssClass("primary")
	for _, b := range []*gtk.Button{o.shuffle, o.deselect, o.submit} {
		gtkx.Append(row, &b.Widget)
	}
	gtkx.Append(col, &row.Widget)
	gtkx.Append(&f.body, &col.Widget)

	o.onWord = func(b gtk.Button) {
		id, _ := strconv.Atoi(b.GetName())
		o.g.Toggle(id)
		o.g.Message = ""
		o.update()
	}
	f.keyDown = func(k uint32) bool {
		switch k {
		case keyEnter:
			o.submitFour()
		case 0xff08: // Backspace
			o.onClear(gtk.Button{})
		default:
			return false
		}
		return true
	}
}

func (o *oddGroups) reset() {
	o.g = games.NewGroups(o.puzzle)
	o.order = nil
	o.puzzles.set(o.puzzle)
	o.update()
}

func (o *oddGroups) redraw() {}

func (o *oddGroups) submitFour() {
	before := o.g.Solved
	o.g.Submit()
	for i := range o.g.Solved {
		if o.g.Solved[i] && !before[i] {
			o.order = append(o.order, i)
		}
	}
	o.update()
}

// bar is a found group: its name over its words.
func (o *oddGroups) bar(group, tone int, missed bool) {
	w := games.GroupPuzzles[o.g.Puzzle][group]
	box := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	box.AddCssClass("group-bar")
	box.AddCssClass(fmt.Sprintf("tone-%d", tone))
	if missed {
		box.AddCssClass("missed")
	}
	t := gtk.NewLabel(strings.ToUpper(w.Title))
	t.AddCssClass("group-title")
	gtkx.Append(box, &t.Widget)
	words := gtk.NewLabel(strings.Join(w.Words[:], ", "))
	words.AddCssClass("group-words")
	gtkx.Append(box, &words.Widget)
	gtkx.Append(&o.solvedBox, &box.Widget)
}

func (o *oddGroups) update() {
	g := o.g
	gtkx.Clear(&o.solvedBox)
	for n, i := range o.order {
		o.bar(i, n, false)
	}
	if g.Result == games.Lost {
		for i, s := range g.Solved {
			if !s {
				o.bar(i, 3, true)
			}
		}
	}

	gtkx.Clear(&o.wordsBox)
	var row *gtk.Box
	n := 0
	for _, id := range g.Order {
		if g.Solved[id/4] || g.Result == games.Lost {
			continue
		}
		if n%4 == 0 {
			row = gtk.NewBox(gtk.OrientationHorizontalValue, 8)
			row.SetHomogeneous(true)
			gtkx.Append(&o.wordsBox, &row.Widget)
		}
		n++
		b := gtk.NewButtonWithLabel(games.GroupPuzzles[g.Puzzle][id/4].Words[id%4])
		b.SetName(strconv.Itoa(id))
		b.AddCssClass("word")
		if g.Selected[id] {
			b.AddCssClass("on")
		}
		b.SetSensitive(g.Result == games.Playing)
		b.ConnectClicked(&o.onWord)
		gtkx.Append(row, &b.Widget)
	}

	gtkx.Clear(&o.dots)
	for i := range 4 {
		d := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		d.AddCssClass("group-dot")
		if i < 4-g.Mistakes {
			d.AddCssClass("on")
		}
		gtkx.Append(&o.dots, &d.Widget)
	}

	picked := 0
	for _, s := range g.Selected {
		if s {
			picked++
		}
	}
	solved := 0
	for _, s := range g.Solved {
		if s {
			solved++
		}
	}
	o.found.SetText(fmt.Sprintf("%d / 3", solved))
	o.mistakes.SetText(fmt.Sprint(4 - g.Mistakes))
	o.submit.SetSensitive(g.Result == games.Playing && picked == 4)
	o.deselect.SetSensitive(g.Result == games.Playing && picked > 0)
	o.shuffle.SetSensitive(g.Result == games.Playing)
	switch {
	case g.Result == games.Won:
		o.f.say(fmt.Sprintf("All three found, with %d mistake%s. Pick another puzzle above.", g.Mistakes, map[bool]string{true: "s"}[g.Mistakes != 1]), "good")
	case g.Result == games.Lost:
		o.f.say("Out of guesses. These were the groups.", "bad")
	case g.Message == "One word away.":
		o.f.say("One word away…", "bad")
	case strings.HasPrefix(g.Message, "Not a group"):
		o.f.say("Not a group. Look for another connection.", "bad")
	case g.Message != "" && solved > 0:
		o.f.say("Found: "+g.Message+".", "good")
	default:
		o.f.say(fmt.Sprintf("Pick four words that belong together, then submit. %d picked.", picked), "")
	}
}
