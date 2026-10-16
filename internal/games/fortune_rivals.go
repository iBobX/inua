package games

import "github.com/lyricat/inua/internal/i18n"

// The computer players are characters, each playing in a way of its own:
// what it keeps in hand, what it buys, where it builds, which cards it
// buys and how readily it plays them on the others.

// Rival is a computer player's character.
type Rival struct {
	Name, Title, About string // in English, for i18n.T

	Reserve  int // the cash it keeps when it buys
	BuyCheap int // a street of a group it has not started, at most this price; 0 any
	// The cash it keeps when it builds on a street of a whole group, and on
	// a lone one; -1 never on a lone one.
	BuildReserve, LoneReserve int
	Cards                     int        // the cards it buys at a shop, at most
	CardReserve               int        // the cash it keeps when it buys one
	Likes                     []CardKind // the cards it buys, best first
	Mean                      bool       // plays its cards on the others readily
	DiceGain                  int        // what Loaded Dice must win it to be played
	BailAt                    int        // the cash, over the bail, at which it pays out of jail
}

// Rivals are the computer players, in the order they sit by default.
var Rivals = []Rival{
	{
		Name: i18n.M("Mika"), Title: i18n.M("the careful banker"),
		About:   i18n.M("Keeps cash in hand, passes on the dearest streets, buys few cards and pays bail early."),
		Reserve: 200, BuyCheap: 340, BuildReserve: 100, LoneReserve: 380,
		Cards: 1, CardReserve: 600, Likes: []CardKind{CardFreeRent, CardJailPass, CardPermit, CardRequisition},
		DiceGain: 200, BailAt: 200,
	},
	{
		Name: i18n.M("Ren"), Title: i18n.M("the bold developer"),
		About:   i18n.M("Buys whatever the dice land on, with little cash to spare, and sits out jail rather than pay."),
		Reserve: 90, BuildReserve: 180, LoneReserve: 380,
		Cards: 2, CardReserve: 300, Likes: []CardKind{CardPermit, CardRequisition, CardDice, CardDemolish, CardFreeRent},
		DiceGain: 120, BailAt: 900,
	},
	{
		Name: i18n.M("Sol"), Title: i18n.M("the trickster"),
		About:   i18n.M("Lives by the cards: sleeping pills, turtles and audits for whoever leads; builds anywhere."),
		Reserve: 110, BuildReserve: 100, LoneReserve: 220,
		Cards: 3, CardReserve: 200, Likes: []CardKind{CardSleep, CardTurtle, CardAudit, CardRob, CardDemolish, CardDice, CardRequisition, CardFreeRent},
		Mean: true, DiceGain: 80, BailAt: 400,
	},
}

// RivalNamed is the character called name, the first if none is.
func RivalNamed(name string) int {
	for i, r := range Rivals {
		if r.Name == name {
			return i
		}
	}
	return 0
}

// standIn is how the computer plays for a person who is away: steadily,
// none of the characters' leanings.
var standIn = Rival{
	Reserve: 150, BuildReserve: 250, LoneReserve: 450,
	Cards: 2, CardReserve: 400, Likes: []CardKind{CardRequisition, CardDice, CardPermit, CardDemolish, CardFreeRent, CardSleep, CardTurtle, CardAudit, CardRob, CardJailPass},
	DiceGain: 120, BailAt: 400,
}

// rival is how the computer plays player pi: its character, or for a
// person away, standIn.
func (g *Fortune) rival(pi int) Rival {
	p := g.Players[pi]
	if !p.CPU {
		return standIn
	}
	return Rivals[p.Rival]
}
