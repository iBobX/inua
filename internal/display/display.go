// Package display tells from the kernel whether a laptop's built-in
// display should be off: its lid is closed while another display is
// connected. It reads /proc and /sys, not Hyprland, so it answers before
// the compositor runs too, and counts only displays plugged in (not
// Hyprland's virtual screens).
package display

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// System is where the kernel's files are read: "/" on the computer, a
// temporary directory in tests.
type System struct {
	Root string
	// Logind answers whether the lid is closed when /proc has no lid
	// (logind's LidClosed); nil asks logind on the system bus.
	Logind func() (closed bool, err error)
}

// Local is this computer.
var Local = System{Root: "/"}

// Internal reports whether a connector ("eDP-1") is a built-in panel.
func Internal(connector string) bool {
	for _, p := range []string{"eDP-", "LVDS-", "DSI-"} {
		if strings.HasPrefix(connector, p) {
			return true
		}
	}
	return false
}

// HasLid reports whether the computer has a lid the kernel reports.
func (s System) HasLid() bool {
	states, _ := filepath.Glob(filepath.Join(s.Root, "proc/acpi/button/lid/*/state"))
	return len(states) > 0
}

// LidClosed reports whether a lid is closed: ACPI's state, else logind's.
// A computer without a lid has none closed.
func (s System) LidClosed() bool {
	states, _ := filepath.Glob(filepath.Join(s.Root, "proc/acpi/button/lid/*/state"))
	for _, path := range states {
		if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), "closed") {
			return true
		}
	}
	if len(states) > 0 {
		return false
	}
	ask := s.Logind
	if ask == nil {
		ask = logindLidClosed
	}
	closed, err := ask()
	return err == nil && closed
}

// Connected lists the connectors with a display plugged in ("eDP-1",
// "DP-2"), by the names Hyprland gives them, sorted.
func (s System) Connected() []string {
	statuses, _ := filepath.Glob(filepath.Join(s.Root, "sys/class/drm/card*-*/status"))
	var names []string
	for _, path := range statuses {
		data, err := os.ReadFile(path)
		if err != nil || strings.TrimSpace(string(data)) != "connected" {
			continue
		}
		// card1-DP-2/status: the connector after the card's name.
		_, name, ok := strings.Cut(filepath.Base(filepath.Dir(path)), "-")
		if ok && !strings.HasPrefix(name, "Writeback-") {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// PanelsOff lists the built-in panels to turn off: all connected ones
// while the lid is closed and another display is connected, else none.
// Without another display the laptop has no screen but its own, and
// logind suspends it when the lid closes.
func (s System) PanelsOff() []string {
	connected := s.Connected()
	var panels []string
	external := false
	for _, name := range connected {
		if Internal(name) {
			panels = append(panels, name)
		} else {
			external = true
		}
	}
	if !external || len(panels) == 0 || !s.LidClosed() {
		return nil
	}
	return panels
}

// logindLidClosed asks logind for its LidClosed property.
func logindLidClosed() (bool, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return false, err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var v dbus.Variant
	err = conn.Object("org.freedesktop.login1", "/org/freedesktop/login1").
		CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, "org.freedesktop.login1.Manager", "LidClosed").Store(&v)
	if err != nil {
		return false, err
	}
	closed, _ := v.Value().(bool)
	return closed, nil
}
