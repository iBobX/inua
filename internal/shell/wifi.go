package shell

import (
	"errors"

	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/system"
)

// WifiLists splits the networks for the Wi-Fi page: the one connected
// (or connecting), the saved ones, and the others in range.
func WifiLists(l system.WifiList) (current *system.WifiNetwork, known, nearby []system.WifiNetwork) {
	for i, n := range l.Networks {
		switch {
		case (n.Connected || n.Connecting) && current == nil:
			current = &l.Networks[i]
		case n.Saved:
			known = append(known, n)
		default:
			nearby = append(nearby, n)
		}
	}
	return current, known, nearby
}

// What the Wi-Fi page is doing with a network.
const (
	WifiConnect    = "connect"
	WifiDisconnect = "disconnect"
	WifiForget     = "forget"
)

// WifiStatus is the line under a network's name: what is being done with
// it (busy), else its state and security.
func WifiStatus(n system.WifiNetwork, busy string) string {
	switch busy {
	case WifiConnect:
		return i18n.T("Connecting…")
	case WifiDisconnect:
		return i18n.T("Disconnecting…")
	case WifiForget:
		return i18n.T("Forgetting…")
	}
	sec := WifiSecurity(n.Security)
	switch {
	case n.Connected:
		return i18n.Tf("Connected · %s · %s", WifiBand(n.Frequency), sec)
	case n.Connecting:
		return i18n.T("Connecting…")
	case !n.InRange:
		return i18n.T("Not in range")
	case n.Saved:
		return i18n.Tf("Saved · %s", sec)
	}
	return sec
}

// WifiSecurity names a network's security as people know it.
func WifiSecurity(security string) string {
	switch security {
	case "":
		return i18n.T("Open")
	case "wep":
		return "WEP"
	case "wpa":
		return "WPA2"
	case "sae":
		return "WPA3"
	case "owe":
		return i18n.T("Enhanced open")
	case "enterprise":
		return i18n.T("Enterprise")
	}
	return security
}

// WifiBand is a frequency's band: "2.4 GHz", "5 GHz" or "6 GHz".
func WifiBand(mhz int) string {
	switch {
	case mhz >= 5925:
		return "6 GHz"
	case mhz >= 5000:
		return "5 GHz"
	case mhz > 0:
		return "2.4 GHz"
	}
	return "—"
}

// WifiNetworkIcon is a network's signal icon.
func WifiNetworkIcon(n system.WifiNetwork) string {
	if !n.InRange {
		return "network-wireless-offline-symbolic"
	}
	return WifiIcon(n.Strength)
}

// WifiError is what a failed connection says.
func WifiError(err error) string {
	var we *system.WifiError
	switch {
	case errors.Is(err, system.ErrEnterprise):
		return i18n.T("This network signs in with a user name or certificate; inua does not set that up yet. Use nmtui or nm-connection-editor.")
	case errors.As(err, &we) && we.WrongPassword():
		return i18n.T("The password was not accepted.")
	case errors.As(err, &we) && we.Reason == 10:
		return i18n.T("The network did not answer in time.")
	case errors.As(err, &we):
		return i18n.Tf("Not connected (NetworkManager's reason %d).", we.Reason)
	}
	return i18n.Tf("Not connected: %s", err.Error())
}
