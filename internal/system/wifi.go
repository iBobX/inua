package system

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// WifiList is what the Wi-Fi page and the control center's list show:
// the networks in range and the saved ones, read when wanted (polled
// while shown), not followed: access points change their strength every
// moment.
type WifiList struct {
	Device   string // the Wi-Fi device's object, "" without one
	Hardware string // its MAC address
	Address  string // the connected network's IPv4 address, "" for none
	Gateway  string
	DNS      []string // the connected network's DNS servers (IPv4)
	// Networks are one a name: the connected one first, then those in
	// range by strength, then the saved ones out of range by name.
	Networks []WifiNetwork
}

// WifiNetwork is one network name (SSID): the strongest access point in
// range with it, and the connection saved for it, if any.
type WifiNetwork struct {
	SSID       string
	Strength   int    // 0-100, the strongest access point's
	Frequency  int    // MHz, the strongest access point's
	Security   string // "" (open), "wep", "wpa", "sae" (WPA3), "owe" (enhanced open) or "enterprise"
	InRange    bool
	Saved      bool
	Connected  bool
	Connecting bool
	Conn       string // the saved connection's object
	AP         string // the strongest access point's object
}

// Secure reports whether connecting needs a password or more.
func (n WifiNetwork) Secure() bool { return n.Security != "" && n.Security != "owe" }

// WifiError is NetworkManager's reason for a connection that did not come
// up (NMDeviceStateReason).
type WifiError struct{ Reason uint32 }

func (e *WifiError) Error() string {
	return fmt.Sprintf("system: wifi: not connected (reason %d)", e.Reason)
}

// WrongPassword reports whether the network turned the password down.
func (e *WifiError) WrongPassword() bool {
	return e.Reason == 7 || e.Reason == 8 // no secrets; the supplicant disconnected
}

// ErrEnterprise is connecting to a network that signs in with a user
// name and certificates (802.1X), which inua does not set up.
var ErrEnterprise = errors.New("system: wifi: enterprise networks are not supported")

// activateLimit is how long a connection may take to come up.
const activateLimit = 45 * time.Second

// Access point flags (NM80211ApFlags, NM80211ApSecurityFlags).
const (
	apPrivacy  = 0x1
	keyPSK     = 0x100
	key8021X   = 0x200
	keySAE     = 0x400
	keyOWE     = 0x800
	keyOWETM   = 0x1000
	keyEAP192  = 0x2000
	deviceWifi = 2
)

// wifiDevice is the first Wi-Fi device, or "".
func (n *Network) wifiDevice(ctx context.Context, conn *dbus.Conn) dbus.ObjectPath {
	props, err := getAll(ctx, conn.Object(nmName, "/org/freedesktop/NetworkManager"), nmName)
	if err != nil {
		return ""
	}
	for _, dev := range value[[]dbus.ObjectPath](props, "Devices") {
		if d, err := getAll(ctx, conn.Object(nmName, dev), nmName+".Device"); err == nil && value[uint32](d, "DeviceType") == deviceWifi {
			return dev
		}
	}
	return ""
}

// conn is the source's connection, or an error while not connected.
func (n *Network) conn() (*dbus.Conn, error) {
	n.src.mu.Lock()
	defer n.src.mu.Unlock()
	if n.src.conn == nil {
		return nil, errNotConnected
	}
	return n.src.conn, nil
}

// nameservers are an IP4Config's DNS servers: NameserverData's addresses,
// or, from a NetworkManager older than 1.14, Nameservers' numbers (each an
// address in network byte order).
func nameservers(p map[string]dbus.Variant) []string {
	var out []string
	for _, d := range value[[]map[string]dbus.Variant](p, "NameserverData") {
		if a := value[string](d, "address"); a != "" {
			out = append(out, a)
		}
	}
	if out != nil {
		return out
	}
	for _, n := range value[[]uint32](p, "Nameservers") {
		out = append(out, fmt.Sprintf("%d.%d.%d.%d", byte(n), byte(n>>8), byte(n>>16), byte(n>>24)))
	}
	return out
}

// ReadWifi reads the networks now.
func (n *Network) ReadWifi(ctx context.Context) (WifiList, error) {
	conn, err := n.conn()
	if err != nil {
		return WifiList{}, err
	}
	return readWifi(ctx, conn, n.wifiDevice(ctx, conn))
}

