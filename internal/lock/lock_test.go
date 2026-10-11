package lock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeHelper behaves like unix_chkpwd for the password "right": it checks
// the user argument and the NUL-terminated password on stdin.
func fakeHelper(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "chkpwd")
	script := `#!/bin/sh
[ "$1" = alice ] && [ "$2" = nonull ] || exit 9
got=$(od -An -c | tr -d ' \n')
[ "$got" = 'right\0' ] && exit 0
exit 7
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCheck(t *testing.T) {
	h := fakeHelper(t)
	ctx := context.Background()
	if err := Check(ctx, h, "alice", "right"); err != nil {
		t.Errorf("right password: %v", err)
	}
	for _, pw := range []string{"wrong", "", "right\x00extra", "righ"} {
		if err := Check(ctx, h, "alice", pw); !errors.Is(err, ErrWrong) {
			t.Errorf("password %q: %v, want ErrWrong", pw, err)
		}
	}
	if err := Check(ctx, filepath.Join(t.TempDir(), "missing"), "alice", "right"); err == nil || errors.Is(err, ErrWrong) {
		t.Errorf("missing helper: %v, want an error that is not ErrWrong", err)
	}
}

func TestBackoff(t *testing.T) {
	for failures, want := range map[int]time.Duration{
		0: 0, 2: 0, 3: 2 * time.Second, 4: 4 * time.Second, 6: 16 * time.Second, 7: 30 * time.Second, 50: 30 * time.Second,
	} {
		if got := Backoff(failures); got != want {
			t.Errorf("Backoff(%d) = %v, want %v", failures, got, want)
		}
	}
}
