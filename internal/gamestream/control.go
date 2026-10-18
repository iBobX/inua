package gamestream

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/lyricat/inua/internal/gamestream/enet"
)

// The control stream's message types (Sunshine, encrypted control).
const (
	ctlEncrypted   = 0x0001
	ctlPeriodic    = 0x0200
	ctlIDR         = 0x0302 // request an IDR frame; also the first start message
	ctlStartB      = 0x0307
	ctlInput       = 0x0206
	ctlTermination = 0x0109
)

// The control stream's ENet channels.
const (
	chanGeneric  = 0x00
	chanUrgent   = 0x01
	chanKeyboard = 0x02
	chanMouse    = 0x03
	chanUTF8     = 0x06
	chanCount    = 0x30
)

// ErrHostEnded is a session the host ended, and Code says how.
type ErrHostEnded struct{ Code uint32 }

func (e *ErrHostEnded) Error() string {
	return fmt.Sprintf("gamestream: the host ended the session (%#x)", e.Code)
}

// control is the session's control stream: encrypted messages on ENet.
type control struct {
	conn *enet.Conn
	gcm  cipher.AEAD

	mu  sync.Mutex
	seq uint32

	done chan struct{}
	err  error
}

func newGCM(key [16]byte) cipher.AEAD {
	b, _ := aes.NewCipher(key[:])
	g, _ := cipher.NewGCM(b)
	return g
}

// iv is a control message's: its sequence number, little-endian, and two
// bytes for who sent it ('C' the client, 'H' the host) on which stream
// ('C' control).
func iv(seq uint32, from byte) []byte {
	v := make([]byte, 12)
	binary.LittleEndian.PutUint32(v, seq)
	v[10], v[11] = from, 'C'
	return v
}

// seal encrypts a message as the host reads it: its encrypted header, then
// the GCM tag, then the ciphertext of the type, length and payload.
func seal(gcm cipher.AEAD, seq uint32, ptype uint16, payload []byte) []byte {
	plain := make([]byte, 4+len(payload))
	binary.LittleEndian.PutUint16(plain, ptype)
	binary.LittleEndian.PutUint16(plain[2:], uint16(len(payload)))
	copy(plain[4:], payload)
	sealed := gcm.Seal(nil, iv(seq, 'C'), plain, nil) // ciphertext, then tag
	ct, tag := sealed[:len(plain)], sealed[len(plain):]
	out := make([]byte, 8, 8+len(sealed))
	binary.LittleEndian.PutUint16(out, ctlEncrypted)
	binary.LittleEndian.PutUint16(out[2:], uint16(4+len(sealed)))
	binary.LittleEndian.PutUint32(out[4:], seq)
	out = append(out, tag...)
	return append(out, ct...)
}

// open decrypts a message sealed by from ('H' the host).
func open(gcm cipher.AEAD, msg []byte, from byte) (uint16, []byte, error) {
	if len(msg) < 8+16+4 || binary.LittleEndian.Uint16(msg) != ctlEncrypted {
		return 0, nil, errors.New("gamestream: not an encrypted control message")
	}
	n := int(binary.LittleEndian.Uint16(msg[2:]))
	seq := binary.LittleEndian.Uint32(msg[4:])
	if n < 4+16+4 || 4+n > len(msg) {
		return 0, nil, errors.New("gamestream: a control message cut short")
	}
	body := msg[8 : 4+n]
	tag, ct := body[:16], body[16:]
	plain, err := gcm.Open(nil, iv(seq, from), append(append([]byte(nil), ct...), tag...), nil)
	if err != nil {
		return 0, nil, err
	}
	if len(plain) < 4 {
		return 0, nil, errors.New("gamestream: an empty control message")
	}
	return binary.LittleEndian.Uint16(plain), plain[4:], nil
}

// dialControl connects the control stream and starts the session's
// streams.
func dialControl(ctx context.Context, host string, port int, connectData uint32, key [16]byte) (*control, error) {
	conn, err := enet.Dial(ctx, net.JoinHostPort(host, strconv.Itoa(port)), chanCount, connectData)
	if err != nil {
		return nil, fmt.Errorf("gamestream: control stream: %w", err)
	}
	c := &control{conn: conn, gcm: newGCM(key), done: make(chan struct{})}
	go c.read()
	go c.ping()
	if err := c.send(ctlIDR, []byte{0, 0}, chanGeneric, true); err != nil {
		c.close()
		return nil, err
	}
	if err := c.send(ctlStartB, []byte{0}, chanGeneric, true); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func (c *control) send(ptype uint16, payload []byte, channel uint8, reliable bool) error {
	c.mu.Lock()
	seq := c.seq
	c.seq++
	msg := seal(c.gcm, seq, ptype, payload)
	c.mu.Unlock()
	return c.conn.Send(channel, msg, reliable)
}

// requestIDR asks the host for a frame that needs none before it.
func (c *control) requestIDR() error { return c.send(ctlIDR, []byte{0, 0}, chanUrgent, true) }

// input sends an input packet.
func (c *control) input(packet []byte, channel uint8) error {
	return c.send(ctlInput, packet, channel, true)
}

// ping tells the host the client is there, as Moonlight does.
func (c *control) ping() {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	payload := []byte{4, 0, 0, 0, 0, 0, 0, 0} // its length, then a time nobody reads
	for {
		select {
		case <-c.done:
			return
		case <-c.conn.Done():
			return
		case <-t.C:
			c.send(ctlPeriodic, payload, chanGeneric, true)
		}
	}
}

// read reads the host's messages until the stream ends; the host's
// termination ends it with its code.
func (c *control) read() {
	for p := range c.conn.Packets() {
		ptype, payload, err := open(c.gcm, p.Data, 'H')
		if err != nil {
			continue
		}
		if ptype == ctlTermination {
			code := uint32(0)
			if len(payload) >= 4 {
				code = binary.BigEndian.Uint32(payload)
			}
			c.end(&ErrHostEnded{Code: code})
			return
		}
	}
	c.end(c.conn.Err())
}

func (c *control) end(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.done:
		return
	default:
	}
	c.err = err
	close(c.done)
}

func (c *control) close() {
	c.conn.Close()
	c.end(enet.ErrClosed)
}
