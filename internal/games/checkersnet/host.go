// Package checkersnet hosts a game of Chinese checkers for the local
// network: people at the host's screen, visitors in a browser who join
// with the host's code, the computer and the user's AI. The lobby sets the
// game up seat by seat (who plays it, and in which team); the lead does:
// the host's screen, or, when the host only hosts, the first visitor still
// there, who also starts and restarts the game from the browser. The host
// holds the game and plays it out; someone gone has the computer play for
// them until they come back with their seat's key.
//
// A Host's state lives in one place, where Options.Run runs what changes
// it (the GTK main loop, for the light app): its methods other than Serve,
// URLs and Close are called there, and its callbacks come from there.
package checkersnet

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	mrand "math/rand/v2"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/games/checkersai"
	"github.com/lyricat/inua/internal/games/gameweb"
	"github.com/lyricat/inua/internal/i18n"
)

// SeatKind is who plays a seat.
type SeatKind uint8

const (
	Open     SeatKind = iota // waiting for a visitor; the computer at the start
	Local                    // a person at the host's screen
	Visitor                  // a person in a browser
	Computer                 // the computer, at Level
	AI                       // the user's AI, asking Profile
)

var kindNames = map[SeatKind]string{Open: "open", Local: "local", Visitor: "visitor", Computer: "computer", AI: "ai"}

// Seat is a player of the game, or of the lobby before it.
type Seat struct {
	Name    string
	Kind    SeatKind
	Level   checkersai.Level // a computer's
	Profile string           // the AI's profile; "" for the default
	Team    int
	// Here: a visitor's browser is connected. Away: gone long enough that
	// the computer plays their turns. Kicked: sent away by the host.
	Here, Away, Kicked bool

	key  string
	conn *gameweb.Client
	gone *time.Timer
	at   int // when a visitor joined, for who leads after the first
}

// Options set up a Host.
type Options struct {
	// Players is how many play at first: 2, 3, 4 or 6.
	Players int
	// Local are the names of the people at the host's screen, sitting
	// first; none when the host only hosts, and the first visitor leads.
	Local     []string
	NoParking bool
	// Run runs f where the Host lives, one f at a time.
	Run func(f func())
	// Changed tells the host's screen that the lobby or the game changed;
	// Events hands it what happened in the game, as Checkers.Take does.
	Changed func()
	Events  func([]games.CheckersEvent)
	// Choose asks the user's AI (checkersai.Driver); nil when it cannot
	// play. Profiles are the profiles an AI seat may ask.
	Choose   func(ctx context.Context, profile, instructions, state string, options []string) (int, error)
	Profiles []string
	Seed     uint64
}

// MarbleColors are the players' colours by the point they start in, as
// the host's screen and the page draw them.
var MarbleColors = [games.Corners]string{"#bf4430", "#d9a43a", "#3f7a3a", "#2b4c7e", "#6b4a8e", "#1f1b17"}

// LevelNames are the computer's names at each level: the plum, the bamboo
// and the pine.
var LevelNames = [...]string{"Ume", "Take", "Matsu"}

// Host holds the lobby, then the game.
type Host struct {
	opt       Options
	Code      string  // what visitors join with
	Seats     []*Seat // in the order of play
	NoParking bool
	Game      *games.Checkers
	clients   map[*gameweb.Client]bool
	joins     int
	cpu       *time.Timer // the computer's next move, paced to the board
	ai        *checkersai.Driver
	rng       *mrand.Rand
	closed    bool
	srv       *server
}

// paceScale speeds the computer's pace up, for tests.
var paceScale = 1.0

// AwayAfter is how long a visitor may be gone in a game before the
// computer plays for them: long enough to reload the page.
var AwayAfter = 15 * time.Second

// NewHost opens a lobby: the people at the host's screen, then seats open
// for visitors, each player a team of their own.
func NewHost(opt Options) *Host {
	h := &Host{opt: opt, Code: newCode(), NoParking: opt.NoParking, clients: map[*gameweb.Client]bool{},
		rng: mrand.New(mrand.NewPCG(opt.Seed, opt.Seed^0x2545f4914f6cdd1d))}
	if opt.Choose != nil {
		h.ai = checkersai.New(opt.Choose, opt.Run, opt.Seed)
	}
	n := opt.Players
	if _, ok := games.SeatCorners[n]; !ok {
		n = 2
	}
	for _, name := range opt.Local[:min(len(opt.Local), 6)] {
		h.Seats = append(h.Seats, &Seat{Name: name, Kind: Local, Here: true})
	}
	n = max(n, len(h.Seats))
	for len(h.Seats) < n {
		h.Seats = append(h.Seats, &Seat{Kind: Open})
	}
	h.SetPlayers(fit(n))
	for i, s := range h.Seats {
		s.Team = i
	}
	return h
}

