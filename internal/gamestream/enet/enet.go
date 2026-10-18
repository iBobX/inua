// Package enet is a client for ENet (http://enet.bespin.org), the reliable
// UDP protocol GameStream's control stream runs on: the part of it that
// stream uses. It connects to one host, sends reliable and unsequenced
// packets on a channel, acknowledges and orders what the host sends, sends
// again what the host has not acknowledged, and keeps the connection alive.
// No compression, no checksums, no bandwidth throttling: Sunshine uses none.
package enet

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// The protocol's commands and flags (ENet's protocol.h).
const (
	cmdAcknowledge       = 1
	cmdConnect           = 2
	cmdVerifyConnect     = 3
	cmdDisconnect        = 4
	cmdPing              = 5
	cmdSendReliable      = 6
	cmdSendUnreliable    = 7
	cmdSendFragment      = 8
	cmdSendUnsequenced   = 9
	cmdBandwidthLimit    = 10
	cmdThrottleConfigure = 11
	cmdUnreliableFrag    = 12
	cmdMask              = 0x0F

	flagAcknowledge = 1 << 7
	flagUnsequenced = 1 << 6

	headerSentTime     = 1 << 15
	headerCompressed   = 1 << 14
	headerSessionMask  = 3 << 12
	headerSessionShift = 12
	maxPeerID          = 0xFFF

	// What a client asks for, as ENet's own client does.
	mtu            = 1400
	windowSize     = 65536
	throttleWindow = 5000 // packetThrottleInterval
	throttleAccel  = 2
	throttleDecel  = 2
)

// commandSize is each command's size, its header included.
var commandSize = [...]int{
	cmdAcknowledge: 8, cmdConnect: 48, cmdVerifyConnect: 44, cmdDisconnect: 8, cmdPing: 4,
	cmdSendReliable: 6, cmdSendUnreliable: 8, cmdSendFragment: 24, cmdSendUnsequenced: 8,
	cmdBandwidthLimit: 12, cmdThrottleConfigure: 16, cmdUnreliableFrag: 24,
}

// ErrClosed is a connection that is closed, by either side.
var ErrClosed = errors.New("enet: connection closed")

// ErrTimeout is a host that stopped acknowledging.
var ErrTimeout = errors.New("enet: the host stopped answering")

// Packet is a packet the host sent, and the channel it came on.
type Packet struct {
	Channel uint8
	Data    []byte
}

// Conn is a connection to one host.
type Conn struct {
	udp      *net.UDPConn
	start    time.Time
	channels int

	mu         sync.Mutex
	peerID     uint16 // the host's ID for this connection
	outSession uint8  // the session in the headers this side sends
	inSession  uint8  // the session in the headers the host sends
	outSeq     []uint16
	inSeq      []uint16             // the last reliable sequence number delivered, by channel
	early      []map[uint16]waiting // reliable packets come before their turn, by channel
	frags      map[fragKey]*fragment
	unseqGroup uint16
	unacked    map[ackKey]*sent
	rto        time.Duration
	err        error
	done       chan struct{}

	in chan Packet
}

type ackKey struct {
	channel uint8
	seq     uint16
}

// sent is a reliable command not acknowledged yet.
type sent struct {
	datagram []byte // the whole datagram, its sent time to be rewritten
	first    time.Time
	last     time.Time
	tries    int
}

// waiting is a reliable packet come before its turn, and the sequence
// numbers it takes (more than one when it came in fragments).
type waiting struct {
	data []byte
	span uint16
}

type fragKey struct {
	channel uint8
	start   uint16
}

type fragment struct {
	data []byte
	have []bool
	left int
}

// timeout is how long a reliable command may go unacknowledged before
// the connection is given up, as ENet's default maximum.
const timeout = 15 * time.Second

