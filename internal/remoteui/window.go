package remoteui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/remotedesk"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gamestream"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// window is Remote Desktop's window: the index of This computer and the
// machines, and the chosen one's page.
type window struct {
	c   *appui.Ctx
	win *gtk.ApplicationWindow

	sideSharing, sideFind, sideAdd                          gtk.Button
	sideSharingSub, status                                  gtk.Label
	sideMachines                                            gtk.Box
	pages                                                   gtk.Stack
	sharingDesc, sharingNeedText, sharingNeedFix            gtk.Label
	sharingSwitch                                           gtk.Switch
	sharingVirtual                                          gtk.DropDown
	sharingNeed, sharingPendingSection, sharingPending      gtk.Box
	sharingClients                                          gtk.Box
	machineTitle, machineState, machineAddr                 gtk.Label
	machinePinText, machinePin                              gtk.Label
	machineForget, machineQuit, machinePair, machineConnect gtk.Button
	machinePinBox                                           gtk.Box
	streamSize, streamFPS, streamCodec                      gtk.DropDown
	streamBitrate                                           gtk.SpinButton
	streamHostAudio                                         gtk.Switch
	streamCodecNote                                         gtk.Label
	addAddr                                                 gtk.Entry
	addOK                                                   gtk.Button
	addError                                                gtk.Label

	settings *remotedesk.Settings
	sharing  *remotedesk.Sharing
	finder   *remotedesk.Finder
	id       *gamestream.Identity

	found    []remotedesk.Candidate
	selected string // "sharing", "add", or a machine's address
	info     map[string]machineInfo
	pairing  context.CancelFunc // while pairing with the machine selected
	streams  map[string]*stream
	closed   bool
	cancel   context.CancelFunc

	// Rows made in Go, by their button.
	machineRows map[uintptr]string            // → address
	pendingRows map[uintptr]pendingRow        // → device waiting, and its PIN entry
	clientRows  map[uintptr]remotedesk.Client // → paired device
	sideButtons map[string]*gtk.Button        // by address, to mark the selected
	loading     bool                          // filling the stream settings: not a change

	onClick    func(gtk.Button)
	onSwitch   func(gtk.Switch, bool) bool
	onSpin     func(gtk.SpinButton)
	onNotify   func(uintptr, uintptr, uintptr)
	onVirtual  func(uintptr, uintptr, uintptr)
	onClose    func(gtk.Window) bool
	onAddEnter func(gtk.Entry)
}

type pendingRow struct {
	p     remotedesk.Pairing
	entry *gtk.Entry
}

// machineInfo is what a machine said last, or that it did not answer.
type machineInfo struct {
	info  gamestream.ServerInfo
	err   error
	asked bool
}