// fit is the number of players a game may have at least n seats for.
func fit(n int) int {
	for _, k := range []int{2, 3, 4, 6} {
		if k >= n {
			return k
		}
	}
	return 6
}

// HostLeads reports whether the host's screen sets the game up: when
// people play at it. Otherwise the lead visitor does (Lead).
func (h *Host) HostLeads() bool {
	return slices.ContainsFunc(h.Seats, func(s *Seat) bool { return s.Kind == Local })
}

// SetAI lets the user's AI play seats, asking with choose one of profiles;
// nil, it cannot.
func (h *Host) SetAI(choose func(ctx context.Context, profile, instructions, state string, options []string) (int, error), profiles []string) {
	h.opt.Choose, h.opt.Profiles = choose, profiles
	h.ai = nil
	if choose != nil {
		h.ai = checkersai.New(choose, h.opt.Run, h.opt.Seed)
	}
	h.lobby()
}

// Lead is the visitor's seat that sets the game up when the host only
// hosts: the one who came first of those here; -1 for none, or when the
// host's screen leads.
func (h *Host) Lead() int {
	if h.HostLeads() {
		return -1
	}
	lead, first := -1, 0
	for i, s := range h.Seats {
		if s.Kind == Visitor && s.Here && (lead < 0 || s.at < first) {
			lead, first = i, s.at
		}
	}
	return lead
}

// SetPlayers sets how many play: 2, 3, 4 or 6. Seats are added open, or
// taken away from the last that no person sits in; false when there are
// more people than that.
func (h *Host) SetPlayers(n int) bool {
	if h.Game != nil {
		return false
	}
	if _, ok := games.SeatCorners[n]; !ok {
		return false
	}
	people := 0
	for _, s := range h.Seats {
		if s.Kind == Local || s.Kind == Visitor {
			people++
		}
	}
	if people > n {
		return false
	}
	for len(h.Seats) > n {
		last := len(h.Seats) - 1
		for h.Seats[last].Kind == Local || h.Seats[last].Kind == Visitor {
			last--
		}
		h.removeSeat(last)
	}
	for len(h.Seats) < n {
		h.Seats = append(h.Seats, &Seat{Kind: Open, Team: h.freeTeam()})
	}
	h.lobby()
	return true
}

// freeTeam is a team no seat is in.
func (h *Host) freeTeam() int {
	for t := 0; ; t++ {
		if !slices.ContainsFunc(h.Seats, func(s *Seat) bool { return s.Team == t }) {
			return t
		}
	}
}

// SetSeat sets who plays a seat no visitor sits in: a person at the host's
// screen (named name), open for a visitor, the computer at a level, or
// the AI asking a profile.
func (h *Host) SetSeat(seat int, kind SeatKind, level checkersai.Level, profile, name string) bool {
	if h.Game != nil || seat < 0 || seat >= len(h.Seats) {
		return false
	}
	s := h.Seats[seat]
	if s.Kind == Visitor || kind == Visitor || kind > AI || level > checkersai.Hard {
		return false
	}
	if kind == Local {
		s.Name = cleanName(name)
	} else if s.Kind == Local {
		s.Name = ""
	}
	if kind == AI && h.opt.Choose == nil {
		return false
	}
	if kind == AI && profile != "" && !slices.Contains(h.opt.Profiles, profile) {
		profile = ""
	}
	s.Kind, s.Level, s.Profile = kind, level, profile
	h.lobby()
	return true
}

// SetTeam puts a seat in a team: the same number for partners.
func (h *Host) SetTeam(seat, team int) bool {
	if h.Game != nil || seat < 0 || seat >= len(h.Seats) || team < 0 || team >= 6 {
		return false
	}
	h.Seats[seat].Team = team
	h.lobby()
	return true
}

// SetNoParking sets the rule against stopping in others' points.
func (h *Host) SetNoParking(on bool) {
	if h.Game == nil {
		h.NoParking = on
		h.lobby()
	}
}

// Why is why the game cannot start; "" when it can.
func (h *Host) Why() string {
	if h.Game != nil {
		return i18n.T("The game is on.")
	}
	teams := map[int]bool{}
	for _, s := range h.Seats {
		teams[s.Team] = true
	}
	if len(teams) < 2 {
		return i18n.T("Everyone is in one team: make two at least.")
	}
	return ""
}

