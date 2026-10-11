package system

import (
	"os/exec"
	"strings"
)

// dndMode is the mako mode that hides notifications. It needs a matching
// section in mako's config: [mode=do-not-disturb] invisible=1
const dndMode = "do-not-disturb"

// DNDAvailable reports whether mako's control tool is installed.
func DNDAvailable() bool {
	_, err := exec.LookPath("makoctl")
	return err == nil
}

// DND reports whether do not disturb is on.
func DND() (bool, error) {
	out, err := exec.Command("makoctl", "mode").Output()
	if err != nil {
		return false, err
	}
	return hasMode(string(out), dndMode), nil
}

// SetDND turns do not disturb on or off.
func SetDND(on bool) error {
	flag := "-r"
	if on {
		flag = "-a"
	}
	return exec.Command("makoctl", "mode", flag, dndMode).Run()
}

func hasMode(out, mode string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == mode {
			return true
		}
	}
	return false
}
