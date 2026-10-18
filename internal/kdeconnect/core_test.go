package kdeconnect

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/x509"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

const typePing = "kdeconnect.ping"

// peer is a core on loopback, and what it was told.
type peer struct {
	*Core
	changed chan Device
	packets chan Packet
}

func newPeer(t *testing.T, ctx context.Context, name string) *peer {
	t.Helper()
	p := &peer{changed: make(chan Device, 64), packets: make(chan Packet, 64)}
	c, err := New(Config{
		Dir:      t.TempDir(),
		Name:     name,
		Type:     "desktop",
		Incoming: []string{typePing},
		Outgoing: []string{typePing},
		Loopback: true,
		Changed:  func(d Device) { p.changed <- d },
		Packet:   func(_ Device, pk Packet) { p.packets <- pk },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	p.Core = c
	return p
}

// announce has a announce itself to b alone.
func announce(a, b *peer) {
	a.udp.WriteToUDP(a.announcement(), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: b.UDPPort()})
}

// await waits for the core to say a device came to be as ok wants.
func (p *peer) await(t *testing.T, what string, ok func(Device) bool) Device {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case d := <-p.changed:
			if ok(d) {
				return d
			}
		case <-timeout:
			t.Fatalf("%s: timed out", what)
		}
	}
}

func pairBoth(t *testing.T, a, b *peer) {
	t.Helper()
	announce(a, b)
	a.await(t, "a links", func(d Device) bool { return d.ID == b.ID() && d.Reachable })
	b.await(t, "b links", func(d Device) bool { return d.ID == a.ID() && d.Reachable })
	if err := a.RequestPair(b.ID()); err != nil {
		t.Fatal(err)
	}
	asked := b.await(t, "b is asked", func(d Device) bool { return d.Pair == RequestedByPeer })
	mine, _ := a.Device(b.ID())
	if asked.Key == "" || asked.Key != mine.Key {
		t.Fatalf("the keys shown differ: %q and %q", asked.Key, mine.Key)
	}
	if err := b.AcceptPair(a.ID()); err != nil {
		t.Fatal(err)
	}
	a.await(t, "a paired", func(d Device) bool { return d.Pair == Paired })
}

func TestPairAndPackets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, b := newPeer(t, ctx, "Desk (one)"), newPeer(t, ctx, "Phone")
	if err := a.Send(b.ID(), NewPacket(typePing, nil)); err != ErrNotPaired {
		t.Fatalf("a packet before pairing: %v", err)
	}
	pairBoth(t, a, b)

	d, _ := b.Device(a.ID())
	if d.Name != "Desk one" || !d.Takes(typePing) || d.Pair != Paired {
		t.Fatalf("b sees a as %+v", d)
	}
	if _, ok := b.Store().Get(a.ID()); !ok {
		t.Fatal("b did not keep a as paired")
	}
	if err := a.Send(b.ID(), NewPacket(typePing, map[string]string{"message": "hi"})); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-b.packets:
		var body struct{ Message string }
		if p.Decode(&body); p.Type != typePing || body.Message != "hi" {
			t.Fatalf("b got %+v", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("b got no ping")
	}
	if err := a.Send(b.ID(), NewPacket("kdeconnect.battery", nil)); err != ErrUnsupported {
		t.Fatalf("a packet b does not take: %v", err)
	}

	if err := b.Unpair(a.ID()); err != nil {
		t.Fatal(err)
	}
	a.await(t, "a unpaired", func(d Device) bool { return d.Pair == NotPaired })
	if _, ok := a.Store().Get(b.ID()); ok {
		t.Fatal("a still keeps b as paired")
	}
}

func TestPayload(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, b := newPeer(t, ctx, "A"), newPeer(t, ctx, "B")
	pairBoth(t, a, b)
	data := make([]byte, 3<<20+17)
	rand.Read(data)
	errc := make(chan error, 1)
	go func() {
		errc <- a.SendPayload(ctx, b.ID(), NewPacket(typePing, nil), bytes.NewReader(data), int64(len(data)))
	}()
	var p Packet
	select {
	case p = <-b.packets:
	case <-time.After(5 * time.Second):
		t.Fatal("no packet")
	}
	if p.PayloadSize != int64(len(data)) || p.PayloadTransferInfo == nil {
		t.Fatalf("the packet: %+v", p)
	}
	r, err := b.Payload(ctx, a.ID(), p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	r.Close()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("the payload: %d bytes, %v", len(got), err)
	}
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
}

// A paired device that comes back with another certificate is not
// linked: someone else took its ID.
func TestPinnedCertificate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, b := newPeer(t, ctx, "A"), newPeer(t, ctx, "B")
	pairBoth(t, a, b)

	other, err := New(Config{Dir: t.TempDir(), Name: "Impostor", Loopback: true})
	if err != nil {
		t.Fatal(err)
	}
	other.me.ID = b.ID() // b's ID, but its own certificate
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(a.port))
	info := DeviceInfo{DeviceID: b.ID(), DeviceName: "B", ProtocolVersion: ProtocolVersion}
	if _, err := Dial(ctx, other.me, info, addr, a.ID(), func(string) *x509.Certificate { return nil }); err == nil {
		t.Fatal("a linked with an impostor")
	}
}

// A request to pair from a clock far off is not taken.
func TestPairClock(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, b := newPeer(t, ctx, "A"), newPeer(t, ctx, "B")
	announce(a, b)
	a.await(t, "a links", func(d Device) bool { return d.Reachable })
	b.await(t, "b links", func(d Device) bool { return d.Reachable })
	a.mu.Lock()
	l := a.devices[b.ID()].link
	a.mu.Unlock()
	l.Send(NewPacket(TypePair, pairBody{Pair: true, Timestamp: time.Now().Unix() - 3600}))
	l.Send(NewPacket(TypePair, pairBody{Pair: true, Timestamp: time.Now().Unix()}))
	d := b.await(t, "b is asked", func(d Device) bool { return d.Pair != NotPaired })
	if d.Pair != RequestedByPeer {
		t.Fatalf("b: %v", d.Pair)
	}
}

func TestVerificationKey(t *testing.T) {
	x, _ := LoadIdentity(t.TempDir())
	y, _ := LoadIdentity(t.TempDir())
	k1 := VerificationKey(x.Cert.Leaf, y.Cert.Leaf, 1700000000)
	k2 := VerificationKey(y.Cert.Leaf, x.Cert.Leaf, 1700000000)
	if k1 != k2 || len(k1) != 8 {
		t.Fatalf("%q and %q", k1, k2)
	}
	if k1 == VerificationKey(x.Cert.Leaf, y.Cert.Leaf, 1700000001) {
		t.Fatal("the time does not change the key")
	}
}
