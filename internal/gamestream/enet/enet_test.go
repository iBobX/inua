package enet

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"sync"
	"testing"
	"time"
)

// host is a fake ENet host: it verifies a connection, acknowledges what it
// is sent (dropping the first copy of one reliable packet, to have it sent
// again), and sends reliable packets back out of order and in fragments.
type host struct {
	t       *testing.T
	udp     *net.UDPConn
	mu      sync.Mutex
	client  *net.UDPAddr
	headers []uint16 // the client's header peer IDs, after the connect
	got     map[uint16]int
	data    [][]byte
	dropped bool
	gone    chan struct{}
}

const (
	hostPeer    = 5
	hostInSess  = 1 // in the headers the host sends
	hostOutSess = 2 // in the headers the client sends
)

func newHost(t *testing.T) *host {
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	h := &host{t: t, udp: udp, got: map[uint16]int{}, gone: make(chan struct{})}
	go h.serve()
	t.Cleanup(func() { udp.Close() })
	return h
}

func (h *host) send(cmds ...[]byte) {
	d := make([]byte, 4)
	binary.BigEndian.PutUint16(d, 0|hostInSess<<headerSessionShift|headerSentTime)
	binary.BigEndian.PutUint16(d[2:], 1234)
	for _, c := range cmds {
		d = append(d, c...)
	}
	h.mu.Lock() // serve sets the client, on its own goroutine
	client := h.client
	h.mu.Unlock()
	h.udp.WriteToUDP(d, client)
}

func cmd(command, channel uint8, seq uint16, body ...[]byte) []byte {
	b := []byte{command, channel, 0, 0}
	binary.BigEndian.PutUint16(b[2:], seq)
	for _, x := range body {
		b = append(b, x...)
	}
	return b
}

func u16(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }
func u32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }

func (h *host) serve() {
	buf := make([]byte, 2048)
	for {
		n, addr, err := h.udp.ReadFromUDP(buf)
		if err != nil {
			return
		}
		d := buf[:n]
		peer := binary.BigEndian.Uint16(d)
		sent := binary.BigEndian.Uint16(d[2:])
		c := d[4:]
		h.mu.Lock()
		h.client = addr
		h.mu.Unlock()
		switch c[0] & cmdMask {
		case cmdConnect:
			b := c[4:]
			verify := cmd(cmdVerifyConnect|flagAcknowledge, 0xFF, 1,
				u16(hostPeer), []byte{hostInSess, hostOutSess}, u32(1400), u32(65536), u32(binary.BigEndian.Uint32(b[12:])),
				u32(0), u32(0), b[24:28], b[28:32], b[32:36], b[36:40])
			h.send(cmd(cmdAcknowledge, 0xFF, 1, u16(1), u16(sent)), verify)
		default:
			h.mu.Lock()
			h.headers = append(h.headers, peer&^headerSentTime)
			h.mu.Unlock()
			switch c[0] & cmdMask {
			case cmdSendReliable:
				seq := binary.BigEndian.Uint16(c[2:])
				h.mu.Lock()
				h.got[seq]++
				drop := seq == 2 && !h.dropped
				if drop {
					h.dropped = true
				}
				if !drop && h.got[seq] == 1 || h.got[seq] == 2 && seq == 2 {
					h.data = append(h.data, append([]byte(nil), c[6:]...))
				}
				h.mu.Unlock()
				if !drop {
					h.send(cmd(cmdAcknowledge, c[1], seq, u16(seq), u16(sent)))
				}
			case cmdDisconnect:
				close(h.gone)
				return
			}
		}
	}
}

func (h *host) reliable(channel uint8, seq uint16, data string) []byte {
	return cmd(cmdSendReliable|flagAcknowledge, channel, seq, u16(uint16(len(data))), []byte(data))
}

func TestConnectSendReceiveInOrder(t *testing.T) {
	h := newHost(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, h.udp.LocalAddr().String(), 0x30, 0xCAFE)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"one", "two", "three"} {
		if err := c.Send(3, []byte(s), true); err != nil {
			t.Fatal(err)
		}
	}
	// Sent again after the drop, then all acknowledged.
	deadline := time.Now().Add(3 * time.Second)
	for {
		c.mu.Lock()
		n := len(c.unacked)
		c.mu.Unlock()
		if n == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.mu.Lock()
	if h.got[2] != 2 {
		t.Errorf("the dropped packet was sent %d times", h.got[2])
	}
	for _, p := range h.headers {
		if p != hostPeer|hostOutSess<<headerSessionShift {
			t.Errorf("a header said peer %#x", p)
			break
		}
	}
	h.mu.Unlock()

	// Out of order, a fragmented packet in the middle (sequence 2 and 3),
	// and a copy of the first sent again.
	frag := func(seq uint16, n uint32, off uint32, data string) []byte {
		return cmd(cmdSendFragment|flagAcknowledge, 0, seq, u16(2), u16(uint16(len(data))), u32(2), u32(n), u32(10), u32(off), []byte(data))
	}
	h.send(h.reliable(0, 4, "four"))
	h.send(frag(3, 1, 5, "World"))
	h.send(h.reliable(0, 1, "first"))
	h.send(frag(2, 0, 0, "Hello"))
	h.send(h.reliable(0, 1, "first"))
	var got []string
	for len(got) < 3 {
		select {
		case p := <-c.Packets():
			got = append(got, string(p.Data))
		case <-ctx.Done():
			t.Fatalf("got only %q", got)
		}
	}
	if want := []string{"first", "HelloWorld", "four"}; !equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}

	c.Close()
	select {
	case <-h.gone:
	case <-time.After(2 * time.Second):
		t.Error("the host was not told the connection ended")
	}
	if c.Send(0, []byte("x"), true) == nil {
		t.Error("sent on a closed connection")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDialGivesUpOnASilentHost(t *testing.T) {
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	if _, err := Dial(ctx, udp.LocalAddr().String(), 1, 0); err == nil {
		t.Fatal("connected to a host that never answered")
	}
}

func TestAPacketPastTheMTUIsRefused(t *testing.T) {
	h := newHost(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, h.udp.LocalAddr().String(), 0x30, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Send(1, []byte("move"), false); err != nil {
		t.Fatal(err)
	}
	if err := c.Send(1, bytes.Repeat([]byte("x"), 2000), true); err == nil {
		t.Error("a packet past the MTU was sent")
	}
}
