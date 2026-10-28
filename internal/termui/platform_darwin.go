package termui

import "codeberg.org/puregotk/puregotk/v4/gdk"

// On macOS the window's keys are on Command, as a Mac app's are, and
// Control stays the programs'. GTK reports Command as Meta; it is
// "super" here, as in Ghostty's keys, so a chord names it the same way
// on both systems.

func keyMods(state gdk.ModifierType) gdk.ModifierType {
	if state&gdk.MetaMaskValue != 0 {
		state = state&^gdk.MetaMaskValue | gdk.SuperMaskValue
	}
	return state
}

// The keys the settings do not change: Command with Option and an arrow
// moves the focus, Command with Control resizes; Command and a digit
// shows a session, with Shift a workspace; Command+click opens a link.
const (
	focusMods     = gdk.SuperMaskValue | gdk.AltMaskValue
	resizeMods    = gdk.SuperMaskValue | gdk.ControlMaskValue
	sessionMods   = gdk.SuperMaskValue
	workspaceMods = gdk.SuperMaskValue | gdk.ShiftMaskValue
	linkMods      = gdk.SuperMaskValue
)

// capNames are the modifiers as a Mac's keys show them, written together:
// "⌘⇧T".
var capNames = map[string]string{"ctrl": "⌃", "shift": "⇧", "alt": "⌥", "super": "⌘"}

const capSep = ""
