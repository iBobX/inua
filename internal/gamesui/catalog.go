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
		"Fly missions to climb the ranks, from Cadet to Fleet Admiral. Hit the three yellow mission targets to light a mission, " +
			"then shoot the launch ramp (right) to accept it; the display shows what to shoot and the time left. Missions burn fuel: " +
			"roll through the inlanes and the re-entry lanes to refuel. The hyperspace ramp (left) climbs five levels, lighting an extra " +
			"ball at the third and paying a jackpot at the fifth. The booster bank lights Lock: lock two balls in the black hole for " +
			"multiball, where the launch ramp is the jackpot. The wormhole lets the ball out of the black hole. Light all three " +
			"re-entry lanes to raise the field multiplier. The plunger's pull picks the lane a launch drops into: hit the blinking " +
			"one for the skill shot. Nudge to save a ball, but not too often: the table tilts. Three balls; your best score is kept.",
		"A or ← left flipper  ·  D or → right flipper  ·  hold Space to pull the plunger  ·  X . ↑ nudge  ·  M sound  ·  P pause  ·  N new game",
		func() play { return &pinball{} }},
	{"small-fortune", "SmallFortune", "Small Fortune",
		"A property game against one to three computer players on a board of 40 squares. Roll two dice and walk the board; a double rolls again, three in a row go to jail. " +
			"Buy the streets, ferry piers and utilities you land on; rivals landing there pay you rent. Every street of yours your pawn passes or lands on may go up one level that turn (outlined on the board): house by house to four, then a hotel; " +
			"owning a whole colour group doubles its bare rent and adds half to its built rents. Piers pay more the more of them you own; utilities charge by the dice. " +
			"Passing Start pays $200. Taxes and fines fill the fountain, and whoever lands there takes it. Who cannot pay sells buildings and property at half price, or is out. " +
			"After 30 rounds, or when one player is left, the most worth wins: cash, property and buildings.",
		"Space the main action  ·  R roll  ·  B buy  ·  X pass  ·  E end turn  ·  J pay bail  ·  H build on the square in focus  ·  N new game  ·  click a square to see it",
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
