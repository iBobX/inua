package appui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lyricat/inua/internal/apps/games"
)

func (a *gamesApp) renderLast() {
	g := a.last
	a.setBoard(5, 5)
	for i := range g.Tiles {
		p := games.Point{X: i % 5, Y: i / 5}
		label, style := "", ""
		if !g.Tiles[i] {
			label, style = "·", "blocked"
		}
		if g.Gold[i] {
			label, style = "◆", "gold"
		}
		if p == g.Exit {
			label, style = "OUT", "exit"
		}
		if p == g.Player {
			label, style = "●", "player"
		}
		a.tile(i, label, style, fmt.Sprintf("Column %d, row %d", p.X+1, p.Y+1))
	}
	a.stats.SetText(fmt.Sprintf("MAP %02d / 10     MOVES %d / 10     COINS %d / 4", g.Level+1, g.Steps, g.Coins))
	a.status.SetText("Plan your exit before the floor disappears.")
	a.result(g.Result, fmt.Sprintf("Escaped with %d / 4 coins in %d moves.", g.Coins, g.Steps), "No way out. Restart to try a different route.")
	a.action("next", "Next map →", true)
}

func (a *gamesApp) renderOffice() {
	g := a.office
	a.setBoard(6, 6)
	guards, next := g.Guards(g.Steps), g.Guards(g.Steps+1)
	for i := range g.Walls {
		p := games.Point{X: i % 6, Y: i / 6}
		label, style := "", ""
		if g.Walls[i] {
			label, style = "■", "blocked"
		}
		if p == (games.Point{X: 5, Y: 0}) {
			label, style = "OUT", "exit"
		}
		for _, q := range next {
			if p == q {
				label, style = "+", "danger"
			}
		}
		for _, q := range guards {
			if p == q {
				label, style = "B", "danger"
			}
		}
		if p == g.Player {
			label, style = "●", "player"
		}
		a.tile(i, label, style, fmt.Sprintf("Column %d, row %d", p.X+1, p.Y+1))
	}
	a.stats.SetText(fmt.Sprintf("SHIFT %d / 3     TURNS %d / 30", g.Level+1, g.Steps))
	a.status.SetText("The + marks show where colleagues will move next.")
	a.result(g.Result, fmt.Sprintf("Out of office. You escaped in %d turns.", g.Steps), "Caught in another meeting. Try a different route or wait a turn.")
	a.action("wait", "Wait one turn", g.Result == games.Playing)
	a.action("next", "Next shift →", true)
}

func (a *gamesApp) renderFridge() {
	g := a.fridge
	a.setBoard(6, 5)
	count := 0
	for i, id := range g.Cells {
		label, style, tip := "❄", "cold", "Freezer: frozen food only"
		if i/6 >= 2 {
			label, style, tip = "·", "", "Fresh shelf: fresh food only"
		}
		if id > 0 {
			label, style, tip = games.Foods[id-1].Mark, "filled", games.Foods[id-1].Name
		}
		a.tile(i, label, style, tip)
	}
	for _, p := range g.Placed {
		if p {
			count++
		}
	}
	a.stats.SetText(fmt.Sprintf("PACKED %d / 9     TOP 2 ROWS: FROZEN     BOTTOM 3: FRESH", count))
	if g.Result == games.Playing {
		food := games.Foods[g.Selected]
		zone := "fresh"
		if food.Frozen {
			zone = "frozen"
		}
		shape := g.Shape()
		w, h := 0, 0
		for _, p := range shape {
			w, h = max(w, p.X+1), max(h, p.Y+1)
		}
		var preview []string
		for y := 0; y < h; y++ {
			row := ""
			for x := 0; x < w; x++ {
				cell := "· "
				for _, p := range shape {
					if p.X == x && p.Y == y {
						cell = "■ "
					}
				}
				row += cell
			}
			preview = append(preview, row)
		}
		a.status.SetText(fmt.Sprintf("Selected: %s (%s)\n%s", food.Name, zone, strings.Join(preview, "\n")))
	}
	a.result(g.Result, "Everything fits. The fridge is ready for the week.", "")
	a.action("rotate", "Rotate ↻", g.Result == games.Playing)
	removeLabel := "Pick up food"
	if a.removeFood {
		removeLabel = "Cancel pick-up"
		a.status.SetText("Click a placed food to pick it up.")
	}
	a.action("remove-food", removeLabel, count > 0)
	for i, food := range games.Foods {
		label := food.Mark + " · " + food.Name
		if g.Placed[i] {
			label = "✓ " + food.Name
		} else if g.Selected == i {
			label = "→ " + food.Name
		}
		a.action(fmt.Sprintf("food:%d", i), label, !g.Placed[i])
	}
}

