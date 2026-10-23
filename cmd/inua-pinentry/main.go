// Command inua-pinentry is a pinentry for gpg-agent that asks in inua's
// password dialog, and on the web terminal's pages open (a phone's, over
// inua-termd): the first answer wins. Point gpg-agent at it
// (pinentry-program in ~/.gnupg/gpg-agent.conf). With no inua shell
// running it runs the usual pinentry instead, so a passphrase can still
// be asked for elsewhere.
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
	"github.com/lyricat/inua/internal/term/host"
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
		wait := time.Hour // the user takes their time
		if q.Timeout > 0 {
			wait = time.Duration(q.Timeout)*time.Second + 5*time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), wait)
		defer cancel()
		// The desktop's dialog, and the web terminal's pages open (a
		// phone's): the first answer wins, and the other asks no more.
		desk := make(chan reply, 1)
		go func() { desk <- askShell(ctx, sock, q) }()
		pages := make(chan reply, 1)
		if s, ok := forPages(q); ok {
			go func() { pages <- askPages(ctx, s) }()
		}
		var r, failed reply
	wait:
		for desk != nil || pages != nil {
			select {
			case r = <-desk:
				desk = nil
				if r.answer != keyring.Failed || pages == nil {
					break wait
				}
				failed = r // a page may still answer
			case r = <-pages:
				pages = nil
				if !r.none {
					break wait
				}
				if desk == nil { // no page open, and the dialog failed
					r = failed
					break wait
				}
			}
		}
		cancel()
		if q.Mode == pinentry.GetPin {
			read.Answer = r.answer
			if err := keyring.RecordAccess(keyring.AccessPath(), read); err != nil {
				fmt.Fprintln(os.Stderr, "inua-pinentry: record:", err)
			}
		}
		if r.answer == keyring.Failed {
			return r.a, r.err
		}
		return r.a, nil
	}
	if err := pinentry.Serve(os.Stdin, os.Stdout, ask); err != nil {
		fmt.Fprintln(os.Stderr, "inua-pinentry:", err)
		os.Exit(1)
	}
}

// reply is how a question was answered: gpg's answer, and the keyring's
// word for it (keyring.Entered, Cancelled, Failed with err); none when no
// web page was open to ask.
type reply struct {
	a      pinentry.Answer
	answer string
	err    error
	none   bool
}

// askShell asks in the shell's dialog.
func askShell(ctx context.Context, sock string, q pinentry.Request) reply {
	arg, err := json.Marshal(q)
	if err != nil {
		return reply{answer: keyring.Failed, err: err}
	}
	resp, err := ipc.Call(ctx, sock, ipc.Request{Op: "call", Target: "password", Function: string(q.Mode), Args: []string{string(arg)}})
	r := reply{answer: keyring.Entered}
	switch {
	case err == nil:
		r.a.OK, r.a.Pin = true, resp.Result
	case resp.Error == "cancelled":
		r.answer = keyring.Cancelled
	case resp.Error == "not ok":
		r.a.NotOK = true
	default:
		r.answer, r.err = keyring.Failed, err
	}
	return r
}

// forPages is what the web terminal's pages show of q: a passphrase, a
// yes or no, or a note. A new passphrase (asked twice) and inua-keyring's
// approvals are the desktop's alone.
func forPages(q pinentry.Request) (host.Secret, bool) {
	switch {
	case q.Repeat != "":
		return host.Secret{}, false
	case q.Mode != pinentry.GetPin && q.Mode != pinentry.Confirm && q.Mode != pinentry.Message:
		return host.Secret{}, false
	}
	who := cmp.Or(q.App, "gpg")
	title := i18n.Tf("%s asks for your passphrase", who)
	if q.Mode != pinentry.GetPin {
		title = i18n.Tf("%s asks", who)
	}
	if q.Keyring {
		title = i18n.Tf("inua-keyring asks for your passphrase, for %s", who)
	}
	return host.Secret{
		Mode: string(q.Mode), Title: title, Desc: q.Desc, Prompt: q.Prompt, Error: q.Error,
		OK: q.OK, Cancel: q.Cancel, NotOK: q.NotOK,
	}, true
}

// askPages asks the web terminal's pages open, through its host; none
// when no host runs or no page is open. ctx ending takes the question
// back.
func askPages(ctx context.Context, s host.Secret) reply {
	c, err := host.Dial(host.SocketPath(), nil, nil)
	if err != nil {
		return reply{none: true}
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	defer c.Close()
	answer, pin, err := c.AskSecret(s)
	switch {
	case err != nil || answer == "":
		return reply{none: true}
	case answer == host.AnswerOK:
		return reply{a: pinentry.Answer{OK: true, Pin: pin}, answer: keyring.Entered}
	case answer == host.AnswerNo:
		return reply{a: pinentry.Answer{NotOK: true}, answer: keyring.Entered}
	}
	return reply{answer: keyring.Cancelled}
}

// describe tells the dialog who wants the key and what for: the app and
// secret inua-keyring named, else the program running gpg; what that runs
// under ("pass", "zsh ← ghostty"); the pass entry gpg decrypts; and the
// key, from gpg's keyring.
func describe(q *pinentry.Request, a keyring.Access, forKeyring string, viaKeyring bool) {
	q.App, q.Chain, q.Entry = a.App, a.Chain, a.Pass
	if viaKeyring {
		q.Keyring = true
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
