package connect

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/kdeconnect"
)

const (
	mprisPrefix = "org.mpris.MediaPlayer2."
	mprisPath   = dbus.ObjectPath("/org/mpris/MediaPlayer2")
	mprisRoot   = "org.mpris.MediaPlayer2"
	mprisPlayer = "org.mpris.MediaPlayer2.Player"
)

// mediaControl gives the devices this computer's players (MPRIS): their
// list, what each plays, and their buttons.
type mediaControl struct {
	s *Service

	mu      sync.Mutex
	pending *time.Timer
}

func newMediaControl(s *Service) *mediaControl {
	s.bus.AddMatchSignal(dbus.WithMatchObjectPath(mprisPath), dbus.WithMatchInterface("org.freedesktop.DBus.Properties"))
	s.bus.AddMatchSignal(dbus.WithMatchSender("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg0Namespace("org.mpris.MediaPlayer2"))
	return &mediaControl{s: s}
}

// players are the players on the bus, by the name they show, to their
// bus names.
func (m *mediaControl) players() map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var names []string
	if m.s.bus.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.ListNames", 0).Store(&names) != nil {
		return nil
	}
	out := map[string]string{}
	for _, n := range names {
		if !strings.HasPrefix(n, mprisPrefix) {
			continue
		}
		v, err := m.s.bus.Object(n, mprisPath).GetProperty(mprisRoot + ".Identity")
		id, _ := v.Value().(string)
		if err != nil || id == "" {
			id = strings.TrimPrefix(n, mprisPrefix)
		}
		if _, taken := out[id]; taken {
			id += " (" + strings.TrimPrefix(n, mprisPrefix) + ")"
		}
		out[id] = n
	}
	return out
}

// sendList gives the device the players' names.
func (m *mediaControl) sendList(id string) {
	var list []string
	for name := range m.players() {
		list = append(list, name)
	}
	slices.Sort(list)
	if list == nil {
		list = []string{}
	}
	m.s.core.Send(id, kdeconnect.NewPacket(typeMpris, map[string]any{"playerList": list, "supportAlbumArtPayload": false}))
}

// signal follows the players: a change is sent to the devices, a burst of
// them once.
func (m *mediaControl) signal(sig *dbus.Signal) {
	isPlayer := sig.Path == mprisPath && sig.Name == "org.freedesktop.DBus.Properties.PropertiesChanged"
	isName := sig.Name == "org.freedesktop.DBus.NameOwnerChanged"
	if !isPlayer && !isName {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending != nil {
		m.pending.Stop()
	}
	m.pending = time.AfterFunc(200*time.Millisecond, func() { m.broadcast(isName) })
}

func (m *mediaControl) broadcast(list bool) {
	players := m.players()
	for _, d := range m.s.core.Devices() {
		if !d.Reachable || d.Pair != kdeconnect.Paired || !d.Takes(typeMpris) {
			continue
		}
		if list {
			m.sendList(d.ID)
		}
		for name, bus := range players {
			m.sendState(d.ID, name, bus)
		}
	}
}

// sendState gives the device what the player plays.
func (m *mediaControl) sendState(id, name, bus string) {
	obj := m.s.bus.Object(bus, mprisPath)
	var p map[string]dbus.Variant
	if obj.Call("org.freedesktop.DBus.Properties.GetAll", 0, mprisPlayer).Store(&p) != nil {
		return
	}
	str := func(k string) string { s, _ := p[k].Value().(string); return s }
	flag := func(k string) bool { b, _ := p[k].Value().(bool); return b }
	meta, _ := p["Metadata"].Value().(map[string]dbus.Variant)
	metaStr := func(k string) string { s, _ := meta[k].Value().(string); return s }
	artists, _ := meta["xesam:artist"].Value().([]string)
	length := int64(-1)
	switch l := meta["mpris:length"].Value().(type) {
	case int64:
		length = l / 1000
	case uint64:
		length = int64(l / 1000)
	}
	var pos int64
	if v, err := obj.GetProperty(mprisPlayer + ".Position"); err == nil {
		pos, _ = v.Value().(int64)
	}
	vol, _ := p["Volume"].Value().(float64)
	body := map[string]any{
		"player":        name,
		"title":         metaStr("xesam:title"),
		"artist":        strings.Join(artists, ", "),
		"album":         metaStr("xesam:album"),
		"albumArtUrl":   metaStr("mpris:artUrl"),
		"url":           metaStr("xesam:url"),
		"length":        length,
		"pos":           pos / 1000,
		"isPlaying":     str("PlaybackStatus") == "Playing",
		"canPause":      flag("CanPause"),
		"canPlay":       flag("CanPlay"),
		"canGoNext":     flag("CanGoNext"),
		"canGoPrevious": flag("CanGoPrevious"),
		"canSeek":       flag("CanSeek"),
		"volume":        int(vol*100 + 0.5),
	}
	if _, ok := p["LoopStatus"]; ok {
		body["loopStatus"] = str("LoopStatus")
	}
	if _, ok := p["Shuffle"]; ok {
		body["shuffle"] = flag("Shuffle")
	}
	m.s.core.Send(id, kdeconnect.NewPacket(typeMpris, body))
}

// request does what the device asks of a player, and answers.
func (m *mediaControl) request(d kdeconnect.Device, p kdeconnect.Packet) {
	var b struct {
		Player            string  `json:"player"`
		RequestPlayerList bool    `json:"requestPlayerList"`
		RequestNowPlaying bool    `json:"requestNowPlaying"`
		RequestVolume     bool    `json:"requestVolume"`
		Action            string  `json:"action"`
		SetLoopStatus     *string `json:"setLoopStatus"`
		SetShuffle        *bool   `json:"setShuffle"`
		SetVolume         *int    `json:"setVolume"`
		Seek              *int64  `json:"Seek"`
		SetPosition       *int64  `json:"SetPosition"`
	}
	if p.Decode(&b) != nil {
		return
	}
	if b.RequestPlayerList {
		m.sendList(d.ID)
	}
	if b.Player == "" {
		return
	}
	bus, ok := m.players()[b.Player]
	if !ok {
		return
	}
	obj := m.s.bus.Object(bus, mprisPath)
	switch b.Action {
	case "Play", "Pause", "PlayPause", "Next", "Previous", "Stop":
		obj.Call(mprisPlayer+"."+b.Action, 0)
	}
	if b.SetLoopStatus != nil {
		obj.SetProperty(mprisPlayer+".LoopStatus", dbus.MakeVariant(*b.SetLoopStatus))
	}
	if b.SetShuffle != nil {
		obj.SetProperty(mprisPlayer+".Shuffle", dbus.MakeVariant(*b.SetShuffle))
	}
	if b.SetVolume != nil {
		obj.SetProperty(mprisPlayer+".Volume", dbus.MakeVariant(float64(*b.SetVolume)/100))
	}
	if b.Seek != nil {
		obj.Call(mprisPlayer+".Seek", 0, *b.Seek) // in microseconds
	}
	if b.SetPosition != nil {
		var pos int64
		if v, err := obj.GetProperty(mprisPlayer + ".Position"); err == nil {
			pos, _ = v.Value().(int64)
		}
		obj.Call(mprisPlayer+".Seek", 0, *b.SetPosition*1000-pos)
	}
	if b.RequestNowPlaying || b.RequestVolume || b.Action != "" || b.Seek != nil || b.SetPosition != nil {
		m.sendState(d.ID, b.Player, bus)
	}
}
