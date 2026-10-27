// Command inua-askpass asks in inua's password dialog, and on the web
// terminal's pages open, for programs that ask without a terminal: what
// SSH_ASKPASS, SUDO_ASKPASS and GIT_ASKPASS run, with the question as
// the argument. It prints the answer and a newline and exits 0; any other
// exit is a cancel. SSH_ASKPASS_PROMPT says what ssh asks: unset a
// secret, "confirm" a yes or no (ssh-add -c), "none" a note (touch the
// security key). With no inua shell running it runs another askpass.
package main

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/lyricat/inua/internal/asker"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/keyring"
	"github.com/lyricat/inua/internal/passask"
	"github.com/lyricat/inua/internal/pinentry"
)

// fallbacks are the askpass programs run without a shell, the first found.
var fallbacks = []string{
	"ksshaskpass", "lxqt-openssh-askpass", "ssh-askpass",
	"/usr/lib/ssh/ssh-askpass", "/usr/libexec/openssh/ssh-askpass", "/usr/lib/openssh/gnome-ssh-askpass",
}

func main() {
	sock := ipc.SocketPath(cmp.Or(os.Getenv("INUA_INSTANCE"), "default"))
	if !passask.Listening(sock) {
		passask.Fallback("inua-askpass", fallbacks)
	}
	cfg, _, _ := config.Load(config.DefaultPath())
	i18n.Init(cfg.Language)
	// Who asks: ssh, sudo or git, and what runs it; for sudo, the command
	// it runs as root. The shell and the terminal host make the question
	// again from this process, not from what it sends (internal/asker).
	q, yesNo := asker.Question(os.Args[1:], os.Getenv("SSH_ASKPASS_PROMPT"), os.Getenv(asker.ElevateCommandEnv), os.Getppid())
	deadline := time.Now().Add(time.Hour) // the user takes their time
	if s, err := strconv.ParseInt(os.Getenv(passask.ElevateDeadlineEnv), 10, 64); err == nil && s > 0 {
		deadline = time.Unix(s, 0)
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	r := passask.Ask(ctx, sock, q)
	late := ctx.Err() != nil
	cancel()
	out, ok := answer(q, yesNo, r)
	if r.Err != nil && !late {
		fmt.Fprintln(os.Stderr, "inua-askpass:", r.Err)
	}
	if !ok {
		// Mister Morph (tools.bash.elevation.askpass) tells the assistant
		// a declined command from a failed one by these lines.
		if m := morphMarker(late, r); m != "" && os.Getenv(passask.ElevateCommandEnv) != "" {
			fmt.Fprintln(os.Stderr, m)
		}
		os.Exit(1)
	}
	fmt.Print(out)
}

// morphMarker is the line telling Mister Morph why an approval did not
// come: timed out (late) or declined; "" for a failure, which it reports
// as such.
func morphMarker(late bool, r passask.Reply) string {
	switch {
	case late:
		return passask.ElevateTimedOut
	case r.Result == keyring.Failed:
		return ""
	}
	return passask.ElevateDeclined
}

// answer is what to print for r, a line, and whether to exit 0: the text
// typed, or "yes" for a host's key; nothing for a yes or no ssh reads by
// the exit alone, or a note.
func answer(q pinentry.Request, yesNo bool, r passask.Reply) (string, bool) {
	if r.Result != keyring.Entered || !r.Answer.OK {
		return "", false
	}
	switch {
	case yesNo:
		return "yes\n", true
	case q.Mode == pinentry.GetPin:
		return r.Answer.Pin + "\n", true
	}
	return "", true
}
