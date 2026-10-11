package system

import (
	"context"

	"github.com/godbus/dbus/v5"
)

const (
	upowerName    = "org.freedesktop.UPower"
	displayDevice = dbus.ObjectPath("/org/freedesktop/UPower/devices/DisplayDevice")
	profilesName  = "org.freedesktop.UPower.PowerProfiles"
	profilesPath  = dbus.ObjectPath("/org/freedesktop/UPower/PowerProfiles")
)

// BatteryState is UPower's combined ("display") battery.
type BatteryState struct {
	Present  bool
	Percent  float64
	Charging bool
	Full     bool
}

// Battery follows UPower.
type Battery struct{ src source }

func NewBattery(dial Dialer) *Battery {
	return &Battery{src: source{dial: dial, name: upowerName, matches: [][]dbus.MatchOption{
		{dbus.WithMatchSender(upowerName), dbus.WithMatchObjectPath(displayDevice), dbus.WithMatchInterface(propsIface)},
	}}}
}

// Run reports the state until ctx ends.
func (b *Battery) Run(ctx context.Context, onChange func(BatteryState)) {
	e := &emitter[BatteryState]{f: onChange}
	b.src.run(ctx, func(ctx context.Context, conn *dbus.Conn) {
		p, err := getAll(ctx, conn.Object(upowerName, displayDevice), upowerName+".Device")
		// Type 2 is a battery; desktops report a line-power display device.
		if err != nil || !value[bool](p, "IsPresent") || value[uint32](p, "Type") != 2 {
			e.emit(BatteryState{})
			return
		}
		state := value[uint32](p, "State")
		e.emit(BatteryState{
			Present:  true,
			Percent:  value[float64](p, "Percentage"),
			Charging: state == 1 || state == 5, // charging, pending charge
			Full:     state == 4,
		})
	})
}

// PowerProfileState is power-profiles-daemon's state.
type PowerProfileState struct {
	Available bool
	Active    string   // "power-saver", "balanced" or "performance"
	Profiles  []string // in the daemon's order
}

// PowerProfiles follows power-profiles-daemon.
type PowerProfiles struct{ src source }

func NewPowerProfiles(dial Dialer) *PowerProfiles {
	return &PowerProfiles{src: source{dial: dial, name: profilesName, matches: [][]dbus.MatchOption{
		{dbus.WithMatchSender(profilesName), dbus.WithMatchInterface(propsIface)},
	}}}
}

// Run reports the state until ctx ends.
func (p *PowerProfiles) Run(ctx context.Context, onChange func(PowerProfileState)) {
	e := &emitter[PowerProfileState]{f: onChange}
	p.src.run(ctx, func(ctx context.Context, conn *dbus.Conn) {
		props, err := getAll(ctx, conn.Object(profilesName, profilesPath), profilesName)
		if err != nil {
			e.emit(PowerProfileState{})
			return
		}
		s := PowerProfileState{Available: true, Active: value[string](props, "ActiveProfile")}
		for _, m := range value[[]map[string]dbus.Variant](props, "Profiles") {
			if name, ok := m["Profile"].Value().(string); ok {
				s.Profiles = append(s.Profiles, name)
			}
		}
		e.emit(s)
	})
}

// SetProfile switches the active profile.
func (p *PowerProfiles) SetProfile(name string) error {
	return p.src.setProp(profilesName, profilesPath, profilesName, "ActiveProfile", name)
}
