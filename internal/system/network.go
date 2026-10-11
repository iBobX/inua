package system

import (
	"context"

	"github.com/godbus/dbus/v5"
)

const nmName = "org.freedesktop.NetworkManager"

// NetworkState is what NetworkManager reports about the primary connection.
type NetworkState struct {
	Available   bool // NetworkManager is running
	WifiEnabled bool
	WifiDevice  bool   // there is a Wi-Fi device to turn on
	Kind        string // "wifi", "ethernet", "vpn", "other" or "" when offline
	Name        string // SSID for Wi-Fi, else the connection name
	Strength    int    // 0-100, Wi-Fi only
}

// Network follows NetworkManager.
type Network struct{ src source }

func NewNetwork(dial Dialer) *Network {
	return &Network{src: source{dial: dial, name: nmName, matches: [][]dbus.MatchOption{
		// Strength changes arrive from access point objects, so take all of
		// them; a read is a handful of calls.
		{dbus.WithMatchSender(nmName), dbus.WithMatchInterface(propsIface)},
	}}}
}

// Run reports the state until ctx ends.
func (n *Network) Run(ctx context.Context, onChange func(NetworkState)) {
	e := &emitter[NetworkState]{f: onChange}
	n.src.run(ctx, func(ctx context.Context, conn *dbus.Conn) { e.emit(readNetwork(ctx, conn)) })
}

func readNetwork(ctx context.Context, conn *dbus.Conn) NetworkState {
	props, err := getAll(ctx, conn.Object(nmName, "/org/freedesktop/NetworkManager"), nmName)
	if err != nil {
		return NetworkState{}
	}
	s := NetworkState{Available: true, WifiEnabled: value[bool](props, "WirelessEnabled")}
	for _, dev := range value[[]dbus.ObjectPath](props, "Devices") {
		if d, err := getAll(ctx, conn.Object(nmName, dev), nmName+".Device"); err == nil && value[uint32](d, "DeviceType") == 2 {
			s.WifiDevice = true
			break
		}
	}
	primary := value[dbus.ObjectPath](props, "PrimaryConnection")
	if primary == "" || primary == "/" {
		return s
	}
	ac, err := getAll(ctx, conn.Object(nmName, primary), nmName+".Connection.Active")
	if err != nil {
		return s
	}
	s.Name = value[string](ac, "Id")
	switch value[string](ac, "Type") {
	case "802-11-wireless":
		s.Kind = "wifi"
		if ap := value[dbus.ObjectPath](ac, "SpecificObject"); ap != "" && ap != "/" {
			if p, err := getAll(ctx, conn.Object(nmName, ap), nmName+".AccessPoint"); err == nil {
				if ssid := value[[]byte](p, "Ssid"); len(ssid) > 0 {
					s.Name = string(ssid)
				}
				s.Strength = int(value[byte](p, "Strength"))
			}
		}
	case "802-3-ethernet":
		s.Kind = "ethernet"
	case "vpn", "wireguard":
		s.Kind = "vpn"
	default:
		s.Kind = "other"
	}
	return s
}

// SetWifi turns Wi-Fi on or off.
func (n *Network) SetWifi(on bool) error {
	return n.src.setProp(nmName, "/org/freedesktop/NetworkManager", nmName, "WirelessEnabled", on)
}