func readWifi(ctx context.Context, conn *dbus.Conn, dev dbus.ObjectPath) (WifiList, error) {
	var l WifiList
	if dev == "" {
		return l, nil
	}
	l.Device = string(dev)
	d, err := getAll(ctx, conn.Object(nmName, dev), nmName+".Device")
	if err != nil {
		return l, err
	}
	w, err := getAll(ctx, conn.Object(nmName, dev), nmName+".Device.Wireless")
	if err != nil {
		return l, err
	}
	l.Hardware = value[string](w, "HwAddress")
	state := value[uint32](d, "State")

	// The connection the device has, and its network.
	var activeConn dbus.ObjectPath
	if ac := value[dbus.ObjectPath](d, "ActiveConnection"); ac != "" && ac != "/" {
		if a, err := getAll(ctx, conn.Object(nmName, ac), nmName+".Connection.Active"); err == nil {
			activeConn = value[dbus.ObjectPath](a, "Connection")
		}
	}
	if state == 100 {
		if ip := value[dbus.ObjectPath](d, "Ip4Config"); ip != "" && ip != "/" {
			if p, err := getAll(ctx, conn.Object(nmName, ip), nmName+".IP4Config"); err == nil {
				for _, a := range value[[]map[string]dbus.Variant](p, "AddressData") {
					if addr := value[string](a, "address"); addr != "" {
						l.Address = addr
						break
					}
				}
				l.Gateway = value[string](p, "Gateway")
				l.DNS = nameservers(p)
			}
		}
	}

	byName := map[string]*WifiNetwork{}
	var order []string
	get := func(ssid string) *WifiNetwork {
		if nw := byName[ssid]; nw != nil {
			return nw
		}
		byName[ssid] = &WifiNetwork{SSID: ssid}
		order = append(order, ssid)
		return byName[ssid]
	}

	for _, ap := range value[[]dbus.ObjectPath](w, "AccessPoints") {
		p, err := getAll(ctx, conn.Object(nmName, ap), nmName+".AccessPoint")
		if err != nil {
			continue // gone meanwhile
		}
		ssid := string(value[[]byte](p, "Ssid"))
		if ssid == "" {
			continue // a hidden network
		}
		strength := int(value[byte](p, "Strength"))
		nw := get(ssid)
		if nw.InRange && strength <= nw.Strength {
			continue
		}
		nw.InRange, nw.Strength, nw.AP = true, strength, string(ap)
		nw.Frequency = int(value[uint32](p, "Frequency"))
		nw.Security = apSecurity(value[uint32](p, "Flags"), value[uint32](p, "WpaFlags"), value[uint32](p, "RsnFlags"))
	}

	// The saved Wi-Fi connections, the latest used for a name.
	var conns []dbus.ObjectPath
	settings := conn.Object(nmName, "/org/freedesktop/NetworkManager/Settings")
	callCtx, cancel := context.WithTimeout(ctx, callLimit)
	err = settings.CallWithContext(callCtx, nmName+".Settings.ListConnections", 0).Store(&conns)
	cancel()
	if err != nil {
		return l, err
	}
	used := map[string]uint64{}
	for _, c := range conns {
		s, err := connSettings(ctx, conn, c)
		if err != nil || value[string](s["connection"], "type") != "802-11-wireless" {
			continue
		}
		if mode := value[string](s["802-11-wireless"], "mode"); mode != "" && mode != "infrastructure" {
			continue // a hotspot this computer makes
		}
		ssid := string(value[[]byte](s["802-11-wireless"], "ssid"))
		if ssid == "" {
			continue
		}
		stamp := value[uint64](s["connection"], "timestamp")
		nw := get(ssid)
		// The connection in use stays; else the one used last.
		if nw.Saved && (dbus.ObjectPath(nw.Conn) == activeConn || (c != activeConn && stamp < used[ssid])) {
			continue
		}
		nw.Saved, nw.Conn, used[ssid] = true, string(c), stamp
		if !nw.InRange {
			nw.Security = savedSecurity(s)
		}
		if c == activeConn {
			nw.Connected = state == 100
			nw.Connecting = state >= 40 && state < 100
		}
	}

	for _, ssid := range order {
		l.Networks = append(l.Networks, *byName[ssid])
	}
	SortWifi(l.Networks)
	return l, nil
}

// SortWifi puts the connected (or connecting) network first, then those in
// range by strength, then the saved ones out of range by name.
func SortWifi(nets []WifiNetwork) {
	rank := func(n WifiNetwork) int {
		switch {
		case n.Connected || n.Connecting:
			return 0
		case n.InRange:
			return 1
		}
		return 2
	}
	slices.SortStableFunc(nets, func(a, b WifiNetwork) int {
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra - rb
		}
		if a.InRange && a.Strength != b.Strength {
			return b.Strength - a.Strength
		}
		return strings.Compare(strings.ToLower(a.SSID), strings.ToLower(b.SSID))
	})
}

