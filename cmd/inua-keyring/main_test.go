package main

import (
	"testing"

	"github.com/lyricat/inua/internal/keyring"
)

func TestReadNote(t *testing.T) {
	title, body := readNote(keyring.Access{App: "gh", Secret: "Password for 'ann' on <gh>", Chain: "claude ← zsh"})
	if title != "gh read a secret" {
		t.Errorf("title %q", title)
	}
	if want := "“Password for 'ann' on &lt;gh&gt;” · <b>gh</b> ← claude ← zsh"; body != want {
		t.Errorf("body %q, want %q", body, want)
	}
	if _, body := readNote(keyring.Access{App: "a&b", Secret: "s"}); body != "“s” · <b>a&amp;b</b>" {
		t.Errorf("no chain: body %q", body)
	}
}
