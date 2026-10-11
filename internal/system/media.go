package system

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
)

const (
	mprisPrefix = "org.mpris.MediaPlayer2."
	mprisPath   = dbus.ObjectPath("/org/mpris/MediaPlayer2")
	mprisRoot   = "org.mpris.MediaPlayer2"
	mprisPlayer = "org.mpris.MediaPlayer2.Player"
)

// MediaState is the player the control center shows.
type MediaState struct {
	Available    bool
	Player       string // bus name
	Identity     string // e.g. "Firefox"
	Title        string
	Artist       string
	ArtURL       string
	Playing      bool
	CanNext      bool
	CanPrev      bool
	CanPlayPause bool
}

// Media follows MPRIS players. It shows a playing player if there is one,
// otherwise keeps the one it showed last, otherwise the first by name.
type Media struct {
	src source

	mu     sync.Mutex
	player string
}

func NewMedia(dial Dialer) *Media {
	return &Media{src: source{dial: dial, matches: [][]dbus.MatchOption{
		{dbus.WithMatchObjectPath(mprisPath), dbus.WithMatchInterface(propsIface)},
		{dbus.WithMatchSender("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg0Namespace("org.mpris.MediaPlayer2")},
	}}}
}

// Run reports the state until ctx ends.
func (m *Media) Run(ctx context.Context, onChange func(MediaState)) {
	e := &emitter[MediaState]{f: onChange}
	m.src.run(ctx, func(ctx context.Context, conn *dbus.Conn) {
		m.mu.Lock()
		prev := m.player
		m.mu.Unlock()
		s := readMedia(ctx, conn, prev)
		m.mu.Lock()
		m.player = s.Player
		m.mu.Unlock()
		e.emit(s)
	})
}

func readMedia(ctx context.Context, conn *dbus.Conn, prev string) MediaState {
	cctx, cancel := context.WithTimeout(ctx, callLimit)
	defer cancel()
	var names []string
	if err := conn.BusObject().CallWithContext(cctx, "org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		return MediaState{}
	}
	var players []MediaState
	for _, name := range names {
		if strings.HasPrefix(name, mprisPrefix) {
			if s, ok := readPlayer(ctx, conn, name); ok {
				players = append(players, s)
			}
		}
	}
	if len(players) == 0 {
		return MediaState{}
	}
	slices.SortFunc(players, func(a, b MediaState) int { return strings.Compare(a.Player, b.Player) })
	for _, p := range players {
		if p.Playing {
			return p
		}
	}
	for _, p := range players {
		if p.Player == prev {
			return p
		}
	}
	return players[0]
}

func readPlayer(ctx context.Context, conn *dbus.Conn, name string) (MediaState, bool) {
	obj := conn.Object(name, mprisPath)
	p, err := getAll(ctx, obj, mprisPlayer)
	if err != nil {
		return MediaState{}, false
	}
	s := MediaState{
		Available:    true,
		Player:       name,
		Playing:      value[string](p, "PlaybackStatus") == "Playing",
		CanNext:      value[bool](p, "CanGoNext"),
		CanPrev:      value[bool](p, "CanGoPrevious"),
		CanPlayPause: value[bool](p, "CanPlay") || value[bool](p, "CanPause"),
	}
	if root, err := getAll(ctx, obj, mprisRoot); err == nil {
		s.Identity = value[string](root, "Identity")
	}
	meta := value[map[string]dbus.Variant](p, "Metadata")
	s.Title = value[string](meta, "xesam:title")
	s.Artist = strings.Join(value[[]string](meta, "xesam:artist"), ", ")
	s.ArtURL = value[string](meta, "mpris:artUrl")
	return s, true
}

func (m *Media) control(method string) error {
	m.mu.Lock()
	player := m.player
	m.mu.Unlock()
	if player == "" {
		return errors.New("system: no media player")
	}
	return m.src.call(player, mprisPath, mprisPlayer+"."+method)
}

func (m *Media) PlayPause() error { return m.control("PlayPause") }
func (m *Media) Next() error      { return m.control("Next") }
func (m *Media) Previous() error  { return m.control("Previous") }
