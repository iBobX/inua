package gamestream

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"net"
	"strings"
	"testing"
	"time"
)

// hostOpen decrypts a client's control message as Sunshine does: the GCM
// tag before the ciphertext, the IV its sequence number then 'C', 'C'.
func hostOpen(t *testing.T, key [16]byte, msg []byte) (uint16, []byte) {
	t.Helper()
	if binary.LittleEndian.Uint16(msg) != 0x0001 {
		t.Fatalf("not encrypted: % x", msg[:4])
	}
	length := int(binary.LittleEndian.Uint16(msg[2:]))
	if len(msg) != 4+length {
		t.Fatalf("length %d for a %d byte message", length, len(msg))
	}
	seq := binary.LittleEndian.Uint32(msg[4:])
	tagged := msg[8:]
	tag, ct := tagged[:16], tagged[16:]
	b, _ := aes.NewCipher(key[:])
	g, _ := cipher.NewGCM(b)
	v := make([]byte, 12)
	binary.LittleEndian.PutUint32(v, seq)
	v[10], v[11] = 'C', 'C'
	plain, err := g.Open(nil, v, append(append([]byte(nil), ct...), tag...), nil)
	if err != nil {
		t.Fatal(err)
	}
	if int(binary.LittleEndian.Uint16(plain[2:])) != len(plain)-4 {
		t.Fatalf("payload length %d of %d", binary.LittleEndian.Uint16(plain[2:]), len(plain)-4)
	}
	return binary.LittleEndian.Uint16(plain), plain[4:]
}

// hostSeal encrypts a host's control message as Sunshine does.
func hostSeal(key [16]byte, seq uint32, ptype uint16, payload []byte) []byte {
	b, _ := aes.NewCipher(key[:])
	g, _ := cipher.NewGCM(b)
	plain := binary.LittleEndian.AppendUint16(nil, ptype)
	plain = binary.LittleEndian.AppendUint16(plain, uint16(len(payload)))
	plain = append(plain, payload...)
	v := make([]byte, 12)
	binary.LittleEndian.PutUint32(v, seq)
	v[10], v[11] = 'H', 'C'
	sealed := g.Seal(nil, v, plain, nil)
	ct, tag := sealed[:len(plain)], sealed[len(plain):]
	out := binary.LittleEndian.AppendUint16(nil, 0x0001)
	out = binary.LittleEndian.AppendUint16(out, uint16(4+16+len(ct)))
	out = binary.LittleEndian.AppendUint32(out, seq)
	return append(append(out, tag...), ct...)
}

func TestControlMessagesAreSealedAsTheHostReadsThem(t *testing.T) {
	var key [16]byte
	copy(key[:], "0123456789abcdef")
	g := newGCM(key)
	ptype, payload := hostOpen(t, key, seal(g, 7, ctlInput, keyPacket(0x41, true, ModCtrl)))
	if ptype != ctlInput || !bytes.Equal(payload, keyPacket(0x41, true, ModCtrl)) {
		t.Errorf("host read %#x % x", ptype, payload)
	}
	ptype, payload, err := open(g, hostSeal(key, 3, ctlTermination, []byte{0x80, 0x03, 0, 1}), 'H')
	if err != nil || ptype != ctlTermination || !bytes.Equal(payload, []byte{0x80, 0x03, 0, 1}) {
		t.Errorf("client read %#x % x, %v", ptype, payload, err)
	}
	// A message the client sealed itself is not the host's.
	if _, _, err := open(g, seal(g, 3, ctlTermination, nil), 'H'); err == nil {
		t.Error("the client's own message passed as the host's")
	}
}

func TestInputPacketsByteForByte(t *testing.T) {
	for _, c := range []struct {
		name string
		got  []byte
		want string
	}{
		{"key down", keyPacket(0x41, true, ModCtrl), "0000000a 03000000 00 4180 02 0000"},
		{"key up", keyPacket(0x41, false, 0), "0000000a 04000000 00 4180 00 0000"},
		{"mouse to", mouseAbsPacket(100, 200, 1920, 1080), "0000000e 05000000 0064 00c8 0000 077f 0437"},
		{"mouse by", mouseRelPacket(-1, 2), "00000008 07000000 ffff 0002"},
		{"button", buttonPacket(ButtonRight, true), "00000005 08000000 03"},
		{"button up", buttonPacket(ButtonLeft, false), "00000005 09000000 01"},
		{"scroll", scrollPacket(-WheelDelta), "0000000a 0a000000 ff88 ff88 0000"},
		{"hscroll", hscrollPacket(WheelDelta), "00000006 01000055 0078"},
	} {
		want, _ := hex.DecodeString(strings.ReplaceAll(c.want, " ", ""))
		if !bytes.Equal(c.got, want) {
			t.Errorf("%s = % x, want % x", c.name, c.got, want)
		}
	}
	text := strings.Repeat("é", 20) // 40 bytes: two packets, not splitting a character
	ps := textPackets(text)
	if len(ps) != 2 || len(ps[0]) != 8+32 || string(ps[0][8:])+string(ps[1][8:]) != text {
		t.Errorf("text packets: %d, % x", len(ps), ps)
	}
	if binary.BigEndian.Uint32(ps[1]) != uint32(4+8) || binary.LittleEndian.Uint32(ps[1][4:]) != magicText {
		t.Errorf("text header % x", ps[1][:8])
	}
}

