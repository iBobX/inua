package main

import (
	"errors"
	"testing"

	"github.com/lyricat/inua/internal/asker"
	"github.com/lyricat/inua/internal/keyring"
	"github.com/lyricat/inua/internal/passask"
	"github.com/lyricat/inua/internal/pinentry"
)

// What it prints and how it exits: the answer and a newline with 0, or
// "yes" for a host's key; a cancel, "no" and a failure exit 1.
func TestAnswer(t *testing.T) {
	ok := passask.Reply{Answer: pinentry.Answer{OK: true, Pin: "secret"}, Result: keyring.Entered}
	ask := func(prompt, kind string) (pinentry.Request, bool) {
		return asker.Question([]string{prompt}, kind, "", 0)
	}
	secret, _ := ask("u@host's password:", "")
	host, yesNo := ask("Are you sure you want to continue connecting (yes/no/[fingerprint])?", "")
	confirm, _ := ask("Allow use of key?", "confirm")
	note, _ := ask("Touch your key", "none")
	for _, tc := range []struct {
		name  string
		q     pinentry.Request
		yesNo bool
		r     passask.Reply
		out   string
		exit0 bool
	}{
		{"a secret", secret, false, ok, "secret\n", true},
		{"a host's key", host, yesNo, passask.Reply{Answer: pinentry.Answer{OK: true}, Result: keyring.Entered}, "yes\n", true},
		{"a confirm", confirm, false, passask.Reply{Answer: pinentry.Answer{OK: true}, Result: keyring.Entered}, "", true},
		{"a note", note, false, passask.Reply{Answer: pinentry.Answer{OK: true}, Result: keyring.Entered}, "", true},
		{"cancelled", secret, false, passask.Reply{Result: keyring.Cancelled}, "", false},
		{"not ok", confirm, false, passask.Reply{Answer: pinentry.Answer{NotOK: true}, Result: keyring.Entered}, "", false},
		{"failed", secret, false, passask.Reply{Result: keyring.Failed, Err: errors.New("no shell")}, "", false},
	} {
		if out, exit0 := answer(tc.q, tc.yesNo, tc.r); out != tc.out || exit0 != tc.exit0 {
			t.Errorf("%s: %q, exit 0 %v", tc.name, out, exit0)
		}
	}
}

// Through Mister Morph, a command not approved says why in its words,
// which tell the assistant a declined command from a failed one; a failure
// says nothing more.
func TestMorphMarker(t *testing.T) {
	for _, tc := range []struct {
		late bool
		r    passask.Reply
		want string
	}{
		{false, passask.Reply{Result: keyring.Cancelled}, passask.ElevateDeclined},
		{true, passask.Reply{Result: keyring.Failed}, passask.ElevateTimedOut},
		{false, passask.Reply{Result: keyring.Failed, Err: errors.New("no shell")}, ""},
	} {
		if got := morphMarker(tc.late, tc.r); got != tc.want {
			t.Errorf("%v %+v: %q", tc.late, tc.r, got)
		}
	}
}
