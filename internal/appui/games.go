package appui

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/games"
	"github.com/lyricat/inua/internal/gtkx"
)

var gameCatalog = []struct{ ID, Title, Summary, Rules, Help string }{
	{"last-tile", "Last Tile", "Ten steps. Four coins. One way out.", "Reach OUT in at most 10 moves. Every tile you leave collapses. Collect up to four coins before escaping.", "Arrow keys / WASD or click a neighboring tile. ● you · ◆ coin · OUT exit · · collapsed. Ten repeatable maps."},
	{"full-fridge", "Full Fridge", "A place for every last grocery.", "Fit all nine groceries. Frozen food belongs in the top two rows; fresh food in the bottom three. Select a food, rotate it, then click the top-left of its shape preview (even when that corner is a hole).", "Use Pick up food, then click a placed item to lift it. R rotates the selected food. The preview uses ■ for food and · for empty space."},
	{"clock-out", "Clock Out", "Make it home without another meeting.", "Reach OUT within 30 turns. Colleagues move after every step or wait. Avoid their destination and do not cross through them.", "Arrow keys / WASD or click to move. Space waits. ● you · B colleague · + their next tile · ■ desk. Three patrol timings."},
	{"harbor", "One-button Harbor", "A small boat. A careful arrival.", "Sail into the striped berth at the upper right, heading right →. Hold Space or the sea to turn clockwise; release to sail straight. Avoid rocks and shore. You have 60 seconds.", "Start first, then hold to turn. P pauses. Leaving this window automatically pauses the game. Three courses."},
	{"dice-shop", "Three Dice Shop", "Seven days to keep the lights on.", "Assign one die each to stock, sales and repairs. Stock costs $2 each. Sales earn $5 each, with condition / 3 extra customers. Repairs change condition by die − 3 (0–9). Rent is $6 per day. Finish day seven with at least $65 cash.", "Select a die, then its job. Each die can do one job. Stock arrives before sales; new condition applies the following day."},
	{"odd-groups", "Odd Groups", "Twelve words. Three connections.", "Find three groups of four related words. Select exactly four, then submit. Four wrong guesses end the puzzle.", "Six curated puzzles. Shuffle changes the layout, not the answers. Answers are revealed when the round ends."},
	{"minesweeper", "Minesweeper", "Clear the field, trust the numbers.", "Reveal every safe square. Numbers count mines in the eight neighboring squares. Your first reveal and its neighbors are safe.", "Right-click to flag, or use Flag mode (F). Click a revealed number with enough neighboring flags to open the rest. Incorrect flags can explode a mine."},
	{"space-pinball", "Space Pinball", "Three balls, one more orbit.", "Keep the ball in orbit with two flippers. Each bumper hit scores 100. Launch each of your three balls and aim for a high score.", "Space launches · A / ← left flipper · D / → right flipper · P pauses. Hold either half of the table to raise that flipper. Leaving this window pauses."},
	{"small-fortune", "Small Fortune", "Buy a street. Build a little fortune.", "Play 12 rounds against the computer. Pass Start for $120. Buy streets, build up to two houses, and collect rent. Chance changes your cash; tax costs $80; jail skips one turn. Cash below zero means bankruptcy.", "Roll, buy or build if offered, then end your turn. Highest cash + property value wins after round 12. YOU and CPU mark positions; Y/C mark ownership. Click a street for its price and rent."},
}

type gamesApp struct {
	initial, current                     string
	c                                    *Ctx
	win                                  *gtk.ApplicationWindow
	library, play                        gtk.Box
	heading, rules, stats, status, help  gtk.Label
	back, restart                        gtk.Button
	board, actions                       gtk.Grid
	field                                gtk.Picture
	tiles                                []*gtk.Button
	cols, rows, actionCount              int
	level, selectedDie                   int
	flagMode, running, turn, left, right bool
	removeFood                           bool
	lastTime                             time.Time
	timer                                uint32
	last                                 *games.LastTile
	fridge                               *games.Fridge
	office                               *games.ClockOut
	boat                                 *games.Harbor
	shop                                 *games.Shop
	groups                               *games.Groups
	mines                                *games.Mines
	pinball                              *games.Pinball
	fortune                              *games.Fortune
	notice                               string
	onAction                             func(gtk.Button)
	onKey                                func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onRelease                            func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType)
	onPress, onPointerRelease            func(gtk.GestureClick, int32, float64, float64)
	onPointerStop                        func(gtk.GestureClick)
	onFlag                               func(gtk.GestureClick, int32, float64, float64)
	onTick                               glib.SourceFunc
}

