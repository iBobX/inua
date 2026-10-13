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
	"strings"
	"syscall"
	"time"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/keyring"
	"github.com/lyricat/inua/internal/pinentry"
)

// fallbacks are the pinentries run without a shell, the first found.
var fallbacks = []string{"pinentry-gnome3", "pinentry-gtk-2", "pinentry-qt", "pinentry-curses", "pinentry-tty"}

func main() {
	sock := ipc.SocketPath(cmp.Or(os.Getenv("INUA_INSTANCE"), "default"))
	if !listening(sock) {
		fallback()
	}
	cfg, _, _ := config.Load(config.DefaultPath())
	i18n.Init(cfg.Language)
	// inua-keyring names the app and the secret when its pass asks.
	forKeyring, viaKeyring := strings.CutPrefix(os.Getenv("PINENTRY_USER_DATA"), pinentry.UserData)
	if !viaKeyring {
		forKeyring = "" // someone else's
	}
	ask := func(q pinentry.Request) (pinentry.Answer, error) {
		read := keyring.PassphraseAccess(q.Owner, forKeyring, q.Desc, keyring.StoreDir())
		describe(&q, read, forKeyring, viaKeyring)
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
		answer := keyring.Entered
		switch {
		case err == nil:
			a.OK, a.Pin = true, resp.Result
		case resp.Error == "cancelled":
			answer = keyring.Cancelled
		case resp.Error == "not ok":
			a.NotOK = true
		default:
			answer = keyring.Failed
		}
		if q.Mode == pinentry.GetPin {
			read.Answer = answer
			if err := keyring.RecordAccess(keyring.AccessPath(), read); err != nil {
				fmt.Fprintln(os.Stderr, "inua-pinentry: record:", err)
			}
		}
		if answer == keyring.Failed {
			return a, err
		}
		return a, nil
	}
	if err := pinentry.Serve(os.Stdin, os.Stdout, ask); err != nil {
		fmt.Fprintln(os.Stderr, "inua-pinentry:", err)
		os.Exit(1)
	}
}

// describe tells the dialog who wants the key and what for: the app and
// secret inua-keyring named, else the program running gpg, what it runs
// under ("pass", "zsh ← ghostty") and the pass entry gpg decrypts; and
// the key, from gpg's keyring.
func describe(q *pinentry.Request, a keyring.Access, forKeyring string, viaKeyring bool) {
	q.App, q.Chain, q.Entry = a.App, a.Chain, a.Pass
	if viaKeyring {
		q.Keyring, q.Chain = true, ""
		if app, secret, ok := strings.Cut(forKeyring, "\t"); ok {
			q.Secret = secret
			forKeyring = app
		}
		if q.App == "" {
			q.App, _, _ = strings.Cut(forKeyring, " (pid ")
		}
	}
	if k, ok := pinentry.LookupKey(q.KeyGrip); ok {
		q.Key = &k
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