func (a *gamesApp) renderShop() {
	g := a.shop
	a.board.SetVisible(false)
	a.stats.SetText(fmt.Sprintf("DAY %d / 7     CASH $%d     STOCK %d     CONDITION %d / 9", g.Day, g.Cash, g.Stock, g.Condition))
	jobNames := []string{"Stock", "Sales", "Repairs"}
	var jobs []string
	for i, d := range g.Jobs {
		value := "—"
		if d >= 0 {
			value = fmt.Sprintf("die %d (%d)", d+1, g.Dice[d])
		}
		jobs = append(jobs, jobNames[i]+": "+value)
	}
	a.status.SetText(strings.Join(jobs, "\n") + "\n\n" + g.Message)
	a.result(g.Result, fmt.Sprintf("A good first week. You finished with $%d cash.", g.Cash), fmt.Sprintf("Week over: $%d cash. The target was $65. Try a different allocation.", g.Cash))
	for i, die := range g.Dice {
		label := fmt.Sprintf("Die %d: %s %d", i+1, string(rune('⚀'+die-1)), die)
		if i == a.selectedDie {
			label = "→ " + label
		}
		a.action(fmt.Sprintf("die:%d", i), label, g.Result == games.Playing)
	}
	for i, name := range jobNames {
		a.action(fmt.Sprintf("job:%d", i), "Assign to "+name, g.Result == games.Playing)
	}
	ready := g.Result == games.Playing
	for _, d := range g.Jobs {
		ready = ready && d >= 0
	}
	a.action("day", "Open for the day →", ready)
}

func (a *gamesApp) renderGroups() {
	g := a.groups
	a.setBoard(3, 4)
	for i, id := range g.Order {
		style := "word"
		if g.Selected[id] {
			style += " selected"
		}
		if g.Solved[id/4] {
			style += " exit"
		}
		word := games.GroupPuzzles[g.Puzzle][id/4].Words[id%4]
		a.tile(i, word, style, word)
		a.tiles[i].SetSensitive(!g.Solved[id/4] && g.Result == games.Playing)
	}
	solved := 0
	var answers []string
	for i, group := range games.GroupPuzzles[g.Puzzle] {
		if g.Solved[i] {
			solved++
		}
		if g.Solved[i] || g.Result != games.Playing {
			answers = append(answers, group.Title+": "+strings.Join(group.Words[:], ", "))
		}
	}
	a.stats.SetText(fmt.Sprintf("PUZZLE %d / %d     GROUPS %d / 3     MISTAKES %d / 4", g.Puzzle+1, len(games.GroupPuzzles), solved, g.Mistakes))
	message := g.Message
	if g.Result == games.Won {
		message = "All three connections found."
	}
	if g.Result == games.Lost {
		message = "No guesses left. Here are the connections:"
	}
	a.status.SetText(message + "\n" + strings.Join(answers, "\n"))
	a.action("submit", "Submit four", g.Result == games.Playing)
	a.action("deselect", "Deselect", g.Result == games.Playing)
	a.action("shuffle", "Shuffle", g.Result == games.Playing)
	a.action("next", "Next puzzle →", true)
}

func (a *gamesApp) renderMines() {
	g := a.mines
	a.setBoard(g.Width, g.Height)
	flags, safe := 0, 0
	for i := range g.Open {
		label, style, tip := "", "small", "Hidden square"
		if g.Flag[i] {
			flags++
			label, style, tip = "⚑", "small gold", "Flagged square"
		}
		if g.Open[i] {
			style, tip = "small revealed", "Empty square"
			if !g.Bomb[i] {
				safe++
			}
			if n := g.Around(i); n > 0 {
				label = strconv.Itoa(n)
				tip = fmt.Sprintf("%d neighboring mines", n)
			} else {
				label = "·"
			}
		}
		if g.Bomb[i] && (g.Open[i] || g.Result != games.Playing) {
			label, style, tip = "✹", "small danger", "Mine"
		}
		if g.Flag[i] && !g.Bomb[i] && g.Result == games.Lost {
			label, style, tip = "×", "small danger", "Incorrect flag"
		}
		a.tile(i, label, style, fmt.Sprintf("Row %d, column %d: %s", i/g.Width+1, i%g.Width+1, tip))
	}
	a.stats.SetText(fmt.Sprintf("MINES %d     FLAGS %d     SAFE %d / %d", g.Count, flags, safe, g.Width*g.Height-g.Count))
	mode := "Reveal"
	if a.flagMode {
		mode = "Flag"
	}
	a.status.SetText("Mode: " + mode + ". The first reveal is always safe.")
	a.result(g.Result, "Field cleared. Every safe square is open.", "A mine exploded. Restart for a new field.")
	a.action("flag", "Mode: "+mode, g.Result == games.Playing)
	difficulty := "12 × 12 · 24 mines"
	if a.level%2 == 1 {
		difficulty = "9 × 9 · 10 mines"
	}
	a.action("next", difficulty, true)
}

