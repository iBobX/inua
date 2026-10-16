package fortunenet

import (
	"cmp"
	"math"

	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/i18n"
)

// The messages, as JSON, each with its type in "t". A browser sends join,
// then act; the host sends hello, then lobby until the game starts, then
// start once and events and state as it goes; bye when it is done with it.

// inMsg is what a browser sends: join (the code, a name, a seat's key to
// come back with) or act (an Action).
type inMsg struct {
	T    string `json:"t"`
	Code string `json:"code"`
	Name string `json:"name"`
	Key  string `json:"key"`
	Action
}

type joinMsg = inMsg

// helloMsg seats a browser: its seat's key to keep, and the seat, -1 to
// watch.
type helloMsg struct {
	T    string `json:"t"`
	Key  string `json:"key"`
	Seat int    `json:"seat"`
}

type byeMsg struct {
	T    string `json:"t"`
	Text string `json:"text"`
}

type seatMsg struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"` // local, visitor, computer, ai
	Here   bool   `json:"here"`
	Away   bool   `json:"away"`
	Kicked bool   `json:"kicked"`
}

// lobbyMsg is who is in the lobby, and the game they will play.
type lobbyMsg struct {
	T      string    `json:"t"`
	Seats  []seatMsg `json:"seats"`
	You    int       `json:"you"`
	Room   int       `json:"room"`
	Rivals int       `json:"rivals"`
	City   string    `json:"city"`
	Rounds int       `json:"rounds"`
	Cash   int       `json:"cash"`
	Text   texts     `json:"text"`
}

type squareMsg struct {
	Tile  tileMsg `json:"tile"`
	Name  string  `json:"name"`
	Kind  string  `json:"kind"`
	Group int     `json:"group"`
	Price int     `json:"price"`
	House int     `json:"house"` // a street's building cost
	Rents []int   `json:"rents,omitempty"`
	Next  []int   `json:"next"`
}

// tileMsg is where a square lies on the board (games.Tile): its outline
// and colour band as x, y pairs, its middle, the way along the track and
// into the loop, its length and depth.
type tileMsg struct {
	Outline []float64  `json:"outline"`
	Band    []float64  `json:"band,omitempty"`
	Center  [2]float64 `json:"center"`
	Tan     [2]float64 `json:"tan"`
	In      [2]float64 `json:"in"`
	Along   float64    `json:"along"`
	Across  float64    `json:"across"`
}

func tileOf(t games.Tile) tileMsg {
	r := func(f float64) float64 { return math.Round(f*10) / 10 }
	pts := func(ps []games.Vec) []float64 {
		var out []float64
		for _, p := range ps {
			out = append(out, r(p.X), r(p.Y))
		}
		return out
	}
	return tileMsg{Outline: pts(t.Outline), Band: pts(t.Band), Center: [2]float64{r(t.Center.X), r(t.Center.Y)},
		Tan: [2]float64{t.Tan.X, t.Tan.Y}, In: [2]float64{t.In.X, t.In.Y}, Along: r(t.Along), Across: r(t.Across)}
}

type cardMsg struct {
	Name   string `json:"name"`
	Text   string `json:"text"`
	Price  int    `json:"price"`
	Target string `json:"target"` // none, player, square, steps
}

type branchMsg struct {
	Fork, First, Last, Rejoin int
}

// startMsg is the game a browser is in: the board, the cards, the
// players, which of them it plays (-1 to watch), and the words it shows.
type startMsg struct {
	T        string  `json:"t"`
	You      int     `json:"you"`
	City     string  `json:"city"` // its ID: tokyo, new-york …
	CityName string  `json:"cityName"`
	Local    string  `json:"local"`
	Rounds   int     `json:"rounds"`
	OneDie   bool    `json:"oneDie"`
	Size     float64 `json:"size"` // the board's side, in its units
	Band     float64 `json:"band"` // a street's colour band
	Print    bool    `json:"print"`
	Art      bool    `json:"art"` // pictures at /art/
	// PierRents are a pier's rent by how many its owner has; UtilityTimes
	// what a utility charges a point of the dice, by how many.
	PierRents    []int       `json:"pierRents"`
	UtilityTimes []int       `json:"utilityTimes"`
	Pieces       []Piece     `json:"pieces"` // what stands on the board
	Squares      []squareMsg `json:"squares"`
	Branches     []branchMsg `json:"branches"`
	Cards        []cardMsg   `json:"cards"`
	Players      []seatMsg   `json:"players"`
	HandMax      int         `json:"handMax"`
	Text         texts       `json:"text"`
}

