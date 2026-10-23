package webterm

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNoKeyring is a computer whose keychain could not keep the token.
var ErrNoKeyring = errors.New("the login keychain could not keep the token")

// account is the token's account in the keychain item.
const account = "inua-term"

// Token is the page's token from the login keychain, made and kept there
// the first time; renew makes a new one, and the links with the old stop
// working.
func Token(ctx context.Context, renew bool) (string, error) {
	if !renew {
		out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-s", Schema, "-a", account, "-w").Output()
		if t := strings.TrimSpace(string(out)); err == nil && t != "" {
			return t, nil
		}
		// Not there yet (security's exit 44), or not readable: a new one.
	}
	token, err := newToken()
	if err != nil {
		return "", err
	}
	// Through security's own command line on stdin, so the token is in no
	// process's arguments for ps to show.
	cmd := exec.CommandContext(ctx, "security", "-i")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -U -s %s -a %s -l %q -w %s\n", Schema, account, "inua-term serve", token))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %v %s", ErrNoKeyring, err, strings.TrimSpace(string(out)))
	}
	// security -i reports a command that failed in its output alone: the
	// token read back says it was kept.
	if kept, err := exec.CommandContext(ctx, "security", "find-generic-password", "-s", Schema, "-a", account, "-w").Output(); err != nil || strings.TrimSpace(string(kept)) != token {
		return "", fmt.Errorf("%w: %s", ErrNoKeyring, strings.TrimSpace(string(out)))
	}
	return token, nil
}