func openWindow(c *appui.Ctx) *window {
	w := &window{
		c: c, win: &gtk.ApplicationWindow{}, settings: remotedesk.LoadSettings(), sharing: remotedesk.NewSharing(),
		info: map[string]machineInfo{}, streams: map[string]*stream{}, selected: "sharing",
		machineRows: map[uintptr]string{}, pendingRows: map[uintptr]pendingRow{}, clientRows: map[uintptr]remotedesk.Client{},
		sideButtons: map[string]*gtk.Button{},
	}
	b := c.Load("remote.ui")
	b.Get("remote", w.win)
	for name, ptr := range map[string]gobject.Ptr{
		"side_sharing": &w.sideSharing, "side_find": &w.sideFind, "side_add": &w.sideAdd,
		"side_sharing_sub": &w.sideSharingSub, "status": &w.status, "side_machines": &w.sideMachines, "pages": &w.pages,
		"sharing_desc": &w.sharingDesc, "sharing_need_text": &w.sharingNeedText, "sharing_need_fix": &w.sharingNeedFix,
		"sharing_switch": &w.sharingSwitch, "sharing_need": &w.sharingNeed, "sharing_pending_section": &w.sharingPendingSection,
		"sharing_pending": &w.sharingPending, "sharing_clients": &w.sharingClients, "sharing_virtual": &w.sharingVirtual,
		"machine_title": &w.machineTitle, "machine_state": &w.machineState, "machine_addr": &w.machineAddr,
		"machine_pin_text": &w.machinePinText, "machine_pin": &w.machinePin, "machine_forget": &w.machineForget,
		"machine_quit": &w.machineQuit, "machine_pair": &w.machinePair, "machine_connect": &w.machineConnect,
		"machine_pin_box": &w.machinePinBox, "stream_size": &w.streamSize, "stream_fps": &w.streamFPS,
		"stream_codec": &w.streamCodec, "stream_bitrate": &w.streamBitrate, "stream_host_audio": &w.streamHostAudio,
		"stream_codec_note": &w.streamCodecNote, "add_addr": &w.addAddr, "add_ok": &w.addOK, "add_error": &w.addError,
	} {
		b.Get(name, ptr)
	}
	b.Close()

	id, err := gamestream.LoadIdentity(remotedesk.IdentityDir())
	if err != nil {
		log.Printf("remote: identity: %v", err)
		w.fail(i18n.Tf("This computer has no identity to pair with: %v", err))
	}
	w.id = id
	if id != nil {
		w.finder = remotedesk.NewFinder(id)
	}

	w.onClick = func(b gtk.Button) { w.clicked(b) }
	for _, btn := range []*gtk.Button{&w.sideSharing, &w.sideFind, &w.sideAdd, &w.machineForget, &w.machineQuit,
		&w.machinePair, &w.machineConnect, &w.addOK} {
		btn.ConnectClicked(&w.onClick)
	}
	w.onSwitch = func(s gtk.Switch, on bool) bool { return w.switched(s, on) }
	w.sharingSwitch.ConnectStateSet(&w.onSwitch)
	w.streamHostAudio.ConnectStateSet(&w.onSwitch)
	w.onSpin = func(gtk.SpinButton) { w.streamChanged() }
	w.streamBitrate.ConnectValueChanged(&w.onSpin)
	w.onNotify = func(uintptr, uintptr, uintptr) { w.streamChanged() }
	for _, d := range []*gtk.DropDown{&w.streamSize, &w.streamFPS, &w.streamCodec} {
		gtkx.ConnectNotify(d, "selected", &w.onNotify)
	}
	w.onVirtual = func(uintptr, uintptr, uintptr) { w.virtualChanged() }
	gtkx.ConnectNotify(&w.sharingVirtual, "selected", &w.onVirtual)
	w.onAddEnter = func(gtk.Entry) { w.add() }
	w.addAddr.ConnectActivate(&w.onAddEnter)
	w.onClose = func(gtk.Window) bool {
		w.closed = true
		w.cancel()
		if w.pairing != nil {
			w.pairing()
		}
		return false
	}
	w.win.ConnectCloseRequest(&w.onClose)

	w.loadStream()
	w.fillSide()
	if len(w.settings.Machines) > 0 {
		w.choose(w.settings.Machines[0].Addr)
	} else {
		w.choose("sharing")
	}
	var ctx context.Context
	ctx, w.cancel = context.WithCancel(context.Background())
	go w.watch(ctx)
	w.find()
	w.win.Present()
	return w
}

// fail says what went wrong at the foot of the index; "" clears it.
func (w *window) fail(msg string) {
	w.status.SetText(msg)
	setClass(&w.status.Widget, "error", msg != "")
}

func setClass(wd *gtk.Widget, class string, on bool) {
	if on {
		wd.AddCssClass(class)
	} else {
		wd.RemoveCssClass(class)
	}
}

// background runs f off the main thread, then done on it, unless the
// window closed meanwhile.
func (w *window) background(f func() error, done func(error)) {
	go func() {
		err := f()
		gtkx.Post(func() {
			if !w.closed {
				done(err)
			}
		})
	}()
}

