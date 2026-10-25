package checkersnet

import "github.com/lyricat/inua/internal/i18n"

// texts are the page's words, by the names the page asks for them by.
type texts map[string]string

// webTexts are the page's words in the host's language; formats keep their
// verbs (%s, %d) for the page to fill in.
func webTexts() texts {
	return texts{
		"title":       i18n.T("Chinese Checkers"),
		"name":        i18n.T("Your name"),
		"joinButton":  i18n.T("Join"),
		"watch":       i18n.T("Watch"),
		"waitingHost": i18n.T("Waiting for the host to start the game…"),
		"waitingLead": i18n.T("Waiting for %s to start the game…"),
		"youLead":     i18n.T("You set up the game: who plays each seat, the teams, the rules."),
		"full":        i18n.T("The game is full: you can watch it."),
		"watching":    i18n.T("Watching"),
		"reconnect":   i18n.T("Connection lost: trying again…"),
		"players":     i18n.T("PLAYERS"),
		"setup":       i18n.T("THE GAME"),
		"happened":    i18n.T("WHAT HAPPENED"),
		"round":       i18n.T("ROUND"),
		"playersN":    i18n.T("%d players"),
		"open":        i18n.T("Open: for someone to join"),
		"local":       i18n.T("at the host's screen"),
		"visitor":     i18n.T("in a browser"),
		"computer":    i18n.T("Computer"),
		"ai":          i18n.T("AI"),
		"aiDefault":   i18n.T("the default profile"),
		"easy":        i18n.T("Easy"),
		"normal":      i18n.T("Normal"),
		"hard":        i18n.T("Hard"),
		"team":        i18n.T("Team %s"),
		"you":         i18n.T("you"),
		"leads":       i18n.T("sets up the game"),
		"away":        i18n.T("away, the computer plays"),
		"noParking":   i18n.T("Stay out of other points"),
		"noParkingNo": i18n.T("A marble may cross another player's point, but not stop in it."),
		"start":       i18n.T("Start the game"),
		"restart":     i18n.T("New game"),
		"toLobby":     i18n.T("Set up again"),
		"turn":        i18n.T("%s's turn"),
		"yourTurn":    i18n.T("Your turn: pick one of your marbles."),
		"forPartner":  i18n.T("You are home: move %s's marbles."),
		"pickHole":    i18n.T("Now pick where it goes, or another marble."),
		"home":        i18n.T("home"),
		"place":       i18n.T("#%d home"),
		"left":        i18n.T("%d to go"),
		"hops":        i18n.T("%d hops"),
		"moved":       i18n.T("%s moved"),
		"movedHops":   i18n.T("%s hopped %d times"),
		"movedFor":    i18n.T("%s moved %s's marble"),
		"passed":      i18n.T("%s could not move"),
		"cameHome":    i18n.T("%s is home"),
		"wins":        i18n.T("%s wins"),
		"teamWins":    i18n.T("%s win"),
		"and":         i18n.T("%s and %s"),
		"draw":        i18n.T("No one got home in time: a draw."),
		"over":        i18n.T("The game is over."),
		"rotate":      i18n.T("Drag to turn the board, scroll to come closer."),
	}
}
