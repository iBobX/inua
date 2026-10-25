package checkersnet

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/games/checkersai"
)

func init() {
	paceScale = 0.01
	AwayAfter = 50 * time.Millisecond
}

// testHost is a Host listening on the loopback, its state behind a lock
// as the GTK main loop would keep it.
type testHost struct {
	*Host
	mu     sync.Mutex
	events int
}

func newTestHost(t *testing.T, opt Options) *testHost {
	t.Helper()
	x := &testHost{}
	opt.Run = func(f func()) { x.mu.Lock(); defer x.mu.Unlock(); f() }
	opt.Events = func(e []games.CheckersEvent) { x.events += len(e) }
	opt.Seed = 1
	x.Host = NewHost(opt)
	if err := x.Serve("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { x.do(func() { x.Close() }) })
	return x
}

func (x *testHost) do(f func()) {
	x.mu.Lock()
	defer x.mu.Unlock()
	f()
}

type browser struct {
	t  *testing.T
	ws *websocket.Conn
}

func (x *testHost) join(t *testing.T, name, key string) (*browser, helloMsg) {
	t.Helper()
	ws, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://127.0.0.1:%d/ws", x.Port()), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	b := &browser{t, ws}
	b.send(inMsg{T: "join", Code: x.Code, Name: name, Key: key})
	var hello helloMsg
	b.expect("hello", &hello)
	return b, hello
}

func (b *browser) send(m inMsg) {
	b.t.Helper()
	if err := b.ws.WriteJSON(m); err != nil {
		b.t.Fatal(err)
	}
}

// expect reads until a message of type t, into v; the last of a run of
// them already waiting is not looked for.
func (b *browser) expect(t string, v any) {
	b.t.Helper()
	b.ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, data, err := b.ws.ReadMessage()
		if err != nil {
			b.t.Fatalf("waiting for %s: %v", t, err)
		}
		var head struct{ T string }
		json.Unmarshal(data, &head)
		if head.T == t {
			if v != nil {
				json.Unmarshal(data, v)
			}
			return
		}
	}
}

// until reads messages of type t until one passes ok.
func (b *browser) until(t string, ok func(data []byte) bool) {
	b.t.Helper()
	b.ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		_, data, err := b.ws.ReadMessage()
		if err != nil {
			b.t.Fatalf("waiting for %s: %v", t, err)
		}
		var head struct{ T string }
		json.Unmarshal(data, &head)
		if head.T == t && ok(data) {
			return
		}
	}
}

func lobbyOf(data []byte) lobbyMsg {
	var m lobbyMsg
	json.Unmarshal(data, &m)
	return m
}

// With the host only hosting, the first visitor leads: sets the players,
// the seats and the teams, starts; the second watches the setup.
func TestPureHostLead(t *testing.T) {
	x := newTestHost(t, Options{Players: 4})
	ann, hello := x.join(t, "Ann", "")
	if hello.Seat != 0 {
		t.Fatalf("Ann's seat %d", hello.Seat)
	}
	ann.until("lobby", func(d []byte) bool { return lobbyOf(d).Lead })
	bob, hello := x.join(t, "Bob", "")
	if hello.Seat != 1 {
		t.Fatalf("Bob's seat %d", hello.Seat)
	}
	bob.until("lobby", func(d []byte) bool { m := lobbyOf(d); return !m.Lead && m.LeadName == "Ann" })

	// Bob may not set the game up.
	bob.send(inMsg{T: "players", Players: 2})
	ann.send(inMsg{T: "seat", Index: 2, Kind: "computer", Level: int(checkersai.Hard)})
	ann.send(inMsg{T: "team", Index: 2, Team: 0})
	ann.send(inMsg{T: "team", Index: 3, Team: 1})
	ann.until("lobby", func(d []byte) bool {
		m := lobbyOf(d)
		return len(m.Seats) == 4 && m.Seats[2].Kind == "computer" && m.Seats[2].Level == 2 && m.Seats[2].Team == 0 && m.Seats[3].Team == 1
	})
	ann.send(inMsg{T: "start"})
	var start startMsg
	ann.expect("start", &start)
	if len(start.Holes) != 121 || len(start.Players) != 4 || start.Players[2].Name != "Matsu" || start.Players[3].Name != "Take" {
		t.Errorf("start %d holes, players %+v", len(start.Holes), start.Players)
	}
	x.do(func() {
		if x.Game == nil || x.Game.Players[2].Team != 0 || x.Game.Players[3].Team != 1 {
			t.Errorf("teams not set")
		}
	})
}

// The host's screen leads when people play at it: visitors may not set
// the game up.
func TestHostLeads(t *testing.T) {
	x := newTestHost(t, Options{Players: 2, Local: []string{"Host"}})
	ann, _ := x.join(t, "Ann", "")
	ann.until("lobby", func(d []byte) bool { m := lobbyOf(d); return !m.Lead && m.HostLeads })
	ann.send(inMsg{T: "players", Players: 6})
	time.Sleep(50 * time.Millisecond)
	x.do(func() {
		if len(x.Seats) != 2 || x.Lead() != -1 {
			t.Errorf("%d seats, lead %d", len(x.Seats), x.Lead())
		}
		if !x.SetPlayers(3) || len(x.Seats) != 3 {
			t.Error("the host could not set the players")
		}
		if x.SetPlayers(2) != true || x.Seats[1].Name != "Ann" {
			t.Error("a seat with a person taken away")
		}
	})
}