func connSettings(ctx context.Context, conn *dbus.Conn, path dbus.ObjectPath) (map[string]map[string]dbus.Variant, error) {
	ctx, cancel := context.WithTimeout(ctx, callLimit)
	defer cancel()
	var s map[string]map[string]dbus.Variant
	err := conn.Object(nmName, path).CallWithContext(ctx, nmName+".Settings.Connection.GetSettings", 0).Store(&s)
	return s, err
}

// apSecurity names an access point's security from its flags.
func apSecurity(flags, wpa, rsn uint32) string {
	keys := wpa | rsn
	switch {
	case keys&(key8021X|keyEAP192) != 0:
		return "enterprise"
	case keys&keySAE != 0 && keys&keyPSK == 0:
		return "sae"
	case keys&keyPSK != 0:
		return "wpa"
	case keys&(keyOWE|keyOWETM) != 0:
		return "owe"
	case flags&apPrivacy != 0:
		return "wep"
	}
	return ""
}

// savedSecurity names a saved connection's security.
func savedSecurity(s map[string]map[string]dbus.Variant) string {
	sec, ok := s["802-11-wireless-security"]
	if !ok {
		return ""
	}
	switch value[string](sec, "key-mgmt") {
	case "wpa-psk":
		return "wpa"
	case "sae":
		return "sae"
	case "owe":
		return "owe"
	case "wpa-eap", "wpa-eap-suite-b-192":
		return "enterprise"
	}
	return "wep"
}

// ScanWifi asks the device to look for networks; NetworkManager turns
// down a request soon after its last scan, which is no error here.
func (n *Network) ScanWifi(ctx context.Context) error {
	conn, err := n.conn()
	if err != nil {
		return err
	}
	dev := n.wifiDevice(ctx, conn)
	if dev == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, callLimit)
	defer cancel()
	err = conn.Object(nmName, dev).CallWithContext(ctx, nmName+".Device.Wireless.RequestScan", 0, map[string]dbus.Variant{}).Err
	var dbusErr dbus.Error
	if errors.As(err, &dbusErr) && strings.HasSuffix(dbusErr.Name, ".NotAllowed") {
		return nil
	}
	return err
}

// ConnectWifi connects to a network and waits until it is up. A saved
// network connects with what was saved, unless password is given, which
// replaces the saved one; a new network is saved, with password when it
// needs one, and forgotten again when it does not come up.
func (n *Network) ConnectWifi(ctx context.Context, nw WifiNetwork, password string) error {
	conn, err := n.conn()
	if err != nil {
		return err
	}
	if nw.Security == "enterprise" && !nw.Saved {
		return ErrEnterprise
	}
	dev := n.wifiDevice(ctx, conn)
	if dev == "" {
		return errors.New("system: no Wi-Fi device")
	}
	nm := conn.Object(nmName, "/org/freedesktop/NetworkManager")
	ap := dbus.ObjectPath("/")
	if nw.AP != "" {
		ap = dbus.ObjectPath(nw.AP)
	}
	callCtx, cancel := context.WithTimeout(ctx, callLimit)
	defer cancel()

	var active dbus.ObjectPath
	if nw.Saved {
		path := dbus.ObjectPath(nw.Conn)
		if password != "" {
			if err := updatePassword(callCtx, conn, path, nw.Security, password); err != nil {
				return err
			}
		}
		if err := nm.CallWithContext(callCtx, nmName+".ActivateConnection", 0, path, dev, ap).Store(&active); err != nil {
			return err
		}
		return waitActive(ctx, conn, dev, active)
	}

	settings := map[string]map[string]dbus.Variant{
		"connection":      {"id": dbus.MakeVariant(nw.SSID), "type": dbus.MakeVariant("802-11-wireless")},
		"802-11-wireless": {"ssid": dbus.MakeVariant([]byte(nw.SSID)), "mode": dbus.MakeVariant("infrastructure")},
	}
	if sec := securitySettings(nw.Security, password); sec != nil {
		settings["802-11-wireless-security"] = sec
	}
	var path dbus.ObjectPath
	if err := nm.CallWithContext(callCtx, nmName+".AddAndActivateConnection", 0, settings, dev, ap).Store(&path, &active); err != nil {
		return err
	}
	if err := waitActive(ctx, conn, dev, active); err != nil {
		// Not kept: a network saved with a wrong password would be tried
		// again by itself.
		delCtx, cancel := context.WithTimeout(context.Background(), callLimit)
		defer cancel()
		conn.Object(nmName, path).CallWithContext(delCtx, nmName+".Settings.Connection.Delete", 0)
		return err
	}
	return nil
}

