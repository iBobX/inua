package ui

import (
	"fmt"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

// The Wi-Fi details dialog: the connected network's addresses (IP, router,
// DNS, hardware) and how it is reached, each value selectable. The page
// shows the network; its addresses are here, a click away (Details…). It is
// a card over the Settings window, as the Benchmark dialog is, and follows
// the page's reads while open; it closes when the network is no longer the
// one connected.

// wifiDetails is the open dialog.
type wifiDetails struct {
	scrim *gtk.Box
	ssid  string
	title *gtk.Label
	rows  *gtk.Box
	shown string // what the rows show, to lay them out only when it changes
}

// openWifiDetails opens the details of the network connected as ssid.
func (s *shellUI) openWifiDetails(ssid string) {
	st := &s.settings
	if st.win == nil {
		return
	}
	s.closeWifiDetails()
	d := &wifiDetails{ssid: ssid}
	st.wifi.details = d
	s.settingsOverlay()
	d.scrim = gtk.NewBox(gtk.OrientationVerticalValue, 0)
	d.scrim.AddCssClass("settings-scrim")
	box := gtk.NewBox(gtk.OrientationVerticalValue, 12)
	box.AddCssClass("settings-dialog")
	box.AddCssClass("wifi-details")
	box.SetHalign(gtk.AlignCenterValue)
	box.SetValign(gtk.AlignCenterValue)
	box.SetHexpand(true)
	box.SetVexpand(true)
	box.SetSizeRequest(440, -1)
	d.title = gtk.NewLabel(ssid)
	d.title.AddCssClass("title")
	d.title.SetXalign(0)
	d.title.SetEllipsize(3) // end
	gtkx.Append(box, &d.title.Widget)
	d.rows = gtk.NewBox(gtk.OrientationVerticalValue, 0)
	d.rows.AddCssClass("section-body") // a page section's rows and hairlines
	gtkx.Append(box, &d.rows.Widget)
	buttons := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	buttons.SetHalign(gtk.AlignEndValue)
	buttons.SetMarginTop(6)
	closeButton := gtk.NewButtonWithLabel(i18n.T("Close"))
	closeButton.AddCssClass("outlined")
	closeButton.ConnectClicked(&s.onWifiDetailsClose)
	gtkx.Append(buttons, &closeButton.Widget)
	gtkx.Append(box, &buttons.Widget)
	gtkx.Append(d.scrim, &box.Widget)
	st.overlay.AddOverlay(&d.scrim.Widget)
	d.scrim.Unref() // the overlay's
	current, _, _ := shell.WifiLists(s.wifi.list)
	s.fillWifiDetails(current)
	closeButton.GrabFocus()
}

// fillWifiDetails lays the open dialog's rows out from the network
// connected now, and closes it when that is no longer its network.
func (s *shellUI) fillWifiDetails(current *system.WifiNetwork) {
	d := s.settings.wifi.details
	if d == nil {
		return
	}
	if current == nil || current.SSID != d.ssid || !current.Connected {
		s.closeWifiDetails()
		return
	}
	rows := wifiDetailRows(*current, s.wifi.list)
	key := fmt.Sprint(rows)
	if key == d.shown {
		return
	}
	d.shown = key
	gtkx.Clear(d.rows)
	for i, r := range rows {
		if i > 0 {
			gtkx.Append(d.rows, &gtk.NewSeparator(gtk.OrientationHorizontalValue).Widget)
		}
		row := detailRow(r[0], r[1])
		row.AddCssClass("compact") // a dialog's rows, closer than the page's
		gtkx.Append(d.rows, &row.Widget)
	}
}

// wifiDetailRows are the dialog's rows, a label and a value each: how the
// network is reached, then its addresses; one the system does not know is
// left out.
func wifiDetailRows(n system.WifiNetwork, l system.WifiList) [][2]string {
	var rows [][2]string
	add := func(label, value string) {
		if value != "" {
			rows = append(rows, [2]string{label, value})
		}
	}
	add(i18n.T("Security"), shell.WifiSecurity(n.Security))
	if n.InRange {
		add(i18n.T("Signal"), fmt.Sprintf("%d%%", n.Strength))
	}
	if n.Frequency > 0 {
		add(i18n.T("Band"), shell.WifiBand(n.Frequency))
	}
	add(i18n.T("IP address"), l.Address)
	add(i18n.T("Router"), l.Gateway)
	add(i18n.T("DNS"), strings.Join(l.DNS, "\n"))
	add(i18n.T("Hardware address"), l.Hardware)
	return rows
}

// closeWifiDetails closes the details dialog; false when none is open.
func (s *shellUI) closeWifiDetails() bool {
	st := &s.settings
	d := st.wifi.details
	if d == nil {
		return false
	}
	st.wifi.details = nil
	if st.overlay != nil {
		st.overlay.RemoveOverlay(&d.scrim.Widget)
	}
	return true
}
