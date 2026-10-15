package fortunenet

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/lyricat/inua/internal/games"
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
	events int // events handed to the host's screen
}

func newTestHost(t *testing.T, local []string, rivals int) *testHost {
	t.Helper()
	x := &testHost{}
	x.Host = NewHost(Options{
		Settings: games.FortuneSettings{Rivals: rivals, Rounds: 30, Cash: 1500},
		Local:    local,
		Run:      func(f func()) { x.mu.Lock(); defer x.mu.Unlock(); f() },
		Events:   func(e []games.FortuneEvent) { x.events += len(e) },
		Seed:     1,
	})
	if err := x.Serve("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { x.do(func() { x.Close() }) })
	return x
}

// do runs f with the host's lock, as Options.Run would.
func (x *testHost) do(f func()) {
	x.mu.Lock()
	defer x.mu.Unlock()
	f()
}

// browser is a visitor's connection.
type browser struct {
	t  *testing.T
	ws *websocket.Conn
}

func (x *testHost) dial(t *testing.T) *browser {
	t.Helper()
	ws, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://127.0.0.1:%d/ws", x.Port()), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	return &browser{t, ws}
}

// join joins with the code, a name and a key, and returns the hello.
func (x *testHost) join(t *testing.T, name, key string) (*browser, helloMsg) {
	b := x.dial(t)
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

// expect reads messages until one of type t, into v.
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
			if err := json.Unmarshal(data, v); err != nil {
				b.t.Fatal(err)
			}
			return
		}
	}
}

// until reads states until one satisfies ok.
func (b *browser) until(ok func(stateMsg) bool) stateMsg {
	b.t.Helper()
	for {
		var s stateMsg
		b.expect("state", &s)
		if ok(s) {
			return s
		}
	}
}

