package gamestream

import (
	"net"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

// announce is an mDNS answer as Avahi gives Sunshine's: the instance, its
// service's port and target, and the target's address.
func announce(t *testing.T, instance, target string, port uint16, ip [4]byte) []byte {
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{Response: true, Authoritative: true})
	b.StartAnswers()
	hdr := func(name string, typ dnsmessage.Type) dnsmessage.ResourceHeader {
		return dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName(name), Type: typ, Class: dnsmessage.ClassINET, TTL: 120}
	}
	b.PTRResource(hdr(service, dnsmessage.TypePTR), dnsmessage.PTRResource{PTR: dnsmessage.MustNewName(instance)})
	b.StartAdditionals()
	b.SRVResource(hdr(instance, dnsmessage.TypeSRV), dnsmessage.SRVResource{Port: port, Target: dnsmessage.MustNewName(target)})
	b.AResource(hdr(target, dnsmessage.TypeA), dnsmessage.AResource{A: ip})
	msg, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func TestAnnouncementsNameTheirHosts(t *testing.T) {
	r := &records{srv: map[string]dnsmessage.SRVResource{}, a: map[string]net.IP{}}
	r.read(announce(t, `Lyric\032Desk._nvstream._tcp.local.`, "desk.local.", 47989, [4]byte{192, 168, 1, 20}), net.IPv4(192, 168, 1, 20))
	r.read(announce(t, `nas._nvstream._tcp.local.`, "nas.local.", 48989, [4]byte{192, 168, 1, 30}), net.IPv4(192, 168, 1, 30))
	r.read(announce(t, `nas._nvstream._tcp.local.`, "nas.local.", 48989, [4]byte{192, 168, 1, 30}), net.IPv4(192, 168, 1, 30))
	got := r.found()
	if len(got) != 2 || got[0] != (Found{Name: "Lyric Desk", Addr: "192.168.1.20:47989"}) || got[1].Addr != "192.168.1.30:48989" {
		t.Errorf("found %+v", got)
	}
	r = &records{srv: map[string]dnsmessage.SRVResource{}, a: map[string]net.IP{}}
	r.read([]byte("not dns"), nil)
	if len(r.found()) != 0 {
		t.Error("found a host in garbage")
	}
}
