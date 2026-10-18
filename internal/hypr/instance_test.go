package hypr

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func writeLock(t *testing.T, runtime, sig string, pid int, wayland string) {
	t.Helper()
	dir := filepath.Join(runtime, "hypr", sig)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hyprland.lock"), fmt.Appendf(nil, "%d\n%s\n", pid, wayland), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A process ID no process has: above the kernel's largest.
const gonePID = 2147483646

func TestFindInstanceSkipsLeftovers(t *testing.T) {
	runtime := t.TempDir()
	writeLock(t, runtime, "abc_1700000000_1", gonePID, "wayland-1") // an earlier session's
	writeLock(t, runtime, "abc_1800000000_2", os.Getpid(), "wayland-2")
	// A directory without a lock: an instance that did not finish starting.
	os.MkdirAll(filepath.Join(runtime, "hypr", "abc_1600000000_3"), 0o700)

	i, err := FindInstance(runtime)
	if err != nil {
		t.Fatal(err)
	}
	want := Instance{Signature: "abc_1800000000_2", Runtime: runtime, PID: os.Getpid(), Wayland: "wayland-2"}
	if i != want {
		t.Fatalf("FindInstance = %+v, want %+v", i, want)
	}
	if p := i.Paths(); p.Request != filepath.Join(runtime, "hypr", want.Signature, ".socket.sock") {
		t.Fatalf("Paths = %+v", p)
	}
}

func TestFindInstancePrefersFirstStarted(t *testing.T) {
	runtime := t.TempDir()
	writeLock(t, runtime, "abc_1800000500_9", os.Getpid(), "wayland-2") // nested, started later
	writeLock(t, runtime, "abc_1800000000_1", os.Getpid(), "wayland-1")
	i, err := FindInstance(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if i.Wayland != "wayland-1" {
		t.Fatalf("FindInstance chose %+v, want the session's (wayland-1)", i)
	}
}

func TestFindInstanceNone(t *testing.T) {
	runtime := t.TempDir()
	if _, err := FindInstance(runtime); !errors.Is(err, ErrNoInstance) {
		t.Fatalf("no hypr directory: err = %v, want ErrNoInstance", err)
	}
	writeLock(t, runtime, "abc_1700000000_1", gonePID, "wayland-1")
	if _, err := FindInstance(runtime); !errors.Is(err, ErrNoInstance) {
		t.Fatalf("only a leftover: err = %v, want ErrNoInstance", err)
	}
}
