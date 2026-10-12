// Package gamesui runs inua's light games for cmd/inua-games. Each game
// is a light app of its own (appui.Register): its own process,
// application ID, launcher and window, in the shell's look.
package gamesui

import "github.com/lyricat/inua/internal/appui"

// entry is a game: its command, application ID suffix and title, the
// rules its help shows, and its keys.
type entry struct {
	ID, AppID, Title, Rules, Keys string
	make                          func() play
}

var catalog = []entry{
	{"last-tile", "LastTile", "Last Tile",
		"A floor of 5 × 5 tiles, and every tile you step off falls away. Reach the exit within ten moves, " +
			"picking up as many of the four coins as you can; the best haul on each of the ten maps is kept.",
		"arrows or WASD move  ·  or click a tile next to you  ·  Space next map  ·  N try again",
		func() play { return &lastTile{} }},
	{"full-fridge", "FullFridge", "Full Fridge",
		"Every grocery has to go in, and each fridge has just the room for them. Frozen food goes in the freezer " +
			"at the top, the rest on the shelves below. Pick a grocery from the bag, turn it, and put it where its shadow fits; " +
			"click packed food to take it out again.",
		"click the bag to pick up  ·  R or right-click turns  ·  click the fridge to put in or take out  ·  Esc puts back",
		func() play { return &fullFridge{} }},
	{"clock-out", "ClockOut", "Clock Out",
		"Get from your desk to the exit before the evening is gone. Colleagues walk their rounds one step for each of yours, " +
			"and a step where one is going, or past one, is a meeting. Waiting a turn is often the way. Six offices.",
		"arrows or WASD move  ·  Space waits  ·  or click a square next to you, or your own to wait  ·  N try again",
		func() play { return &clockOut{} }},
	{"harbor", "Harbor", "One-button Harbor",
		"One button steers the boat: hold it and the boat turns right, let go and it sails straight. " +
			"Keep off the rocks and the shore, and come into the berth by the pier heading right, within a minute. Three courses; best times are kept.",
		"hold Space or the sea to turn  ·  P pause  ·  N start over",
		func() play { return &harbor{} }},
	{"dice-shop", "DiceShop", "Three Dice Shop",
		"Each morning brings three dice, one for each job. Stock buys that many crates at $2; Sales sells that many at $5, " +
			"plus one for every three points of the shop's condition; Repairs changes the condition by the die less three. " +
			"Rent is $6 a day. Have $75 by the end of the seventh day.",
		"1 2 3 pick up a die  ·  Z stock  ·  X sales  ·  C repairs  ·  Enter open the shop",
		func() play { return &diceShop{} }},
	{"odd-groups", "OddGroups", "Odd Groups",
		"Twelve words hide three groups of four. Pick four that belong together and submit; four wrong guesses end the puzzle. " +
			"Eight puzzles.",
		"click words to pick them  ·  Enter submits  ·  Backspace clears",
		func() play { return &oddGroups{} }},
	{"minesweeper", "Minesweeper", "Minesweeper",
		"Open every square without a mine. A number tells how many of the eight squares around it hide mines. " +
			"The first square you open is always safe. Three sizes; best times are kept.",
		"click opens  ·  right-click flags  ·  click a number with its flags placed to open around it  ·  arrows, Space, F",
		func() play { return &mines{} }},
	{"space-pinball", "SpacePinball", "Space Pinball",
		"Keep the ball on the table with the two flippers. Pop bumpers score 100, slingshots 10, each drop target 250, and the whole bank of three 1,500. " +
			"Rolling through a lane under the dome lights it; light all three to raise the multiplier, up to ×5, for the rest of the ball. A flipper press moves the lit lanes along. " +
			"For the first seconds of each ball, ball save gives a drained ball back. Three balls a game; your best score is kept.",
		"A or ← left flipper  ·  D or → right flipper  ·  hold Space to pull the plunger  ·  P pause  ·  N new game  ·  or hold the table's halves and the lane",
		func() play { return &pinball{} }},
	{"small-fortune", "SmallFortune", "Small Fortune",
		"A short property game against one to three computer players. Roll two dice and walk the board; a double rolls again, three in a row go to jail. " +
			"Buy the streets you land on. Rivals landing there pay you rent, doubled once you own both streets of a pair; then build up to three houses on them for much more. " +
			"Passing Start pays $200. Taxes and fines fill the fountain, and whoever lands there takes it. Who cannot pay sells houses and streets at half price, or is out. " +
			"After 20 rounds, or when one player is left, the most worth wins: cash, streets and houses.",
		"Space the main action  ·  R roll  ·  B buy  ·  X pass  ·  E end turn  ·  J pay bail  ·  H build on the square in focus  ·  N new game",
		func() play { return &fortune{} }},
}

// Names are the games' command names, in the catalog's order.
func Names() []string {
	var out []string
	for _, g := range catalog {
		out = append(out, g.ID)
	}
	return out
}

// Register adds every game to appui's apps, for appui.Run.
func Register() {
	for _, g := range catalog {
		appui.Register(g.ID, g.AppID, g.Title, func() appui.View { return &view{info: g, g: g.make()} })
	}
}