// watch keeps the sharing page current (devices waiting to pair come and
// go) and the machine selected's state.
func (w *window) watch(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		var page string
		gtkx.Call(time.Second, func() bool { page = w.selected; return true })
		if page == "sharing" {
			w.refreshSharing(ctx)
		} else if page != "add" {
			w.ask(ctx, page)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// --- the index ---

// fillSide lists the machines: the saved ones, then those found and not
// saved.
func (w *window) fillSide() {
	gtkx.Clear(&w.sideMachines)
	w.machineRows = map[uintptr]string{}
	w.sideButtons = map[string]*gtk.Button{}
	saved := map[string]bool{}
	row := func(name, sub, addr string) {
		b := gtk.NewButton()
		b.AddCssClass("index-item")
		b.SetName("machine")
		box := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		img := gtk.NewImageFromIconName("remote-machine-symbolic")
		img.AddCssClass("index-icon")
		gtkx.Append(box, &img.Widget)
		text := gtk.NewBox(gtk.OrientationVerticalValue, 0)
		text.SetHexpand(true)
		t := gtk.NewLabel(name)
		t.AddCssClass("title")
		t.SetXalign(0)
		t.SetEllipsize(3) // end
		gtkx.Append(text, &t.Widget)
		s := gtk.NewLabel(sub)
		s.AddCssClass("sub")
		s.SetXalign(0)
		s.SetEllipsize(3)
		gtkx.Append(text, &s.Widget)
		gtkx.Append(box, &text.Widget)
		b.SetChild(&box.Widget)
		b.ConnectClicked(&w.onClick)
		w.machineRows[b.GoPointer()] = addr
		w.sideButtons[addr] = b
		gtkx.Append(&w.sideMachines, &b.Widget)
	}
	for _, m := range w.settings.Machines {
		saved[m.Addr] = true
		if m.ID != "" {
			saved["id:"+m.ID] = true
		}
		sub := i18n.T("Not paired")
		if m.Paired() {
			sub = i18n.T("Paired")
		}
		if _, on := w.streams[m.Addr]; on {
			sub = i18n.T("Connected")
		}
		row(remotedesk.Label(m), sub, m.Addr)
	}
	for _, f := range w.found {
		if saved[f.Addr] || f.ID != "" && saved["id:"+f.ID] {
			continue
		}
		sub := i18n.T("Found on this network")
		if f.Tailnet {
			sub = i18n.T("Found on the tailnet")
		}
		row(f.Name, sub, f.Addr)
	}
	w.markSide()
}

func (w *window) markSide() {
	setClass(&w.sideSharing.Widget, "selected", w.selected == "sharing")
	setClass(&w.sideAdd.Widget, "selected", w.selected == "add")
	for addr, b := range w.sideButtons {
		setClass(&b.Widget, "selected", addr == w.selected)
	}
}

// find looks for machines sharing their desktop, on the local network
// and the tailnet.
func (w *window) find() {
	if w.finder == nil {
		return
	}
	w.sideFind.SetSensitive(false)
	var found []remotedesk.Candidate
	w.background(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		found = w.finder.Find(ctx)
		return nil
	}, func(error) {
		w.sideFind.SetSensitive(true)
		w.found = found
		w.fillSide()
	})
}

// choose shows a page: "sharing", "add", or a machine's, by its address.
func (w *window) choose(page string) {
	if w.pairing != nil && page != w.selected {
		w.pairing()
		w.pairing = nil
	}
	w.selected = page
	switch page {
	case "sharing":
		w.pages.SetVisibleChildName("sharing")
		w.showSharing(remotedesk.Status{}, nil, nil, false)
		go w.refreshSharing(context.Background())
	case "add":
		w.pages.SetVisibleChildName("add")
		w.addError.SetText("")
		w.addAddr.GrabFocus()
	default:
		w.pages.SetVisibleChildName("machine")
		w.machinePinBox.SetVisible(false)
		w.showMachine()
		go w.ask(context.Background(), page)
	}
	w.markSide()
}

func (w *window) clicked(b gtk.Button) {
	w.fail("")
	switch b.GetName() {
	case "side_sharing":
		w.choose("sharing")
	case "side_add":
		w.choose("add")
	case "side_find":
		w.find()
	case "machine":
		if addr, ok := w.machineRows[b.GoPointer()]; ok {
			w.choose(addr)
		}
	case "machine_connect":
		w.connect()
	case "machine_pair":
		w.pair()
	case "machine_quit":
		w.quit()
	case "machine_forget":
		w.forget()
	case "add_ok":
		w.add()
	case "pending_pair":
		if r, ok := w.pendingRows[b.GoPointer()]; ok {
			w.pairDevice(r)
		}
	case "client_unpair":
		if c, ok := w.clientRows[b.GoPointer()]; ok {
			w.unpairDevice(c)
		}
	}
}

func (w *window) switched(s gtk.Switch, on bool) bool {
	switch s.GetName() {
	case "stream_host_audio":
		if !w.loading {
			w.settings.Stream.HostAudio = on
			w.save()
		}
		return false
	}
	// The sharing switch: on or off once Sunshine has started or stopped.
	if w.loading {
		return false
	}
	w.sharingSwitch.SetSensitive(false)
	w.background(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return w.sharing.SetOn(ctx, on)
	}, func(err error) {
		w.sharingSwitch.SetSensitive(true)
		if err != nil {
			w.fail(i18n.Tf("Sharing did not start: %v", err))
			w.loading = true
			w.sharingSwitch.SetActive(false)
			w.sharingSwitch.SetState(false)
			w.loading = false
			return
		}
		w.sharingSwitch.SetState(on)
		go w.refreshSharing(context.Background())
	})
	return true // the state follows when Sunshine has
}

