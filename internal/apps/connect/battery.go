package connect

import (
	"context"
	"os/exec"
	"time"

	"github.com/lyricat/inua/internal/apps/tailscale"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/kdeconnect"
	"github.com/lyricat/inua/internal/system"
)

// batteryBody is a battery packet's body.
type batteryBody struct {
	CurrentCharge  int  `json:"currentCharge"`
	IsCharging     bool `json:"isCharging"`
	ThresholdEvent int  `json:"thresholdEvent"` // 1: it runs low
}

// lowBattery is the charge this computer's battery is said to run low at.
const lowBattery = 15

// battery keeps a device's battery, and says when it runs low.
func (s *Service) battery(d kdeconnect.Device, p kdeconnect.Packet) {
	var b batteryBody
	if p.Decode(&b) != nil || b.CurrentCharge < 0 {
		return
	}
	s.mu.Lock()
	s.batteries[d.ID] = &Battery{Charge: b.CurrentCharge, Charging: b.IsCharging}
	s.mu.Unlock()
	if b.ThresholdEvent == 1 && !b.IsCharging {
		s.notes.Post(Note{App: d.Name, Icon: "battery-low-symbolic", Title: i18n.Tf("%s's battery is low", d.Name),
			Body: i18n.Tf("%d%% left", b.CurrentCharge)})
	}
	s.emitChanged()
}

// batteryLoop follows this computer's battery and tells the paired
// devices.
func (s *Service) batteryLoop(ctx context.Context) {
	system.NewBattery(system.SystemBus).Run(ctx, func(st system.BatteryState) {
		s.mu.Lock()
		was := s.myBattery
		s.myBattery = st
		ids := make([]string, 0, len(s.ready))
		for id := range s.ready {
			ids = append(ids, id)
		}
		s.mu.Unlock()
		if !st.Present || (was.Present && int(was.Percent) == int(st.Percent) && was.Charging == st.Charging) {
			return
		}
		for _, id := range ids {
			s.sendBattery(id)
		}
	})
}

// sendBattery tells the device this computer's battery, if it has one.
func (s *Service) sendBattery(id string) {
	s.mu.Lock()
	st := s.myBattery
	s.mu.Unlock()
	if !st.Present {
		return
	}
	b := batteryBody{CurrentCharge: int(st.Percent), IsCharging: st.Charging}
	if b.CurrentCharge <= lowBattery && !b.IsCharging {
		b.ThresholdEvent = 1
	}
	s.core.Send(id, kdeconnect.NewPacket(typeBattery, b))
}

// ringSound is what rings while a device looks for this computer.
const ringSound = "/usr/share/sounds/freedesktop/stereo/phone-incoming-call.oga"

// ringFor is how long it rings, at most.
const ringFor = time.Minute

// ring rings this computer, a device looking for it, until the note is
// closed or a minute passes.
func (s *Service) ring(d kdeconnect.Device) {
	ctx, cancel := context.WithTimeout(s.ctx, ringFor)
	s.notes.Post(Note{
		App:     d.Name,
		Icon:    "audio-volume-high-symbolic",
		Title:   i18n.Tf("%s is looking for this computer", d.Name),
		Actions: []Action{{"default", i18n.T("Found it")}},
		Urgent:  true,
		Acted:   func(string) { cancel() },
		Closed:  func(bool) { cancel() },
	})
	go func() {
		defer cancel()
		for ctx.Err() == nil {
			if err := exec.CommandContext(ctx, "pw-play", ringSound).Run(); err != nil && ctx.Err() == nil {
				return
			}
		}
	}()
}

func tailscaleStatus(ctx context.Context) (tailscale.Status, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return tailscale.NewClient("").Status(ctx)
}
