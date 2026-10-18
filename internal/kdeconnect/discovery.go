package kdeconnect

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"slices"
	"strconv"
	"time"
)

// againAfter is how soon a device announcing itself twice is answered
// again, as KDE Connect waits.
const againAfter = 500 * time.Millisecond

// udpLoop reads devices' announcements, and connects to each.
func (c *Core) udpLoop() {
	buf := make([]byte, 64<<10)
	for {
		n, from, err := c.udp.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if from.IP.IsLoopback() && !c.cfg.Loopback {
			continue
		}
		var p Packet
		var info DeviceInfo
		if jsonPacket(buf[:n], &p, &info) != nil || p.Type != TypeIdentity || !ValidID(info.DeviceID) {
			continue
		}
		c.discovered(from.IP, info)
	}
}

// discovered connects to a device that announced itself, unless it is
// linked at that address already.
func (c *Core) discovered(ip net.IP, info DeviceInfo) {
	if info.DeviceID == c.me.ID || info.ProtocolVersion < ProtocolVersion || !c.tcpPortOK(info.TCPPort) {
		return
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	now := time.Now()
	c.mu.Lock()
	if t, ok := c.seen[info.DeviceID]; ok && now.Sub(t) < againAfter {
		c.mu.Unlock()
		return
	}
	for id, t := range c.seen {
		if now.Sub(t) >= againAfter {
			delete(c.seen, id)
		}
	}
	if len(c.seen) >= 1024 {
		c.mu.Unlock()
		return
	}
	c.seen[info.DeviceID] = now
	if d := c.devices[info.DeviceID]; d != nil && d.link != nil && d.addr.Equal(ip) {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	go func() {
		addr := net.JoinHostPort(ip.String(), strconv.Itoa(info.TCPPort))
		l, err := Dial(c.ctx, c.me, c.info(), addr, info.DeviceID, c.trust)
		if err != nil {
			// The way back may be shut: the device is asked to connect here
			// instead.
			c.announceTo([]net.IP{ip})
			return
		}
		c.addLink(l)
	}()
}

func (c *Core) tcpPortOK(port int) bool {
	p := c.cfg.Ports
	if p.TCPMin == 0 {
		return port > 0 && port < 1<<16
	}
	return port >= p.TCPMin && port <= p.TCPMax
}

// announcement is this computer's identity as it announces it.
func (c *Core) announcement() []byte {
	info := c.info()
	b, _ := json.Marshal(NewPacket(TypeIdentity, info))
	return append(b, '\n')
}

// announceTo sends this computer's identity to each address.
func (c *Core) announceTo(ips []net.IP) {
	msg := c.announcement()
	for _, ip := range ips {
		c.udp.WriteToUDP(msg, &net.UDPAddr{IP: ip, Port: c.cfg.Ports.UDP})
	}
}

// Announce tells the devices around that this computer is here: by
// broadcast on each network, and directly to the tailnet's machines and
// to where the paired devices were last.
func (c *Core) Announce(ctx context.Context) {
	ips := broadcasts()
	if c.cfg.Loopback {
		ips = append(ips, net.IPv4(127, 0, 0, 1))
	}
	c.mu.Lock()
	for _, d := range c.devices {
		if d.state == Paired && d.link == nil && d.addr != nil {
			ips = append(ips, d.addr)
		}
	}
	c.mu.Unlock()
	if c.cfg.Peers != nil {
		for _, ip := range c.cfg.Peers(ctx) {
			if !c.linkedAt(ip) {
				ips = append(ips, ip)
			}
		}
	}
	c.announceTo(ips)
}

func (c *Core) linkedAt(ip net.IP) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, d := range c.devices {
		if d.link != nil && d.addr.Equal(ip) {
			return true
		}
	}
	return false
}

// AnnounceTo tells the device at ip, by its UDP port, that this computer
// is here: a device added by its address.
func (c *Core) AnnounceTo(ip net.IP) { c.announceTo([]net.IP{ip}) }

// broadcasts are the broadcast addresses of this computer's IPv4 networks,
// and the network's own.
func broadcasts() []net.IP {
	out := []net.IP{net.IPv4bcast}
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagBroadcast == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil {
				continue
			}
			ip, mask := n.IP.To4(), net.IP(n.Mask).To4()
			if mask == nil {
				continue
			}
			b := make(net.IP, 4)
			for i := range b {
				b[i] = ip[i] | ^mask[i]
			}
			if !slices.ContainsFunc(out, b.Equal) {
				out = append(out, b)
			}
		}
	}
	return out
}

// addrSet is this computer's addresses, to see when its networks change.
func addrSet() string {
	addrs, _ := net.InterfaceAddrs()
	var s []string
	for _, a := range addrs {
		s = append(s, a.String())
	}
	slices.Sort(s)
	b, _ := json.Marshal(s)
	return string(b)
}

// announceLoop announces at the start, when the networks change, and now
// and then while a paired device is away.
func (c *Core) announceLoop(ctx context.Context) {
	c.Announce(ctx)
	last, lastAll := addrSet(), time.Now()
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := addrSet()
		if now != last || time.Since(lastAll) >= 2*time.Minute && c.someAway() {
			if now != last {
				log.Print("kdeconnect: the networks changed; announcing")
			}
			last, lastAll = now, time.Now()
			c.Announce(ctx)
		}
	}
}

func (c *Core) someAway() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, d := range c.devices {
		if d.state == Paired && d.link == nil {
			return true
		}
	}
	return false
}
