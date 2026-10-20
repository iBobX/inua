package shell

import "github.com/lyricat/inua/internal/i18n"

// ProfileLabel is a power-profiles-daemon profile as Settings → Power names
// it; one it does not know by its own name.
func ProfileLabel(name string) string {
	switch name {
	case "power-saver":
		return i18n.T("Power saver")
	case "balanced":
		return i18n.T("Balanced")
	case "performance":
		return i18n.T("Performance")
	}
	return name
}

// ButtonLabel is what logind does with the lid or the power key, as
// Settings → Power names it; one it does not know by logind's own name.
func ButtonLabel(action string) string {
	switch action {
	case "suspend":
		return i18n.T("Suspend")
	case "hibernate":
		return i18n.T("Hibernate")
	case "lock":
		return i18n.T("Lock the screen")
	case "poweroff":
		return i18n.T("Power off")
	case "ignore":
		return i18n.T("Do nothing")
	}
	return action
}
