package shell

// Capture is why the screen may be captured, which hides the password
// dialog's private details on its own.
type Capture string

const (
	NoCapture  Capture = ""
	Recording  Capture = "recording"  // a screencast or recording runs
	Screenshot Capture = "screenshot" // inua's screenshot tool is taking one
)

// PassMask says whether the password dialog hides the key's owner, its
// fingerprint and the pass entry: when the user asked, or while the
// screen is captured, unless the user showed them during that capture.
// The choice lasts for the shell's run, across dialogs.
type PassMask struct {
	hide      bool // the user's choice
	shown     bool // shown by the user during this capture
	casts     int  // screencasts running, as Hyprland told
	recording bool // inua records the screen
	shooting  bool // inua's screenshot tool is open or capturing
}

// Hidden reports whether the details are hidden.
func (m PassMask) Hidden() bool {
	return m.hide || m.Capture() != NoCapture && !m.shown
}

// Auto reports whether they are hidden only for a capture, which the
// dialog says.
func (m PassMask) Auto() bool { return !m.hide && m.Hidden() }

// Capture is what captures the screen now.
func (m PassMask) Capture() Capture {
	switch {
	case m.casts > 0 || m.recording:
		return Recording
	case m.shooting:
		return Screenshot
	}
	return NoCapture
}

// Toggle shows the details if hidden, else hides them.
func (m *PassMask) Toggle() {
	if m.Hidden() {
		m.hide, m.shown = false, m.Capture() != NoCapture
		return
	}
	m.hide, m.shown = true, false
}

// Screencast counts a screencast starting (on) or ending, as Hyprland's
// screencast event says.
func (m *PassMask) Screencast(on bool) {
	if on {
		m.casts++
	} else {
		m.casts = max(0, m.casts-1)
	}
	m.settle()
}

// Recording says inua started or stopped recording the screen: hidden
// from its first frame, before Hyprland tells of the screencast.
func (m *PassMask) Recording(on bool) {
	m.recording = on
	m.settle()
}

// Shooting says inua's screenshot tool started or finished.
func (m *PassMask) Shooting(on bool) {
	m.shooting = on
	m.settle()
}

// settle forgets that the user showed the details once nothing captures:
// the next capture hides them again.
func (m *PassMask) settle() {
	if m.Capture() == NoCapture {
		m.shown = false
	}
}
