package ui

import (
	"context"
	"fmt"
	"log"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

// bluetoothSettings is the Bluetooth page: the adapter's switch, the
// paired devices (connect, disconnect, forget) and, while the page is
// shown, the devices found nearby (pair). A pairing request BlueZ makes
// through the shell's agent is answered under its device; one arriving
// with the window closed opens it here.
type bluetoothSettings struct {
	power                      gtk.Switch
	state, nearbyMeta          gtk.Label
	mine, nearby               gtk.Box
	mineSection, nearbySection gtk.Box

	// What the lists show, to lay them out only when it changes.
	shown     string
	controls  map[*gtk.Box]map[uintptr]btControl // each list's buttons
	busy      map[string]string                  // what is being done with a device, by path (shell.BTConnect…)
	forget    string                             // the device whose Forget asks again
	searching bool                               // the shell's search is on

	prompt *btPrompt  // the pairing request shown, nil for none
	code   *gtk.Entry // its entry, for a PIN or passkey; owned by its row
}

// btControl is what a button on the page does.
type btControl struct{ path, action string }

// btPrompt is a pairing request waiting for the user.
type btPrompt struct {
	p     system.BluetoothPrompt
	reply chan system.BluetoothAnswer // takes one answer
}

func (s *shellUI) getBluetoothSettings(ui *gtkx.Builder) {
	b := &s.settings.bt
	ui.Get("bt_power", &b.power)
	ui.Get("bt_state", &b.state)
	ui.Get("bt_mine", &b.mine)
	ui.Get("bt_nearby", &b.nearby)
	ui.Get("bt_mine_section", &b.mineSection)
	ui.Get("bt_nearby_section", &b.nearbySection)
	ui.Get("bt_nearby_meta", &b.nearbyMeta)
	b.shown, b.forget, b.code = "", "", nil
	b.controls = map[*gtk.Box]map[uintptr]btControl{}
	if b.busy == nil {
		b.busy = map[string]string{}
	}
	if s.onBluetoothButton == nil {
		s.onBluetoothSwitch = func(_ gtk.Switch, on bool) bool {
			if !s.settings.filling {
				go func() { logErr("bluetooth", s.svc.bt.SetPowered(on)) }()
			}
			return false
		}
		s.onBluetoothButton = func(btn gtk.Button) {
			for _, controls := range s.settings.bt.controls {
				if c, ok := controls[btn.GoPointer()]; ok {
					s.bluetoothAction(c)
					return
				}
			}
		}
		s.onBluetoothEntry = func(gtk.Entry) { s.bluetoothAction(btControl{action: "answer"}) }
	}
	b.power.ConnectStateSet(&s.onBluetoothSwitch)
}

// askBluetooth is the agent's Asker: it shows the request on the page,
// opening the window if needed, and waits for the answer. It runs on the
// agent's goroutine.
func (s *shellUI) askBluetooth(ctx context.Context, p system.BluetoothPrompt) system.BluetoothAnswer {
	pr := &btPrompt{p: p, reply: make(chan system.BluetoothAnswer, 1)}
	gtkx.Post(func() { s.showBluetoothPrompt(pr) })
	defer gtkx.Post(func() { s.endBluetoothPrompt(pr) })
	select {
	case a := <-pr.reply:
		return a
	case <-ctx.Done():
		return system.BluetoothAnswer{}
	}
}

func (s *shellUI) showBluetoothPrompt(pr *btPrompt) {
	b := &s.settings.bt
	b.prompt = pr
	if s.settings.win == nil {
		s.openSettings()
	}
	s.showSettingsPage("bluetooth")
	s.settings.win.Present()
	s.showBluetooth()
}

func (s *shellUI) endBluetoothPrompt(pr *btPrompt) {
	b := &s.settings.bt
	if b.prompt == pr {
		b.prompt, b.code = nil, nil
		s.showBluetooth()
	}
}

// bluetoothAction does what a button says.
func (s *shellUI) bluetoothAction(c btControl) {
	b := &s.settings.bt
	bt := s.svc.bt
	if c.action != shell.BTForget {
		b.forget = ""
	}
	switch c.action {
	case "answer", "reject":
		if pr := b.prompt; pr != nil {
			a := system.BluetoothAnswer{OK: c.action == "answer"}
			if b.code != nil {
				a.Text = b.code.GetText()
			}
			select {
			case pr.reply <- a:
			default: // answered already
			}
		}
		return
	case "cancel":
		if pr := b.prompt; pr != nil {
			select {
			case pr.reply <- system.BluetoothAnswer{}:
			default:
			}
		}
		go func() { logErr("bluetooth: cancel pairing", bt.CancelPair(c.path)) }()
		return
	case shell.BTForget:
		if b.forget != c.path {
			b.forget = c.path // asks again first
			s.showBluetooth()
			return
		}
		b.forget = ""
	}
	if b.busy[c.path] != "" {
		return
	}
	b.busy[c.path] = c.action
	s.showBluetooth()
	go func() {
		var err error
		var msg string
		switch c.action {
		case shell.BTConnect:
			if err = bt.Connect(c.path); err != nil {
				msg = i18n.Tf("Not connected: %s", err.Error())
			}
		case shell.BTDisconnect:
			if err = bt.Disconnect(c.path); err != nil {
				msg = i18n.Tf("Not disconnected: %s", err.Error())
			}
		case shell.BTPair:
			if err = bt.Pair(c.path); err != nil {
				msg = i18n.Tf("Not paired: %s", err.Error())
			}
		case shell.BTForget:
			if err = bt.Forget(c.path); err != nil {
				msg = i18n.Tf("Not forgotten: %s", err.Error())
			}
		}
		if err != nil {
			log.Printf("bluetooth: %s %s: %v", c.action, c.path, err)
		}
		gtkx.Post(func() {
			delete(b.busy, c.path)
			s.setStatus(msg, err != nil)
			s.showBluetooth()
		})
	}()
}

// showBluetooth sets the page from BlueZ's state, laying the lists out
// again only when what they show changed, and searches while the page is
// shown.
func (s *shellUI) showBluetooth() {
	st := &s.settings
	b := &st.bt
	if st.win == nil {
		return
	}
	bs := s.sys.BT
	st.filling = true
	defer func() { st.filling = false }()
	b.power.SetActive(bs.Powered)
	b.power.SetSensitive(bs.Available)
	switch {
	case !bs.Available:
		b.state.SetText(i18n.T("No Bluetooth adapter, or BlueZ is not running"))
	case !bs.Powered:
		b.state.SetText(i18n.T("Off"))
	default:
		b.state.SetText(i18n.Tf("Shown to devices nearby as “%s” while this page is open", bs.Name))
	}
	s.syncBluetoothSearch()

	mine, nearby := shell.BluetoothLists(bs)
	b.mineSection.SetVisible(bs.Available)
	b.nearbySection.SetVisible(bs.Available && bs.Powered)
	if bs.Discovering {
		b.nearbyMeta.SetText(i18n.T("Searching…"))
	} else {
		b.nearbyMeta.SetText(i18n.T("Devices that can pair"))
	}
	var prompt any
	if b.prompt != nil {
		prompt = *b.prompt // by value: a new code is a new prompt
	}
	// A request for a device in neither list (one pairing from its side
	// before it was found) goes above the paired ones.
	var stray *btPrompt
	if pr := b.prompt; pr != nil && !hasDevice(mine, pr.p.Device.Path) && !hasDevice(nearby, pr.p.Device.Path) {
		stray = pr
	}
	key := fmt.Sprint(mine, nearby, b.busy, b.forget, prompt)
	if key == b.shown {
		return
	}
	b.shown = key
	// The code being typed, and whether it was, carry over to the new
	// entry; a new request's entry takes the focus.
	typed, focus := "", b.prompt != nil
	if b.code != nil {
		typed, focus = b.code.GetText(), b.code.HasFocus()
	}
	b.code = nil
	s.layOutBluetooth(&b.mine, mine, i18n.T("No devices paired yet"), stray, typed)
	empty := i18n.T("Nothing found yet. Make the device ready to pair: often by holding its Bluetooth button.")
	s.layOutBluetooth(&b.nearby, nearby, empty, nil, typed)
	if b.code != nil && focus {
		b.code.GrabFocus()
	}
}

func hasDevice(devices []system.BluetoothDevice, path string) bool {
	for _, d := range devices {
		if d.Path == path {
			return true
		}
	}
	return false
}

// syncBluetoothSearch searches for devices while the page is shown and
// Bluetooth is on, and stops otherwise.
func (s *shellUI) syncBluetoothSearch() {
	st := &s.settings
	b := &st.bt
	want := st.win != nil && st.pages.GetVisibleChildName() == "bluetooth" && s.sys.BT.Powered
	if want == b.searching {
		return
	}
	b.searching = want
	bt := s.svc.bt
	go func() { logErr("bluetooth: search", bt.SetDiscovering(want)) }()
}

// layOutBluetooth fills a list with its devices' rows, each followed by
// the pairing request for it, if any; first, stray's.
func (s *shellUI) layOutBluetooth(box *gtk.Box, devices []system.BluetoothDevice, empty string, stray *btPrompt, typed string) {
	b := &s.settings.bt
	b.controls[box] = map[uintptr]btControl{} // the old rows' buttons go with them
	gtkx.Clear(box)
	if stray != nil {
		gtkx.Append(box, &s.bluetoothPrompt(box, stray, typed).Widget)
	}
	if len(devices) == 0 {
		row, _ := displayRow(empty, "")
		gtkx.Append(box, &row.Widget)
		return
	}
	for i, d := range devices {
		if i > 0 {
			gtkx.Append(box, &gtk.NewSeparator(gtk.OrientationHorizontalValue).Widget)
		}
		gtkx.Append(box, &s.bluetoothRow(box, d).Widget)
		if pr := b.prompt; pr != nil && pr.p.Device.Path == d.Path {
			gtkx.Append(box, &s.bluetoothPrompt(box, pr, typed).Widget)
		}
	}
}

// bluetoothRow is a device's row: its icon, name and state, and what can
// be done with it.
func (s *shellUI) bluetoothRow(list *gtk.Box, d system.BluetoothDevice) *gtk.Box {
	b := &s.settings.bt
	busy := b.busy[d.Path]
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	row.AddCssClass("row")
	icon := gtk.NewImageFromIconName(shell.BluetoothIcon(d.Icon))
	icon.AddCssClass("bt-icon")
	icon.SetValign(gtk.AlignCenterValue)
	gtkx.Append(row, &icon.Widget)
	text, _ := displayRow(d.Name, shell.BluetoothStatus(d, busy))
	text.RemoveCssClass("row") // inside this row
	text.SetHexpand(true)
	gtkx.Append(row, &text.Widget)

	add := func(label, action string, classes ...string) {
		btn := gtk.NewButtonWithLabel(label)
		btn.AddCssClass("outlined")
		for _, c := range classes {
			btn.AddCssClass(c)
		}
		btn.SetValign(gtk.AlignCenterValue)
		btn.SetSensitive(busy == "")
		b.controls[list][btn.GoPointer()] = btControl{path: d.Path, action: action}
		btn.ConnectClicked(&s.onBluetoothButton)
		gtkx.Append(row, &btn.Widget)
	}
	switch {
	case !d.Paired:
		add(i18n.T("Pair"), shell.BTPair)
	case d.Connected:
		add(i18n.T("Disconnect"), shell.BTDisconnect)
	default:
		add(i18n.T("Connect"), shell.BTConnect)
	}
	if d.Paired {
		if b.forget == d.Path {
			add(i18n.T("Forget it?"), shell.BTForget, "danger")
		} else {
			add(i18n.T("Forget"), shell.BTForget)
		}
	}
	return row
}

// bluetoothPrompt is a pairing request's row: what it asks, and the
// code, entry or buttons that answer it.
func (s *shellUI) bluetoothPrompt(list *gtk.Box, pr *btPrompt, typed string) *gtk.Box {
	b := &s.settings.bt
	p := pr.p
	box := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	box.AddCssClass("bt-prompt")
	title, detail := shell.BluetoothPromptText(p)
	text, _ := displayRow(title, detail)
	text.RemoveCssClass("row")
	gtkx.Append(box, &text.Widget)

	if p.Kind == system.PromptShow || p.Kind == system.PromptConfirm {
		code := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
		code.AddCssClass("bt-code")
		for i, digit := range p.Code {
			key := gtk.NewLabel(string(digit))
			key.AddCssClass("keycap")
			if p.Kind == system.PromptShow && i < p.Typed {
				key.AddCssClass("typed")
			}
			gtkx.Append(code, &key.Widget)
		}
		gtkx.Append(box, &code.Widget)
	}

	buttons := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	if p.Kind == system.PromptPIN || p.Kind == system.PromptPasskey {
		e := gtk.NewEntry()
		e.SetText(typed)
		if p.Kind == system.PromptPasskey {
			e.SetInputPurpose(gtk.InputPurposeDigitsValue)
			e.SetMaxLength(6)
		} else {
			e.SetInputPurpose(gtk.InputPurposePinValue)
			e.SetMaxLength(16)
		}
		e.ConnectActivate(&s.onBluetoothEntry)
		b.code = e
		gtkx.Append(buttons, &e.Widget)
	}
	button := func(label, action string) {
		btn := gtk.NewButtonWithLabel(label)
		btn.AddCssClass("outlined")
		b.controls[list][btn.GoPointer()] = btControl{path: p.Device.Path, action: action}
		btn.ConnectClicked(&s.onBluetoothButton)
		gtkx.Append(buttons, &btn.Widget)
	}
	switch p.Kind {
	case system.PromptShow:
		button(i18n.T("Cancel"), "cancel")
	case system.PromptConfirm:
		button(i18n.T("Pair"), "answer")
		button(i18n.T("Cancel"), "reject")
	case system.PromptAuthorize:
		button(i18n.T("Allow"), "answer")
		button(i18n.T("Deny"), "reject")
	default:
		button(i18n.T("Pair"), "answer")
		button(i18n.T("Cancel"), "reject")
	}
	gtkx.Append(box, &buttons.Widget)
	return box
}