func (a *gamesApp) build(c *Ctx) *gtk.ApplicationWindow {
	a.c = c
	a.win = &gtk.ApplicationWindow{}
	u := c.Load("games.ui")
	u.Get("games", a.win)
	u.Get("library", &a.library)
	u.Get("play", &a.play)
	u.Get("heading", &a.heading)
	u.Get("rules", &a.rules)
	u.Get("stats", &a.stats)
	u.Get("status", &a.status)
	u.Get("help", &a.help)
	u.Get("back", &a.back)
	u.Get("restart", &a.restart)
	u.Get("board", &a.board)
	u.Get("actions", &a.actions)
	u.Get("field", &a.field)
	u.Close()
	a.onAction = func(b gtk.Button) { a.act(b.GetName()) }
	a.onFlag = func(click gtk.GestureClick, _ int32, _, _ float64) {
		if a.current != "minesweeper" {
			return
		}
		w := click.GetWidget()
		if w == nil {
			return
		}
		_, id, _ := strings.Cut(w.GetName(), ":")
		w.Unref()
		i, err := strconv.Atoi(id)
		if err == nil {
			a.mines.ToggleFlag(i)
			a.render()
		}
	}
	a.back.ConnectClicked(&a.onAction)
	a.restart.ConnectClicked(&a.onAction)
	intro := text("A little time to play.", "Nine small games. No accounts, no network, no waiting.")
	intro.AddCssClass("game-intro")
	gtkx.Append(&a.library, &intro.Widget)
	for i, g := range gameCatalog {
		row := text(fmt.Sprintf("%02d   %s", i+1, g.Title), g.Summary)
		b := rowButton("open:"+g.ID, &row.Widget, &a.onAction)
		row.Unref()
		b.AddCssClass("game-entry")
		gtkx.Append(&a.library, &b.Widget)
	}
	a.onKey = func(_ gtk.EventControllerKey, key, _ uint32, mod gdk.ModifierType) bool {
		if mod&(gdk.ControlMaskValue|gdk.AltMaskValue) != 0 {
			return false
		}
		return a.key(key, true)
	}
	a.onRelease = func(_ gtk.EventControllerKey, key, _ uint32, _ gdk.ModifierType) { a.key(key, false) }
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue)
	keys.ConnectKeyPressed(&a.onKey)
	keys.ConnectKeyReleased(&a.onRelease)
	a.win.AddController(&keys.EventController)
	a.onPress = func(_ gtk.GestureClick, _ int32, x, _ float64) {
		if a.current == "harbor" {
			a.turn = true
		}
		if a.current == "space-pinball" {
			if x < float64(a.field.GetWidth())/2 {
				a.left = true
			} else {
				a.right = true
			}
		}
	}
	a.onPointerRelease = func(_ gtk.GestureClick, _ int32, _, _ float64) { a.turn, a.left, a.right = false, false, false }
	a.onPointerStop = func(gtk.GestureClick) { a.turn, a.left, a.right = false, false, false }
	click := gtk.NewGestureClick()
	click.ConnectPressed(&a.onPress)
	click.ConnectReleased(&a.onPointerRelease)
	click.ConnectStopped(&a.onPointerStop)
	a.field.AddController(&click.EventController)
	a.onTick = func(uintptr) bool {
		now := time.Now()
		dt := now.Sub(a.lastTime).Seconds()
		a.lastTime = now
		if !a.running {
			a.timer = 0
			return false
		}
		if !a.win.IsActive() {
			a.running, a.turn, a.left, a.right = false, false, false, false
			a.render()
			a.timer = 0
			return false
		}
		switch a.current {
		case "harbor":
			a.boat.Step(dt, a.turn)
			if a.boat.Result != games.Playing {
				a.running = false
				a.render()
			}
		case "space-pinball":
			active := a.pinball.Active
			a.pinball.Step(dt, a.left, a.right)
			if active && !a.pinball.Active {
				a.running = false
				a.render()
			}
		}
		a.arcadeStats()
		a.drawArcade()
		if !a.running {
			a.timer = 0
		}
		return a.running
	}
	a.lastTime = time.Now()
	a.showPage(a.initial)
	return a.win
}

func (a *gamesApp) themeChanged() {
	if a.current == "harbor" || a.current == "space-pinball" {
		a.drawArcade()
	}
}

func (a *gamesApp) showPage(name string) {
	for _, g := range gameCatalog {
		if g.ID == name {
			a.current, a.level = name, 0
			a.reset()
			return
		}
	}
	a.current, a.running = "", false
	a.play.SetVisible(false)
	a.library.SetVisible(true)
	a.back.SetVisible(false)
	a.restart.SetVisible(false)
	a.heading.SetText("Games")
}

