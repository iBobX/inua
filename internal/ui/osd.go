package ui

import (
	"slices"

	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

const (
	osdMS     = 1500 // how long the indicator stays after the last change
	osdFadeMS = 200  // transition-duration of its revealer
)

// osd is the volume, brightness, Wi-Fi and Bluetooth indicator, after the
// phones': whatever changes either (the keys, the wheel on the bar, the
// control center, a network coming or going), a card shows at the corner
// of the focused monitor for a moment. It takes no input, and stays away
// while the control center, which has the sliders, is open.
type osd struct {
	win   *gtk.ApplicationWindow // built on first use, then kept
	refs  gtkx.Scope
	rev   gtk.Revealer
	icon  gtk.Image
	level gtk.LevelBar
	value gtk.Label
	gen   uintptr // the latest showing; older timeouts do nothing

	// seen is the system state the last checkOSD observed; wifi is the
	// Wi-Fi network last shown, so a blip while connecting (no network,
	// then the same one) shows nothing.
	seen shell.System
	wifi string
}

func (s *shellUI) bindOSDHandlers() {
	s.onOSDHide = func(gen uintptr) bool {
		if o := &s.osd; gen == o.gen {
			o.rev.SetRevealChild(false)
			glib.TimeoutAdd(osdFadeMS, &s.onOSDGone, gen)
		}
		return false
	}
	s.onOSDGone = func(gen uintptr) bool {
		if o := &s.osd; gen == o.gen {
			o.win.SetVisible(false)
		}
		return false
	}
}

// ensureOSD builds the indicator's window and anchors it where the
// settings put it. It is built on first use, then kept: only the card's
// contents change.
func (s *shellUI) ensureOSD() {
	o := &s.osd
	if o.win != nil {
		return
	}
	o.win = &gtk.ApplicationWindow{}
	ui := s.load("osd.ui", &o.refs)
	ui.Get("osd", o.win)
	ui.Get("reveal", &o.rev)
	ui.Get("icon", &o.icon)
	ui.Get("level", &o.level)
	ui.Get("value", &o.value)
	ui.Close()
	o.rev.SetTransitionDuration(osdFadeMS)
	o.win.SetApplication(s.app)
	w := o.win.GoPointer()
	if s.layer {
		layershell.InitForWindow(w)
		layershell.SetNamespace(w, "inua-osd")
		layershell.SetLayer(w, layershell.LayerOverlay)
		layershell.SetKeyboardMode(w, layershell.KeyboardNone)
		layershell.SetExclusiveZone(w, -1) // over the dock, not beside it
		s.placeOSD(w)
	} else {
		o.win.SetTitle(i18n.T("inua volume"))
	}
	o.win.SetVisible(false)
}

// osdMargin keeps the card clear of the bar or the dock at the edge it
// anchors to; it is over both, with no exclusive zone of its own.
const osdMargin = 64

// placeOSD anchors the indicator's window where [osd] position puts it.
// A centred position anchors neither side, so the card is centred.
func (s *shellUI) placeOSD(w uintptr) {
	top, left, right := false, false, false
	switch s.cfg.OSD.Position {
	case "top-center":
		top = true
	case "top-left":
		top, left = true, true
	case "top-right":
		top, right = true, true
	case "bottom-left":
		left = true
	case "bottom-right":
		right = true
	}
	layershell.SetAnchor(w, layershell.EdgeTop, top)
	layershell.SetAnchor(w, layershell.EdgeBottom, !top)
	layershell.SetAnchor(w, layershell.EdgeLeft, left)
	layershell.SetAnchor(w, layershell.EdgeRight, right)
	tm, bm := int32(0), int32(osdMargin)
	if top {
		tm, bm = osdMargin, 0
	}
	layershell.SetMargin(w, layershell.EdgeTop, tm)
	layershell.SetMargin(w, layershell.EdgeBottom, bm)
}

// showVolume shows the volume a on the indicator.
func (s *shellUI) showVolume(a system.AudioState) {
	level := max(0, min(a.Volume, 1))
	if a.Muted {
		level = 0
	}
	s.showLevel(shell.VolumeIcon(a), -1, level, shell.VolumeText(a))
}

// showBrightness shows the screen's brightness, 0–100, on the indicator;
// its icon brightens with the screen, as the control center's does.
func (s *shellUI) showBrightness(percent float64) {
	s.showLevel("display-brightness-symbolic", percent, percent/100, shell.Percent(percent/100))
}

// showLevel shows the indicator, for osdMS from now: an icon (brightened
// to bright, 0–100, or not when bright is negative), a level from 0 to 1
// and its readout. Nothing shows while the control center, which has the
// sliders, is open, or when [osd] enable is off.
func (s *shellUI) showLevel(icon string, bright, level float64, text string) {
	if !s.cfg.OSD.Enable || s.control.open {
		return
	}
	s.ensureOSD()
	o := &s.osd
	o.icon.SetFromIconName(icon)
	toggle(&o.icon.Widget, "bright-icon", bright >= 0)
	brightLevel(&o.icon.Widget, bright)
	// A card with no level (Wi-Fi, Bluetooth) is its word alone.
	o.level.SetVisible(level >= 0)
	o.level.SetValue(max(level, 0))
	o.value.SetHexpand(level < 0)
	o.value.SetXalign(1)
	if level < 0 {
		o.value.SetXalign(0)
	}
	o.value.SetText(text)
	if !o.win.GetVisible() {
		// Without layer shell the window is an ordinary toplevel, and
		// Present() would raise it and take the focus at every change;
		// there it stays out of the way entirely.
		if s.layer {
			if mon := s.focusedMonitor(); mon != nil {
				layershell.SetMonitor(o.win.GoPointer(), mon.GoPointer())
			}
			o.win.SetVisible(true)
			o.win.Present()
			gtkx.PassInput(&o.win.Window)
		}
	}
	o.rev.SetRevealChild(true)
	o.gen++
	glib.TimeoutAdd(osdMS, &s.onOSDHide, o.gen)
}

// showOSDCard shows the indicator for a change with no degree: Wi-Fi and
// Bluetooth coming on, going off, or connecting to something. The level
// bar stays out of the way.
func (s *shellUI) showOSDCard(icon, text string) {
	s.showLevel(icon, -1, -1, text)
}

// hideLevel puts the indicator away at once.
func (s *shellUI) hideLevel() {
	if o := &s.osd; o.win != nil {
		o.gen++
		o.win.SetVisible(false)
	}
}

// reanchorOSD moves the indicator to the position the settings now ask
// for. The window is kept; only its anchors change, so a new position
// takes effect at once, without a restart.
func (s *shellUI) reanchorOSD() {
	o := &s.osd
	if o.win == nil || !s.layer {
		return
	}
	s.placeOSD(o.win.GoPointer())
}

// checkOSD diffs the system state against what the last update saw and
// shows a card for Wi-Fi or Bluetooth: turned on or off, or joined to a
// network or a device. It runs in update, so every path to a change passes
// here once. A service's first report (Available turning true, at login
// or when it restarts) is not a change, nor are wired networks and VPNs,
// nor a network or device going away, which the bar's icons show.
func (s *shellUI) checkOSD(st *shell.System) {
	o := &s.osd
	before := o.seen
	o.seen = *st
	if n, b := st.Net, before.Net; n.Available && b.Available {
		switch {
		case n.WifiEnabled != b.WifiEnabled:
			o.wifi = ""
			if n.WifiEnabled {
				s.showOSDCard("network-wireless-signal-excellent-symbolic", i18n.T("Wi-Fi on"))
			} else {
				s.showOSDCard("network-wireless-disabled-symbolic", i18n.T("Wi-Fi off"))
			}
		case n.Kind == "wifi" && n.Name != "" && n.Name != o.wifi:
			o.wifi = n.Name
			s.showOSDCard(shell.NetIcon(n), n.Name)
		}
	} else if n.Kind == "wifi" {
		o.wifi = n.Name // already joined when it first reported
	}
	if bt, b := st.BT, before.BT; bt.Available && b.Available {
		switch {
		case bt.Powered != b.Powered:
			if bt.Powered {
				s.showOSDCard("bluetooth-active-symbolic", i18n.T("Bluetooth on"))
			} else {
				s.showOSDCard("bluetooth-disabled-symbolic", i18n.T("Bluetooth off"))
			}
		default:
			for _, name := range bt.Connected {
				if !slices.Contains(b.Connected, name) {
					s.showOSDCard("bluetooth-active-symbolic", name)
					break
				}
			}
		}
	}
}