// virtualChanged keeps the virtual screen's setting, and adds or removes
// the screen now.
func (w *window) virtualChanged() {
	if w.loading {
		return
	}
	mode := ""
	if i := int(w.sharingVirtual.GetSelected()); i > 0 && i <= len(remotedesk.VirtualModes) {
		mode = remotedesk.VirtualModes[i-1]
	}
	if err := w.sharing.SetVirtual(mode); err != nil {
		w.fail(err.Error())
		return
	}
	w.sharingVirtual.SetSensitive(false)
	w.background(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return w.sharing.SyncVirtual(ctx)
	}, func(err error) {
		w.sharingVirtual.SetSensitive(true)
		if err != nil {
			w.fail(i18n.Tf("The virtual screen did not change: %v", err))
		}
		go w.refreshSharing(context.Background())
	})
}

func (w *window) save() {
	if err := w.settings.Save(); err != nil {
		w.fail(err.Error())
	}
}

// --- this computer: sharing ---

func (w *window) refreshSharing(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	st := w.sharing.Status(ctx)
	var pending []remotedesk.Pairing
	var clients []remotedesk.Client
	if st.Running {
		pending, _ = w.sharing.Pending(ctx)
		clients, _ = w.sharing.Clients(ctx)
	}
	gtkx.Post(func() {
		if !w.closed {
			w.showSharing(st, pending, clients, true)
		}
	})
}

