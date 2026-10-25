package gtkx

import "os"

// ForcePortals has GTK ask the portal for what it can (choosing files,
// most of all) even outside a sandbox, so inua's own apps get the picker
// inua serves (docs/picker.md): GDK_DEBUG=portals, read once as GTK opens
// its display. The function returned takes it out of the environment
// again, once the display is open, so the programs the app starts do not
// inherit it.
func ForcePortals(on bool) (restore func()) {
	old, had := os.LookupEnv("GDK_DEBUG")
	if !on {
		return func() {}
	}
	v := "portals"
	if had && old != "" {
		v = old + ",portals"
	}
	os.Setenv("GDK_DEBUG", v)
	return func() {
		if had {
			os.Setenv("GDK_DEBUG", old)
		} else {
			os.Unsetenv("GDK_DEBUG")
		}
	}
}
