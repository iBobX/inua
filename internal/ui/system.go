package ui

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/lyricat/inua/internal/apps/tailscale"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

type services struct {
	net       *system.Network
	bt        *system.Bluetooth
	prof      *system.PowerProfiles
	media     *system.Media
	audio     *system.Audio
	backlight *system.Backlight // nil without a panel backlight
	ts        *system.Tailscale

	volume     *shell.Setter
	brightness *shell.Setter
}

// startSystem runs the services; their updates reach the bars and the
// control center on the main thread.
func (s *shellUI) startSystem() {
	ctx := context.Background()
	v := &s.svc
	v.net = system.NewNetwork(system.SystemBus)
	v.bt = system.NewBluetooth(system.SystemBus)
	v.prof = system.NewPowerProfiles(system.SystemBus)
	v.media = system.NewMedia(system.SessionBus)
	v.audio = system.NewAudio()
	v.ts = system.NewTailscale("")
	v.volume = shell.NewSetter(func(f float64) { logErr("volume", v.audio.SetVolume(f)) })
	if b, err := system.FindBacklight(system.BacklightDir, system.SystemBus); err == nil {
		v.backlight = b
		v.brightness = shell.NewSetter(func(f float64) { logErr("brightness", b.SetPercent(f)) })
	}

	go v.net.Run(ctx, func(st system.NetworkState) { s.update(func(x *shell.System) { x.Net = st }) })
	go v.bt.Run(ctx, func(st system.BluetoothState) { s.update(func(x *shell.System) { x.BT = st }) })
	go v.prof.Run(ctx, func(st system.PowerProfileState) { s.update(func(x *shell.System) { x.Prof = st }) })
	go v.media.Run(ctx, func(st system.MediaState) { s.update(func(x *shell.System) { x.Media = st }) })
	go v.audio.Run(ctx, func(st system.AudioState) { s.update(func(x *shell.System) { x.Audio = st }) })
	go system.NewBattery(system.SystemBus).Run(ctx, func(st system.BatteryState) { s.update(func(x *shell.System) { x.Bat = st }) })
}

func (s *shellUI) update(f func(*shell.System)) {
	gtkx.Post(func() {
		f(&s.sys)
		for _, b := range s.bars {
			b.setStatus(&s.sys)
		}
		if s.control.open {
			s.applyControl()
		}
	})
}

func logErr(what string, err error) {
	if err != nil {
		log.Printf("%s: %v", what, err)
	}
}

// tailscaleApp is the Tailscale light app's desktop entry.
const tailscaleApp = "io.github.lyricat.Inua.Tailscale"

// readTailscale reads Tailscale's state for the control center.
func (s *shellUI) readTailscale() {
	ts := s.svc.ts
	if ts == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		st := ts.Read(ctx)
		cancel()
		s.update(func(x *shell.System) { x.TS = st })
	}()
}

// toggleTailscale connects or disconnects Tailscale from its tile; when
// something is in the way (logged out, the daemon stopped, not allowed to
// change it yet) it opens the app, which says what and offers the fix.
func (s *shellUI) toggleTailscale() {
	_, on, toggles := shell.TailscaleTile(s.sys.TS)
	if !toggles {
		s.closeControl()
		s.openLightApp(tailscaleApp, "tailscale")
		return
	}
	ts := s.svc.ts
	// Show the change now; the next read corrects a failure.
	if on {
		s.sys.TS.Backend = "Stopped"
	} else {
		s.sys.TS.Backend = "Starting"
	}
	s.applyControl()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := ts.SetRunning(ctx, !on)
		cancel()
		gtkx.Post(func() {
			if errors.Is(err, tailscale.ErrPermission) {
				log.Printf("tailscale: not allowed to change it yet; opening the app")
				s.closeControl()
				s.openLightApp(tailscaleApp, "tailscale")
			} else {
				logErr("tailscale", err)
			}
			s.readTailscale()
		})
	}()
}
