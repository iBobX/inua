package kdeconnect

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"
)

// Ports are the protocol's ports: UDP for discovery, a TCP range for
// links, and one for payloads. Zero, in tests, is any port free.
type Ports struct {
	UDP                    int
	TCPMin, TCPMax         int
	PayloadMin, PayloadMax int
}

// DefaultPorts are KDE Connect's.
var DefaultPorts = Ports{UDP: 1716, TCPMin: 1716, TCPMax: 1764, PayloadMin: 1739, PayloadMax: 1764}

// Config is what the core is told: this computer as devices see it, and
// what to do with what devices send.
type Config struct {
	Dir      string // the identity and the paired devices
	Name     string
	Type     string   // desktop or laptop
	Incoming []string // the packets this computer takes
	Outgoing []string // the packets it sends
	Ports    Ports
	// Peers are addresses to announce to directly, besides the broadcast
	// and the paired devices' last addresses: the tailnet's machines.
	Peers func(ctx context.Context) []net.IP
	// Loopback takes devices on this computer, for tests.
	Loopback bool

	// Changed is called when a device is found, lost, or its pairing
	// changes; Packet with each packet of a paired device. Both are called
	// from the core's goroutines, never with its lock held.
	Changed func(Device)
	Packet  func(Device, Packet)
}

// Core is this computer's end of the protocol: it finds devices, links
// with them, pairs, and passes their packets on.
type Core struct {
	cfg   Config
	me    *Identity
	store *Store

	tcp  net.Listener
	udp  *net.UDPConn
	port int // the TCP port listened on

	mu      sync.Mutex
	devices map[string]*device
	seen    map[string]time.Time // when each device last announced itself

	ctx context.Context
	wg  sync.WaitGroup
}

// maxUnpaired is how many devices not paired may be linked at once.
const maxUnpaired = 42

// ErrNotPaired is a packet for a device not paired, or not linked.
var ErrNotPaired = errors.New("kdeconnect: the device is not paired and reachable")

// ErrUnsupported is a packet the device does not take.
var ErrUnsupported = errors.New("kdeconnect: the device does not take that packet")

// New makes the core: the identity and paired devices read from cfg.Dir.
func New(cfg Config) (*Core, error) {
	me, err := LoadIdentity(cfg.Dir)
	if err != nil {
		return nil, err
	}
	store, err := OpenStore(filepath.Join(cfg.Dir, "devices.json"))
	if err != nil {
		return nil, err
	}
	c := &Core{cfg: cfg, me: me, store: store, devices: map[string]*device{}, seen: map[string]time.Time{}}
	for id, t := range store.All() {
		d := &device{id: id, state: Paired, info: DeviceInfo{DeviceID: id, DeviceName: t.Name, DeviceType: t.Type}}
		d.cert, _ = ParseCertPEM(t.Cert)
		d.addr = net.ParseIP(t.LastAddr)
		c.devices[id] = d
	}
	return c, nil
}

// ID is this computer's device ID.
func (c *Core) ID() string { return c.me.ID }

// Store is the paired devices' records, where their settings are kept.
func (c *Core) Store() *Store { return c.store }

// info is this computer's identity, as packets give it.
func (c *Core) info() DeviceInfo {
	return DeviceInfo{
		DeviceID:             c.me.ID,
		DeviceName:           FilterName(c.cfg.Name),
		DeviceType:           c.cfg.Type,
		ProtocolVersion:      ProtocolVersion,
		IncomingCapabilities: c.cfg.Incoming,
		OutgoingCapabilities: c.cfg.Outgoing,
		TCPPort:              c.port,
	}
}

// Start listens and announces until ctx ends; Wait waits for it to have
// ended.
func (c *Core) Start(ctx context.Context) error {
	c.ctx = ctx
	if err := c.listen(); err != nil {
		return err
	}
	c.wg.Add(4)
	go func() { defer c.wg.Done(); c.acceptLoop() }()
	go func() { defer c.wg.Done(); c.udpLoop() }()
	go func() { defer c.wg.Done(); c.announceLoop(ctx) }()
	go func() {
		defer c.wg.Done()
		<-ctx.Done()
		c.tcp.Close()
		c.udp.Close()
		c.mu.Lock()
		for _, d := range c.devices {
			if d.link != nil {
				d.link.Close()
			}
			c.stopPairing(d)
		}
		c.mu.Unlock()
	}()
	return nil
}

// Wait waits until the core has stopped.
func (c *Core) Wait() { c.wg.Wait() }

// listen opens the UDP port and the first TCP port free in the range.
func (c *Core) listen() error {
	p := c.cfg.Ports
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{Port: p.UDP})
	if err != nil {
		return fmt.Errorf("kdeconnect: UDP port %d: %w", p.UDP, err)
	}
	for port := p.TCPMin; port <= p.TCPMax; port++ {
		if c.tcp, err = net.Listen("tcp", fmt.Sprintf(":%d", port)); err == nil {
			break
		}
	}
	if c.tcp == nil {
		udp.Close()
		return fmt.Errorf("kdeconnect: no TCP port free from %d to %d", p.TCPMin, p.TCPMax)
	}
	c.mu.Lock()
	c.udp = udp
	c.port = c.tcp.Addr().(*net.TCPAddr).Port
	c.mu.Unlock()
	return nil
}

