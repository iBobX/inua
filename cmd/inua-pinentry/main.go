// Command inua-pinentry is a pinentry for gpg-agent that asks in inua's
// password dialog. Point gpg-agent at it (pinentry-program in
// ~/.gnupg/gpg-agent.conf). With no inua shell running it runs the usual
// pinentry instead, so a passphrase can still be asked for elsewhere.
package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/pinentry"
)

// fallbacks are the pinentries run without a shell, the first found.
var fallbacks = []string{"pinentry-gnome3", "pinentry-gtk-2", "pinentry-qt", "pinentry-curses", "pinentry-tty"}

func main() {
	sock := ipc.SocketPath(cmp.Or(os.Getenv("INUA_INSTANCE"), "default"))
	if !listening(sock) {
		fallback()
	}
	ask := func(q pinentry.Request) (pinentry.Answer, error) {
		arg, err := json.Marshal(q)
		if err != nil {
			return pinentry.Answer{}, err
		}
		wait := time.Hour // the user takes their time
		if q.Timeout > 0 {
			wait = time.Duration(q.Timeout)*time.Second + 5*time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), wait)
		defer cancel()
		resp, err := ipc.Call(ctx, sock, ipc.Request{Op: "call", Target: "password", Function: string(q.Mode), Args: []string{string(arg)}})
		var a pinentry.Answer
		switch {
		case err == nil:
			a.OK, a.Pin = true, resp.Result
		case resp.Error == "cancelled":
			// a.OK false
		case resp.Error == "not ok":
			a.NotOK = true
		default:
			return a, err
		}
		return a, nil
	}
	if err := pinentry.Serve(os.Stdin, os.Stdout, ask); err != nil {
		fmt.Fprintln(os.Stderr, "inua-pinentry:", err)
		os.Exit(1)
	}
}

// listening reports whether a shell answers on its control socket.
func listening(sock string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := ipc.Call(ctx, sock, ipc.Request{Op: "show"})
	return err == nil
}

// fallback replaces this process with another pinentry, before anything
// was read from the agent.
func fallback() {
	for _, name := range fallbacks {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		err = syscall.Exec(path, append([]string{path}, os.Args[1:]...), os.Environ())
		fmt.Fprintln(os.Stderr, "inua-pinentry:", errors.Join(fmt.Errorf("running %s", path), err))
	}
	fmt.Fprintln(os.Stderr, "inua-pinentry: no inua shell, and no other pinentry")
	os.Exit(1)
}