// Start starts the game with the seats as they are; open seats are the
// computer's. False when it cannot (Why).
func (h *Host) Start() bool {
	if h.Why() != "" {
		return false
	}
	for _, s := range h.Seats {
		if s.Kind == Open {
			s.Kind, s.Level = Computer, checkersai.Normal
		}
	}
	h.name()
	var set games.CheckersSettings
	set.NoParking = h.NoParking
	for _, s := range h.Seats {
		set.Players = append(set.Players, games.CheckersSeat{Name: s.Name, Team: s.Team})
	}
	h.Game = games.NewCheckers(set)
	for _, s := range h.Seats {
		s.Away = s.Kind == Visitor && !s.Here
	}
	h.changed()
	for c := range h.clients {
		c.Send(h.startMsg(c))
	}
	h.Flush()
	return true
}

// name names the computer's and the AI's seats: the computer by its level
// (a second of a level numbered), the AI by its profile.
func (h *Host) name() {
	count := map[string]int{}
	for _, s := range h.Seats {
		var base string
		switch s.Kind {
		case Computer:
			base = LevelNames[s.Level]
		case AI:
			base = s.Profile
			if base == "" {
				base = i18n.T("AI")
			}
		default:
			continue
		}
		count[base]++
		s.Name = base
		if n := count[base]; n > 1 {
			s.Name = fmt.Sprintf("%s %d", base, n)
		}
	}
}

// Restart starts a new game with the same seats; ToLobby ends the game for
// the seats to be set up again. Seats played by the computer for someone
// sent away stay the computer's.
func (h *Host) Restart() bool {
	if h.Game == nil {
		return false
	}
	h.stop()
	h.Game = nil
	return h.Start()
}

func (h *Host) ToLobby() bool {
	if h.Game == nil {
		return false
	}
	h.stop()
	h.Game = nil
	for _, s := range h.Seats {
		if s.Kicked {
			s.Kind, s.Kicked, s.Level = Computer, false, checkersai.Normal
		}
		s.Away = false
	}
	h.lobby()
	return true
}

func (h *Host) stop() {
	if h.cpu != nil {
		h.cpu.Stop()
		h.cpu = nil
	}
	if h.ai != nil {
		h.ai.Stop()
	}
}

// Kick sends a visitor away: their seat open again in the lobby, or in a
// game the computer's to the end.
func (h *Host) Kick(seat int) {
	if seat < 0 || seat >= len(h.Seats) || h.Seats[seat].Kind != Visitor {
		return
	}
	s := h.Seats[seat]
	if s.conn != nil {
		s.conn.Bye(i18n.T("The host sent you away."))
		delete(h.clients, s.conn)
	}
	if s.gone != nil {
		s.gone.Stop()
		s.gone = nil
	}
	s.key, s.conn, s.Here = "", nil, false
	if h.Game == nil {
		s.Kind, s.Name = Open, ""
		h.lobby()
		return
	}
	s.Kicked, s.Away = true, true
	h.Flush()
}

// Act moves the marble in hole from to hole to for the person at seat
// (at the host's screen, or a visitor); false when it is not theirs to.
func (h *Host) Act(seat, from, to int) bool {
	g := h.Game
	if g == nil || seat != g.Turn || seat < 0 || seat >= len(h.Seats) || !h.person(seat) {
		return false
	}
	if !g.Move(from, to) {
		return false
	}
	h.Flush()
	return true
}

// person reports whether a person plays a seat's turns now: one at the
// host's screen, or a visitor not away.
func (h *Host) person(seat int) bool {
	s := h.Seats[seat]
	return s.Kind == Local || (s.Kind == Visitor && !s.Away)
}

// Flush hands what happened to the host's screen and every browser, with
// the game as it now stands; then has the computer or the AI move when
// its turn comes, once the board has shown it.
func (h *Host) Flush() {
	g := h.Game
	if g == nil || h.closed {
		return
	}
	events := g.Take()
	if h.opt.Events != nil && len(events) > 0 {
		h.opt.Events(events)
	}
	for c := range h.clients {
		if len(events) > 0 {
			c.Send(eventsMsg{T: "events", Events: eventsOf(events)})
		}
		c.Send(h.stateMsg(c))
	}
	h.changed()
	if g.Over || h.person(g.Turn) {
		return
	}
	wait := time.Duration(float64(pace(events)) * paceScale)
	s := h.Seats[g.Turn]
	if s.Kind == AI && h.ai != nil {
		h.ai.Next(g, s.Profile, wait, h.Flush)
		return
	}
	if h.cpu != nil {
		return
	}
	level := s.Level
	if s.Kind != Computer {
		level = checkersai.Normal // playing for someone away, or the AI with no driver
	}
	h.cpu = time.AfterFunc(wait, func() {
		h.opt.Run(func() {
			h.cpu = nil
			if h.closed || h.Game != g || g.Over || h.person(g.Turn) {
				h.Flush()
				return
			}
			checkersai.Play(g, level, h.rng)
			h.Flush()
		})
	})
}

