package ui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

// wifiState is the networks NetworkManager knows, shared by the Wi-Fi
// page and the control center's list. Unlike the state the bar shows,
// the list is read only while one of them shows it: every few seconds,
// with a scan now and then.
type wifiState struct {
	list    system.WifiList
	read    bool // the list was read at least once
	reading bool
	polling bool
	scanned time.Time

	busy map[string]string // what is being done with a network, by SSID (shell.WifiConnect…)
	errs map[string]string // why the last action on a network failed, by SSID
}

// wifiPollMS and wifiScanEvery pace the reads while a list is shown.
const (
	wifiPollMS    = 3000
	wifiScanEvery = 20 * time.Second
)

// wifiSettings is the Wi-Fi page: the switch, the network in use (its
// addresses, disconnect, forget), the saved networks and those in range.
// A network that needs a password asks for it under its row.
type wifiSettings struct {
	power                                       gtk.Switch
	state, nearbyMeta                           gtk.Label
	current, known, nearby                      gtk.Box
	currentSection, knownSection, nearbySection gtk.Box

	shown    string                  // what the lists show, to lay them out only when it changes
	controls map[uintptr]wifiControl // the rows' buttons
	forget   string                  // the network whose Forget asks again
	ask      string                  // the network whose password is asked for
	password *gtk.PasswordEntry      // its entry; owned by its row
}

// wifiControl is what a button on the page does.
type wifiControl struct{ ssid, action string }

func (s *shellUI) getWifiSettings(ui *gtkx.Builder) {
	w := &s.settings.wifi
	ui.Get("wifi_power", &w.power)
	ui.Get("wifi_state", &w.state)
	ui.Get("wifi_current", &w.current)
	ui.Get("wifi_known", &w.known)
	ui.Get("wifi_nearby", &w.nearby)
	ui.Get("wifi_current_section", &w.currentSection)
	ui.Get("wifi_known_section", &w.knownSection)
	ui.Get("wifi_nearby_section", &w.nearbySection)
	ui.Get("wifi_nearby_meta", &w.nearbyMeta)
	w.shown, w.forget, w.ask, w.password = "", "", "", nil
	w.controls = map[uintptr]wifiControl{}
	w.power.ConnectStateSet(&s.onWifiSwitch)
}

func (s *shellUI) bindWifiHandlers() {
	s.wifi.busy, s.wifi.errs = map[string]string{}, map[string]string{}
	s.onWifiSwitch = func(_ gtk.Switch, on bool) bool {
		if !s.settings.filling {
			go func() { logErr("wifi", s.svc.net.SetWifi(on)) }()
		}
		return false
	}
	s.onWifiButton = func(btn gtk.Button) {
		if c, ok := s.settings.wifi.controls[btn.GoPointer()]; ok {
			s.wifiAction(c)
		}
	}
	s.onWifiEntry = func(gtk.PasswordEntry) {
		s.wifiAction(wifiControl{ssid: s.settings.wifi.ask, action: "join"})
	}
	s.onWifiPoll = func(uintptr) bool {
		if !s.wantWifi() {
			s.wifi.polling = false
			return false
		}
		s.readWifiList()
		return true
	}
}

// wantWifi reports whether a list of networks is shown: the Wi-Fi page,
// or the control center's.
func (s *shellUI) wantWifi() bool {
	st := &s.settings
	page := st.win != nil && st.pages.GetVisibleChildName() == "wifi"
	return page || (s.control.open && !s.control.closing && s.control.detail == "wifi")
}

// pollWifi reads the networks now and every few seconds while they are
// shown.
func (s *shellUI) pollWifi() {
	if !s.wantWifi() {
		return
	}
	s.readWifiList()
	if !s.wifi.polling {
		s.wifi.polling = true
		glib.TimeoutAdd(wifiPollMS, &s.onWifiPoll, 0)
	}
}

// readWifiList reads the networks off the main thread, scanning when the
// last scan is old, and shows them.
func (s *shellUI) readWifiList() {
	w := &s.wifi
	if w.reading || s.svc.net == nil {
		return
	}
	w.reading = true
	scan := time.Since(w.scanned) > wifiScanEvery && s.sys.Net.WifiEnabled
	if scan {
		w.scanned = time.Now()
	}
	net := s.svc.net
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if scan {
			logErr("wifi: scan", net.ScanWifi(ctx))
		}
		l, err := net.ReadWifi(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("wifi: %v", err)
		}
		gtkx.Post(func() {
			w.reading = false
			if err == nil {
				w.list, w.read = l, true
			}
			s.showWifi()
			s.showControlDetail()
		})
	}()
}

// wifiNetwork is the network named ssid in the list last read.
func (s *shellUI) wifiNetwork(ssid string) (system.WifiNetwork, bool) {
	for _, n := range s.wifi.list.Networks {
		if n.SSID == ssid {
			return n, true
		}
	}
	return system.WifiNetwork{}, false
}