func (w *window) showSharing(st remotedesk.Status, pending []remotedesk.Pairing, clients []remotedesk.Client, known bool) {
	on := w.sharing.On()
	name, _ := os.Hostname()
	switch {
	case st.Running:
		w.sideSharingSub.SetText(i18n.T("Shared"))
		w.sharingDesc.SetText(i18n.Tf("Other devices see this computer as %s, on this network and the tailnet. A device pairs once, with a PIN typed here.", name))
	case on:
		w.sideSharingSub.SetText(i18n.T("Starting…"))
		w.sharingDesc.SetText(i18n.T("Sunshine is starting."))
	default:
		w.sideSharingSub.SetText(i18n.T("Off"))
		w.sharingDesc.SetText(i18n.T("Lets your other devices see and control this desktop, with Moonlight or Remote Desktop."))
	}
	w.loading = true
	w.sharingSwitch.SetActive(on)
	w.sharingSwitch.SetState(on && (st.Running || !known))
	w.sharingVirtual.SetSelected(uint32(slices.Index(remotedesk.VirtualModes, w.sharing.Virtual()) + 1))
	w.loading = false

	need, fix := "", ""
	switch st.Need {
	case remotedesk.NeedSunshine:
		need = i18n.T("Sharing needs Sunshine, which is not installed.")
		fix = i18n.T("Install the package for your system from github.com/LizardByte/Sunshine/releases (Ubuntu: sudo apt install ./sunshine_…_amd64.deb).")
	case remotedesk.NeedInput:
		need = i18n.T("The other device's keyboard and mouse cannot reach this desktop: /dev/uinput is not yours.")
		fix = "echo '" + remotedesk.UinputRule + "' | sudo tee /etc/udev/rules.d/60-sunshine.rules && sudo udevadm control --reload && sudo udevadm trigger && sudo modprobe uinput"
	case remotedesk.NeedDisplay:
		need = i18n.T("Sharing needs a graphical session to show.")
	}
	w.sharingNeed.SetVisible(need != "")
	w.sharingNeedText.SetText(need)
	w.sharingNeedFix.SetText(fix)
	w.sharingNeedFix.SetVisible(fix != "")
	w.sharingSwitch.SetSensitive(st.Need != remotedesk.NeedSunshine || on)

	// Keep a PIN being typed: rebuild the rows only when the devices change.
	same := len(pending) == len(w.pendingRows)
	for _, r := range w.pendingRows {
		found := false
		for _, p := range pending {
			found = found || p.ID == r.p.ID
		}
		same = same && found
	}
	if !same {
		gtkx.Clear(&w.sharingPending)
		w.pendingRows = map[uintptr]pendingRow{}
		for _, p := range pending {
			row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
			row.AddCssClass("row")
			text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
			text.SetHexpand(true)
			l := gtk.NewLabel(cmpOr(p.Name, i18n.T("A device")))
			l.AddCssClass("label")
			l.SetXalign(0)
			gtkx.Append(text, &l.Widget)
			s := gtk.NewLabel(i18n.Tf("at %s: type the PIN it shows", p.Address))
			s.AddCssClass("sub")
			s.SetXalign(0)
			gtkx.Append(text, &s.Widget)
			gtkx.Append(row, &text.Widget)
			e := gtk.NewEntry()
			e.SetMaxLength(4)
			e.SetWidthChars(6)
			e.SetPlaceholderText("PIN")
			e.SetInputPurpose(gtk.InputPurposeDigitsValue)
			e.SetValign(gtk.AlignCenterValue)
			gtkx.Append(row, &e.Widget)
			btn := gtk.NewButtonWithLabel(i18n.T("Pair"))
			btn.AddCssClass("suggested-action")
			btn.SetName("pending_pair")
			btn.SetValign(gtk.AlignCenterValue)
			btn.ConnectClicked(&w.onClick)
			w.pendingRows[btn.GoPointer()] = pendingRow{p: p, entry: e}
			gtkx.Append(row, &btn.Widget)
			gtkx.Append(&w.sharingPending, &row.Widget)
		}
	}
	w.sharingPendingSection.SetVisible(len(pending) > 0)

	gtkx.Clear(&w.sharingClients)
	w.clientRows = map[uintptr]remotedesk.Client{}
	if len(clients) == 0 {
		l := gtk.NewLabel(i18n.T("No device is paired yet."))
		l.AddCssClass("sub")
		l.AddCssClass("row")
		l.SetXalign(0)
		if !st.Running {
			l.SetText(i18n.T("The paired devices show while sharing is on."))
		}
		gtkx.Append(&w.sharingClients, &l.Widget)
	}
	for _, c := range clients {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
		row.AddCssClass("row")
		l := gtk.NewLabel(c.Name)
		l.AddCssClass("label")
		l.SetXalign(0)
		l.SetHexpand(true)
		gtkx.Append(row, &l.Widget)
		btn := gtk.NewButtonWithLabel(i18n.T("Unpair"))
		btn.AddCssClass("outlined")
		btn.SetName("client_unpair")
		btn.SetValign(gtk.AlignCenterValue)
		btn.ConnectClicked(&w.onClick)
		w.clientRows[btn.GoPointer()] = c
		gtkx.Append(row, &btn.Widget)
		gtkx.Append(&w.sharingClients, &row.Widget)
	}
}

