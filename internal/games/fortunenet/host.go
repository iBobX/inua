// Package fortunenet hosts a game of Small Fortune for the local network:
// people at the host's screen and visitors in a browser, who join with the
// host's code, play one game, which the host holds and plays out. Visitors
// get the web page and send their moves over a WebSocket; each sees the
// game as their player (games.FortuneEvent.For), their own cards and no
// one else's. Someone gone has the computer play for them until they come
// back with their seat's key.
//
// A Host's state lives in one place, where Options.Run runs what changes
// it (the GTK main loop, for the light app): its methods other than Serve,
// URLs and Close are called there, and its callbacks come from there.
package fortunenet

import (
	"crypto/rand"
	"encoding/hex"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/games/fortuneai"
	"github.com/lyricat/inua/internal/i18n"
)

// SeatKind is who plays a seat.
type SeatKind uint8

const (
	Local    SeatKind = iota // a person at the host's screen
	Visitor                  // a person in a browser
	Computer                 // a computer player
	AI                       // the host's AI (fortuneai)
)

// Seat is a player of the game, or of the lobby before it.
type Seat struct {
	Name string
	Kind SeatKind
	// Here: a visitor's browser is connected. Kicked: the host sent the
	// visitor away; the computer plays their seat to the end.
	Here, Kicked bool

	key  string  // a visitor's, to come back with
	conn *client // a visitor's browser, while connected
	gone *time.Timer
}

// Options set up a Host.
type Options struct {
	// The game's settings: its AIs and Rivals are the AI and computer
	// players after the people, as many as there is room for.
	Settings games.FortuneSettings
	// Local are the names of the people at the host's screen, playing
	// first; none when the host only hosts.
	Local []string
	// Open is how many seats are open to visitors: 0 for as many as there
	// is room for.
	Open int
	// Run runs f where the Host lives, one f at a time.
	Run func(f func())
	// Changed tells the host's screen that the lobby or the game changed;
	// Events hands it what happened in the game, as Fortune.Take does.
	Changed func()
	Events  func([]games.FortuneEvent)
	// Print is the board's print for a city, as the host's screen shows
	// it: a JPEG for the browsers' board; nil, a plain board.
	Print func(city int) []byte
	// Art is a picture of the game's as the host's screen draws it:
	// "card-N" (card kind N), "card-N-price" (with its price tag),
	// "card-back", "sign-N" (square N's sign), "house-A-O" and "hotel-A-O"
	// (along the track or across it, A 1 or 0, its owner O), and the
	// pieces Pieces names; nil when it has none.
	Art func(city int, name string) *Picture
	// Pieces are what stands on a city's board where the host's screen
	// has it: landmarks, trees, the fountain.
	Pieces func(city int) []Piece
	// AI plays the AI players, with the host's AI; nil when none play.
	AI   *fortuneai.Driver
	Seed uint64
}

// Picture is a picture of the game's, a PNG. A piece that stands on the
// board has its size too, in board units, and its foot: FootX from its
// left, its lowest edge Dv in front of the point it stands at (towards
// the viewer, as the host's screen looks at the board).
type Picture struct {
	PNG       []byte
	W, H      float64
	FootX, Dv float64
}