type playerMsg struct {
	seatMsg
	Cash     int  `json:"cash"`
	At       int  `json:"at"`
	InJail   bool `json:"inJail"`
	Out      bool `json:"out"`
	Cards    int  `json:"cards"`
	RentFree bool `json:"rentFree"`
	Turtle   int  `json:"turtle"`
	Asleep   bool `json:"asleep"`
	Worth    int  `json:"worth"`
}

// stateMsg is the game as it stands, as one player sees it: their hand,
// and in their turn what they may do.
type stateMsg struct {
	T       string      `json:"t"`
	Round   int         `json:"round"`
	Turn    int         `json:"turn"`
	Phase   string      `json:"phase"` // roll, buy, shop, end, cpu, over
	Dice    [2]int      `json:"dice"`
	Pot     int         `json:"pot"`
	Winner  int         `json:"winner"`
	Players []playerMsg `json:"players"`
	Owner   []int       `json:"owner"`
	Houses  []int       `json:"houses"`
	Rent    []int       `json:"rent"` // what landing on each square costs now
	Hand    []int       `json:"hand"`
	Offer   []int       `json:"offer"`
	// In their turn: the streets they may raise or sell a level of, the
	// cards they may buy at the shop, and each card's targets (nil: not
	// playable now; [0] for a card played on nothing).
	Build   []int   `json:"build"`
	Sell    []int   `json:"sell"`
	CanShop []bool  `json:"canShop"`
	Plays   [][]int `json:"plays"`
	Steps   int     `json:"steps"` // the most a Loaded Dice walks
	Bail    bool    `json:"bail"`  // may pay bail
}

type eventsMsg struct {
	T      string               `json:"t"`
	Events []games.FortuneEvent `json:"events"`
}

var kindNames = map[games.SquareKind]string{
	games.Street: "street", games.StartSquare: "start", games.JailSquare: "jail", games.Fountain: "fountain",
	games.GoToJail: "gotojail", games.ChanceSquare: "chance", games.TaxSquare: "tax", games.Pier: "pier",
	games.Utility: "utility", games.CardShop: "shop",
}

var phaseNames = map[games.FortunePhase]string{
	games.PhaseRoll: "roll", games.PhaseBuy: "buy", games.PhaseShop: "shop", games.PhaseEnd: "end",
	games.PhaseCPU: "cpu", games.PhaseOver: "over",
}

var targetNames = [...]string{games.TargetNone: "none", games.TargetPlayer: "player", games.TargetSquare: "square", games.TargetSteps: "steps"}

func (h *Host) seatMsgs() []seatMsg {
	var out []seatMsg
	for i, s := range h.Seats {
		m := seatMsg{Name: s.Name, Kind: [...]string{"local", "visitor", "computer", "ai"}[s.Kind], Here: s.Here || s.Kind != Visitor, Kicked: s.Kicked}
		if h.Game != nil {
			m.Name, m.Away = h.Game.Players[i].Name, h.Game.Players[i].Away
		}
		out = append(out, m)
	}
	return out
}

func (h *Host) lobbyMsg(c *client) lobbyMsg {
	s := h.opt.Settings
	city := games.FortuneCities[max(0, min(len(games.FortuneCities)-1, s.City))]
	return lobbyMsg{T: "lobby", Seats: h.seatMsgs(), You: c.seat, Room: h.Room(), Rivals: s.Rivals + len(s.AIs),
		City: i18n.T(city.Name), Rounds: cmp.Or(s.Rounds, games.DefaultFortune.Rounds), Cash: cmp.Or(s.Cash, games.DefaultFortune.Cash), Text: webTexts()}
}

