package fortunenet

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/i18n"
)

// webTexts are the page's words in the host's language, by the names the
// page asks for them by; formats keep their verbs (%s, %d, %[2]s …) for
// the page to fill in.
func webTexts() texts {
	t := texts{
		"title":       i18n.T("Small Fortune"),
		"name":        i18n.T("Your name"),
		"joinButton":  i18n.T("Join"),
		"watch":       i18n.T("Watch"),
		"waiting":     i18n.T("Waiting for the host to start the game…"),
		"full":        i18n.T("The game is full: you can watch it."),
		"watching":    i18n.T("Watching"),
		"reconnect":   i18n.T("Connection lost: trying again…"),
		"lobbyCity":   i18n.T("%s · %d rounds · $%s each"),
		"seatsOpen":   i18n.T("Room for %d more"),
		"local":       i18n.T("at the host's screen"),
		"visitor":     i18n.T("in a browser"),
		"you":         i18n.T("you"),
		"away":        i18n.T(" · away, the computer plays"),
		"ai":          i18n.T(" · the AI plays"),
		"players":     i18n.T("PLAYERS"),
		"square":      i18n.T("SQUARE"),
		"happened":    i18n.T("WHAT HAPPENED"),
		"round":       i18n.T("ROUND"),
		"fountain":    i18n.T("FOUNTAIN"),
		"roll":        i18n.T("Roll the dice"),
		"rollDouble":  i18n.T("Roll for a double"),
		"rollSix":     i18n.T("Roll for a six"),
		"bail":        i18n.Tf("Pay $%d bail", games.Bail),
		"buy":         i18n.T("Buy it for $%d"),
		"pass":        i18n.T("Don't buy"),
		"leave":       i18n.T("Leave the shop"),
		"end":         i18n.T("End turn"),
		"playing":     i18n.T("%s is playing…"),
		"yourTurn":    i18n.T("Your turn"),
		"turnOf":      i18n.T("%s's turn"),
		"roundOf":     i18n.T("round %d of %d"),
		"worth":       i18n.T("worth $%s"),
		"bankrupt":    i18n.T("bankrupt"),
		"inJail":      i18n.T(" · in jail"),
		"rentFree":    i18n.T(" · free rent"),
		"turtle":      i18n.T(" · turtle"),
		"asleep":      i18n.T(" · asleep"),
		"forSale":     i18n.T("For sale: $%d"),
		"owns":        i18n.T("%s owns it"),
		"youOwn":      i18n.T("You own it"),
		"rentNow":     i18n.T("rent now"),
		"street":      i18n.T("Street · price $%d · building $%d"),
		"pier":        i18n.T("Station or pier · price $%d"),
		"utility":     i18n.T("Utility · price $%d"),
		"chance":      i18n.T("Chance"),
		"shop":        i18n.T("Card shop"),
		"tax":         i18n.T("Tax · $%d"),
		"corner":      i18n.T("Corner"),
		"house":       i18n.T("House · $%d"),
		"hotel":       i18n.T("Hotel · $%d"),
		"hotelBuilt":  i18n.T("Hotel built"),
		"sell":        i18n.T("Sell · $%d"),
		"hand":        i18n.T("Your cards · %d of %d"),
		"handPlay":    i18n.T("Your cards · %d of %d · click one to play it"),
		"shopSub":     i18n.T("Click a card to buy it: any of them, or none."),
		"onWhom":      i18n.T("On whom?"),
		"howMany":     i18n.T("How many squares?"),
		"pickSquare":  i18n.T("Pick a square on the board"),
		"cancel":      i18n.T("Cancel"),
		"won":         i18n.T("%s won."),
		"youWon":      i18n.T("You won!"),
		"bankruptOf":  i18n.T("%s (bankrupt)"),
		"tierBare":    i18n.T("Bare"),
		"tierHotel":   i18n.T("Hotel"),
		"aCard":       i18n.T("a card"),
		"rentByDice":  i18n.T("The rent is the dice times this."),
		"soundOn":     i18n.T("Sound on: M turns it off"),
		"soundOff":    i18n.T("Sound off: M turns it on"),
		"rentByPiers": i18n.T("The rent is by how many piers the owner has."),
	}
	t["noteStart"] = i18n.Tf("Collect $%d every time you pass.", games.StartPay)
	t["noteJailDouble"] = i18n.Tf("Only visiting, unless sent here. In jail, roll %s to leave, or pay $50 before rolling; after three tries you pay and go.", i18n.T("a double"))
	t["noteJailSix"] = i18n.Tf("Only visiting, unless sent here. In jail, roll %s to leave, or pay $50 before rolling; after three tries you pay and go.", i18n.T("a six"))
	t["noteFountain"] = i18n.T("Taxes and fines go into the fountain; the next player to land here takes it all: $%d now.")
	t["noteGoToJail"] = i18n.T("Straight to jail, without passing Start.")
	t["noteChance"] = i18n.T("Draw a card: money, a fine, a trip, or a card for your hand.")
	t["noteShop"] = i18n.Tf("Three cards on offer: buy any of them, or none. A hand holds up to %d cards.", games.HandMax)
	t["noteTax"] = i18n.T("Pay $%d into the fountain.")
	t["wholeGroup"] = i18n.T("The whole group: bare rent doubled, built rents half again.")
	t["withGroup"] = i18n.T("With the whole group: bare rent doubled, built rents half again.")
	t["group"] = i18n.T("Group:")
	t["times"] = i18n.T("× %d")
	for n := 1; n <= 4; n++ {
		t[fmt.Sprint("piers", n)] = i18n.Nf("%d pier", "%d piers", n, n)
	}
	for n := 1; n <= 2; n++ {
		t[fmt.Sprint("utilities", n)] = i18n.Nf("%d utility", "%d utilities", n, n)
	}
	for n := 1; n <= games.HandMax; n++ {
		t[fmt.Sprint("cards", n)] = i18n.Nf(" · %d card", " · %d cards", n, n)
	}
	for n := 1; n < MaxPlayers; n++ {
		t[fmt.Sprint("rivals", n)] = i18n.Nf("and %d computer player", "and %d computer players", n, n)
	}
	for n := 1; n < games.Hotel; n++ {
		t[fmt.Sprint("houses", n)] = i18n.Nf("%d house", "%d houses", n, n)
	}
	return t
}

// serveTexts serves the page's words, for the join form before the lobby.
func serveTexts(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(webTexts())
}
