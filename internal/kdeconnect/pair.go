package kdeconnect

import "time"

// pairBody is a pairing packet's body: a request has its time, an answer
// does not.
type pairBody struct {
	Pair      bool  `json:"pair"`
	Timestamp int64 `json:"timestamp,omitempty"`
}

// pairPacket follows a device's pairing packet, as KDE Connect's pairing
// does.
func (c *Core) pairPacket(d *device, p Packet) {
	var b struct {
		Pair      bool   `json:"pair"`
		Timestamp *int64 `json:"timestamp"`
	}
	if p.Decode(&b) != nil {
		return
	}
	c.mu.Lock()
	var paired, unpaired bool
	switch {
	case b.Pair && d.state == Requested:
		c.stopPairing(d)
		d.state, paired = Paired, true
	case b.Pair && d.state == RequestedByPeer:
		c.mu.Unlock()
		return
	case b.Pair && d.state == Paired && b.Timestamp == nil:
		c.mu.Unlock()
		return
	case b.Pair:
		// A request: from a device not paired, or one that forgot this
		// computer.
		if b.Timestamp == nil {
			c.mu.Unlock()
			return
		}
		if skew := time.Now().Unix() - *b.Timestamp; skew > pairClockSkew || skew < -pairClockSkew {
			c.mu.Unlock()
			return
		}
		unpaired = d.state == Paired
		d.state, d.stamp = RequestedByPeer, *b.Timestamp
		c.startPairing(d)
	default:
		unpaired = d.state == Paired
		c.stopPairing(d)
		d.state = NotPaired
	}
	snap := d.snapshot(c.me.Cert.Leaf)
	c.mu.Unlock()
	if paired {
		c.trustDevice(snap, d)
	}
	if unpaired {
		c.store.Delete(d.id)
	}
	c.changed(snap)
}

// startPairing starts the wait for an answer; the lock is held.
func (c *Core) startPairing(d *device) {
	c.stopPairing(d)
	d.timer = time.AfterFunc(pairTimeout, func() {
		c.mu.Lock()
		if d.state != Requested && d.state != RequestedByPeer {
			c.mu.Unlock()
			return
		}
		d.state, d.timer = NotPaired, nil
		l := d.link
		snap := d.snapshot(c.me.Cert.Leaf)
		c.mu.Unlock()
		if l != nil {
			l.Send(NewPacket(TypePair, pairBody{Pair: false}))
		}
		c.changed(snap)
	})
}

// stopPairing ends the wait; the lock is held.
func (c *Core) stopPairing(d *device) {
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
}

func (c *Core) trustDevice(snap Device, d *device) {
	c.mu.Lock()
	t := Trusted{Name: d.info.DeviceName, Type: d.info.DeviceType, Cert: CertPEM(d.cert)}
	if d.addr != nil {
		t.LastAddr = d.addr.String()
	}
	c.mu.Unlock()
	c.store.Put(snap.ID, t)
}

// RequestPair asks the device id to pair.
func (c *Core) RequestPair(id string) error {
	c.mu.Lock()
	d := c.devices[id]
	if d == nil || d.link == nil {
		c.mu.Unlock()
		return ErrNotPaired
	}
	switch d.state {
	case Paired, Requested:
		c.mu.Unlock()
		return nil
	case RequestedByPeer:
		c.mu.Unlock()
		return c.AcceptPair(id)
	}
	d.state, d.stamp = Requested, time.Now().Unix()
	c.startPairing(d)
	l, stamp := d.link, d.stamp
	snap := d.snapshot(c.me.Cert.Leaf)
	c.mu.Unlock()
	if err := l.Send(NewPacket(TypePair, pairBody{Pair: true, Timestamp: stamp})); err != nil {
		c.mu.Lock()
		c.stopPairing(d)
		d.state = NotPaired
		snap = d.snapshot(c.me.Cert.Leaf)
		c.mu.Unlock()
		c.changed(snap)
		return err
	}
	c.changed(snap)
	return nil
}

// AcceptPair accepts the device's request to pair.
func (c *Core) AcceptPair(id string) error {
	c.mu.Lock()
	d := c.devices[id]
	if d == nil || d.link == nil || d.state != RequestedByPeer {
		c.mu.Unlock()
		return ErrNotPaired
	}
	c.stopPairing(d)
	l := d.link
	c.mu.Unlock()
	if err := l.Send(NewPacket(TypePair, pairBody{Pair: true})); err != nil {
		return err
	}
	c.mu.Lock()
	d.state = Paired
	snap := d.snapshot(c.me.Cert.Leaf)
	c.mu.Unlock()
	c.trustDevice(snap, d)
	c.changed(snap)
	return nil
}

// Unpair refuses the device's request, cancels this computer's, or
// forgets a paired device.
func (c *Core) Unpair(id string) error {
	c.mu.Lock()
	d := c.devices[id]
	if d == nil {
		c.mu.Unlock()
		return nil
	}
	c.stopPairing(d)
	d.state = NotPaired
	l := d.link
	if l == nil {
		delete(c.devices, id)
	}
	snap := d.snapshot(c.me.Cert.Leaf)
	c.mu.Unlock()
	if l != nil {
		l.Send(NewPacket(TypePair, pairBody{Pair: false}))
	}
	err := c.store.Delete(id)
	c.changed(snap)
	return err
}
