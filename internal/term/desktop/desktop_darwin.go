package desktop

import (
	"os/exec"
	"strings"

	"github.com/lyricat/inua/internal/term/host"
)

// New is the Mac's desktop: notifications through osascript. A
// notification posted this way opens nothing when clicked, and there is
// no window to raise yet: the pane is shown to the clients there are.
func New() host.Desktop { return mac{} }

type mac struct{}

func (mac) Notify(summary, body string, show func()) error {
	script := "display notification " + quote(body) + " with title " + quote(summary)
	return exec.Command("osascript", "-e", script).Run()
}

func (mac) Raise() {}

// quote is s as an AppleScript string.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