// wifiAction does what a button says.
func (s *shellUI) wifiAction(c wifiControl) {
	w := &s.settings.wifi
	if c.action != shell.WifiForget {
		w.forget = ""
	}
	n, ok := s.wifiNetwork(c.ssid)
	switch c.action {
	case "cancel":
		w.ask = ""
		s.showWifi()
		return
	case shell.WifiConnect:
		if ok && n.Secure() && !n.Saved && n.Security != "enterprise" {
			w.ask = c.ssid // the password first
			delete(s.wifi.errs, c.ssid)
			s.showWifi()
			return
		}
	case "join":
		if w.password == nil || w.password.GetText() == "" {
			return
		}
	case shell.WifiForget:
		if w.forget != c.ssid {
			w.forget = c.ssid // asks again first
			s.showWifi()
			return
		}
		w.forget = ""
	}
	if !ok {
		return
	}
	password := ""
	if c.action == "join" {
		password = w.password.GetText()
		c.action = shell.WifiConnect
	}
	s.startWifiAction(n, c.action, password)
}

// startWifiAction connects, disconnects or forgets a network off the main
// thread; it is the control center's too. What failed shows under the
// network; a password turned down asks for another.
func (s *shellUI) startWifiAction(n system.WifiNetwork, action, password string) {
	ws := &s.wifi
	if ws.busy[n.SSID] != "" {
		return
	}
	ws.busy[n.SSID] = action
	delete(ws.errs, n.SSID)
	s.showWifi()
	s.showControlDetail()
	net := s.svc.net
	go func() {
		ctx := context.Background()
		var err error
		switch action {
		case shell.WifiConnect:
			err = net.ConnectWifi(ctx, n, password)
		case shell.WifiDisconnect:
			err = net.DisconnectWifi(ctx)
		case shell.WifiForget:
			err = net.ForgetWifi(ctx, n.SSID)
		}
		if err != nil {
			log.Printf("wifi: %s %q: %v", action, n.SSID, err)
		}
		gtkx.Post(func() {
			delete(ws.busy, n.SSID)
			w := &s.settings.wifi
			var we *system.WifiError
			switch {
			case err == nil:
				if w.ask == n.SSID {
					w.ask = ""
				}
			case action == shell.WifiConnect:
				ws.errs[n.SSID] = shell.WifiError(err)
				if errors.As(err, &we) && we.WrongPassword() && n.Security != "enterprise" {
					w.ask = n.SSID // another try
				}
			case action == shell.WifiDisconnect:
				ws.errs[n.SSID] = i18n.Tf("Not disconnected: %s", err.Error())
			default:
				ws.errs[n.SSID] = i18n.Tf("Not forgotten: %s", err.Error())
			}
			s.readWifiList()
			s.showWifi()
			s.showControlDetail()
		})
	}()
}

// showWifi sets the page from NetworkManager's state and the list last
// read, laying the lists out again only when what they show changed.
func (s *shellUI) showWifi() {
	st := &s.settings
	w := &st.wifi
	if st.win == nil {
		return
	}
	n := s.sys.Net
	st.filling = true
	defer func() { st.filling = false }()
	w.power.SetActive(n.WifiEnabled)
	w.power.SetSensitive(n.Available && n.WifiDevice)
	switch {
	case !n.Available:
		w.state.SetText(i18n.T("NetworkManager is not running"))
	case !n.WifiDevice:
		w.state.SetText(i18n.T("No Wi-Fi device"))
	case !n.WifiEnabled:
		w.state.SetText(i18n.T("Off"))
	case n.Kind == "wifi":
		w.state.SetText(i18n.Tf("Connected to “%s”", n.Name))
	case n.Kind != "":
		w.state.SetText(i18n.Tf("On; the computer uses “%s”", n.Name))
	default:
		w.state.SetText(i18n.T("On, not connected"))
	}

	on := n.Available && n.WifiDevice && n.WifiEnabled
	current, known, nearby := shell.WifiLists(s.wifi.list)
	if !on {
		current, nearby = nil, nil
	}
	w.currentSection.SetVisible(current != nil)
	w.knownSection.SetVisible(n.Available && n.WifiDevice && len(known) > 0)
	w.nearbySection.SetVisible(on)
	if s.wifi.read {
		w.nearbyMeta.SetText(i18n.T("Networks in range"))
	} else {
		w.nearbyMeta.SetText(i18n.T("Searching…"))
	}

	l := s.wifi.list
	key := fmt.Sprint(current, known, nearby, l.Address, l.Gateway, l.Hardware, s.wifi.busy, s.wifi.errs, w.forget, w.ask, on)
	if key == w.shown {
		return
	}
	w.shown = key
	// The password being typed, and whether it was, carry over to the
	// new entry; a new request's entry takes the focus.
	typed, focus := "", w.ask != ""
	if w.password != nil {
		typed, focus = w.password.GetText(), w.password.HasFocus()
	}
	w.password = nil
	w.controls = map[uintptr]wifiControl{}

	gtkx.Clear(&w.current)
	if current != nil {
		gtkx.Append(&w.current, &s.wifiRow(*current, typed).Widget)
		for _, d := range [][2]string{
			{i18n.T("IP address"), l.Address},
			{i18n.T("Router"), l.Gateway},
			{i18n.T("Hardware address"), l.Hardware},
		} {
			if d[1] == "" {
				continue
			}
			gtkx.Append(&w.current, &gtk.NewSeparator(gtk.OrientationHorizontalValue).Widget)
			gtkx.Append(&w.current, &detailRow(d[0], d[1]).Widget)
		}
	}
	s.layOutWifi(&w.known, known, "", typed)
	empty := i18n.T("Nothing found yet.")
	if s.wifi.read {
		empty = i18n.T("No other networks in range.")
	}
	s.layOutWifi(&w.nearby, nearby, empty, typed)
	if w.password != nil && focus {
		w.password.GrabFocus()
	}
}

