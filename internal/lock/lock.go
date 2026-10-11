// Package lock checks the user's password for the lock screen, without
// cgo: it asks unix_chkpwd, the setgid helper pam_unix itself uses, which
// may only check the password of the user running it.
package lock

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Helper is unix_chkpwd's usual path.
const Helper = "/usr/sbin/unix_chkpwd"

// ErrWrong is a password that is not the user's.
var ErrWrong = errors.New("wrong password")

// Check asks helper whether password is user's. It returns nil for the
// right password, ErrWrong for a wrong one, and another error when the
// password could not be checked at all.
func Check(ctx context.Context, helper, user, password string) error {
	if strings.ContainsRune(password, 0) {
		return ErrWrong // the helper reads up to a NUL
	}
	cmd := exec.CommandContext(ctx, helper, user, "nonull")
	// As pam_unix sends it: the password and its terminating NUL.
	cmd.Stdin = strings.NewReader(password + "\x00")
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		return fmt.Errorf("%s: %w", helper, ctx.Err())
	case errors.As(err, &exit) && exit.ExitCode() > 0:
		return ErrWrong
	}
	return fmt.Errorf("%s: %w", helper, err)
}

// Backoff is how long the lock screen waits before taking another password
// after failures wrong ones in a row: none for the first three, then 2, 4,
// 8 … seconds, at most 30.
func Backoff(failures int) time.Duration {
	if failures < 3 {
		return 0
	}
	return min(30*time.Second, time.Second<<min(failures-2, 5))
}
