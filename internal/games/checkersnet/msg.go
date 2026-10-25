package checkersnet

import (
	"math"

	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/games/gameweb"
)

// The messages, as JSON, each with its type in "t". A browser sends join,
// then move in its turn; the lead also players, seat, team and rules in
// the lobby, start, and in a game restart and lobby. The host sends hello,
// then lobby until the game starts, then start once and events and state
// as it goes; lobby again when the game goes back to it; bye when it is
// done with the browser.

// inMsg is what a browser sends.
type inMsg struct {
	T         string `json:"t"`
	Code      string `json:"code"` // join
	Name      string `json:"name"`
	Key       string `json:"key"`
	From      int    `json:"from"` // move
	To        int    `json:"to"`
	Players   int    `json:"players"` // players
	Index     int    `json:"index"`   // seat, team
	Kind      string `json:"kind"`    // seat: open, computer, ai
	Level     int    `json:"level"`
	Profile   string `json:"profile"`
	Team      int    `json:"team"`
	NoParking bool   `json:"noParking"` // rules
}

// helloMsg seats a browser: its seat's key to keep, and the seat, -1 to
// watch.
type helloMsg struct {
	T    string `json:"t"`
	Key  string `json:"key"`
	Seat int    `json:"seat"`
}

type seatMsg struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"` // open, local, visitor, computer, ai
	Level   int    `json:"level"`
	Profile string `json:"profile"`
	Team    int    `json:"team"`
	Here    bool   `json:"here"`
	Away    bool   `json:"away"`
	Color   string `json:"color"`
}

func (h *Host) seatsMsg() []seatMsg {
	n := len(h.Seats)
	out := make([]seatMsg, n)
	for i, s := range h.Seats {
		out[i] = seatMsg{Name: s.Name, Kind: kindNames[s.Kind], Level: int(s.Level), Profile: s.Profile, Team: s.Team,
			Here: s.Here, Away: s.Away}
		if corners, ok := games.SeatCorners[n]; ok {
			out[i].Color = MarbleColors[corners[i]]
		}
	}
	return out
}

// lobbyMsg is how the game is set up, and whether this browser may change
// it (Lead): the host's screen leads when people play at it.
type lobbyMsg struct {
	T         string    `json:"t"`
	Seats     []seatMsg `json:"seats"`
	You       int       `json:"you"`
	Lead      bool      `json:"lead"`
	LeadName  string    `json:"leadName"`
	HostLeads bool      `json:"hostLeads"`
	NoParking bool      `json:"noParking"`
	Levels    []string  `json:"levels"`
	AI        bool      `json:"ai"` // the AI can play a seat
	Profiles  []string  `json:"profiles"`
	Why       string    `json:"why"` // why it cannot start; "" when it can
	Text      texts     `json:"text"`
}

func (h *Host) lobbyMsg(c *gameweb.Client) lobbyMsg {
	m := lobbyMsg{T: "lobby", Seats: h.seatsMsg(), You: c.Seat, Lead: h.lead(c), HostLeads: h.HostLeads(),
		NoParking: h.NoParking, Levels: LevelNames[:], AI: h.opt.Choose != nil, Profiles: h.opt.Profiles, Why: h.Why(), Text: webTexts()}
	if l := h.Lead(); l >= 0 {
		m.LeadName = h.Seats[l].Name
	}
	return m
}

// holeMsg is a hole: where it is drawn (games.StarXY, in gaps between
// holes) and the point it lies in, -1 in the middle.
type holeMsg [3]float64

// startMsg is the game a browser is in: the board, the players, which of
// them it plays (-1 to watch), and the words it shows.
type startMsg struct {
	T         string      `json:"t"`
	You       int         `json:"you"`
	Holes     []holeMsg   `json:"holes"`
	Players   []playerMsg `json:"players"`
	NoParking bool        `json:"noParking"`
	Text      texts       `json:"text"`
}

type playerMsg struct {
	seatMsg
	Start int `json:"start"`
	Goal  int `json:"goal"`
}

func (h *Host) startMsg(c *gameweb.Client) startMsg {
	m := startMsg{T: "start", You: c.Seat, NoParking: h.Game.NoParking, Text: webTexts()}
	r := func(f float64) float64 { return math.Round(f*1000) / 1000 }
	for i := range games.StarHoles {
		x, y := games.StarXY(i)
		m.Holes = append(m.Holes, holeMsg{r(x), r(y), float64(games.CornerOf(i))})
	}
	seats := h.seatsMsg()
	for i, p := range h.Game.Players {
		m.Players = append(m.Players, playerMsg{seatMsg: seats[i], Start: p.Start, Goal: p.Goal})
	}
	return m
}

// stateMsg is the game as it stands: whose marble is in each hole, whose
// turn it is and whose marbles they may move, and for each player whether
// they are home and how far they have still to go.
type stateMsg struct {
	T         string        `json:"t"`
	Round     int           `json:"round"`
	Turn      int           `json:"turn"`
	Movers    []int         `json:"movers"`
	Holes     []int         `json:"holes"`
	Last      []int         `json:"last"`
	Over      bool          `json:"over"`
	WinTeam   int           `json:"winTeam"`
	Standings []int         `json:"standings"`
	Players   []stateplayer `json:"players"`
	Lead      bool          `json:"lead"` // this browser may restart
	LeadName  string        `json:"leadName"`
}

type stateplayer struct {
	Home  bool `json:"home"`
	Place int  `json:"place"`
	Left  int  `json:"left"`
	Here  bool `json:"here"`
	Away  bool `json:"away"`
	Hops  int  `json:"hops"`
}

func (h *Host) stateMsg(c *gameweb.Client) stateMsg {
	g := h.Game
	m := stateMsg{T: "state", Round: g.Round, Turn: g.Turn, Movers: g.Movers(), Holes: g.Holes, Last: g.Last,
		Over: g.Over, WinTeam: g.WinTeam, Standings: g.Standings(), Lead: h.lead(c)}
	if m.Movers == nil {
		m.Movers = []int{}
	}
	if l := h.Lead(); l >= 0 {
		m.LeadName = h.Seats[l].Name
	}
	for i, p := range g.Players {
		s := h.Seats[i]
		m.Players = append(m.Players, stateplayer{Home: p.Home, Place: p.Place, Left: g.Left(i), Here: s.Here || s.Kind != Visitor, Away: s.Away, Hops: p.Hops})
	}
	return m
}

// eventsMsg is what happened, for the board to show.
type eventsMsg struct {
	T      string     `json:"t"`
	Events []eventMsg `json:"events"`
}

type eventMsg struct {
	Kind    string `json:"kind"` // move, pass, home, win, draw
	Player  int    `json:"player"`
	Owner   int    `json:"owner"`
	Path    []int  `json:"path,omitempty"`
	Team    int    `json:"team"`
	Players []int  `json:"players,omitempty"`
}

var eventKinds = map[games.CheckersEventKind]string{games.CkMove: "move", games.CkPass: "pass", games.CkHome: "home", games.CkWin: "win", games.CkDraw: "draw"}

func eventsOf(events []games.CheckersEvent) []eventMsg {
	out := make([]eventMsg, len(events))
	for i, e := range events {
		out[i] = eventMsg{Kind: eventKinds[e.Kind], Player: e.Player, Owner: e.Owner, Path: e.Path, Team: e.Team, Players: e.Players}
	}
	return out
}