// detailRow is a row of a label and a value to select and copy.
func detailRow(label, val string) *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 16)
	row.AddCssClass("row")
	l := gtk.NewLabel(label)
	l.AddCssClass("title")
	l.SetXalign(0)
	l.SetHexpand(true)
	gtkx.Append(row, &l.Widget)
	v := gtk.NewLabel(val)
	v.AddCssClass("readout")
	v.SetSelectable(true)
	gtkx.Append(row, &v.Widget)
	return row
}

// layOutWifi fills a list with its networks' rows.
func (s *shellUI) layOutWifi(box *gtk.Box, nets []system.WifiNetwork, empty, typed string) {
	gtkx.Clear(box)
	if len(nets) == 0 && empty != "" {
		row, _ := displayRow(empty, "")
		gtkx.Append(box, &row.Widget)
		return
	}
	for i, n := range nets {
		if i > 0 {
			gtkx.Append(box, &gtk.NewSeparator(gtk.OrientationHorizontalValue).Widget)
		}
		gtkx.Append(box, &s.wifiRow(n, typed).Widget)
	}
}

// wifiRow is a network's row: its signal, name and state, what can be
// done with it, and under it the password asked for and what failed.
func (s *shellUI) wifiRow(n system.WifiNetwork, typed string) *gtk.Box {
	w := &s.settings.wifi
	busy := s.wifi.busy[n.SSID]
	box := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	row.AddCssClass("row")
	icon := gtk.NewImageFromIconName(shell.WifiNetworkIcon(n))
	icon.AddCssClass("bt-icon")
	icon.SetValign(gtk.AlignCenterValue)
	gtkx.Append(row, &icon.Widget)
	text, _ := displayRow(n.SSID, shell.WifiStatus(n, busy))
	text.RemoveCssClass("row") // inside this row
	text.SetHexpand(true)
	gtkx.Append(row, &text.Widget)
	if n.Secure() {
		lock := gtk.NewImageFromIconName("system-lock-screen-symbolic")
		lock.AddCssClass("wifi-lock")
		lock.SetValign(gtk.AlignCenterValue)
		lock.SetTooltipText(shell.WifiSecurity(n.Security))
		gtkx.Append(row, &lock.Widget)
	}

	add := func(parent *gtk.Box, label, action string, classes ...string) {
		btn := gtk.NewButtonWithLabel(label)
		btn.AddCssClass("outlined")
		for _, c := range classes {
			btn.AddCssClass(c)
		}
		btn.SetValign(gtk.AlignCenterValue)
		btn.SetSensitive(busy == "")
		w.controls[btn.GoPointer()] = wifiControl{ssid: n.SSID, action: action}
		btn.ConnectClicked(&s.onWifiButton)
		gtkx.Append(parent, &btn.Widget)
	}
	switch {
	case n.Connected || n.Connecting:
		add(row, i18n.T("Disconnect"), shell.WifiDisconnect)
	case n.InRange && w.ask != n.SSID:
		add(row, i18n.T("Connect"), shell.WifiConnect)
	}
	if n.Saved {
		if w.forget == n.SSID {
			add(row, i18n.T("Forget it?"), shell.WifiForget, "danger")
		} else {
			add(row, i18n.T("Forget"), shell.WifiForget)
		}
	}
	gtkx.Append(box, &row.Widget)

	if w.ask == n.SSID {
		ask := gtk.NewBox(gtk.OrientationVerticalValue, 10)
		ask.AddCssClass("bt-prompt")
		title, _ := displayRow(i18n.Tf("The password for “%s”", n.SSID), "")
		title.RemoveCssClass("row")
		gtkx.Append(ask, &title.Widget)
		line := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
		e := gtk.NewPasswordEntry()
		e.SetShowPeekIcon(true)
		e.SetText(typed)
		e.SetHexpand(true)
		e.ConnectActivate(&s.onWifiEntry)
		w.password = e
		gtkx.Append(line, &e.Widget)
		add(line, i18n.T("Connect"), "join")
		add(line, i18n.T("Cancel"), "cancel")
		gtkx.Append(ask, &line.Widget)
		gtkx.Append(box, &ask.Widget)
	}
	if msg := s.wifi.errs[n.SSID]; msg != "" {
		l := gtk.NewLabel(msg)
		l.AddCssClass("row-error")
		l.SetXalign(0)
		l.SetWrap(true)
		l.SetSelectable(true)
		gtkx.Append(box, &l.Widget)
	}
	return box
}