// Dial connects to the ENet host at addr with channels channels, sending
// data in the connect command (GameStream's X-SS-Connect-Data), and returns
// once the host has verified the connection.
func Dial(ctx context.Context, addr string, channels int, data uint32) (*Conn, error) {
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	udp, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return nil, err
	}
	c := &Conn{
		udp: udp, start: time.Now(), channels: channels,
		peerID: maxPeerID, outSeq: make([]uint16, 256), inSeq: make([]uint16, 256),
		early: make([]map[uint16]waiting, 256), frags: map[fragKey]*fragment{},
		unacked: map[ackKey]*sent{}, rto: 500 * time.Millisecond,
		done: make(chan struct{}), in: make(chan Packet, 256),
	}
	var id [4]byte
	rand.Read(id[:])
	connectID := binary.BigEndian.Uint32(id[:])

	verified := make(chan error, 1)
	go c.read(connectID, verified)
	go c.tick()

	cmd := make([]byte, commandSize[cmdConnect])
	c.mu.Lock()
	c.outSeq[0xFF]++
	putHeader(cmd, cmdConnect|flagAcknowledge, 0xFF, c.outSeq[0xFF])
	c.mu.Unlock()
	b := cmd[4:]
	binary.BigEndian.PutUint16(b[0:], 0) // this side's peer index
	b[2], b[3] = 0xFF, 0xFF              // sessions: the host chooses
	binary.BigEndian.PutUint32(b[4:], mtu)
	binary.BigEndian.PutUint32(b[8:], windowSize)
	binary.BigEndian.PutUint32(b[12:], uint32(channels))
	binary.BigEndian.PutUint32(b[16:], 0) // incoming bandwidth: unlimited
	binary.BigEndian.PutUint32(b[20:], 0) // outgoing bandwidth
	binary.BigEndian.PutUint32(b[24:], throttleWindow)
	binary.BigEndian.PutUint32(b[28:], throttleAccel)
	binary.BigEndian.PutUint32(b[32:], throttleDecel)
	binary.BigEndian.PutUint32(b[36:], connectID)
	binary.BigEndian.PutUint32(b[40:], data)
	if err := c.sendReliable(0xFF, c.outSeq[0xFF], cmd); err != nil {
		c.Close()
		return nil, err
	}

	select {
	case err := <-verified:
		if err != nil {
			c.Close()
			return nil, err
		}
		return c, nil
	case <-c.done:
		return nil, c.closeErr()
	case <-ctx.Done():
		c.Close()
		return nil, ctx.Err()
	}
}

func putHeader(b []byte, command, channel uint8, seq uint16) {
	b[0], b[1] = command, channel
	binary.BigEndian.PutUint16(b[2:], seq)
}

// sentTime is the time ENet's headers carry: milliseconds, 16 bits.
func (c *Conn) sentTime() uint16 { return uint16(time.Since(c.start).Milliseconds()) }

// datagram wraps one command in a protocol header.
func (c *Conn) datagram(cmd []byte) []byte {
	c.mu.Lock()
	peer := c.peerID
	if peer < maxPeerID {
		peer |= uint16(c.outSession) << headerSessionShift
	}
	c.mu.Unlock()
	d := make([]byte, 4+len(cmd))
	binary.BigEndian.PutUint16(d[0:], peer|headerSentTime)
	binary.BigEndian.PutUint16(d[2:], c.sentTime())
	copy(d[4:], cmd)
	return d
}

func (c *Conn) sendReliable(channel uint8, seq uint16, cmd []byte) error {
	d := c.datagram(cmd)
	now := time.Now()
	c.mu.Lock()
	c.unacked[ackKey{channel, seq}] = &sent{datagram: d, first: now, last: now, tries: 1}
	c.mu.Unlock()
	_, err := c.udp.Write(d)
	return err
}

// Send sends data to the host on channel: reliably and in order, or
// unsequenced (whenever it arrives, if it does).
func (c *Conn) Send(channel uint8, data []byte, reliable bool) error {
	if err := c.closeErr(); err != nil {
		return err
	}
	if len(data) > mtu-4-commandSize[cmdSendReliable] {
		return fmt.Errorf("enet: a packet of %d bytes needs fragments, which this client does not send", len(data))
	}
	if !reliable {
		cmd := make([]byte, commandSize[cmdSendUnsequenced]+len(data))
		c.mu.Lock()
		c.unseqGroup++
		group := c.unseqGroup
		c.mu.Unlock()
		putHeader(cmd, cmdSendUnsequenced|flagUnsequenced, channel, 0)
		binary.BigEndian.PutUint16(cmd[4:], group)
		binary.BigEndian.PutUint16(cmd[6:], uint16(len(data)))
		copy(cmd[8:], data)
		_, err := c.udp.Write(c.datagram(cmd))
		return err
	}
	cmd := make([]byte, commandSize[cmdSendReliable]+len(data))
	c.mu.Lock()
	c.outSeq[channel]++
	seq := c.outSeq[channel]
	c.mu.Unlock()
	putHeader(cmd, cmdSendReliable|flagAcknowledge, channel, seq)
	binary.BigEndian.PutUint16(cmd[4:], uint16(len(data)))
	copy(cmd[6:], data)
	return c.sendReliable(channel, seq, cmd)
}

// Packets are the host's packets, in order on each reliable channel; the
// channel closes with the connection.
func (c *Conn) Packets() <-chan Packet { return c.in }

// Done is closed when the connection ends; Err says why.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err is why the connection ended, or nil while it lasts.
func (c *Conn) Err() error { return c.closeErr() }

