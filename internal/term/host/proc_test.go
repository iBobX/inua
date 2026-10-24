package host

import (
	"slices"
	"testing"
)

// macOS gives a process's arguments and environment as KERN_PROCARGS2:
// the count, the executable's path, NULs to align, the arguments, the
// environment; an empty argument is still one.
func TestParseProcArgs(t *testing.T) {
	b := []byte{3, 0, 0, 0}
	b = append(b, "/usr/local/bin/claude\x00\x00\x00\x00"...)
	b = append(b, "node\x00\x00--continue\x00"...)
	b = append(b, "INUA_TERM_AGENT=claude\x00HOME=/Users/me\x00\x00\x00"...)
	args, env := parseProcArgs(b)
	if want := []string{"node", "", "--continue"}; !slices.Equal(args, want) {
		t.Errorf("args %q, want %q", args, want)
	}
	if got := lookupEnv(env, "INUA_TERM_AGENT"); got != "claude" {
		t.Errorf("INUA_TERM_AGENT %q", got)
	}
	if got := lookupEnv(env, "HOME"); got != "/Users/me" {
		t.Errorf("HOME %q", got)
	}
	for _, short := range [][]byte{nil, {1, 0}, {1, 0, 0, 0, 'x'}} {
		if args, env := parseProcArgs(short); args != nil || env != nil {
			t.Errorf("%q: %q %q", short, args, env)
		}
	}
}
