package kdeconnect

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"time"
)

// Packet is one of the protocol's packets: a type, a body, and, for a
// file, the size of its payload and where to fetch it.
type Packet struct {
	ID                  int64           `json:"id"`
	Type                string          `json:"type"`
	Body                json.RawMessage `json:"body"`
	PayloadSize         int64           `json:"payloadSize,omitempty"`
	PayloadTransferInfo *TransferInfo   `json:"payloadTransferInfo,omitempty"`
}

// TransferInfo says where a payload is served.
type TransferInfo struct {
	Port int `json:"port"`
}

// The packets of the link itself.
const (
	TypeIdentity = "kdeconnect.identity"
	TypePair     = "kdeconnect.pair"
)

// NewPacket is a packet of type t with body (a struct or a map), stamped
// with the time as its ID, as KDE Connect does.
func NewPacket(t string, body any) Packet {
	if body == nil {
		body = struct{}{}
	}
	b, _ := json.Marshal(body)
	return Packet{ID: time.Now().UnixMilli(), Type: t, Body: b}
}

// Decode reads the packet's body into v.
func (p Packet) Decode(v any) error {
	if len(p.Body) == 0 {
		return nil
	}
	return json.Unmarshal(p.Body, v)
}

// Encode is the packet as it goes on the wire: compact JSON and a newline.
func (p Packet) Encode() []byte {
	b, _ := json.Marshal(p)
	return append(b, '\n')
}

// maxPacket is the longest line a packet may be; an identity is far
// shorter, a notification with its conversation can be long.
const maxPacket = 4 << 20

// ErrTooLong is a packet longer than any the protocol sends.
var ErrTooLong = errors.New("kdeconnect: a packet too long")

// reader reads packets, one a line.
type reader struct{ r *bufio.Reader }

func newReader(r io.Reader) *reader { return &reader{r: bufio.NewReaderSize(r, 64<<10)} }

func (r *reader) read() (Packet, error) {
	var line []byte
	for {
		part, prefix, err := r.r.ReadLine()
		if err != nil {
			return Packet{}, err
		}
		line = append(line, part...)
		if len(line) > maxPacket {
			return Packet{}, ErrTooLong
		}
		if !prefix {
			break
		}
	}
	var p Packet
	if err := json.Unmarshal(line, &p); err != nil {
		return Packet{}, err
	}
	return p, nil
}

// DeviceInfo is a device as its identity packet describes it.
type DeviceInfo struct {
	DeviceID              string   `json:"deviceId"`
	DeviceName            string   `json:"deviceName"`
	DeviceType            string   `json:"deviceType,omitempty"`
	ProtocolVersion       int      `json:"protocolVersion"`
	IncomingCapabilities  []string `json:"incomingCapabilities,omitempty"`
	OutgoingCapabilities  []string `json:"outgoingCapabilities,omitempty"`
	TCPPort               int      `json:"tcpPort,omitempty"`
	TargetDeviceID        string   `json:"targetDeviceId,omitempty"`
	TargetProtocolVersion int      `json:"targetProtocolVersion,omitempty"`
}

// jsonPacket reads a packet from line, and its body into body.
func jsonPacket(line []byte, p *Packet, body any) error {
	if err := json.Unmarshal(line, p); err != nil {
		return err
	}
	return p.Decode(body)
}

// UnmarshalJSON reads an identity, its numbers given as numbers or as
// strings: KDE Connect for Android sends targetProtocolVersion as "8".
func (d *DeviceInfo) UnmarshalJSON(b []byte) error {
	type plain DeviceInfo
	var aux struct {
		plain
		ProtocolVersion       flexInt `json:"protocolVersion"`
		TCPPort               flexInt `json:"tcpPort"`
		TargetProtocolVersion flexInt `json:"targetProtocolVersion"`
	}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	*d = DeviceInfo(aux.plain)
	d.ProtocolVersion, d.TCPPort, d.TargetProtocolVersion = int(aux.ProtocolVersion), int(aux.TCPPort), int(aux.TargetProtocolVersion)
	return nil
}

// flexInt is a number, written as one or as a string.
type flexInt int

func (n *flexInt) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if s == "" {
			*n = 0
			return nil
		}
		b = []byte(s)
	}
	var v int
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*n = flexInt(v)
	return nil
}
