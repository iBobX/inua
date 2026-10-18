package gamestream

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

// controlHost is a fake control stream host: an ENet host that verifies
// the connection, acknowledges what it is sent, and opens the client's
// encrypted messages as Sunshine does.
type controlHost struct {
	t      *testing.T
	udp    *net.UDPConn
	key    func() [16]byte
	mu     sync.Mutex
	client *net.UDPAddr
	data   uint32 // the connect data the client sent
	msgs   []uint16
	inputs [][]byte
}

func newControlHost(t *testing.T, key func() [16]byte) *controlHost {
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	h := &controlHost{t: t, udp: udp, key: key}
	t.Cleanup(func() { udp.Close() })
	go h.serve()
	return h
}

func (h *controlHost) port() int { return h.udp.LocalAddr().(*net.UDPAddr).Port }

func (h *controlHost) send(cmds ...[]byte) {
	d := []byte{0x80 | 1<<4, 0, 0, 0} // peer 0, session 1, a sent time
	for _, c := range cmds {
		d = append(d, c...)
	}
	h.mu.Lock()
	to := h.client
	h.mu.Unlock()
	h.udp.WriteToUDP(d, to)
}

func be16(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }
func be32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }

func (h *controlHost) serve() {
	buf := make([]byte, 2048)
	for {
		n, addr, err := h.udp.ReadFromUDP(buf)
		if err != nil {
			return
		}
		d := buf[4:n]
		sent := binary.BigEndian.Uint16(buf[2:])
		h.mu.Lock()
		h.client = addr
		h.mu.Unlock()
		ack := func() []byte {
			return append([]byte{1, d[1], d[2], d[3]}, append(append([]byte(nil), d[2:4]...), be16(sent)...)...)
		}
		switch d[0] & 0x0F {
		case 2: // connect
			b := d[4:]
			h.mu.Lock()
			h.data = binary.BigEndian.Uint32(b[40:])
			h.mu.Unlock()
			verify := append([]byte{3 | 0x80, 0xFF, 0, 1}, be16(7)...)
			verify = append(verify, 1, 2)
			verify = append(verify, be32(1400)...)
			verify = append(verify, be32(65536)...)
			verify = append(verify, b[12:16]...)
			verify = append(verify, be32(0)...)
			verify = append(verify, be32(0)...)
			verify = append(verify, b[24:40]...)
			h.send(ack(), verify)
		case 5: // ping
			h.send(ack())
		case 6: // reliable
			h.send(ack())
			payload := d[6 : 6+binary.BigEndian.Uint16(d[4:])]
			ptype, p := hostOpen(h.t, h.key(), payload)
			h.mu.Lock()
			h.msgs = append(h.msgs, ptype)
			if ptype == ctlInput {
				h.inputs = append(h.inputs, p)
			}
			h.mu.Unlock()
		}
	}
}

// streamHost waits for a stream's ping, then sends packets to whoever
// pinged.
func streamHost(t *testing.T, packets ...[]byte) int {
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { udp.Close() })
	go func() {
		buf := make([]byte, 64)
		n, addr, err := udp.ReadFromUDP(buf)
		if err != nil || n != 20 || string(buf[:16]) != "0123456789abcdef" {
			return
		}
		for _, p := range packets {
			udp.WriteToUDP(p, addr)
		}
	}()
	return udp.LocalAddr().(*net.UDPAddr).Port
}

func TestASessionStreamsAndTakesInput(t *testing.T) {
	f := newFakeHost(t, "4821")
	h := newClient(t, f.addr())
	if err := h.Pair(t.Context(), "4821", "laptop"); err != nil {
		t.Fatal(err)
	}
	key := func() [16]byte {
		f.mu.Lock()
		defer f.mu.Unlock()
		var k [16]byte
		b, _ := hex.DecodeString(f.launched.Get("rikey"))
		copy(k[:], b)
		return k
	}
	ctl := newControlHost(t, key)
	idr := append(header(2), 0, 0, 0, 1, 0x40, 1, 0, 0, 0, 1, 0x26, 1)
	video := streamHost(t, videoPkt(1, 0, 0, 0, 2, flagSOF, idr[:9]), videoPkt(1, 0, 0, 1, 2, flagEOF, idr[9:]))
	audio := streamHost(t, []byte{0x80, 97, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0xFC, 0xFF, 0xFE})
	r := newRTSPHost(t, sunshineDescribe)
	r.ports = [3]int{audio, video, ctl.port()}
	_, rport, _ := net.SplitHostPort(r.ln.Addr().String())
	f.session = "rtsp://127.0.0.1:" + rport

	s, err := Connect(t.Context(), h, 881448767, Config{Width: 1280, Height: 720, FPS: 60, BitrateKbps: 10000, Codec: HEVC})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Config.Codec != HEVC || s.Config.PacketSize != 1392 {
		t.Errorf("config = %+v", s.Config)
	}
	select {
	case fr := <-s.Video:
		if !fr.IDR || !bytes.Equal(fr.Data, idr[8:]) {
			t.Errorf("frame = %+v", fr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no video")
	}
	select {
	case p := <-s.Audio:
		if !bytes.Equal(p, []byte{0xFC, 0xFF, 0xFE}) {
			t.Errorf("audio % x", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no audio")
	}
	if err := s.Key(0x41, true, ModShift); err != nil {
		t.Fatal(err)
	}
	if err := s.MouseTo(10, 20, 1280, 720); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		ctl.mu.Lock()
		n := len(ctl.inputs)
		ctl.mu.Unlock()
		if n >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	ctl.mu.Lock()
	if ctl.data != 3735928559 {
		t.Errorf("connect data %d", ctl.data)
	}
	if len(ctl.msgs) < 2 || ctl.msgs[0] != ctlIDR || ctl.msgs[1] != ctlStartB {
		t.Errorf("first messages % x", ctl.msgs)
	}
	if len(ctl.inputs) != 2 || !bytes.Equal(ctl.inputs[0], keyPacket(0x41, true, ModShift)) ||
		!bytes.Equal(ctl.inputs[1], mouseAbsPacket(10, 20, 1280, 720)) {
		t.Errorf("inputs % x", ctl.inputs)
	}
	ctl.mu.Unlock()

	// The host ends the session.
	ctl.send(append(append([]byte{6 | 0x80, 0, 0, 1}, be16(uint16(len(hostSeal(key(), 0, ctlTermination, be32(0x80030023)))))...),
		hostSeal(key(), 0, ctlTermination, be32(0x80030023))...))
	select {
	case <-s.Done():
		var ended *ErrHostEnded
		if !errors.As(s.Err(), &ended) || ended.Code != 0x80030023 {
			t.Errorf("ended with %v", s.Err())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the host's termination did not end the session")
	}
}
