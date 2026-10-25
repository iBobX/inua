package main

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/portalconf"
)

// cmdPortals makes inua's backends the ones xdg-desktop-portal asks, an
// interface at a time (docs/portals.md).
func cmdPortals(args []string) error {
	if len(args) == 0 {
		usage()
	}
	d := portalconf.Default()
	switch args[0] {
	case "status":
		if len(args) == 1 {
			return portalsStatus(d)
		}
	case "enable", "disable":
		ifaces := args[1:]
		if len(ifaces) == 0 {
			ifaces = portalconf.Interfaces()
		}
		on, err := d.Set(args[0] == "enable", ifaces...)
		if err != nil {
			return err
		}
		if on {
			fmt.Println("Wrote", d.Own)
		} else {
			fmt.Println("No interface left on; removed", d.Own)
		}
		msg, err := d.Apply(on)
		if err != nil {
			return err
		}
		fmt.Println(msg)
		if slices.ContainsFunc(ifaces, func(i string) bool { return strings.EqualFold(i, portalconf.SharePicker) }) {
			if err := portalconf.RestartXdph(); err != nil {
				return err
			}
			fmt.Println("Restarted xdg-desktop-portal-hyprland for its share picker.")
		}
		return nil
	case "dir":
		// For inua-session: the directory, written again, when one
		// interface is on.
		if len(args) == 1 {
			on, err := d.Refresh()
			if err != nil {
				return err
			}
			if !on {
				os.Exit(1)
			}
			fmt.Println(d.Own)
			return nil
		}
	}
	usage()
	return nil
}

func portalsStatus(d portalconf.Dirs) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer conn.Close()
	enabled := d.Enabled()
	for _, b := range portalconf.Backends {
		running := "not running"
		if portalconf.HasOwner(conn, b.Bus) {
			running = "running"
		}
		fmt.Printf("%s (%s, %s)\n", b.Name, b.Bus, running)
		for _, i := range b.Interfaces {
			state := "off (inua portals enable " + i + ")"
			if slices.Contains(enabled, i) {
				state = "on"
			}
			fmt.Printf("  %-14s %s\n", i, state)
		}
	}
	switch got := portalconf.SessionDir(); {
	case len(enabled) == 0:
	case got == d.Own:
		fmt.Println("The frontend reads", d.Own)
	default:
		fmt.Println("The frontend does not read", d.Own, "yet; it will in the next inua session")
	}
	return nil
}
