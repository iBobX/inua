package remotedesk

import (
	"cmp"
	"context"
	"net"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/apps/tailscale"
	"github.com/lyricat/inua/internal/gamestream"
)

// Machine is a computer to connect to: one paired (its certificate kept),
// or one added and not paired yet.
type Machine struct {
	Name string `json:"name"`
	Addr string `json:"addr"`           // host, or host:port of its HTTP port
	ID   string `json:"id,omitempty"`   // the host's own unique ID
	Cert string `json:"cert,omitempty"` // the host's certificate (PEM), once paired
}

// Paired says whether the machine was paired with.
func (m Machine) Paired() bool { return m.Cert != "" }

// Stream is how the viewer streams: its picture and the codec it asks for.
type Stream struct {
	Width     int    `json:"width"`  // 0: the window's monitor
	Height    int    `json:"height"` // 0: the window's monitor
	FPS       int    `json:"fps"`
	Bitrate   int    `json:"bitrate_kbps"`
	Codec     string `json:"codec"`      // h264, hevc, av1
	HostAudio bool   `json:"host_audio"` // the sound plays on the machine too
}

// Settings are the viewer's, kept in ~/.local/share/inua/remote-desktop.json.
type Settings struct {
	Machines []Machine `json:"machines,omitempty"`
	Stream   Stream    `json:"stream"`
}

const storeName = "remote-desktop"

// LoadSettings reads the settings, or the defaults.
func LoadSettings() *Settings {
	s := &Settings{Stream: Stream{FPS: 60, Bitrate: 20000, Codec: "hevc"}}
	store.Load(storeName, s)
	return s
}

// Save writes the settings.
func (s *Settings) Save() error { return store.Save(storeName, s) }

// IdentityDir is where this computer's identity to the machines it pairs
// with is kept.
func IdentityDir() string { return filepath.Join(store.Dir(), "remote-desktop") }

// Keep puts m in the list, in place of the one at its address or with its
// ID.
func (s *Settings) Keep(m Machine) {
	i := slices.IndexFunc(s.Machines, func(x Machine) bool {
		return x.Addr == m.Addr || m.ID != "" && x.ID == m.ID
	})
	if i >= 0 {
		s.Machines[i] = m
	} else {
		s.Machines = append(s.Machines, m)
	}
}

// Forget takes the machine at addr off the list.
func (s *Settings) Forget(addr string) {
	s.Machines = slices.DeleteFunc(s.Machines, func(x Machine) bool { return x.Addr == addr })
}

// GameCodec is the stream's codec as gamestream names it.
func (st Stream) GameCodec() gamestream.Codec {
	switch st.Codec {
	case "h264":
		return gamestream.H264
	case "av1":
		return gamestream.AV1
	}
	return gamestream.HEVC
}

// Candidate is a machine found, sharing its desktop: on the local network
// (it announced itself) or on the tailnet (it answered there).
type Candidate struct {
	Name, Addr string
	ID         string
	Tailnet    bool
}

// Finder finds machines: announced on the local network, and on the
// tailnet those whose GameStream port answers.
type Finder struct {
	Browse    func(ctx context.Context, wait time.Duration) ([]gamestream.Found, error)
	Tailscale func(ctx context.Context) (tailscale.Status, error)
	Info      func(ctx context.Context, addr string) (gamestream.ServerInfo, error)
}

// NewFinder finds machines on this computer's networks.
func NewFinder(id *gamestream.Identity) *Finder {
	ts := tailscale.NewClient("")
	return &Finder{
		Browse:    gamestream.Browse,
		Tailscale: ts.Status,
		Info: func(ctx context.Context, addr string) (gamestream.ServerInfo, error) {
			return (&gamestream.Host{Addr: addr, ID: id}).Info(ctx)
		},
	}
}

// Find returns the machines that share their desktop, each once, the
// local network's first.
func (f *Finder) Find(ctx context.Context) []Candidate {
	var mu sync.Mutex
	var out []Candidate
	seen := map[string]bool{}
	add := func(c Candidate) {
		mu.Lock()
		defer mu.Unlock()
		key := cmp.Or(c.ID, c.Addr)
		if seen[key] || seen[c.Addr] {
			return
		}
		seen[key], seen[c.Addr] = true, true
		out = append(out, c)
	}
	var wg sync.WaitGroup
	probe := func(name, addr string, tailnet bool) {
		defer wg.Done()
		pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		info, err := f.Info(pctx, addr)
		if err != nil {
			return
		}
		add(Candidate{Name: cmp.Or(info.Hostname, name), Addr: addr, ID: info.UniqueID, Tailnet: tailnet})
	}
	if found, err := f.Browse(ctx, 1500*time.Millisecond); err == nil {
		for _, h := range found {
			wg.Add(1)
			go probe(h.Name, h.Addr, false)
		}
	}
	wg.Wait() // the local network's first
	if f.Tailscale != nil {
		tctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		st, err := f.Tailscale(tctx)
		cancel()
		if err == nil {
			for _, p := range st.Peers("") {
				if !p.Online || p.IPv4() == "" {
					continue
				}
				wg.Add(1)
				go probe(p.Name(), net.JoinHostPort(p.IPv4(), strconv.Itoa(gamestream.DefaultPort)), true)
			}
		}
		wg.Wait()
	}
	return out
}

// Label is how a machine is called in the list: its name, or its address.
func Label(m Machine) string {
	if strings.TrimSpace(m.Name) != "" {
		return m.Name
	}
	return m.Addr
}
