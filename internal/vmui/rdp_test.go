package vmui

import (
	"io"
	"slices"
	"strings"
	"testing"
)

func TestRDPArgsFollowMonitorScale(t *testing.T) {
	args := rdpArgs("127.0.0.1", 3389, "Inua", "secret", &rdpDisplay{Width: 1920, Height: 1080, Scale: 2})
	for _, want := range []string{"/size:1536x864", "/scale-desktop:200", "/scale-device:180"} {
		if !slices.Contains(args, want) {
			t.Errorf("2x display: missing %q in %v", want, args)
		}
	}
	args = rdpArgs("127.0.0.1", 3389, "Inua", "secret", &rdpDisplay{Width: 1280, Height: 1024, Scale: 1})
	if !slices.Contains(args, "/scale-desktop:100") || !slices.Contains(args, "/size:1024x819") {
		t.Errorf("1x display: %v", args)
	}
	if slices.Contains(args, "/f") {
		t.Errorf("rdp opens fullscreen: %v", args)
	}
	if args := rdpArgs("127.0.0.1", 3389, "Inua", "secret", nil); slices.Contains(args, "/size:0x0") {
		t.Errorf("unknown display got a size: %v", args)
	}
}

// FreeRDP 3 takes its arguments on standard input: the password must not be
// on the command line, which any user of the machine can read.
func TestRDPPasswordStaysOffTheCommandLine(t *testing.T) {
	args := rdpArgs("127.0.0.1", 3389, "Inua", "s3cret", nil)
	cmd := rdpCommand("/usr/bin/sdl-freerdp3", args, true)
	if want := []string{"/usr/bin/sdl-freerdp3", "/args-from:stdin"}; !slices.Equal(cmd.Args, want) {
		t.Fatalf("command line = %q, want %q", cmd.Args, want)
	}
	in, err := io.ReadAll(cmd.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Join(args, "\n") + "\n"; string(in) != want {
		t.Errorf("standard input = %q, want %q", in, want)
	}
	// FreeRDP 2 has no /args-from.
	if cmd := rdpCommand("/usr/bin/xfreerdp", args, false); !slices.Contains(cmd.Args, "/p:s3cret") || cmd.Stdin != nil {
		t.Errorf("FreeRDP 2 command = %q", cmd.Args)
	}
}

func TestFreeRDPVersion(t *testing.T) {
	for _, tc := range []struct {
		name, bin string
		want      bool
	}{
		{"sdl-freerdp3", "", true},
		{"sdl-freerdp", "", true},
		{"wlfreerdp3", "", true},
		{"xfreerdp3", "", true},
	} {
		if got := freerdp3(tc.name, tc.bin); got != tc.want {
			t.Errorf("freerdp3(%q) = %v", tc.name, got)
		}
	}
	for version, want := range map[string]int{
		"This is FreeRDP version 3.10.3 (3.10.3)\n": 3,
		"This is FreeRDP version 2.11.7 (2.11.7)\n": 2,
		"xfreerdp: command not found":               0,
	} {
		if got := freerdpMajor(version); got != want {
			t.Errorf("freerdpMajor(%q) = %d, want %d", version, got, want)
		}
	}
}
