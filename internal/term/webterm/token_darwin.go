package webterm

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNoKeyring is a computer where the token could be kept nowhere.
var ErrNoKeyring = errors.New("the token could be kept neither in the login keychain nor in a file")

// account is the token's account in the keychain item.
const account = "inua-term"

// Token is the page's token from the login keychain, made and kept there
// the first time, or in a file when the keychain is locked and cannot ask
// (over SSH: "User interaction is not allowed"); renew makes a new one,
// and the links with the old stop working.
func Token(ctx context.Context, renew bool) (string, error) {
	t, err := keptToken(renew, keychain{ctx}, tokenFile())
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNoKeyring, err)
	}
	return t, nil
}

// keychain is the login keychain, through the security command.
type keychain struct{ ctx context.Context }

func (k keychain) load() (string, error) {
	out, err := exec.CommandContext(k.ctx, "security", "find-generic-password", "-s", Schema, "-a", account, "-w").Output()
	return strings.TrimSpace(string(out)), err
}

func (k keychain) save(token string) error {
	// Through security's own command line on stdin, so the token is in no
	// process's arguments for ps to show.
	cmd := exec.CommandContext(k.ctx, "security", "-i")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -U -s %s -a %s -l %q -w %s\n", Schema, account, "inua-term serve", token))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("the login keychain: %v %s", err, strings.TrimSpace(string(out)))
	}
	// security -i reports a command that failed in its output alone: the
	// token read back says it was kept.
	if kept, err := k.load(); err != nil || kept != token {
		return fmt.Errorf("the login keychain: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
