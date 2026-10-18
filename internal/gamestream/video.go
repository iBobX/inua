package gamestream

import (
	"context"
	"encoding/binary"
	"net"
	"strconv"
	"sync"
	"time"
)

// Frame is one video frame: the codec's bitstream (Annex B for H.264 and
// HEVC), and whether it needs no frame before it.
type Frame struct {
	Index uint32
	IDR   bool
	Data  []byte
}

// The video packets' flags.
const (
	flagPicData = 0x1
	flagEOF     = 0x2
	flagSOF     = 0x4
)

// videoPacket is a video packet's header: RTP with its extension bit set
// and four bytes of extension, then GameStream's own (little-endian), then
// the frame's data.
type videoPacket struct {
	frame      uint32
	flags      uint8
	block      uint8 // this FEC block, of last+1
	lastBlock  uint8
	dataShards int
	shard      int
	payload    []byte
}

func parseVideo(p []byte) (videoPacket, bool) {
	if len(p) < 12 {
		return videoPacket{}, false
	}
	off := 12
	if p[0]&0x10 != 0 { // the RTP header's extension: four bytes, always there from Sunshine
		off = 16
	}
	if len(p) < off+16 {
		return videoPacket{}, false
	}
	h := p[off : off+16]
	fec := binary.LittleEndian.Uint32(h[12:])
	v := videoPacket{
		frame:      binary.LittleEndian.Uint32(h[4:]),
		flags:      h[8],
		block:      (h[11] >> 4) & 0x3,
		lastBlock:  (h[11] >> 6) & 0x3,
		dataShards: int(fec>>22) & 0x3FF,
		shard:      int(fec>>12) & 0x3FF,
		payload:    p[off+16:],
	}
	return v, v.dataShards > 0
}

// assembler puts frames back together from their packets. It does not
// repair losses with the parity packets: a frame missing a packet is
// dropped, and the frames after it until the host sends an IDR frame.
type assembler struct {
	codec   Codec
	current uint32 // the frame being put together, or the last one
	started bool
	active  bool // current is being put together
	blocks  [4][][]byte
	missing [4]int
	last    uint8
	waitIDR bool // a frame was lost; only an IDR frame mends it
}

// add takes a packet; it returns a frame when one is complete, and whether
// a frame was lost (and an IDR frame should be asked for).
func (a *assembler) add(v videoPacket) (*Frame, bool) {
	lost := false
	switch {
	case a.started && v.frame == a.current:
		if !a.active {
			return nil, false // a late packet of a frame done or given up on
		}
	case !a.started || int32(v.frame-a.current) > 0:
		// A new frame: the one before it is lost if it was not complete,
		// or if frames between them never came.
		if a.started && (a.active || v.frame != a.current+1) {
			lost = true
			a.waitIDR = true
		}
		a.started, a.active, a.current = true, true, v.frame
		a.blocks, a.missing = [4][][]byte{}, [4]int{}
	default:
		return nil, false // an older frame's
	}
	if v.shard >= v.dataShards {
		return nil, lost // parity: not used
	}
	if a.blocks[v.block] == nil {
		a.blocks[v.block] = make([][]byte, v.dataShards)
		a.missing[v.block] = v.dataShards
	}
	if len(a.blocks[v.block]) != v.dataShards || a.blocks[v.block][v.shard] != nil {
		return nil, lost
	}
	a.blocks[v.block][v.shard] = append([]byte(nil), v.payload...)
	a.missing[v.block]--
	a.last = v.lastBlock
	for b := 0; b <= int(a.last); b++ {
		if a.blocks[b] == nil || a.missing[b] > 0 {
			return nil, lost
		}
	}
	a.active = false
	f := a.frame()
	if f == nil || a.waitIDR && !f.IDR {
		return nil, lost
	}
	a.waitIDR = false
	return f, lost
}

// frame joins the frame's shards and takes its header off.
func (a *assembler) frame() *Frame {
	var data []byte
	for b := 0; b <= int(a.last); b++ {
		for _, s := range a.blocks[b] {
			data = append(data, s...)
		}
	}
	f := &Frame{Index: a.current}
	// The first packet's payload starts with the frame header: 8 bytes in
	// Sunshine's short form (0x01 first), its fourth byte the frame's type.
	if len(data) < 8 || data[0] != 0x01 {
		return nil
	}
	f.IDR = data[3] == 2
	if a.codec == AV1 {
		// AV1 cannot take the padding after the last packet's data; the
		// header says how much of that packet is data.
		lastLen := int(binary.LittleEndian.Uint16(data[4:]))
		lastShards := a.blocks[a.last]
		pad := len(lastShards[len(lastShards)-1]) - lastLen
		if lastLen > 0 && pad > 0 && pad < len(data)-8 {
			data = data[:len(data)-pad]
		}
	} else if !f.IDR {
		f.IDR = hasIDR(data[8:], a.codec)
	}
	f.Data = data[8:]
	return f
}

// hasIDR looks for an IDR slice (H.264) or an IRAP picture (HEVC) in an
// Annex B bitstream.
func hasIDR(b []byte, codec Codec) bool {
	for i := 0; i+3 < len(b); i++ {
		if b[i] != 0 || b[i+1] != 0 || b[i+2] != 1 {
			continue
		}
		nal := b[i+3]
		switch codec {
		case H264:
			if nal&0x1F == 5 {
				return true
			}
		case HEVC:
			if t := (nal >> 1) & 0x3F; t >= 16 && t <= 21 {
				return true
			}
		}
	}
	return false
}

// pinger sends the ping that tells the host where a stream goes, every
// half second until ctx ends: the payload the RTSP setup gave, and a
// sequence number.
func pinger(ctx context.Context, conn *net.UDPConn, payload string) {
	msg := make([]byte, 20)
	copy(msg, payload)
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for seq := uint32(1); ; seq++ {
		binary.BigEndian.PutUint32(msg[16:], seq)
		conn.Write(msg)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// receiver reads a stream's RTP packets from the host.
type receiver struct {
	conn   *net.UDPConn
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func dialStream(ctx context.Context, host string, port int, payload string, read func([]byte)) (*receiver, error) {
	raddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return nil, err
	}
	conn.SetReadBuffer(8 << 20)
	ctx, cancel := context.WithCancel(ctx)
	r := &receiver{conn: conn, cancel: cancel}
	r.wg.Add(2)
	go func() { defer r.wg.Done(); pinger(ctx, conn, payload) }()
	go func() {
		defer r.wg.Done()
		buf := make([]byte, 65536)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				continue // an ICMP refusal while the host is not listening yet
			}
			read(buf[:n])
		}
	}()
	return r, nil
}

func (r *receiver) close() {
	r.cancel()
	r.conn.Close()
	r.wg.Wait()
}
