package shell

import (
	"slices"

	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/system"
)

// BluetoothLists splits the devices for the settings page: the paired
// ones, and those found nearby with a name (one without a name cannot be
// told from another).
func BluetoothLists(st system.BluetoothState) (mine, nearby []system.BluetoothDevice) {
	for _, d := range st.Devices {
		switch {
		case d.Paired:
			mine = append(mine, d)
		case d.Name != "":
			nearby = append(nearby, d)
		}
	}
	return mine, nearby
}

// What the settings page is doing with a device.
const (
	BTConnect    = "connect"
	BTDisconnect = "disconnect"
	BTPair       = "pair"
	BTForget     = "forget"
)

// BluetoothStatus is the line under a device's name: what is being done
// with it (busy), or whether it is connected, with its battery.
func BluetoothStatus(d system.BluetoothDevice, busy string) string {
	switch busy {
	case BTConnect:
		return i18n.T("Connecting…")
	case BTDisconnect:
		return i18n.T("Disconnecting…")
	case BTPair:
		return i18n.T("Pairing…")
	case BTForget:
		return i18n.T("Forgetting…")
	}
	switch {
	case !d.Paired:
		return ""
	case d.Connected && d.Battery >= 0:
		return i18n.Tf("Connected, battery %d%%", d.Battery)
	case d.Connected:
		return i18n.T("Connected")
	}
	return i18n.T("Not connected")
}

// bluetoothIcons are BlueZ's device icons the shell's icon set has
// (scripts/phosphor-icons.sh).
var bluetoothIcons = []string{
	"audio-card", "audio-headphones", "audio-headset", "camera-photo", "camera-video", "computer",
	"input-gaming", "input-keyboard", "input-mouse", "input-tablet", "phone", "printer", "video-display",
}

// BluetoothIcon is the icon for a device of BlueZ's icon name; another
// gets Bluetooth's.
func BluetoothIcon(name string) string {
	if slices.Contains(bluetoothIcons, name) {
		return name + "-symbolic"
	}
	return "bluetooth-active-symbolic"
}

// BluetoothPromptText is what a pairing prompt says: its title and the
// line under it.
func BluetoothPromptText(p system.BluetoothPrompt) (title, detail string) {
	name := p.Device.Name
	if name == "" {
		name = p.Device.Address
	}
	switch p.Kind {
	case system.PromptShow:
		return i18n.Tf("Type %s on %s", p.Code, name), i18n.T("Then press Enter on it.")
	case system.PromptConfirm:
		return i18n.Tf("Does %s show %s?", name, p.Code), i18n.T("Pair only if the numbers match.")
	case system.PromptPIN:
		return i18n.Tf("PIN for %s", name), i18n.T("Often 0000 or 1234; see its manual.")
	case system.PromptPasskey:
		return i18n.Tf("The number %s shows", name), i18n.T("Type the six digits it shows.")
	}
	return i18n.Tf("Let %s connect?", name), i18n.T("It asks to pair or to use this computer without a code.")
}
