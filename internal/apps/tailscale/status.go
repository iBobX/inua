package tailscale

import (
	"slices"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/i18n"
)

// Status is what the local API's status says, as much as the app uses.
type Status struct {
	BackendState   string // "Running", "Stopped", "NeedsLogin", "NeedsMachineAuth", "Starting", "NoState"
	AuthURL        string // where to log in, while logging in
	Self           *Peer
	Peer           map[string]*Peer // by public key
	CurrentTailnet *Tailnet
	Health         []string
}

// Tailnet is the network this device is in.
type Tailnet struct {
	Name            string
	MagicDNSSuffix  string
	MagicDNSEnabled bool
}

// Peer is a device in the tailnet, or this one.
type Peer struct {
	ID             string // stable node ID
	HostName       string
	DNSName        string // "name.tailnet.ts.net."
	OS             string
	TailscaleIPs   []string
	Tags           []string
	Online         bool
	Active         bool // traffic lately
	ExitNode       bool // in use as this device's exit node
	ExitNodeOption bool // offers to be one
	CurAddr        string
	Relay          string // the DERP region it is reached through
	RxBytes        int64
	TxBytes        int64
	LastSeen       time.Time
	KeyExpiry      *time.Time
	TaildropTarget int // 0 unknown; see NoFileSharingReason
}

// Prefs are this device's settings, as much as the app changes.
type Prefs struct {
	WantRunning            bool
	LoggedOut              bool
	ExitNodeID             string
	ExitNodeAllowLANAccess bool
	RouteAll               bool // use the subnet routes others offer
	CorpDNS                bool // use Tailscale's DNS settings
	ShieldsUp              bool // refuse incoming connections
	RunSSH                 bool // the Tailscale SSH server
	AdvertiseRoutes        []string
	OperatorUser           string
}

// Edit changes some of the settings: those whose …Set is true. The local
// API calls this MaskedPrefs.
type Edit struct {
	WantRunning               bool `json:",omitempty"`
	WantRunningSet            bool `json:",omitempty"`
	ExitNodeID                string
	ExitNodeIDSet             bool `json:",omitempty"`
	ExitNodeIP                string
	ExitNodeIPSet             bool     `json:",omitempty"`
	ExitNodeAllowLANAccess    bool     `json:",omitempty"`
	ExitNodeAllowLANAccessSet bool     `json:",omitempty"`
	RouteAll                  bool     `json:",omitempty"`
	RouteAllSet               bool     `json:",omitempty"`
	CorpDNS                   bool     `json:",omitempty"`
	CorpDNSSet                bool     `json:",omitempty"`
	ShieldsUp                 bool     `json:",omitempty"`
	ShieldsUpSet              bool     `json:",omitempty"`
	RunSSH                    bool     `json:",omitempty"`
	RunSSHSet                 bool     `json:",omitempty"`
	AdvertiseRoutes           []string // sent even when empty: no routes
	AdvertiseRoutesSet        bool     `json:",omitempty"`
}

// The routes a device offers to be an exit node.
var exitRoutes = []string{"0.0.0.0/0", "::/0"}

// OffersExit reports whether this device offers to be an exit node.
func (p Prefs) OffersExit() bool {
	return slices.Contains(p.AdvertiseRoutes, exitRoutes[0]) || slices.Contains(p.AdvertiseRoutes, exitRoutes[1])
}

// OfferExit is the edit that starts or stops offering this device as an
// exit node, keeping the subnet routes it offers.
func (p Prefs) OfferExit(on bool) Edit {
	routes := slices.DeleteFunc(slices.Clone(p.AdvertiseRoutes), func(r string) bool { return slices.Contains(exitRoutes, r) })
	if on {
		routes = append(routes, exitRoutes...)
	}
	if routes == nil {
		routes = []string{}
	}
	return Edit{AdvertiseRoutes: routes, AdvertiseRoutesSet: true}
}

// UseExitNode is the edit that sends this device's traffic through a
// peer, or through no one when id is "".
func UseExitNode(id string) Edit {
	return Edit{ExitNodeID: id, ExitNodeIDSet: true, ExitNodeIP: "", ExitNodeIPSet: true}
}

// Name is how the app names a device: its MagicDNS name, else its host
// name.
func (p *Peer) Name() string {
	if n, _, _ := strings.Cut(p.DNSName, "."); n != "" {
		return n
	}
	return p.HostName
}

