package kdeconnect

import (
	"crypto/x509"
	"net"
	"slices"
	"time"
)

// PairState is where a device is in pairing with this computer.
type PairState int

const (
	NotPaired       PairState = iota
	Requested                 // this computer asked, the device has not answered
	RequestedByPeer           // the device asked, the user has not answered
	Paired
)

// pairTimeout is how long a request to pair waits for its answer, as long
// as KDE Connect waits.
const pairTimeout = 30 * time.Second

// pairClockSkew is how far a request's time may be from this computer's.
const pairClockSkew = 1800

// Device is a device as this computer knows it at a moment: found on the
// network, paired, or both.
type Device struct {
	ID        string
	Name      string
	Type      string // phone, tablet, desktop, laptop, tv
	Reachable bool
	Pair      PairState
	// Key is what both screens show while they pair (VerificationKey).
	Key      string
	Addr     net.IP
	Incoming []string // the packets it takes
	Outgoing []string // the packets it sends
}

// Takes says whether the device takes packets of type t.
func (d Device) Takes(t string) bool { return slices.Contains(d.Incoming, t) }

// Sends says whether the device sends packets of type t.
func (d Device) Sends(t string) bool { return slices.Contains(d.Outgoing, t) }

// device is a device's state in the core; the core's lock guards it.
type device struct {
	id    string
	info  DeviceInfo
	link  *Link
	cert  *x509.Certificate // the link's, or the one pinned
	addr  net.IP
	state PairState
	stamp int64 // the pairing's time, while one waits
	timer *time.Timer
}

func (d *device) snapshot(me *x509.Certificate) Device {
	out := Device{
		ID:        d.id,
		Name:      d.info.DeviceName,
		Type:      d.info.DeviceType,
		Reachable: d.link != nil,
		Pair:      d.state,
		Addr:      d.addr,
		Incoming:  slices.Clone(d.info.IncomingCapabilities),
		Outgoing:  slices.Clone(d.info.OutgoingCapabilities),
	}
	if (d.state == Requested || d.state == RequestedByPeer) && d.cert != nil {
		out.Key = VerificationKey(me, d.cert, d.stamp)
	}
	return out
}