// eventually waits for the host to come to ok.
func (x *testHost) eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for range 500 {
		done := false
		x.do(func() { done = ok() })
		if done {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("never %s", what)
}

// A visitor joins with the code and is seated after the people at the
// host's screen; a wrong code is turned away; the start tells everyone
// the board and their seat.
func TestFortuneNetJoinAndStart(t *testing.T) {
	x := newTestHost(t, []string{"Ann"}, 1)
	wrong := x.dial(t)
	wrong.send(inMsg{T: "join", Code: "NOPE", Name: "Eve"})
	var bye byeMsg
	wrong.expect("bye", &bye)

	ben, hello := x.join(t, "  Ben\n ", "")
	if hello.Seat != 1 || hello.Key == "" {
		t.Fatalf("hello %+v", hello)
	}
	var lobby lobbyMsg
	ben.expect("lobby", &lobby)
	if len(lobby.Seats) != 2 || lobby.Seats[1].Name != "Ben" || lobby.Seats[1].Kind != "visitor" || lobby.Room != 1 {
		t.Fatalf("lobby %+v", lobby)
	}
	x.do(func() {
		if !x.Start() {
			t.Fatal("not started")
		}
	})
	var start startMsg
	ben.expect("start", &start)
	if start.You != 1 || len(start.Players) != 3 || start.Players[2].Kind != "computer" || len(start.Squares) != len(games.Board) {
		t.Fatalf("start: you %d, players %+v, %d squares", start.You, start.Players, len(start.Squares))
	}
	s := ben.until(func(stateMsg) bool { return true })
	if s.Turn != 0 || s.Phase != "roll" || s.Players[0].Name != "Ann" || s.Build != nil {
		t.Fatalf("state %+v", s)
	}
}

// Only the player whose turn it is moves; each browser sees its own hand
// and what happened, and the computer plays its turns by itself.
func TestFortuneNetMoves(t *testing.T) {
	x := newTestHost(t, nil, 1)
	ann, _ := x.join(t, "Ann", "")
	ben, _ := x.join(t, "Ben", "")
	x.do(func() { x.Start() })
	x.do(func() { x.Game.Players[0].Cards = []games.CardKind{games.CardFreeRent} })

	ben.send(inMsg{T: "act", Action: Action{Do: "roll"}})
	ann.send(inMsg{T: "act", Action: Action{Do: "roll"}})
	var ev eventsMsg
	ann.expect("events", &ev)
	if len(ev.Events) == 0 || ev.Events[0].Kind != games.EvDice || ev.Events[0].Player != 0 {
		t.Fatalf("events %+v", ev.Events)
	}
	ben.expect("events", &ev)
	if ev.Events[0].Player != 0 {
		t.Fatal("Ben's roll was taken")
	}
	// Ann ends her turn whatever she landed on; Ben's comes, then the
	// computer's, then Ann's again.
	x.do(func() {
		for _, a := range []string{"pass", "leave", "end"} {
			x.Act(0, Action{Do: a})
		}
	})
	b := ben.until(func(s stateMsg) bool { return s.Turn == 1 })
	if b.Hand == nil || len(b.Hand) != 0 || b.Phase != "roll" {
		t.Fatalf("Ben's state: hand %v phase %s", b.Hand, b.Phase)
	}
	x.do(func() {
		if x.Act(0, Action{Do: "roll"}) {
			t.Error("Ann rolled in Ben's turn")
		}
	})
	ben.send(inMsg{T: "act", Action: Action{Do: "roll"}})
	ben.until(func(s stateMsg) bool { return s.Turn == 1 && s.Phase != "roll" })
	x.do(func() {
		for _, a := range []string{"pass", "leave", "end"} {
			x.Act(1, Action{Do: a})
		}
	})
	a := ann.until(func(s stateMsg) bool { return s.Turn == 0 && s.Round == 2 })
	if len(a.Hand) != 1 || a.Hand[0] != int(games.CardFreeRent) || a.Plays == nil {
		t.Fatalf("Ann's state: hand %v plays %v", a.Hand, a.Plays)
	}
	if x.events == 0 {
		t.Error("the host's screen was told nothing")
	}
}

// A visitor gone has the computer play for them, and is back in their
// seat with its key; the seat opened in another window takes it over.
func TestFortuneNetAwayAndBack(t *testing.T) {
	x := newTestHost(t, []string{"Ann"}, 0)
	ben, hello := x.join(t, "Ben", "")
	x.do(func() { x.Start() })
	ben.ws.Close()
	x.eventually(t, "away", func() bool { return x.Game.Players[1].Away })

	again, back := x.join(t, "", hello.Key)
	if back.Seat != 1 || back.Key != hello.Key {
		t.Fatalf("back as %+v", back)
	}
	x.eventually(t, "back", func() bool { return !x.Game.Players[1].Away })
	var start startMsg
	again.expect("start", &start)

	_, third := x.join(t, "", hello.Key)
	if third.Seat != 1 {
		t.Fatalf("another window: %+v", third)
	}
	var bye byeMsg
	again.expect("bye", &bye)
	if !strings.Contains(bye.Text, "another window") {
		t.Errorf("bye %q", bye.Text)
	}
}

// The host sends a visitor away: out of the lobby, or in a game their
// seat to the computer, the key no good any more.
func TestFortuneNetKick(t *testing.T) {
	x := newTestHost(t, []string{"Ann"}, 0)
	eve, _ := x.join(t, "Eve", "")
	ben, hello := x.join(t, "Ben", "")
	x.do(func() { x.Kick(1) })
	var bye byeMsg
	eve.expect("bye", &bye)
	var lobby lobbyMsg
	ben.until2(&lobby, func() bool { return len(lobby.Seats) == 2 && lobby.You == 1 })

	x.do(func() { x.Start(); x.Kick(1) })
	ben.expect("bye", &bye)
	x.do(func() {
		if !x.Game.Players[1].Away || !x.Seats[1].Kicked {
			t.Error("Ben's seat not the computer's")
		}
	})
	_, back := x.join(t, "", hello.Key)
	if back.Seat != -1 {
		t.Errorf("came back in a kicked seat: %+v", back)
	}
}

// With no room left a visitor watches: seated nowhere, with no hand.
func TestFortuneNetWatch(t *testing.T) {
	x := newTestHost(t, []string{"Ann", "Ben", "Cy"}, 1)
	b, hello := x.join(t, "Dee", "")
	if hello.Seat != -1 || hello.Key != "" {
		t.Fatalf("seated with no room: %+v", hello)
	}
	x.do(func() { x.Start() })
	s := b.until(func(stateMsg) bool { return true })
	if s.Hand != nil || len(s.Players) != 4 {
		t.Fatalf("watching: hand %v, %d players", s.Hand, len(s.Players))
	}
}

// until2 reads lobby messages into v until ok.
func (b *browser) until2(v *lobbyMsg, ok func() bool) {
	b.t.Helper()
	for {
		b.expect("lobby", v)
		if ok() {
			return
		}
	}
}

// The page: its files with ETags (unchanged, nothing to send again), its
// words, and the board's print once the game is on, drawn once.
func TestFortuneNetPage(t *testing.T) {
	prints := 0
	x := &testHost{}
	x.Host = NewHost(Options{
		Settings: games.FortuneSettings{Rivals: 1},
		Local:    []string{"Ann"},
		Run:      func(f func()) { x.mu.Lock(); defer x.mu.Unlock(); f() },
		Print:    func(city int) []byte { prints++; return []byte("jpeg") },
	})
	if err := x.Serve("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { x.do(func() { x.Close() }) })
	base := fmt.Sprintf("http://127.0.0.1:%d", x.Port())
	get := func(path, etag string) (*http.Response, string) {
		t.Helper()
		req, _ := http.NewRequest("GET", base+path, nil)
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res, string(b)
	}
	res, body := get("/", "")
	tag := res.Header.Get("ETag")
	if res.StatusCode != 200 || !strings.Contains(body, "fortune.js") || tag == "" {
		t.Fatalf("page: %d %q", res.StatusCode, tag)
	}
	if res, _ := get("/", tag); res.StatusCode != http.StatusNotModified {
		t.Errorf("unchanged page sent again: %d", res.StatusCode)
	}
	if res, _ := get("/vendor/three/three.module.min.js", ""); res.StatusCode != 200 {
		t.Errorf("three.js: %d", res.StatusCode)
	}
	if _, body := get("/texts.json", ""); !strings.Contains(body, `"roll":`) {
		t.Errorf("texts: %.80s", body)
	}
	if res, _ := get("/board.jpg", ""); res.StatusCode != http.StatusNotFound {
		t.Errorf("a print before the game: %d", res.StatusCode)
	}
	x.do(func() { x.Start() })
	for range 2 {
		if res, body := get("/board.jpg", ""); res.StatusCode != 200 || body != "jpeg" || res.Header.Get("Content-Type") != "image/jpeg" {
			t.Errorf("print: %d %q", res.StatusCode, body)
		}
	}
	if prints != 1 {
		t.Errorf("print drawn %d times", prints)
	}
}

// Visitors are given this computer's addresses on the network, not its
// containers' bridges.
func TestFortuneNetBridges(t *testing.T) {
	for name, want := range map[string]bool{"docker0": true, "br-3f2a": true, "veth12": true, "virbr0": true, "wlan0": false, "enp3s0": false, "tailscale0": false} {
		if bridge(name) != want {
			t.Errorf("%s: bridge %v", name, !want)
		}
	}
}
