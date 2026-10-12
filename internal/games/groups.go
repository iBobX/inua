package games

import (
	"math/rand/v2"
	"slices"
)

type WordGroup struct {
	Title string
	Words [4]string
}

// Curated puzzles deliberately use concrete categories, avoiding obscure
// trivia and overlapping answers. Each puzzle has three groups of four.
var GroupPuzzles = [][3]WordGroup{
	{{"Chess pieces", [4]string{"ROOK", "PAWN", "BISHOP", "KNIGHT"}}, {"Pasta shapes", [4]string{"PENNE", "FUSILLI", "FARFALLE", "RIGATONI"}}, {"Cloud types", [4]string{"CIRRUS", "CUMULUS", "STRATUS", "NIMBUS"}}},
	{{"Keyboard keys", [4]string{"SHIFT", "ESCAPE", "TAB", "ENTER"}}, {"Tree nuts", [4]string{"ALMOND", "CASHEW", "PECAN", "WALNUT"}}, {"Sailing knots", [4]string{"BOWLINE", "CLOVE HITCH", "REEF KNOT", "SHEET BEND"}}},
	{{"Moon landmarks", [4]string{"CRATER", "MARE", "RILLE", "HIGHLANDS"}}, {"Coffee drinks", [4]string{"ESPRESSO", "LATTE", "MOCHA", "CAPPUCCINO"}}, {"Musical tempos", [4]string{"ADAGIO", "LARGO", "ALLEGRO", "PRESTO"}}},
	{{"Git commands", [4]string{"COMMIT", "REBASE", "FETCH", "CLONE"}}, {"Kitchen tools", [4]string{"WHISK", "LADLE", "SIEVE", "SPATULA"}}, {"Japanese scripts / writing", [4]string{"HIRAGANA", "KATAKANA", "KANJI", "ROMAJI"}}},
	{{"Board games", [4]string{"CHESS", "GO", "REVERSI", "CHECKERS"}}, {"Citrus fruit", [4]string{"LEMON", "LIME", "POMELO", "MANDARIN"}}, {"Planetary rings", [4]string{"JUPITER", "SATURN", "URANUS", "NEPTUNE"}}},
	{{"Bridge designs", [4]string{"SUSPENSION", "ARCH", "BEAM", "CANTILEVER"}}, {"Constellations", [4]string{"ORION", "LYRA", "CYGNUS", "DRACO"}}, {"Breads", [4]string{"BAGEL", "NAAN", "PITA", "BRIOCHE"}}},
	{{"Units of length", [4]string{"METRE", "INCH", "FATHOM", "FURLONG"}}, {"Teas", [4]string{"OOLONG", "SENCHA", "DARJEELING", "ASSAM"}}, {"Shades of blue", [4]string{"NAVY", "COBALT", "AZURE", "CERULEAN"}}},
	{{"Fabric weaves", [4]string{"PLAIN", "TWILL", "SATIN", "BASKET"}}, {"Percussion instruments", [4]string{"TIMPANI", "MARIMBA", "BONGO", "CYMBAL"}}, {"Web protocols", [4]string{"HTTP", "HTTPS", "WEBSOCKET", "WEBRTC"}}},
}

type Groups struct {
	Puzzle, Mistakes int
	Order            [12]int
	Selected         [12]bool // indexed by original word ID
	Solved           [3]bool
	Result           Outcome
	Message          string
	// Tried are the wrong guesses made: the same one again costs nothing.
	Tried [][12]bool
}

func NewGroups(puzzle int) *Groups {
	g := &Groups{Puzzle: puzzle % len(GroupPuzzles)}
	for i := range g.Order {
		g.Order[i] = i
	}
	g.Shuffle()
	return g
}

func (g *Groups) Shuffle() {
	rand.Shuffle(12, func(i, j int) { g.Order[i], g.Order[j] = g.Order[j], g.Order[i] })
}

func (g *Groups) Toggle(id int) {
	if id < 0 || id >= 12 || g.Result != Playing || g.Solved[id/4] {
		return
	}
	n := 0
	for _, s := range g.Selected {
		if s {
			n++
		}
	}
	if g.Selected[id] || n < 4 {
		g.Selected[id] = !g.Selected[id]
	}
}

func (g *Groups) Submit() {
	if g.Result != Playing {
		return
	}
	counts := [3]int{}
	n := 0
	for id, selected := range g.Selected {
		if selected {
			counts[id/4]++
			n++
		}
	}
	if n != 4 {
		g.Message = "Choose exactly four words."
		return
	}
	for i, count := range counts {
		if count == 4 {
			g.Solved[i] = true
			g.Selected = [12]bool{}
			g.Message = GroupPuzzles[g.Puzzle][i].Title
			if g.Solved == [3]bool{true, true, true} {
				g.Result = Won
			}
			return
		}
	}
	if slices.Contains(g.Tried, g.Selected) {
		g.Message = "Already tried."
		return
	}
	g.Tried = append(g.Tried, g.Selected)
	g.Mistakes++
	g.Message = "Not a group. Try another connection."
	for _, count := range counts {
		if count == 3 {
			g.Message = "One word away."
		}
	}
	if g.Mistakes >= 4 {
		g.Result = Lost
	}
}