// pace is about how long the board takes to show events, and a breath
// after: the computer moves once its last move has been seen.
func pace(events []games.CheckersEvent) time.Duration {
	s := 0.45
	for _, e := range events {
		switch e.Kind {
		case games.CkMove:
			s += 0.2 * float64(len(e.Path)-1)
		case games.CkHome, games.CkWin:
			s += 0.8
		}
	}
	return time.Duration(s * float64(time.Second))
}

func (h *Host) changed() {
	if h.opt.Changed != nil {
		h.opt.Changed()
	}
}

// lobby tells everyone in it how the game is set up.
func (h *Host) lobby() {
	if h.Game != nil {
		return
	}
	for c := range h.clients {
		c.Send(h.lobbyMsg(c))
	}
	h.changed()
}

// join seats a browser: back in its seat with its key, else in the lobby
// in the first open seat, else to watch.
func (h *Host) join(c *gameweb.Client, m inMsg) {
	if m.Code != h.Code {
		c.Bye(i18n.T("That is not this game's code."))
		return
	}
	c.Seat = -1
	for i, s := range h.Seats {
		if m.Key != "" && s.key == m.Key {
			if s.conn != nil && s.conn != c { // the same seat in another tab: this one now
				s.conn.Bye(i18n.T("Your seat was taken up in another window."))
				delete(h.clients, s.conn)
			}
			c.Seat = i
		}
	}
	if open := slices.IndexFunc(h.Seats, func(s *Seat) bool { return s.Kind == Open }); c.Seat < 0 && h.Game == nil && open >= 0 && strings.TrimSpace(m.Name) != "" {
		h.joins++
		s := h.Seats[open]
		s.Name, s.Kind, s.key, s.at = cleanName(m.Name), Visitor, newKey(), h.joins
		c.Seat = open
	}
	h.clients[c] = true
	key := ""
	if c.Seat >= 0 {
		s := h.Seats[c.Seat]
		s.conn, s.Here, key = c, true, s.key
		if s.gone != nil {
			s.gone.Stop()
			s.gone = nil
		}
	}
	c.Send(helloMsg{T: "hello", Key: key, Seat: c.Seat})
	if h.Game == nil {
		h.lobby()
		return
	}
	c.Send(h.startMsg(c))
	if c.Seat >= 0 && h.Seats[c.Seat].Away && !h.Seats[c.Seat].Kicked {
		h.Seats[c.Seat].Away = false
	}
	h.Flush()
}

// leave forgets a browser gone: its seat open again in the lobby, or in a
// game the computer's once it has been gone AwayAfter.
func (h *Host) leave(c *gameweb.Client) {
	if !h.clients[c] {
		return
	}
	delete(h.clients, c)
	if c.Seat < 0 || c.Seat >= len(h.Seats) || h.Seats[c.Seat].conn != c {
		h.changed()
		return
	}
	s := h.Seats[c.Seat]
	s.conn, s.Here = nil, false
	if h.Game == nil {
		s.Kind, s.Name, s.key = Open, "", ""
		h.lobby()
		return
	}
	s.gone = time.AfterFunc(AwayAfter, func() {
		h.opt.Run(func() {
			if s.gone != nil && !s.Here && !h.closed {
				s.gone = nil
				s.Away = true
				h.Flush()
			}
		})
	})
	h.Flush() // the lead may have changed
}

// lead reports whether a browser leads the game's setup.
func (h *Host) lead(c *gameweb.Client) bool {
	return c.Seat >= 0 && c.Seat == h.Lead()
}

// fromLead does what the lead visitor asked: set the game up, start it,
// start it again, or go back to the lobby.
func (h *Host) fromLead(c *gameweb.Client, m inMsg) {
	if !h.lead(c) {
		return
	}
	switch m.T {
	case "players":
		h.SetPlayers(m.Players)
	case "seat":
		if kind := kindOf(m.Kind); kind != Local { // people at the host's screen are the host's to seat
			h.SetSeat(m.Index, kind, checkersai.Level(m.Level), m.Profile, "")
		}
	case "team":
		h.SetTeam(m.Index, m.Team)
	case "rules":
		h.SetNoParking(m.NoParking)
	case "start":
		h.Start()
	case "restart":
		h.Restart()
	case "lobby":
		h.ToLobby()
	}
}

func kindOf(name string) SeatKind {
	for k, n := range kindNames {
		if n == name {
			return k
		}
	}
	return 255
}

// removeSeat takes a seat away; those after move up.
func (h *Host) removeSeat(seat int) {
	if s := h.Seats[seat]; s.conn != nil {
		s.conn.Seat = -1
	}
	h.Seats = append(h.Seats[:seat], h.Seats[seat+1:]...)
	for c := range h.clients {
		if c.Seat > seat {
			c.Seat--
		}
	}
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
