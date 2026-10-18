package remotedesk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/apps/tailscale"
	"github.com/lyricat/inua/internal/gamestream"
)

func TestFindListsEachMachineOnceTheLocalNetworksFirst(t *testing.T) {
	f := &Finder{
		Browse: func(context.Context, time.Duration) ([]gamestream.Found, error) {
			return []gamestream.Found{{Name: "desk", Addr: "192.168.1.20:47989"}, {Name: "gone", Addr: "192.168.1.99:47989"}}, nil
		},
		Tailscale: func(context.Context) (tailscale.Status, error) {
			return tailscale.Status{Peer: map[string]*tailscale.Peer{
				"a": {DNSName: "desk.tail.ts.net.", TailscaleIPs: []string{"100.64.0.2"}, Online: true},  // desk again
				"b": {DNSName: "nas.tail.ts.net.", TailscaleIPs: []string{"100.64.0.3"}, Online: true},   // no Sunshine
				"c": {DNSName: "work.tail.ts.net.", TailscaleIPs: []string{"100.64.0.4"}, Online: true},  // sharing
				"d": {DNSName: "away.tail.ts.net.", TailscaleIPs: []string{"100.64.0.5"}, Online: false}, // offline
			}}, nil
		},
		Info: func(_ context.Context, addr string) (gamestream.ServerInfo, error) {
			switch addr {
			case "192.168.1.20:47989", "100.64.0.2:47989":
				return gamestream.ServerInfo{Hostname: "Desk", UniqueID: "desk-id"}, nil
			case "100.64.0.4:47989":
				return gamestream.ServerInfo{Hostname: "Work", UniqueID: "work-id"}, nil
			case "100.64.0.5:47989":
				t.Error("an offline peer was asked")
			}
			return gamestream.ServerInfo{}, errors.New("refused")
		},
	}
	got := f.Find(t.Context())
	want := []Candidate{{Name: "Desk", Addr: "192.168.1.20:47989", ID: "desk-id"}, {Name: "Work", Addr: "100.64.0.4:47989", ID: "work-id", Tailnet: true}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("found %+v", got)
	}
}

func TestMachinesAreKeptByAddressOrID(t *testing.T) {
	s := &Settings{}
	s.Keep(Machine{Name: "Desk", Addr: "192.168.1.20:47989", ID: "desk-id"})
	s.Keep(Machine{Name: "Desk", Addr: "100.64.0.2:47989", ID: "desk-id", Cert: "PEM"}) // the same, by the tailnet
	s.Keep(Machine{Name: "Work", Addr: "100.64.0.4:47989"})
	if len(s.Machines) != 2 || s.Machines[0].Addr != "100.64.0.2:47989" || !s.Machines[0].Paired() {
		t.Errorf("machines %+v", s.Machines)
	}
	s.Forget("100.64.0.4:47989")
	if len(s.Machines) != 1 || Label(Machine{Addr: "1.2.3.4"}) != "1.2.3.4" {
		t.Errorf("after forgetting: %+v", s.Machines)
	}
	if (Stream{Codec: "av1"}).GameCodec() != gamestream.AV1 || (Stream{}).GameCodec() != gamestream.HEVC {
		t.Error("codecs")
	}
}
