package ui

import (
	"fmt"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

// controlDetailMax is how many networks the control center lists; the
// Wi-Fi page has the rest.
const controlDetailMax = 8

func (s *shellUI) bindControlDetailHandlers() {
	// The list acts on devices before the settings window was ever open.
	s.settings.bt.busy, s.settings.bt.errs = map[string]string{}, map[string]string{}
	s.onControlItem = func(b gtk.Button) {
		c := &s.control
		key, ok := c.items[b.GoPointer()]
		if !ok {
			return
		}
		switch c.detail {
		case "wifi":
			s.controlWifi(key)
		case "bt":
			s.controlBluetooth(key)
		}
	}
}

// toggleDetail opens a tile's list, or closes it when it is open.
func (s *shellUI) toggleDetail(name string) {
	c := &s.control
	if c.detail == name {
		c.detail = ""
	} else {
		c.detail = name
	}
	s.showControlDetail()
	s.pollWifi()
}

// showControlDetail lays out the list an arrow opened, again only when
// what it shows changed (a hover would flicker).
func (s *shellUI) showControlDetail() {
	c := &s.control
	if !c.open {
		return
	}
	toggle(&c.wifiMore.Widget, "open", c.detail == "wifi")
	toggle(&c.btMore.Widget, "open", c.detail == "bt")
	c.detailReveal.SetRevealChild(c.detail != "")
	var key string
	switch c.detail {
	case "wifi":
		key = fmt.Sprint("wifi", s.sys.Net, s.wifi.read, s.wifi.list.Networks, s.wifi.busy, s.wifi.errs)
	case "bt":
		key = fmt.Sprint("bt", s.sys.BT.Powered, s.sys.BT.Devices, s.settings.bt.busy, s.settings.bt.errs)
	}
	if key == c.detailShown || c.detail == "" {
		return
	}
	c.detailShown = key
	c.items = map[uintptr]string{}
	gtkx.Clear(&c.detailList)
	switch c.detail {
	case "wifi":
		s.layOutControlWifi()
	case "bt":
		s.layOutControlBluetooth()
	}
}

// detailHead is the list's kicker on a hairline, with a button to the
// settings page at the end.
func (s *shellUI) detailHead(title, page string) {
	c := &s.control
	head := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	head.AddCssClass("cc-detail-head")
	k := gtk.NewLabel(title)
	k.AddCssClass("kicker")
	gtkx.Append(head, &k.Widget)
	sep := gtk.NewSeparator(gtk.OrientationHorizontalValue)
	sep.SetHexpand(true)
	sep.SetValign(gtk.AlignCenterValue)
	gtkx.Append(head, &sep.Widget)
	more := gtk.NewButtonWithLabel(i18n.T("Settings…"))
	more.AddCssClass("link")
	more.SetName(page)
	more.ConnectClicked(&s.onControlButton)
	gtkx.Append(head, &more.Widget)
	gtkx.Append(&c.detailList, &head.Widget)
}

// detailNote says why the list is empty.
func (s *shellUI) detailNote(text string) {
	l := gtk.NewLabel(text)
	l.AddCssClass("cc-detail-note")
	l.SetXalign(0)
	l.SetWrap(true)
	gtkx.Append(&s.control.detailList, &l.Widget)
}

// detailItem is a row of the list: an icon, a name, a line under it and
// the status mark, the whole row a button; key is what it is about. A
// failure shows as the line, in the danger colour.
func (s *shellUI) detailItem(key, icon, name, sub string, on, failed, busy bool) {
	c := &s.control
	btn := gtk.NewButton()
	btn.AddCssClass("cc-item")
	box := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	img := gtk.NewImageFromIconName(icon)
	img.SetValign(gtk.AlignCenterValue)
	gtkx.Append(box, &img.Widget)
	text := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	text.SetHexpand(true)
	t := gtk.NewLabel(name)
	t.AddCssClass("title")
	t.SetXalign(0)
	t.SetEllipsize(3)
	gtkx.Append(text, &t.Widget)
	if sub != "" {
		l := gtk.NewLabel(sub)
		l.AddCssClass("sub")
		if failed {
			l.AddCssClass("error")
			l.SetWrap(true)
		} else {
			l.SetEllipsize(3)
		}
		l.SetXalign(0)
		gtkx.Append(text, &l.Widget)
	}
	gtkx.Append(box, &text.Widget)
	mark := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	mark.AddCssClass("mark")
	mark.SetValign(gtk.AlignCenterValue)
	gtkx.Append(box, &mark.Widget)
	btn.SetChild(&box.Widget)
	toggle(&btn.Widget, "on", on)
	btn.SetSensitive(!busy)
	c.items[btn.GoPointer()] = key
	btn.ConnectClicked(&s.onControlItem)
	gtkx.Append(&c.detailList, &btn.Widget)
}

// layOutControlWifi lists the networks in range: a click connects, a
// network that needs a password opens the Wi-Fi page to ask for it.
func (s *shellUI) layOutControlWifi() {
	s.detailHead(i18n.T("Networks"), "wifi-settings")
	n := s.sys.Net
	switch {
	case !n.Available || !n.WifiDevice:
		s.detailNote(shellWifiSub(n))
		return
	case !n.WifiEnabled:
		s.detailNote(i18n.T("Wi-Fi is off."))
		return
	}
	shown := 0
	for _, nw := range s.wifi.list.Networks {
		if !nw.InRange || shown == controlDetailMax {
			continue
		}
		shown++
		busy := s.wifi.busy[nw.SSID]
		sub, failed := shell.WifiStatus(nw, busy), false
		if msg := s.wifi.errs[nw.SSID]; msg != "" && busy == "" {
			sub, failed = msg, true
		}
		s.detailItem(nw.SSID, shell.WifiNetworkIcon(nw), nw.SSID, sub, nw.Connected, failed, busy != "")
	}
	if shown == 0 {
		if s.wifi.read {
			s.detailNote(i18n.T("No networks in range."))
		} else {
			s.detailNote(i18n.T("Searching…"))
		}
	}
}

// shellWifiSub is the Wi-Fi tile's line, for a list that cannot show.
func shellWifiSub(n system.NetworkState) string {
	sub, _ := shell.WifiTile(n)
	return sub
}

// controlWifi connects to a network from the list.
func (s *shellUI) controlWifi(ssid string) {
	n, ok := s.wifiNetwork(ssid)
	if !ok || n.Connected || n.Connecting {
		return
	}
	if n.Secure() && !n.Saved {
		// The password is typed on the Wi-Fi page.
		s.closeControl()
		s.openSettings()
		s.showSettingsPage("wifi")
		s.settings.wifi.ask = ssid
		s.showWifi()
		return
	}
	s.startWifiAction(n, shell.WifiConnect, "")
}

// layOutControlBluetooth lists the paired devices: a click connects or
// disconnects one.
func (s *shellUI) layOutControlBluetooth() {
	s.detailHead(i18n.T("Devices"), "bt-settings")
	bt := s.sys.BT
	switch {
	case !bt.Available:
		s.detailNote(i18n.T("No Bluetooth adapter, or BlueZ is not running"))
		return
	case !bt.Powered:
		s.detailNote(i18n.T("Bluetooth is off."))
		return
	}
	mine, _ := shell.BluetoothLists(bt)
	if len(mine) == 0 {
		s.detailNote(i18n.T("No devices paired yet"))
		return
	}
	b := &s.settings.bt
	for _, d := range mine {
		busy := b.busy[d.Path]
		sub, failed := shell.BluetoothStatus(d, busy), false
		if msg := b.errs[d.Path]; msg != "" && busy == "" {
			sub, failed = msg, true
		}
		s.detailItem(d.Path, shell.BluetoothIcon(d.Icon), d.Name, sub, d.Connected, failed, busy != "")
	}
}

// controlBluetooth connects a paired device, or disconnects it.
func (s *shellUI) controlBluetooth(path string) {
	for _, d := range s.sys.BT.Devices {
		if d.Path != path {
			continue
		}
		action := shell.BTConnect
		if d.Connected {
			action = shell.BTDisconnect
		}
		s.bluetoothAction(btControl{path: path, action: action})
		return
	}
}