// securitySettings is a new connection's 802-11-wireless-security, or nil
// for an open network.
func securitySettings(security, password string) map[string]dbus.Variant {
	switch security {
	case "wpa":
		return map[string]dbus.Variant{"key-mgmt": dbus.MakeVariant("wpa-psk"), "psk": dbus.MakeVariant(password)}
	case "sae":
		return map[string]dbus.Variant{"key-mgmt": dbus.MakeVariant("sae"), "psk": dbus.MakeVariant(password)}
	case "owe":
		return map[string]dbus.Variant{"key-mgmt": dbus.MakeVariant("owe")}
	case "wep":
		keyType := uint32(2) // a passphrase
		if l := len(password); l == 5 || l == 13 || l == 10 || l == 26 {
			keyType = 1 // a key, in ASCII or hex
		}
		return map[string]dbus.Variant{
			"key-mgmt": dbus.MakeVariant("none"), "wep-key0": dbus.MakeVariant(password), "wep-key-type": dbus.MakeVariant(keyType),
		}
	}
	return nil
}

// updatePassword saves a new password into a saved connection.
func updatePassword(ctx context.Context, conn *dbus.Conn, path dbus.ObjectPath, security, password string) error {
	s, err := connSettings(ctx, conn, path)
	if err != nil {
		return err
	}
	sec := s["802-11-wireless-security"]
	if sec == nil {
		sec = securitySettings(security, password)
		if sec == nil {
			return nil // open: nothing to save
		}
	}
	if value[string](sec, "key-mgmt") == "none" {
		sec["wep-key0"] = dbus.MakeVariant(password)
	} else {
		sec["psk"] = dbus.MakeVariant(password)
	}
	s["802-11-wireless-security"] = sec
	return conn.Object(nmName, path).CallWithContext(ctx, nmName+".Settings.Connection.Update", 0, s).Err
}

// waitActive waits until an active connection is up, or has failed.
func waitActive(ctx context.Context, conn *dbus.Conn, dev, active dbus.ObjectPath) error {
	ctx, cancel := context.WithTimeout(ctx, activateLimit)
	defer cancel()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		a, err := getAll(ctx, conn.Object(nmName, active), nmName+".Connection.Active")
		switch {
		case err == nil && value[uint32](a, "State") == 2:
			return nil
		case err != nil && ctx.Err() == nil, err == nil && value[uint32](a, "State") >= 3:
			// Gone or going: the device says why.
			var reason uint32
			if d, err := getAll(ctx, conn.Object(nmName, dev), nmName+".Device"); err == nil {
				if sr, ok := d["StateReason"].Value().([]any); ok && len(sr) == 2 {
					reason, _ = sr[1].(uint32)
				}
			}
			return &WifiError{Reason: reason}
		}
		select {
		case <-ctx.Done():
			return &WifiError{Reason: 10} // a time-out
		case <-tick.C:
		}
	}
}

// DisconnectWifi disconnects the Wi-Fi device; it stays disconnected
// until a network is connected again.
func (n *Network) DisconnectWifi(ctx context.Context) error {
	conn, err := n.conn()
	if err != nil {
		return err
	}
	dev := n.wifiDevice(ctx, conn)
	if dev == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, callLimit)
	defer cancel()
	return conn.Object(nmName, dev).CallWithContext(ctx, nmName+".Device.Disconnect", 0).Err
}

// ForgetWifi deletes every connection saved for a network's name.
func (n *Network) ForgetWifi(ctx context.Context, ssid string) error {
	conn, err := n.conn()
	if err != nil {
		return err
	}
	var conns []dbus.ObjectPath
	callCtx, cancel := context.WithTimeout(ctx, callLimit)
	defer cancel()
	if err := conn.Object(nmName, "/org/freedesktop/NetworkManager/Settings").CallWithContext(callCtx, nmName+".Settings.ListConnections", 0).Store(&conns); err != nil {
		return err
	}
	for _, c := range conns {
		s, err := connSettings(ctx, conn, c)
		if err != nil || value[string](s["connection"], "type") != "802-11-wireless" || string(value[[]byte](s["802-11-wireless"], "ssid")) != ssid {
			continue
		}
		if err := conn.Object(nmName, c).CallWithContext(callCtx, nmName+".Settings.Connection.Delete", 0).Err; err != nil {
			return err
		}
	}
	return nil
}