func (a *gamesApp) reset() {
	a.running, a.turn, a.left, a.right = false, false, false, false
	a.notice = ""
	a.selectedDie = 0
	a.removeFood = false
	switch a.current {
	case "last-tile":
		a.last = games.NewLastTile(a.level)
	case "full-fridge":
		a.fridge = &games.Fridge{}
	case "clock-out":
		a.office = games.NewClockOut(a.level)
	case "harbor":
		a.boat = games.NewHarbor(a.level)
	case "dice-shop":
		a.shop = games.NewShop()
	case "odd-groups":
		a.groups = games.NewGroups(a.level)
	case "minesweeper":
		a.flagMode = false
		if a.level%2 == 0 {
			a.mines = games.NewMines(9, 9, 10)
		} else {
			a.mines = games.NewMines(12, 12, 24)
		}
	case "space-pinball":
		a.pinball = games.NewPinball()
	case "small-fortune":
		a.fortune = games.NewFortune()
	}
	a.render()
}

func (a *gamesApp) setBoard(cols, rows int) {
	if cols == a.cols && rows == a.rows {
		return
	}
	for c := a.board.GetFirstChild(); c != nil; c = a.board.GetFirstChild() {
		a.board.Remove(c)
		c.Unref()
	}
	a.cols, a.rows, a.tiles = cols, rows, nil
	for i := 0; i < cols*rows; i++ {
		b := gtk.NewButton()
		b.AddCssClass("game-tile")
		b.SetName(fmt.Sprintf("cell:%d", i))
		b.ConnectClicked(&a.onAction)
		secondary := gtk.NewGestureClick()
		secondary.SetButton(3)
		secondary.ConnectPressed(&a.onFlag)
		b.AddController(&secondary.EventController)
		a.board.Attach(&b.Widget, int32(i%cols), int32(i/cols), 1, 1)
		a.tiles = append(a.tiles, b)
		b.Unref()
	}
}

func (a *gamesApp) tile(i int, label, style, tip string) {
	b := a.tiles[i]
	b.SetLabel(label)
	b.SetTooltipText(tip)
	for _, c := range []string{"player", "gold", "exit", "blocked", "danger", "selected", "cold", "filled", "revealed", "owner", "computer", "small", "word"} {
		b.RemoveCssClass(c)
	}
	for _, c := range strings.Fields(style) {
		b.AddCssClass(c)
	}
	b.SetSensitive(true)
	b.SetVisible(true)
}

func (a *gamesApp) action(name, label string, enabled bool) {
	b := gtk.NewButtonWithLabel(label)
	b.SetName(name)
	b.SetSensitive(enabled)
	b.ConnectClicked(&a.onAction)
	a.actions.Attach(&b.Widget, int32(a.actionCount%3), int32(a.actionCount/3), 1, 1)
	b.Unref()
	a.actionCount++
}

func (a *gamesApp) render() {
	a.library.SetVisible(false)
	a.play.SetVisible(true)
	a.back.SetVisible(true)
	a.restart.SetVisible(true)
	for _, g := range gameCatalog {
		if g.ID == a.current {
			a.heading.SetText(g.Title)
			a.rules.SetText(g.Rules)
			a.help.SetText(g.Help)
		}
	}
	arcade := a.current == "harbor" || a.current == "space-pinball"
	a.field.SetVisible(arcade)
	a.board.SetVisible(!arcade)
	for c := a.actions.GetFirstChild(); c != nil; c = a.actions.GetFirstChild() {
		a.actions.Remove(c)
		c.Unref()
	}
	a.actionCount = 0
	a.status.SetText("")
	a.status.RemoveCssClass("win")
	a.status.RemoveCssClass("loss")
	switch a.current {
	case "last-tile":
		a.renderLast()
	case "full-fridge":
		a.renderFridge()
	case "clock-out":
		a.renderOffice()
	case "dice-shop":
		a.renderShop()
	case "odd-groups":
		a.renderGroups()
	case "minesweeper":
		a.renderMines()
	case "small-fortune":
		a.renderFortune()
	case "harbor", "space-pinball":
		a.renderArcade()
	}
	if a.notice != "" {
		a.status.SetText(a.notice)
	}
}

func (a *gamesApp) result(r games.Outcome, win, loss string) {
	if r == games.Won {
		a.status.SetText(win)
		a.status.AddCssClass("win")
	}
	if r == games.Lost {
		a.status.SetText(loss)
		a.status.AddCssClass("loss")
	}
}