// TCPPort is the port links are taken on.
func (c *Core) TCPPort() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.port
}

// UDPPort is the port discovery listens on.
func (c *Core) UDPPort() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.udp.LocalAddr().(*net.UDPAddr).Port
}

func (c *Core) acceptLoop() {
	for {
		raw, err := c.tcp.Accept()
		if err != nil {
			return
		}
		if tc, ok := raw.(*net.TCPConn); ok {
			tc.SetKeepAlive(true)
			tc.SetKeepAlivePeriod(30 * time.Second)
		}
		go func() {
			l, err := Accept(c.ctx, raw, c.me, c.info(), c.trust)
			if err != nil {
				log.Printf("kdeconnect: a connection from %s: %v", raw.RemoteAddr(), err)
				return
			}
			c.addLink(l)
		}()
	}
}

// trust is the certificate pinned for a paired device.
func (c *Core) trust(id string) *x509.Certificate {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d := c.devices[id]; d != nil && d.state == Paired {
		return d.cert
	}
	return nil
}

// addLink takes a link done: it replaces the device's link, if it had one,
// and reads its packets.
func (c *Core) addLink(l *Link) {
	id := l.Peer.DeviceID
	c.mu.Lock()
	if c.ctx.Err() != nil {
		c.mu.Unlock()
		l.Close()
		return
	}
	d := c.devices[id]
	if d == nil {
		if c.unpairedLinks() >= maxUnpaired {
			c.mu.Unlock()
			l.Close()
			return
		}
		d = &device{id: id}
		c.devices[id] = d
	}
	if d.cert != nil && !d.cert.Equal(l.Cert) {
		c.mu.Unlock()
		l.Close()
		log.Printf("kdeconnect: %s came back with another certificate", id)
		return
	}
	old := d.link
	d.link, d.info, d.cert, d.addr = l, l.Peer, l.Cert, l.Addr
	paired := d.state == Paired
	snap := d.snapshot(c.me.Cert.Leaf)
	c.mu.Unlock()
	if old != nil {
		old.Close()
	}
	if paired && l.Addr != nil {
		c.store.Update(id, func(t *Trusted) {
			t.Name, t.Type, t.LastAddr = l.Peer.DeviceName, l.Peer.DeviceType, l.Addr.String()
		})
	}
	c.changed(snap)
	c.wg.Add(1)
	go func() { defer c.wg.Done(); c.readLoop(d, l) }()
}

func (c *Core) unpairedLinks() int {
	n := 0
	for _, d := range c.devices {
		if d.link != nil && d.state != Paired {
			n++
		}
	}
	return n
}

func (c *Core) readLoop(d *device, l *Link) {
	for {
		p, err := l.Read()
		if err != nil {
			break
		}
		switch p.Type {
		case TypePair:
			c.pairPacket(d, p)
		case TypeIdentity:
		default:
			c.mu.Lock()
			paired := d.state == Paired && d.link == l
			snap := d.snapshot(c.me.Cert.Leaf)
			c.mu.Unlock()
			if paired && c.cfg.Packet != nil {
				c.cfg.Packet(snap, p)
			}
		}
	}
	l.Close()
	c.mu.Lock()
	if d.link != l {
		c.mu.Unlock()
		return
	}
	d.link = nil
	if d.state != Paired {
		c.stopPairing(d)
		d.state = NotPaired
		delete(c.devices, d.id)
	}
	snap := d.snapshot(c.me.Cert.Leaf)
	c.mu.Unlock()
	c.changed(snap)
}

func (c *Core) changed(d Device) {
	if c.cfg.Changed != nil {
		c.cfg.Changed(d)
	}
}

// Devices are the devices known, paired or reachable, by name.
func (c *Core) Devices() []Device {
	c.mu.Lock()
	out := make([]Device, 0, len(c.devices))
	for _, d := range c.devices {
		out = append(out, d.snapshot(c.me.Cert.Leaf))
	}
	c.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Device is the device id, if it is known.
func (c *Core) Device(id string) (Device, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	d := c.devices[id]
	if d == nil {
		return Device{}, false
	}
	return d.snapshot(c.me.Cert.Leaf), true
}

// Send sends p to the device id, paired and reachable, if it takes it.
func (c *Core) Send(id string, p Packet) error {
	l, err := c.pairedLink(id, p.Type)
	if err != nil {
		return err
	}
	return l.Send(p)
}

func (c *Core) pairedLink(id, typ string) (*Link, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	d := c.devices[id]
	if d == nil || d.link == nil || d.state != Paired {
		return nil, ErrNotPaired
	}
	if !slices.Contains(d.info.IncomingCapabilities, typ) {
		return nil, ErrUnsupported
	}
	return d.link, nil
}
