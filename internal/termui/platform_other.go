//go:build !darwin

package termui

import "codeberg.org/puregotk/puregotk/v4/gdk"

func keyMods(state gdk.ModifierType) gdk.ModifierType { return state }

// The keys the settings do not change: Ctrl+Alt with an arrow moves the
// focus, Ctrl+Shift+Super with one resizes; Alt and a digit shows a
// session, Ctrl+Shift and one a workspace; Ctrl+click opens a link.
const (
	focusMods     = gdk.ControlMaskValue | gdk.AltMaskValue
	resizeMods    = gdk.ControlMaskValue | gdk.ShiftMaskValue | gdk.SuperMaskValue
	sessionMods   = gdk.AltMaskValue
	workspaceMods = gdk.ControlMaskValue | gdk.ShiftMaskValue
	linkMods      = gdk.ControlMaskValue
)

// capNames are the modifiers as the keys show them: "Ctrl Shift T".
var capNames = map[string]string{"ctrl": "Ctrl", "shift": "Shift", "alt": "Alt", "super": "Super"}

const capSep = " "