func cmpOr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func (w *window) pairDevice(r pendingRow) {
	pin := strings.TrimSpace(r.entry.GetText())
	if len(pin) != 4 || strings.Trim(pin, "0123456789") != "" {
		w.fail(i18n.T("A PIN is four digits."))
		return
	}
	w.background(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return w.sharing.Pair(ctx, r.p, pin, cmpOr(r.p.Name, r.p.Address))
	}, func(err error) {
		switch {
		case errors.Is(err, remotedesk.ErrPIN):
			w.fail(i18n.Tf("That is not the PIN %s shows.", cmpOr(r.p.Name, r.p.Address)))
		case err != nil:
			w.fail(err.Error())
		}
		go w.refreshSharing(context.Background())
	})
}

func (w *window) unpairDevice(c remotedesk.Client) {
	w.background(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return w.sharing.Unpair(ctx, c.UUID)
	}, func(err error) {
		if err != nil {
			w.fail(err.Error())
		}
		go w.refreshSharing(context.Background())
	})
}

// --- a machine ---

// machine is the machine selected: saved, or found.
func (w *window) machine() (remotedesk.Machine, bool) {
	for _, m := range w.settings.Machines {
		if m.Addr == w.selected {
			return m, true
		}
	}
	for _, f := range w.found {
		if f.Addr == w.selected {
			return remotedesk.Machine{Name: f.Name, Addr: f.Addr, ID: f.ID}, false
		}
	}
	return remotedesk.Machine{Addr: w.selected}, false
}

func (w *window) host(m remotedesk.Machine) *gamestream.Host {
	h := &gamestream.Host{Addr: m.Addr, ID: w.id}
	if m.Cert != "" {
		h.Cert = []byte(m.Cert)
	}
	return h
}

// ask asks a machine what it is, and shows it if it is still selected.
func (w *window) ask(ctx context.Context, addr string) {
	var m remotedesk.Machine
	gtkx.Call(time.Second, func() bool {
		if w.selected == addr {
			m, _ = w.machine()
		}
		return true
	})
	if m.Addr == "" || w.id == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	info, err := w.host(m).Info(ctx)
	gtkx.Post(func() {
		if w.closed {
			return
		}
		w.info[addr] = machineInfo{info: info, err: err, asked: true}
		if w.selected == addr {
			w.showMachine()
		}
	})
}