func (c *Conn) closeErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close tells the host the connection ends, and ends it.
func (c *Conn) Close() error {
	cmd := make([]byte, commandSize[cmdDisconnect])
	putHeader(cmd, cmdDisconnect|flagUnsequenced, 0xFF, 0)
	if c.closeErr() == nil {
		c.udp.Write(c.datagram(cmd))
	}
	c.fail(ErrClosed)
	return nil
}

// fail ends the connection with err, once.
func (c *Conn) fail(err error) {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return
	}
	c.err = err
	close(c.done)
	c.mu.Unlock()
	c.udp.Close()
}

// tick sends again what the host has not acknowledged, pings the host when
// nothing else is on its way, and gives up on a host that stops answering.
func (c *Conn) tick() {
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	lastPing := time.Now()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
		}
		now := time.Now()
		var again [][]byte
		c.mu.Lock()
		for _, s := range c.unacked {
			if now.Sub(s.first) > timeout {
				c.mu.Unlock()
				c.fail(ErrTimeout)
				return
			}
			if now.Sub(s.last) > c.rto<<min(s.tries-1, 4) {
				binary.BigEndian.PutUint16(s.datagram[2:], c.sentTime())
				s.last, s.tries = now, s.tries+1
				again = append(again, s.datagram)
			}
		}
		idle := len(c.unacked) == 0 && c.peerID < maxPeerID
		c.mu.Unlock()
		for _, d := range again {
			c.udp.Write(d)
		}
		if idle && now.Sub(lastPing) > 500*time.Millisecond {
			lastPing = now
			cmd := make([]byte, commandSize[cmdPing])
			c.mu.Lock()
			c.outSeq[0xFF]++
			seq := c.outSeq[0xFF]
			c.mu.Unlock()
			putHeader(cmd, cmdPing|flagAcknowledge, 0xFF, seq)
			c.sendReliable(0xFF, seq, cmd)
		}
	}
}

// read takes the host's datagrams apart until the connection ends.
func (c *Conn) read(connectID uint32, verified chan<- error) {
	defer close(c.in)
	buf := make([]byte, 65536)
	for {
		n, err := c.udp.Read(buf)
		if err != nil {
			c.fail(ErrClosed)
			return
		}
		if err := c.datagramIn(buf[:n], connectID, verified); err != nil {
			c.fail(err)
			return
		}
	}
}

func (c *Conn) datagramIn(d []byte, connectID uint32, verified chan<- error) error {
	if len(d) < 2 {
		return nil
	}
	h := binary.BigEndian.Uint16(d)
	if h&headerCompressed != 0 {
		return nil // never asked for
	}
	session := uint8((h & headerSessionMask) >> headerSessionShift)
	var sentTime uint16
	d = d[2:]
	if h&headerSentTime != 0 {
		if len(d) < 2 {
			return nil
		}
		sentTime = binary.BigEndian.Uint16(d)
		d = d[2:]
	}
	c.mu.Lock()
	connected := c.peerID < maxPeerID
	wrong := connected && session != c.inSession
	c.mu.Unlock()
	if wrong {
		return nil
	}
	for len(d) >= 4 {
		command := d[0] & cmdMask
		if command == 0 || int(command) >= len(commandSize) || len(d) < commandSize[command] {
			return nil
		}
		size := commandSize[command]
		var payload []byte
		switch command {
		case cmdSendReliable:
			n := int(binary.BigEndian.Uint16(d[4:]))
			if len(d) < size+n {
				return nil
			}
			payload, size = d[size:size+n], size+n
		case cmdSendUnreliable, cmdSendUnsequenced:
			n := int(binary.BigEndian.Uint16(d[6:]))
			if len(d) < size+n {
				return nil
			}
			payload, size = d[size:size+n], size+n
		case cmdSendFragment, cmdUnreliableFrag:
			n := int(binary.BigEndian.Uint16(d[6:]))
			if len(d) < size+n {
				return nil
			}
			payload, size = d[size:size+n], size+n
		}
		if err := c.command(d[:commandSize[command]], payload, sentTime, connectID, verified); err != nil {
			return err
		}
		d = d[size:]
	}
	return nil
}