// videoPkt is a video packet as Sunshine sends it.
func videoPkt(frame uint32, block, lastBlock uint8, shard, shards int, flags uint8, payload []byte) []byte {
	p := make([]byte, 12+4+16)
	p[0] = 0x90 // RTP version 2, with the four bytes of extension Sunshine always sends
	binary.LittleEndian.PutUint32(p[16+4:], frame)
	p[16+8] = flags
	p[16+11] = lastBlock<<6 | block<<4
	binary.LittleEndian.PutUint32(p[16+12:], uint32(shards)<<22|uint32(shard)<<12|20<<4)
	return append(p, payload...)
}

func header(frameType byte) []byte { return []byte{0x01, 0, 0, frameType, 0, 0, 0, 0} }

func feed(a *assembler, pkts ...[]byte) (frames []Frame, lost int) {
	for _, p := range pkts {
		v, ok := parseVideo(p)
		if !ok {
			continue
		}
		f, l := a.add(v)
		if l {
			lost++
		}
		if f != nil {
			frames = append(frames, *f)
		}
	}
	return
}

func TestFramesArePutBackTogether(t *testing.T) {
	a := &assembler{codec: H264}
	idr := append(header(2), 0, 0, 0, 1, 0x65, 1, 2)
	frames, lost := feed(a,
		videoPkt(1, 0, 0, 0, 3, flagSOF, idr[:6]),
		videoPkt(1, 0, 0, 2, 3, flagEOF, idr[10:]),
		videoPkt(1, 0, 0, 1, 3, flagPicData, idr[6:10]), // out of order
		videoPkt(1, 0, 0, 3, 3, 0, []byte("parity")),
		// A frame in two FEC blocks.
		videoPkt(2, 0, 1, 0, 1, flagSOF, append(header(1), 'a')),
		videoPkt(2, 1, 1, 0, 1, flagEOF, []byte("b")),
	)
	if lost != 0 || len(frames) != 2 {
		t.Fatalf("frames %d, lost %d", len(frames), lost)
	}
	if !frames[0].IDR || !bytes.Equal(frames[0].Data, idr[8:]) {
		t.Errorf("IDR frame = %+v", frames[0])
	}
	if frames[1].IDR || string(frames[1].Data) != "ab" || frames[1].Index != 2 {
		t.Errorf("P frame = %+v", frames[1])
	}
}

func TestALostFrameWaitsForTheNextIDR(t *testing.T) {
	a := &assembler{codec: H264}
	p := func(n uint32, ft byte) [][]byte {
		return [][]byte{videoPkt(n, 0, 0, 0, 2, flagSOF, header(ft)), videoPkt(n, 0, 0, 1, 2, flagEOF, []byte("x"))}
	}
	pkts := append(p(1, 2), p(2, 1)[0]) // frame 2 loses its second packet
	pkts = append(pkts, p(3, 1)...)     // a P frame: it needs frame 2
	pkts = append(pkts, p(4, 2)...)     // an IDR frame mends it
	pkts = append(pkts, p(6, 1)...)     // frame 5 never came at all
	frames, lost := feed(a, pkts...)
	var got []uint32
	for _, f := range frames {
		got = append(got, f.Index)
	}
	if lost != 2 || len(got) != 2 || got[0] != 1 || got[1] != 4 {
		t.Errorf("frames %v, lost %d", got, lost)
	}
}

func TestAV1FramesLoseTheirPadding(t *testing.T) {
	a := &assembler{codec: AV1}
	h := header(2)
	binary.LittleEndian.PutUint16(h[4:], 2) // the last packet's data: 2 bytes
	frames, _ := feed(a,
		videoPkt(1, 0, 0, 0, 2, flagSOF, append(h, 'a', 'b')),
		videoPkt(1, 0, 0, 1, 2, flagEOF, []byte{'c', 'd', 0, 0, 0, 0, 0, 0, 0, 0}),
	)
	if len(frames) != 1 || string(frames[0].Data) != "abcd" {
		t.Errorf("frames %+v", frames)
	}
}

func TestIDRIsFoundInTheBitstream(t *testing.T) {
	if !hasIDR([]byte{0, 0, 1, 0x67, 0, 0, 1, 0x65}, H264) || hasIDR([]byte{0, 0, 1, 0x41}, H264) {
		t.Error("H.264")
	}
	if !hasIDR([]byte{0, 0, 0, 1, 19 << 1, 1}, HEVC) || hasIDR([]byte{0, 0, 1, 1 << 1, 1}, HEVC) {
		t.Error("HEVC")
	}
}

func TestAudioPacketsCarryOpus(t *testing.T) {
	rtp := []byte{0x80, 97, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0}
	if got := opusPacket(append(rtp, 0xFC, 1, 2)); !bytes.Equal(got, []byte{0xFC, 1, 2}) {
		t.Errorf("opus = % x", got)
	}
	fec := append([]byte(nil), rtp...)
	fec[1] = 127
	if opusPacket(append(fec, 1, 2)) != nil {
		t.Error("an FEC packet passed as Opus")
	}
}

func TestPingsCarryThePayloadAndACount(t *testing.T) {
	ln, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	conn, err := net.DialUDP("udp", nil, ln.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go pinger(ctx, conn, "0123456789abcdef")
	buf := make([]byte, 64)
	ln.SetReadDeadline(time.Now().Add(2 * time.Second))
	for want := uint32(1); want <= 2; want++ {
		n, _, err := ln.ReadFromUDP(buf)
		if err != nil {
			t.Fatal(err)
		}
		if n != 20 || string(buf[:16]) != "0123456789abcdef" || binary.BigEndian.Uint32(buf[16:]) != want {
			t.Errorf("ping % x", buf[:n])
		}
	}
}