func (a *gamesApp) act(command string) {
	op, arg, _ := strings.Cut(command, ":")
	n, _ := strconv.Atoi(arg)
	a.notice = ""
	switch op {
	case "open":
		a.showPage(arg)
		return
	case "home":
		a.showPage("")
		return
	case "restart":
		a.reset()
		return
	case "next":
		a.level++
		a.reset()
		return
	case "cell":
		a.cellAction(n)
	case "rotate":
		a.fridge.Rotation = (a.fridge.Rotation + 1) % 4
	case "food":
		if !a.fridge.Placed[n] {
			a.fridge.Selected, a.fridge.Rotation = n, 0
			a.removeFood = false
		}
	case "remove-food":
		a.removeFood = !a.removeFood
	case "wait":
		a.office.Move(a.office.Player)
	case "die":
		a.selectedDie = n
	case "job":
		a.shop.Assign(n, a.selectedDie)
	case "day":
		a.shop.EndDay()
	case "submit":
		a.groups.Submit()
	case "shuffle":
		a.groups.Shuffle()
	case "deselect":
		a.groups.Selected = [12]bool{}
	case "flag":
		a.flagMode = !a.flagMode
	case "pause":
		a.running = !a.running
		a.lastTime = time.Now()
		a.turn, a.left, a.right = false, false, false
	case "launch":
		a.pinball.Launch()
		a.running = a.pinball.Active
		a.lastTime = time.Now()
	case "roll":
		a.fortune.Roll(rand.IntN(6) + 1)
	case "buy":
		a.fortune.Buy()
	case "end":
		a.fortune.EndTurn()
	}
	if a.running && a.timer == 0 {
		a.timer = glib.TimeoutAdd(16, &a.onTick, 0)
	}
	a.render()
}

func (a *gamesApp) cellAction(n int) {
	switch a.current {
	case "last-tile":
		a.last.Move(games.Point{X: n % 5, Y: n / 5})
	case "clock-out":
		a.office.Move(games.Point{X: n % 6, Y: n / 6})
	case "full-fridge":
		p := games.Point{X: n % 6, Y: n / 6}
		if a.removeFood {
			a.fridge.Remove(p)
			a.removeFood = false
		} else if !a.fridge.Place(p) {
			a.notice = "It does not fit there. Check the shape and the cold / fresh zone."
		}
	case "odd-groups":
		a.groups.Toggle(a.groups.Order[n])
	case "minesweeper":
		if a.flagMode {
			a.mines.ToggleFlag(n)
		} else {
			a.mines.Reveal(n)
		}
	case "small-fortune":
		i := fortuneIndex(n%6, n/6)
		if i >= 0 {
			price := games.PropertyPrice(i)
			if price > 0 {
				a.notice = fmt.Sprintf("%s: price $%d · rent $%d · house $%d", games.StreetNames[i], price, price/4*(1+a.fortune.Properties[i].Level), price/2)
			} else {
				a.notice = games.StreetNames[i]
			}
		}
	}
}

func (a *gamesApp) key(key uint32, down bool) bool {
	if a.current == "" {
		return false
	}
	if key >= 'A' && key <= 'Z' {
		key += 'a' - 'A'
	}
	if a.current == "harbor" || a.current == "space-pinball" {
		if key == 'p' && down {
			if (a.current == "harbor" && a.boat.Result == games.Playing) || (a.current == "space-pinball" && a.pinball.Active) {
				a.act("pause")
			}
			return true
		}
		if key == ' ' {
			if a.current == "harbor" {
				a.turn = down
			} else if down && !a.pinball.Active {
				a.act("launch")
			}
			return true
		}
		if a.current == "space-pinball" {
			if key == 'a' || key == 0xff51 {
				a.left = down
				return true
			}
			if key == 'd' || key == 0xff53 {
				a.right = down
				return true
			}
		}
		return false
	}
	if !down {
		return false
	}
	if a.current == "minesweeper" && key == 'f' {
		a.act("flag")
		return true
	}
	if a.current == "full-fridge" && key == 'r' {
		a.act("rotate")
		return true
	}
	if a.current != "last-tile" && a.current != "clock-out" {
		return false
	}
	if key == ' ' && a.current == "clock-out" {
		a.act("wait")
		return true
	}
	d, ok := map[uint32]games.Point{'w': {Y: -1}, 0xff52: {Y: -1}, 'a': {X: -1}, 0xff51: {X: -1}, 's': {Y: 1}, 0xff54: {Y: 1}, 'd': {X: 1}, 0xff53: {X: 1}}[key]
	if !ok {
		return false
	}
	if a.current == "last-tile" {
		a.last.Move(a.last.Player.Add(d))
	} else {
		a.office.Move(a.office.Player.Add(d))
	}
	a.render()
	return true
}