// Piece is something standing on the board: its picture's name (Art),
// and the board point it stands at.
type Piece struct {
	Name string  `json:"name"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
}

// MaxPlayers is how many players a game has at most.
const MaxPlayers = 4

// Host holds the lobby, then the game.
type Host struct {
	opt     Options
	Code    string  // what visitors join with
	Seats   []*Seat // the people, local first, then visitors; at Start, the computer players
	Game    *games.Fortune
	clients map[*client]bool
	cpu     *time.Timer // the computer's next move, paced to the board
	closed  bool
	srv     *server
	print   []byte              // the board's print, once asked for
	art     map[string]*Picture // the pictures, each once asked for
}

// paceScale speeds the computer's pace up, for tests.
var paceScale = 1.0

// AwayAfter is how long a visitor may be gone in a game before the
// computer plays for them: long enough to reload the page.
var AwayAfter = 15 * time.Second

// NewHost opens a lobby with the people at the host's screen.
func NewHost(opt Options) *Host {
	h := &Host{opt: opt, Code: newCode(), clients: map[*client]bool{}}
	for _, name := range opt.Local[:min(len(opt.Local), MaxPlayers)] {
		h.Seats = append(h.Seats, &Seat{Name: name, Kind: Local, Here: true})
	}
	return h
}

// Room is how many more visitors the lobby takes: up to four players
// with the computer players asked for, and the seats open (Options.Open),
// while the game has not started.
func (h *Host) Room() int {
	if h.Game != nil {
		return 0
	}
	room := MaxPlayers - len(h.Seats) - len(h.opt.Settings.AIs) - h.opt.Settings.Rivals
	if h.opt.Open > 0 {
		visitors := 0
		for _, s := range h.Seats {
			if s.Kind == Visitor {
				visitors++
			}
		}
		room = min(room, h.opt.Open-visitors)
	}
	return max(0, room)
}

// CanStart reports whether the game can start: one person at least, two
// players with the computer's (NewFortune adds one for a lone person).
func (h *Host) CanStart() bool {
	return h.Game == nil && len(h.Seats) > 0
}

// Start starts the game with the seats as they are and the computer
// players asked for, as many as there is room for.
func (h *Host) Start() bool {
	if !h.CanStart() {
		return false
	}
	s := h.opt.Settings
	s.Humans = nil
	for _, seat := range h.Seats {
		s.Humans = append(s.Humans, seat.Name)
	}
	if len(s.Humans) == 1 && h.Seats[0].Kind == Local {
		s.Humans = nil // one person at the screen: "You"
	}
	s.AIs = s.AIs[:min(len(s.AIs), MaxPlayers-len(h.Seats))]
	s.Rivals = min(s.Rivals, MaxPlayers-len(h.Seats)-len(s.AIs))
	h.Game = games.NewFortune(s, h.opt.Seed)
	for _, p := range h.Game.Players[len(h.Seats):] {
		kind := Computer
		if p.AI {
			kind = AI
		}
		h.Seats = append(h.Seats, &Seat{Name: p.Name, Kind: kind})
	}
	for i, seat := range h.Seats {
		if seat.Kind == Visitor && !seat.Here {
			h.Game.SetAway(i, true)
		}
	}
	h.changed()
	for c := range h.clients {
		c.send(h.startMsg(c))
	}
	h.Flush()
	return true
}

// Restart starts the game again with the same people and visitors, and
// the computer players asked for; those sent away stay away. Every
// browser starts over in its seat. False before a game.
func (h *Host) Restart() bool {
	if h.Game == nil {
		return false
	}
	if h.cpu != nil {
		h.cpu.Stop()
		h.cpu = nil
	}
	old := h.Seats
	h.Seats = nil
	for _, s := range old {
		if s.Kind != Computer && s.Kind != AI && !s.Kicked {
			h.Seats = append(h.Seats, s)
		}
	}
	for c := range h.clients { // their seats, where they are now
		if c.seat >= 0 && c.seat < len(old) {
			c.seat = slices.Index(h.Seats, old[c.seat])
		}
	}
	h.Game = nil
	return h.Start()
}

// Kick sends a visitor away: out of the lobby, or, in a game, their seat
// to the computer for the rest of it.
func (h *Host) Kick(seat int) {
	if seat < 0 || seat >= len(h.Seats) || h.Seats[seat].Kind != Visitor {
		return
	}
	s := h.Seats[seat]
	if s.conn != nil {
		s.conn.bye(i18n.T("The host sent you away."))
	}
	if s.gone != nil {
		s.gone.Stop()
	}
	if s.conn != nil {
		delete(h.clients, s.conn)
	}
	if h.Game == nil {
		h.removeSeat(seat)
		return
	}
	s.key, s.conn, s.Here, s.Kicked = "", nil, false, true
	h.Game.SetAway(seat, true)
	h.Flush()
}

// Act does a move for the player at seat, a person at the host's screen
// or a visitor; false when it is not theirs to do now.
func (h *Host) Act(seat int, a Action) bool {
	g := h.Game
	if g == nil || seat != g.Turn || g.Players[seat].Computer() || g.Players[seat].AI {
		return false
	}
	var ok bool
	switch a.Do {
	case "roll":
		ok = g.Roll()
	case "bail":
		ok = g.PayBail()
	case "buy":
		ok = g.Buy()
	case "pass":
		ok = g.Pass()
	case "shop":
		ok = g.BuyCard(a.Card)
	case "leave":
		ok = g.LeaveShop()
	case "end":
		ok = g.EndTurn()
	case "build":
		ok = a.Square >= 0 && a.Square < len(games.Board) && g.Build(a.Square)
	case "sell":
		ok = a.Square >= 0 && a.Square < len(games.Board) && g.Sell(a.Square)
	case "card":
		ok = g.Play(a.Card, a.Target)
	}
	if ok {
		h.Flush()
	}
	return ok
}

// Action is a move: what to do (roll, bail, buy, pass, shop, leave, end,
// build, sell, card), on which square, with which card, at what target.
type Action struct {
	Do     string `json:"do"`
	Square int    `json:"sq"`
	Card   int    `json:"card"`
	Target int    `json:"target"`
}

// Flush hands what happened to the host's screen and every browser, each
// as their player sees it, with the game as it now stands; then has the
// computer move when its turn comes, once the board has shown it. The
// host's screen calls it after a move it made on Game itself.
func (h *Host) Flush() {
	if h.Game == nil || h.closed {
		return
	}
	events := h.Game.Take()
	if h.opt.Events != nil && len(events) > 0 {
		h.opt.Events(events)
	}
	for c := range h.clients {
		if len(events) > 0 {
			c.send(h.eventsMsg(c, events))
		}
		c.send(h.stateMsg(c))
	}
	h.changed()
	if h.opt.AI != nil {
		h.opt.AI.Next(h.Game, time.Duration(float64(pace(events))*paceScale), h.Flush)
	}
	if h.Game.Phase == games.PhaseCPU && h.cpu == nil {
		h.cpu = time.AfterFunc(time.Duration(float64(pace(events))*paceScale), func() {
			h.opt.Run(func() {
				h.cpu = nil
				if !h.closed && h.Game.PlayCPU() {
					h.Flush()
				} else if !h.closed {
					h.Flush() // someone came back in time: theirs again
				}
			})
		})
	}
}

// pace is about how long the board takes to show events, and a breath
// after: the computer moves once its last move has been seen.
func pace(events []games.FortuneEvent) time.Duration {
	s := 0.5
	for _, e := range events {
		switch e.Kind {
		case games.EvDice:
			s += 0.55
		case games.EvWalk:
			s += 0.14*float64(e.Steps) + 0.08
		case games.EvJump:
			s += 0.45
		case games.EvCard:
			s += 1.1
			if e.Played {
				s += 0.5
			}
		case games.EvNote:
			s += 0.75
		}
	}
	return time.Duration(s * float64(time.Second))
}

func (h *Host) changed() {
	if h.opt.Changed != nil {
		h.opt.Changed()
	}
}

// lobby tells everyone in it who is there.
func (h *Host) lobby() {
	for c := range h.clients {
		c.send(h.lobbyMsg(c))
	}
	h.changed()
}

// join seats a browser: back in its seat with its key, else in the lobby
// as a new visitor while there is room, else to watch.
func (h *Host) join(c *client, m joinMsg) {
	if m.Code != h.Code {
		c.bye(i18n.T("That is not this game's code."))
		return
	}
	c.seat = -1
	for i, s := range h.Seats {
		if m.Key != "" && s.key == m.Key {
			if s.conn != nil && s.conn != c { // the same seat in another tab: this one now
				s.conn.bye(i18n.T("Your seat was taken up in another window."))
				delete(h.clients, s.conn)
			}
			c.seat = i
		}
	}
	if c.seat < 0 && h.Room() > 0 && strings.TrimSpace(m.Name) != "" {
		h.Seats = append(h.Seats, &Seat{Name: cleanName(m.Name), Kind: Visitor, key: newKey()})
		c.seat = len(h.Seats) - 1
	}
	h.clients[c] = true
	key := ""
	if c.seat >= 0 {
		s := h.Seats[c.seat]
		s.conn, s.Here, key = c, true, s.key
		if s.gone != nil {
			s.gone.Stop()
			s.gone = nil
		}
	}
	c.send(helloMsg{T: "hello", Key: key, Seat: c.seat})
	if h.Game == nil {
		h.lobby()
		return
	}
	c.send(h.startMsg(c))
	c.send(h.stateMsg(c))
	if c.seat >= 0 && h.Game.Players[c.seat].Away {
		h.Game.SetAway(c.seat, false)
		h.Flush()
	}
	h.changed()
}

// leave forgets a browser gone: out of the lobby, or, in a game, its seat
// to the computer once it has been gone AwayAfter.
func (h *Host) leave(c *client) {
	if !h.clients[c] {
		return
	}
	delete(h.clients, c)
	if c.seat < 0 || c.seat >= len(h.Seats) || h.Seats[c.seat].conn != c {
		return
	}
	s := h.Seats[c.seat]
	s.conn, s.Here = nil, false
	if h.Game == nil {
		h.removeSeat(c.seat)
		return
	}
	seat := c.seat
	s.gone = time.AfterFunc(AwayAfter, func() {
		h.opt.Run(func() {
			if s.gone != nil && !s.Here && !h.closed {
				s.gone = nil
				h.Game.SetAway(seat, true)
				h.Flush()
			}
		})
	})
	h.changed()
}

// removeSeat takes a visitor out of the lobby; those after move up.
func (h *Host) removeSeat(seat int) {
	h.Seats = append(h.Seats[:seat], h.Seats[seat+1:]...)
	for c := range h.clients {
		if c.seat > seat {
			c.seat--
		}
	}
	h.lobby()
}

// cleanName is a visitor's name as the game shows it: one line, at most
// 20 letters.
func cleanName(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	for utf8.RuneCountInString(name) > 20 {
		_, n := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-n]
	}
	return name
}

// newCode is a lobby's code: six letters and digits not mistaken for
// each other.
func newCode() string {
	const letters = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 6)
	rand.Read(b)
	for i := range b {
		b[i] = letters[int(b[i])%len(letters)]
	}
	return string(b)
}

func newKey() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
