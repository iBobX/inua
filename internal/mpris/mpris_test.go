package mpris

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbustest"
	"github.com/lyricat/inua/internal/system"
)

// The shell's own MPRIS client (the control center's) sees the player,
// follows it, and drives it.
func TestServer(t *testing.T) {
	addr := dbustest.Start(t)
	conn := dbustest.Conn(t, addr)
	var mu sync.Mutex
	var did []string
	note := func(s string) func() { return func() { mu.Lock(); did = append(did, s); mu.Unlock() } }
	pos := 0 * time.Second
	s, err := Serve(conn, "inua.music", "Music", "io.github.lyricat.Inua.Music", Handler{
		PlayPause: note("playpause"), Play: note("play"), Pause: note("pause"), Next: note("next"), Previous: note("previous"),
		SetPosition: func(p time.Duration) { mu.Lock(); pos = p; mu.Unlock() },
		Position:    func() time.Duration { mu.Lock(); defer mu.Unlock(); return pos },
		SetVolume:   func(v float64) { note("volume")() },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Update(State{Status: "Playing", Title: "Tone", Artists: []string{"Test"}, Length: 2 * time.Second, Volume: 0.5, CanNext: true, CanSeek: true, Track: 1})

	m := system.NewMedia(func() (*dbus.Conn, error) { return dbustest.Dial(addr) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	states := make(chan system.MediaState, 16)
	go m.Run(ctx, func(st system.MediaState) { states <- st })
	want := func(f func(system.MediaState) bool, what string) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case st := <-states:
				if f(st) {
					return
				}
			case <-deadline:
				t.Fatalf("never %s", what)
			}
		}
	}
	want(func(st system.MediaState) bool {
		return st.Available && st.Identity == "Music" && st.Title == "Tone" && st.Artist == "Test" && st.Playing && st.CanNext && !st.CanPrev
	}, "seen playing")
	s.Update(State{Status: "Paused", Title: "Tone", Track: 1})
	want(func(st system.MediaState) bool { return !st.Playing }, "seen paused")

	if err := m.PlayPause(); err != nil {
		t.Fatal(err)
	}
	m.Next()
	obj := conn.Object("org.mpris.MediaPlayer2.inua.music", path)
	obj.Call(playerIface+".SetPosition", 0, dbus.ObjectPath("/io/github/lyricat/inua/track/1"), int64(1500000))
	obj.Call(playerIface+".SetPosition", 0, dbus.ObjectPath("/io/github/lyricat/inua/track/9"), int64(9))
	obj.Call(propsIface+".Set", 0, playerIface, "Volume", dbus.MakeVariant(0.25))
	var p dbus.Variant
	if err := obj.Call(propsIface+".Get", 0, playerIface, "Position").Store(&p); err != nil || p.Value() != int64(1500000) {
		t.Errorf("position %v %v", p, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(did) != 3 || did[0] != "playpause" || did[1] != "next" || did[2] != "volume" || pos != 1500*time.Millisecond {
		t.Errorf("did %q, position %v", did, pos)
	}
}