func (w *window) showMachine() {
	m, saved := w.machine()
	mi := w.info[m.Addr]
	w.machineTitle.SetText("[" + strings.ToUpper(remotedesk.Label(m)) + "]")
	w.machineAddr.SetText(m.Addr)
	_, streaming := w.streams[m.Addr]
	paired := m.Paired() && (!mi.asked || mi.err != nil || mi.info.Paired())
	state := i18n.T("Asking…")
	switch {
	case streaming:
		state = i18n.T("Connected: its desktop is in a window of its own.")
	case !mi.asked:
	case mi.err != nil:
		state = i18n.T("It does not answer: it is off, or not sharing its desktop.")
	case !mi.info.Sunshine():
		state = i18n.T("It shares with Nvidia's GameStream, which Remote Desktop does not speak.")
	case m.Paired() && !mi.info.Paired():
		state = i18n.T("It has forgotten this computer: pair again.")
	case !m.Paired():
		state = i18n.T("Not paired: pair once, with a PIN typed on it.")
	case mi.info.Busy():
		state = i18n.T("Paired. Its desktop is being streamed; connecting joins in.")
	default:
		state = i18n.T("Paired, and ready.")
	}
	w.machineState.SetText(state)
	reachable := mi.asked && mi.err == nil && mi.info.Sunshine()
	w.machineConnect.SetVisible(paired)
	w.machineConnect.SetSensitive(!streaming && (reachable || !mi.asked))
	w.machinePair.SetVisible(!paired || mi.asked && mi.err == nil && !mi.info.Paired())
	w.machinePair.SetSensitive(reachable || w.pairing != nil)
	if w.pairing != nil {
		w.machinePair.SetLabel(i18n.T("Cancel"))
	} else {
		w.machinePair.SetLabel(i18n.T("Pair"))
	}
	w.machineQuit.SetVisible(paired && reachable && mi.info.Busy())
	w.machineForget.SetVisible(saved)
}

// pair pairs with the machine selected: it shows a PIN to type there,
// and waits until it is typed.
func (w *window) pair() {
	if w.pairing != nil {
		w.pairing()
		w.pairing = nil
		w.machinePinBox.SetVisible(false)
		w.showMachine()
		return
	}
	m, _ := w.machine()
	h := w.host(m)
	h.Cert = nil
	pin := gamestream.NewPIN()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	w.pairing = cancel
	w.machinePinText.SetText(i18n.Tf("On %s, open Remote Desktop → Sharing (or Sunshine's PIN page) and type:", remotedesk.Label(m)))
	w.machinePin.SetText(pin)
	w.machinePinBox.SetVisible(true)
	w.showMachine()
	name, _ := os.Hostname()
	w.background(func() error {
		return h.Pair(ctx, pin, cmpOr(name, "inua"))
	}, func(err error) {
		cancel()
		if w.pairing == nil { // cancelled, or another page chosen
			return
		}
		w.pairing = nil
		w.machinePinBox.SetVisible(false)
		switch {
		case err == nil:
			info, _ := w.info[m.Addr]
			m.Cert = string(h.Cert)
			m.Name = cmpOr(info.info.Hostname, m.Name)
			m.ID = cmpOr(info.info.UniqueID, m.ID)
			w.settings.Keep(m)
			w.save()
			w.fillSide()
		case errors.Is(err, gamestream.ErrWrongPIN):
			w.fail(i18n.T("The PIN typed there was not this one. Pair again."))
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		default:
			w.fail(i18n.Tf("Pairing failed: %v", err))
		}
		go w.ask(context.Background(), m.Addr)
	})
}

func (w *window) quit() {
	m, _ := w.machine()
	w.background(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return w.host(m).Quit(ctx)
	}, func(err error) {
		if err != nil {
			w.fail(err.Error())
		}
		go w.ask(context.Background(), m.Addr)
	})
}

func (w *window) forget() {
	m, _ := w.machine()
	if s, ok := w.streams[m.Addr]; ok {
		s.close()
	}
	w.settings.Forget(m.Addr)
	w.save()
	go func() { // the machine may forget this computer too
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		w.host(m).Unpair(ctx)
	}()
	w.fillSide()
	w.choose("sharing")
}