// fortuneIndex maps the perimeter of the 6×6 board clockwise from Start.
func fortuneIndex(x, y int) int {
	switch {
	case y == 5:
		return 5 - x
	case x == 0:
		return 10 - y
	case y == 0:
		return 10 + x
	case x == 5:
		return 15 + y
	}
	return -1
}

func (a *gamesApp) renderFortune() {
	g := a.fortune
	a.setBoard(6, 6)
	for n := 0; n < 36; n++ {
		i := fortuneIndex(n%6, n/6)
		if i < 0 {
			a.tile(n, "", "blocked small", "")
			a.tiles[n].SetVisible(false)
			continue
		}
		style, label := "small", games.StreetNames[i]
		p := g.Properties[i]
		if p.Owner == 1 {
			style += " owner"
			label += "\nY" + strings.Repeat("⌂", p.Level)
		}
		if p.Owner == 2 {
			style += " computer"
			label += "\nC" + strings.Repeat("⌂", p.Level)
		}
		if g.Players[0].Position == i {
			label += "\n● YOU"
		}
		if g.Players[1].Position == i {
			label += "\n◆ CPU"
		}
		a.tile(n, label, style, games.StreetNames[i])
	}
	a.stats.SetText(fmt.Sprintf("ROUND %d / 12     LAST ROLL %d\nYOU: $%d cash · $%d worth     CPU: $%d cash · $%d worth", g.Round, g.Die, g.Players[0].Cash, g.Worth(0), g.Players[1].Cash, g.Worth(1)))
	message := strings.Join(g.Log, "\n")
	if message == "" {
		message = "You go first. Roll the die to set off."
	}
	if g.Result != games.Playing {
		outcome := "Draw."
		if g.Winner == 0 {
			outcome = "You win."
		} else if g.Winner == 1 {
			outcome = "The computer wins."
		}
		message = fmt.Sprintf("%s You: $%d total worth · Computer: $%d.\n\n%s", outcome, g.Worth(0), g.Worth(1), message)
	}
	a.status.SetText(message)
	a.action("roll", "Roll die", g.Result == games.Playing && g.Phase == "roll")
	price := games.PropertyPrice(g.Players[0].Position)
	label := fmt.Sprintf("Buy · $%d", price)
	if g.Phase == "upgrade" {
		label = fmt.Sprintf("Build house · $%d", price/2)
	}
	a.action("buy", label, g.Result == games.Playing && (g.Phase == "buy" || g.Phase == "upgrade"))
	a.action("end", "End turn →", g.Result == games.Playing && g.Phase != "roll")
}

func (a *gamesApp) arcadeStats() {
	if a.current == "harbor" {
		a.stats.SetText(fmt.Sprintf("COURSE %d / 3     TIME %.1f / 60 s", a.boat.Level+1, a.boat.Elapsed))
	}
	if a.current == "space-pinball" {
		a.stats.SetText(fmt.Sprintf("SCORE %06d     BALLS %d / 3", a.pinball.Score, a.pinball.Lives))
	}
}

func (a *gamesApp) renderArcade() {
	a.arcadeStats()
	label := "Start / resume"
	if a.running {
		label = "Pause"
	}
	if a.current == "harbor" {
		a.field.SetSizeRequest(360, 350)
		a.status.SetText("Ready at the harbor. Press Start, then hold Space to turn.")
		if a.running {
			a.status.SetText("Release to sail straight. Enter the berth heading right →.")
		} else if a.boat.Elapsed > 0 {
			a.status.SetText("Paused. Resume when you are ready.")
		}
		a.result(a.boat.Result, fmt.Sprintf("Safely docked in %.1f seconds.", a.boat.Elapsed), "Voyage ended. Restart and give the shore and rocks more room.")
		a.action("pause", label, a.boat.Result == games.Playing)
		a.action("next", "Next course →", true)
	} else {
		a.field.SetSizeRequest(360, 480)
		a.status.SetText("Ready for launch. Press Space or Launch ball.")
		if a.pinball.Active {
			if a.running {
				a.status.SetText("A / ← and D / → control the flippers. P pauses.")
			} else {
				a.status.SetText("Paused. Resume to continue this ball.")
			}
		}
		if a.pinball.Result != games.Playing {
			a.status.SetText(fmt.Sprintf("Orbit complete. Final score: %d. Restart for three fresh balls.", a.pinball.Score))
		}
		a.action("launch", "Launch ball", !a.pinball.Active && a.pinball.Result == games.Playing)
		a.action("pause", label, a.pinball.Active)
	}
	a.drawArcade()
}