func (h *Host) startMsg(c *client) startMsg {
	g := h.Game
	city := games.FortuneCities[g.City]
	m := startMsg{T: "start", You: c.seat, City: city.ID, CityName: i18n.T(city.Name), Local: city.Local,
		Rounds: g.Rounds, OneDie: g.OneDie, Players: h.seatMsgs(), HandMax: games.HandMax, Text: webTexts(),
		Size: games.ArtBoard * games.MapK, Band: games.TrackBand, Print: h.opt.Print != nil, Art: h.opt.Art != nil}
	if h.opt.Pieces != nil {
		m.Pieces = h.opt.Pieces(g.City)
	}
	for n := range 5 {
		m.PierRents = append(m.PierRents, games.PierRent(n))
	}
	for n := range 3 {
		m.UtilityTimes = append(m.UtilityTimes, games.UtilityTimes(n))
	}
	tr := games.TrackOf(g.City)
	for sq, s := range games.Board {
		sm := squareMsg{Tile: tileOf(tr.Tiles[sq]), Name: g.SquareName(sq), Kind: kindNames[s.Kind], Group: s.Group, Price: s.Price, Next: games.Next(sq)}
		if s.Kind == games.Street {
			sm.House = games.HouseCost(sq)
			for b := range games.Hotel + 1 {
				sm.Rents = append(sm.Rents, games.RentAt(sq, b, false))
			}
		}
		m.Squares = append(m.Squares, sm)
	}
	for _, b := range games.Branches {
		m.Branches = append(m.Branches, branchMsg{b.Fork, b.First, b.Last, b.Rejoin})
	}
	for _, c := range games.Cards {
		m.Cards = append(m.Cards, cardMsg{Name: i18n.T(c.Name), Text: i18n.T(c.Text), Price: c.Price, Target: targetNames[c.Target]})
	}
	return m
}

func (h *Host) stateMsg(c *client) stateMsg {
	g := h.Game
	v := g.View()
	m := stateMsg{T: "state", Round: g.Round, Turn: g.Turn, Phase: phaseNames[g.Phase], Dice: g.Dice, Pot: g.Pot,
		Winner: g.Winner, Owner: v.Owner[:], Houses: v.Houses[:], Offer: cardInts(g.Offer)}
	seats := h.seatMsgs()
	for i, p := range g.Players {
		m.Players = append(m.Players, playerMsg{seatMsg: seats[i], Cash: p.Cash, At: p.At, InJail: p.InJail, Out: p.Out,
			Cards: len(p.Cards), RentFree: p.RentFree, Turtle: p.Turtle, Asleep: p.Asleep, Worth: g.Worth(i)})
	}
	for sq := range games.Board {
		m.Rent = append(m.Rent, g.Rent(sq))
	}
	me := c.seat
	if me < 0 || me >= len(g.Players) {
		return m
	}
	m.Hand = cardInts(g.Players[me].Cards)
	if me != g.Turn || g.Players[me].Computer() || g.Phase == games.PhaseOver {
		return m
	}
	for sq := range games.Board {
		if g.CanBuild(sq) {
			m.Build = append(m.Build, sq)
		}
		if g.CanSell(sq) {
			m.Sell = append(m.Sell, sq)
		}
	}
	for i := range g.Offer {
		m.CanShop = append(m.CanShop, g.CanBuyCard(i))
	}
	for i := range g.Players[me].Cards {
		var t []int
		if g.Playable(i) {
			t = append([]int{}, g.Targets(i)...)
		}
		m.Plays = append(m.Plays, t)
	}
	m.Steps = g.MaxSteps()
	m.Bail = g.Phase == games.PhaseRoll && g.Players[me].InJail && g.Players[me].Cash >= games.Bail
	return m
}

func (h *Host) eventsMsg(c *client, events []games.FortuneEvent) eventsMsg {
	m := eventsMsg{T: "events"}
	for _, e := range events {
		m.Events = append(m.Events, e.For(c.seat))
	}
	return m
}

func cardInts(cards []games.CardKind) []int {
	out := []int{}
	for _, c := range cards {
		out = append(out, int(c))
	}
	return out
}

// texts are the page's words, in the host's language.
type texts map[string]string