// add adds a machine by its address, once it answers there.
func (w *window) add() {
	addr := strings.TrimSpace(w.addAddr.GetText())
	if addr == "" || w.id == nil {
		return
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(strings.Trim(addr, "[]"), strconv.Itoa(gamestream.DefaultPort))
	}
	w.addOK.SetSensitive(false)
	var info gamestream.ServerInfo
	w.background(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var err error
		info, err = (&gamestream.Host{Addr: addr, ID: w.id}).Info(ctx)
		return err
	}, func(err error) {
		w.addOK.SetSensitive(true)
		if err != nil {
			w.addError.SetText(i18n.Tf("Nothing shares a desktop at %s: %v", addr, err))
			return
		}
		w.settings.Keep(remotedesk.Machine{Name: info.Hostname, Addr: addr, ID: info.UniqueID})
		w.save()
		w.addAddr.SetText("")
		w.fillSide()
		w.choose(addr)
	})
}

// --- the stream's settings ---

var sizes = [][2]int{{0, 0}, {1280, 720}, {1920, 1080}, {2560, 1440}, {3840, 2160}}
var fpses = []int{30, 60, 90, 120}
var codecs = []string{"h264", "hevc", "av1"}

func (w *window) loadStream() {
	st := w.settings.Stream
	w.loading = true
	defer func() { w.loading = false }()
	for i, s := range sizes {
		if s[0] == st.Width && s[1] == st.Height {
			w.streamSize.SetSelected(uint32(i))
		}
	}
	for i, f := range fpses {
		if f == st.FPS {
			w.streamFPS.SetSelected(uint32(i))
		}
	}
	for i, c := range codecs {
		if c == st.Codec {
			w.streamCodec.SetSelected(uint32(i))
		}
	}
	w.streamBitrate.SetValue(float64(max(st.Bitrate, 2000)) / 1000)
	w.streamHostAudio.SetActive(st.HostAudio)
	w.streamHostAudio.SetState(st.HostAudio)
	w.codecNote()
}

func (w *window) streamChanged() {
	if w.loading {
		return
	}
	st := &w.settings.Stream
	if i := int(w.streamSize.GetSelected()); i < len(sizes) {
		st.Width, st.Height = sizes[i][0], sizes[i][1]
	}
	if i := int(w.streamFPS.GetSelected()); i < len(fpses) {
		st.FPS = fpses[i]
	}
	if i := int(w.streamCodec.GetSelected()); i < len(codecs) {
		st.Codec = codecs[i]
	}
	st.Bitrate = int(w.streamBitrate.GetValue()) * 1000
	w.codecNote()
	w.save()
}

// codecNote says whether this computer decodes the codec with its GPU.
func (w *window) codecNote() {
	codec := map[string]string{"h264": "h264", "hevc": "h265", "av1": "av1"}[w.settings.Stream.Codec]
	dec := decoderFor(codec)
	switch {
	case dec == "":
		w.streamCodecNote.SetText(i18n.T("This computer cannot decode it: choose another."))
	case strings.HasPrefix(dec, "avdec") || strings.HasPrefix(dec, "dav1d") || dec == "av1dec":
		w.streamCodecNote.SetText(i18n.T("Decoded by the processor here: slower than the graphics card."))
	default:
		w.streamCodecNote.SetText(i18n.T("Decoded by the graphics card here. HEVC and AV1 look better than H.264 at the same bitrate."))
	}
}

// connect opens the selected machine's desktop in a window of its own.
func (w *window) connect() {
	m, _ := w.machine()
	if s, ok := w.streams[m.Addr]; ok {
		s.win.Present()
		return
	}
	s := openStream(w, m)
	if s == nil {
		return
	}
	w.streams[m.Addr] = s
	w.fillSide()
	w.showMachine()
}

// ended forgets a stream whose window closed.
func (w *window) ended(s *stream, err error) {
	if w.streams[s.m.Addr] == s {
		delete(w.streams, s.m.Addr)
	}
	if err != nil {
		w.fail(fmt.Sprint(err))
	}
	if !w.closed {
		w.fillSide()
		if w.selected == s.m.Addr {
			w.showMachine()
		}
	}
}