// command handles one of the host's commands, acknowledging it when it
// asks to be.
func (c *Conn) command(cmd, payload []byte, sentTime uint16, connectID uint32, verified chan<- error) error {
	command, channel, seq := cmd[0]&cmdMask, cmd[1], binary.BigEndian.Uint16(cmd[2:])
	if cmd[0]&flagAcknowledge != 0 {
		ack := make([]byte, commandSize[cmdAcknowledge])
		putHeader(ack, cmdAcknowledge, channel, seq)
		binary.BigEndian.PutUint16(ack[4:], seq)
		binary.BigEndian.PutUint16(ack[6:], sentTime)
		// After the command: a verified connect gives the header its peer ID.
		defer func() { c.udp.Write(c.datagram(ack)) }()
	}
	switch command {
	case cmdAcknowledge:
		acked := binary.BigEndian.Uint16(cmd[4:])
		c.mu.Lock()
		if s, ok := c.unacked[ackKey{channel, acked}]; ok {
			// The round trip, smoothed, sets how soon a loss is sent again.
			if s.tries == 1 {
				rtt := time.Since(s.first)
				c.rto = max(100*time.Millisecond, min((c.rto*7+rtt*4)/8, 2*time.Second))
			}
			delete(c.unacked, ackKey{channel, acked})
		}
		c.mu.Unlock()
	case cmdVerifyConnect:
		b := cmd[4:]
		ok := binary.BigEndian.Uint32(b[24:]) == throttleWindow && binary.BigEndian.Uint32(b[28:]) == throttleAccel &&
			binary.BigEndian.Uint32(b[32:]) == throttleDecel && binary.BigEndian.Uint32(b[36:]) == connectID
		if !ok {
			verified <- errors.New("enet: the host answered another connection")
			return nil
		}
		c.mu.Lock()
		first := c.peerID == maxPeerID
		c.peerID = binary.BigEndian.Uint16(b[0:])
		c.inSession, c.outSession = b[2], b[3]
		delete(c.unacked, ackKey{0xFF, 1}) // the connect command, answered
		c.mu.Unlock()
		if first {
			verified <- nil
		}
	case cmdDisconnect:
		return ErrClosed
	case cmdSendReliable:
		c.reliable(channel, seq, 1, append([]byte(nil), payload...))
	case cmdSendUnreliable, cmdSendUnsequenced:
		c.deliver(channel, payload)
	case cmdSendFragment:
		c.fragment(cmd, payload)
	}
	return nil
}

// reliable delivers a reliable packet in its turn: an early one waits for
// those before it, and one already delivered (sent again) is dropped. A
// packet that came in fragments takes span sequence numbers.
func (c *Conn) reliable(channel uint8, seq, span uint16, payload []byte) {
	c.mu.Lock()
	next := c.inSeq[channel] + 1
	if int16(seq-next) < 0 {
		c.mu.Unlock()
		return // delivered before
	}
	if seq != next {
		if c.early[channel] == nil {
			c.early[channel] = map[uint16]waiting{}
		}
		c.early[channel][seq] = waiting{payload, span}
		c.mu.Unlock()
		return
	}
	ready := [][]byte{payload}
	c.inSeq[channel] = seq + span - 1
	for {
		w, ok := c.early[channel][c.inSeq[channel]+1]
		if !ok {
			break
		}
		delete(c.early[channel], c.inSeq[channel]+1)
		c.inSeq[channel] += w.span
		ready = append(ready, w.data)
	}
	c.mu.Unlock()
	for _, p := range ready {
		c.deliver(channel, p)
	}
}

// fragment gathers a reliable packet sent in pieces, each piece with its
// own sequence number; it is delivered, in its turn, once every piece is
// there.
func (c *Conn) fragment(cmd, payload []byte) {
	channel := cmd[1]
	start := binary.BigEndian.Uint16(cmd[4:])
	count := binary.BigEndian.Uint32(cmd[8:])
	number := binary.BigEndian.Uint32(cmd[12:])
	total := binary.BigEndian.Uint32(cmd[16:])
	offset := binary.BigEndian.Uint32(cmd[20:])
	if count == 0 || count > 4096 || number >= count || total > 1<<20 || uint64(offset)+uint64(len(payload)) > uint64(total) {
		return
	}
	c.mu.Lock()
	if int16(start-(c.inSeq[channel]+1)) < 0 {
		c.mu.Unlock()
		return // a piece sent again, of a packet delivered
	}
	key := fragKey{channel, start}
	f, ok := c.frags[key]
	if !ok {
		f = &fragment{data: make([]byte, total), have: make([]bool, count), left: int(count)}
		c.frags[key] = f
	}
	if !f.have[number] {
		f.have[number] = true
		f.left--
		copy(f.data[offset:], payload)
	}
	complete := f.left == 0
	if complete {
		delete(c.frags, key)
	}
	c.mu.Unlock()
	if complete {
		c.reliable(channel, start, uint16(count), f.data)
	}
}

func (c *Conn) deliver(channel uint8, p []byte) {
	select {
	case c.in <- Packet{Channel: channel, Data: append([]byte(nil), p...)}:
	case <-c.done:
	}
}