// IPv4 is the device's Tailscale IPv4 address, else its first address.
func (p *Peer) IPv4() string {
	for _, ip := range p.TailscaleIPs {
		if !strings.Contains(ip, ":") {
			return ip
		}
	}
	if len(p.TailscaleIPs) > 0 {
		return p.TailscaleIPs[0]
	}
	return ""
}

// MagicDNS is the device's full MagicDNS name, without the final dot.
func (p *Peer) MagicDNS() string { return strings.TrimSuffix(p.DNSName, ".") }

// Connection says how this device reaches a peer: directly, at an
// address, or through a relay.
func (p *Peer) Connection() string {
	switch {
	case !p.Online:
		return i18n.T("offline")
	case p.CurAddr != "":
		return i18n.Tf("direct, %s", p.CurAddr)
	case p.Relay != "":
		return i18n.Tf("relayed through %s", p.Relay)
	}
	return i18n.T("idle")
}

// Seen says when a peer was last online: "online", "5 min ago",
// "3 d ago", or "" when never known.
func (p *Peer) Seen(now time.Time) string {
	if p.Online {
		return i18n.T("online")
	}
	if p.LastSeen.IsZero() || p.LastSeen.Year() < 2000 {
		return i18n.T("offline")
	}
	return Ago(now.Sub(p.LastSeen))
}

// Ago says how long ago, shortly.
func Ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return i18n.T("just now")
	case d < time.Hour:
		return i18n.Tf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return i18n.Tf("%d h ago", int(d.Hours()))
	case d < 60*24*time.Hour:
		return i18n.Tf("%d d ago", int(d.Hours()/24))
	}
	return i18n.Tf("%d mo ago", int(d.Hours()/24/30))
}

// Expiry says when a device's key expires, "" when it does not.
func (p *Peer) Expiry(now time.Time) string {
	if p.KeyExpiry == nil || p.KeyExpiry.IsZero() {
		return ""
	}
	d := p.KeyExpiry.Sub(now)
	switch {
	case d < 0:
		return i18n.Tf("expired %s", Ago(-d))
	case d < 48*time.Hour:
		return i18n.Tf("in %d h", int(d.Hours()))
	}
	return i18n.Tf("in %d d", int(d.Hours()/24))
}

// Peers are the peers that match a query, any case, in their name, an
// address, their OS or a tag; online ones first, then by name.
func (st Status) Peers(query string) []*Peer {
	q := strings.ToLower(strings.TrimSpace(query))
	var out []*Peer
	for _, p := range st.Peer {
		if q == "" || p.matches(q) {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b *Peer) int {
		if a.Online != b.Online {
			if a.Online {
				return -1
			}
			return 1
		}
		if c := strings.Compare(strings.ToLower(a.Name()), strings.ToLower(b.Name())); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

func (p *Peer) matches(q string) bool {
	if strings.Contains(strings.ToLower(p.Name()), q) || strings.Contains(strings.ToLower(p.HostName), q) ||
		strings.EqualFold(p.OS, q) {
		return true
	}
	for _, ip := range p.TailscaleIPs {
		if strings.HasPrefix(ip, q) {
			return true
		}
	}
	for _, t := range p.Tags {
		if strings.Contains(strings.ToLower(t), q) {
			return true
		}
	}
	return false
}

// ExitNodes are the peers offering to be an exit node, by name.
func (st Status) ExitNodes() []*Peer {
	var out []*Peer
	for _, p := range st.Peers("") {
		if p.ExitNodeOption {
			out = append(out, p)
		}
	}
	slices.SortStableFunc(out, func(a, b *Peer) int {
		return strings.Compare(strings.ToLower(a.Name()), strings.ToLower(b.Name()))
	})
	return out
}

// PeerByID finds a peer by its stable ID.
func (st Status) PeerByID(id string) *Peer {
	for _, p := range st.Peer {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// State says the daemon's state in words, and whether it is connected.
func (st Status) State() (string, bool) {
	switch st.BackendState {
	case "Running":
		return i18n.T("Connected"), true
	case "Stopped":
		return i18n.T("Disconnected"), false
	case "NeedsLogin":
		return i18n.T("Logged out"), false
	case "NeedsMachineAuth":
		return i18n.T("Waiting for approval"), false
	case "Starting":
		return i18n.T("Connecting…"), false
	case "":
		return i18n.T("Not running"), false
	}
	return st.BackendState, false
}

// Online counts the online peers.
func (st Status) Online() int {
	n := 0
	for _, p := range st.Peer {
		if p.Online {
			n++
		}
	}
	return n
}
