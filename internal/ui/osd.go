package ui

import (
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

const (
	osdMS     = 1500 // how long the volume indicator stays after the last change
	osdFadeMS = 200  // transition-duration of its revealer
)

// osd is the volume indicator, after the phones': whatever changes the
// volume (the keys, the wheel on the bar, `inua system volume`), a card
// shows the level at the foot of the focused monitor for a moment. It
// takes no input, and stays away while the control center, which has the
// slider, is open.
type osd struct {
	win   *gtk.ApplicationWindow // built on first use, then kept
	refs  gtkx.Scope
	rev   gtk.Revealer
	icon  gtk.Image
	level gtk.LevelBar
	value gtk.Label
	gen   uintptr // the latest showing; older timeouts do nothing
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
		layershell.SetAnchor(w, layershell.EdgeBottom, true)
		layershell.SetMargin(w, layershell.EdgeBottom, 64)
	} else {
		o.win.SetTitle(i18n.T("inua volume"))
	}
}

// showVolume shows the volume indicator with the level a, for osdMS from
// now.
func (s *shellUI) showVolume(a system.AudioState) {
	if s.control.open {
		return
	}
	s.ensureOSD()
	o := &s.osd
	o.icon.SetFromIconName(shell.VolumeIcon(a))
	level := max(0, min(a.Volume, 1))
	if a.Muted {
		level = 0
	}
	o.level.SetValue(level)
	o.value.SetText(shell.VolumeText(a))
	if !o.win.GetVisible() {
		if s.layer {
			if mon := s.focusedMonitor(); mon != nil {
				layershell.SetMonitor(o.win.GoPointer(), mon.GoPointer())
			}
		}
		o.win.SetVisible(true)
		o.win.Present()
		gtkx.PassInput(&o.win.Window)
	}
	o.rev.SetRevealChild(true)
	o.gen++
	glib.TimeoutAdd(osdMS, &s.onOSDHide, o.gen)
}

// hideVolume puts the volume indicator away at once.
func (s *shellUI) hideVolume() {
	if o := &s.osd; o.win != nil {
		o.gen++
		o.win.SetVisible(false)
	}
}