// One team is no game; a team of two plays against the rest.
func TestTeams(t *testing.T) {
	x := newTestHost(t, Options{Players: 2, Local: []string{"A", "B"}})
	x.do(func() {
		x.SetTeam(1, 0)
		if x.Why() == "" || x.Start() {
			t.Error("started with one team")
		}
		x.SetTeam(1, 1)
		if !x.Start() {
			t.Errorf("did not start: %s", x.Why())
		}
	})
}

// A visitor moves in their turn only; the computer plays its seats, and
// the turn comes back to the visitor.
func TestPlay(t *testing.T) {
	x := newTestHost(t, Options{Players: 2})
	ann, _ := x.join(t, "Ann", "")
	ann.until("lobby", func(d []byte) bool { return lobbyOf(d).Lead })
	ann.send(inMsg{T: "seat", Index: 1, Kind: "computer"})
	ann.send(inMsg{T: "start"})
	ann.expect("start", nil)
	var from, to int
	x.do(func() {
		m, _ := checkersai.Best(x.Game, checkersai.Normal, x.rng)
		from, to = m.From, m.To
	})
	ann.send(inMsg{T: "move", From: from, To: to})
	ann.until("state", func(d []byte) bool {
		var s stateMsg
		json.Unmarshal(d, &s)
		return s.Turn == 0 && s.Round == 2 // the computer has moved
	})
	x.do(func() {
		if x.Game.Holes[to] != 0 || x.events < 2 {
			t.Errorf("hole %d: %d, %d events", to, x.Game.Holes[to], x.events)
		}
		// Not Ann's turn's marble: refused.
		if x.Act(0, to, to) {
			t.Error("a move to where it is")
		}
	})
}

// Restart starts again with the same seats; back to the lobby, they can
// be changed.
func TestRestartAndLobby(t *testing.T) {
	x := newTestHost(t, Options{Players: 2})
	ann, _ := x.join(t, "Ann", "")
	ann.until("lobby", func(d []byte) bool { return lobbyOf(d).Lead })
	ann.send(inMsg{T: "start"})
	ann.expect("start", nil)
	var first *games.Checkers
	x.do(func() { first = x.Game })
	ann.send(inMsg{T: "restart"})
	ann.expect("start", nil)
	x.do(func() {
		if x.Game == first || x.Game == nil {
			t.Error("not a new game")
		}
	})
	ann.send(inMsg{T: "lobby"})
	ann.until("lobby", func(d []byte) bool { return lobbyOf(d).Lead })
	ann.send(inMsg{T: "players", Players: 6})
	ann.until("lobby", func(d []byte) bool { return len(lobbyOf(d).Seats) == 6 })
}

// The lead gone, the next visitor leads; a visitor gone from a game has
// the computer play for them.
func TestLeadLeaves(t *testing.T) {
	x := newTestHost(t, Options{Players: 2})
	ann, _ := x.join(t, "Ann", "")
	ann.until("lobby", func(d []byte) bool { return lobbyOf(d).Lead })
	bob, _ := x.join(t, "Bob", "")
	bob.expect("lobby", nil)
	ann.send(inMsg{T: "start"})
	bob.expect("start", nil)
	ann.ws.Close()
	bob.until("state", func(d []byte) bool { var s stateMsg; json.Unmarshal(d, &s); return s.Lead })
	// Ann's turn first; once she is away the computer moves for her.
	bob.until("state", func(d []byte) bool { var s stateMsg; json.Unmarshal(d, &s); return s.Turn == 1 })
	x.do(func() {
		if !x.Seats[0].Away {
			t.Error("Ann not away")
		}
	})
}

// The AI plays its seat with the model's pick.
func TestAISeat(t *testing.T) {
	asked := make(chan string, 4)
	x := newTestHost(t, Options{Players: 2, Local: []string{"Host"}, Profiles: []string{"fast"},
		Choose: func(_ context.Context, profile, _, _ string, options []string) (int, error) {
			asked <- profile
			return 0, nil
		}})
	x.do(func() {
		if !x.SetSeat(1, AI, 0, "fast", "") || !x.Start() {
			t.Fatal("no AI seat")
		}
		if x.Seats[1].Name != "fast" {
			t.Errorf("named %q", x.Seats[1].Name)
		}
		m, _ := checkersai.Best(x.Game, checkersai.Normal, x.rng)
		x.Act(0, m.From, m.To)
	})
	select {
	case p := <-asked:
		if p != "fast" {
			t.Errorf("asked %q", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the AI was not asked")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var turn int
		x.do(func() { turn = x.Game.Turn })
		if turn == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the AI did not move")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The page's files, three.js and its words.
func TestPage(t *testing.T) {
	x := newTestHost(t, Options{Players: 2})
	for _, p := range []string{"/", "/vendor/three/three.module.min.js", "/texts.json"} {
		res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d%s", x.Port(), p))
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("%s: %v", p, err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}
}
