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
