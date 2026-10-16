package keyring

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeProc makes procRoot/PID for a process running exe with args, in
// cwd, with env.
func fakeProc(t *testing.T, root string, pid, exe, cwd string, args, env []string) {
	t.Helper()
	dir := filepath.Join(root, pid)
	if err := os.MkdirAll(filepath.Join(dir, "root"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.Symlink(exe, filepath.Join(dir, "exe"))
	os.Symlink(cwd, filepath.Join(dir, "cwd"))
	os.WriteFile(filepath.Join(dir, "cmdline"), []byte(strings.Join(args, "\x00")+"\x00"), 0o644)
	os.WriteFile(filepath.Join(dir, "environ"), []byte(strings.Join(env, "\x00")+"\x00"), 0o644)
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestIdentify(t *testing.T) {
	root := t.TempDir()
	old := procRoot
	procRoot = root
	t.Cleanup(func() { procRoot = old })
	bin := t.TempDir()
	app := filepath.Join(bin, "gh")
	os.WriteFile(app, []byte("gh v1"), 0o755)

	fakeProc(t, root, "10", app, bin, []string{app, "auth", "token"}, []string{"HOME=/home/x"})
	id, err := Identify(10)
	if err != nil {
		t.Fatal(err)
	}
	if id.Exe != app || id.Hash != sum([]byte("gh v1")) || id.System || id.Tampered != "" || id.Name() != "gh" {
		t.Errorf("gh: %+v", id)
	}

	// A new version of the file is hashed again.
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(app, []byte("gh v2, longer"), 0o755)
	if id, _ := Identify(10); id.Hash != sum([]byte("gh v2, longer")) {
		t.Errorf("after a change: %s", id.Hash)
	}

	// Behind an interpreter, the script is the app.
	py := filepath.Join(bin, "python3")
	os.WriteFile(py, []byte("python"), 0o755)
	os.WriteFile(filepath.Join(bin, "tool.py"), []byte("print()"), 0o644)
	fakeProc(t, root, "11", py, bin, []string{"python3", "-u", "tool.py", "--flag"}, nil)
	id, _ = Identify(11)
	if id.Script != filepath.Join(bin, "tool.py") || id.Name() != "tool.py" {
		t.Errorf("script: %+v", id)
	}
	if want := sum([]byte(sum([]byte("python")) + sum([]byte("print()")))); id.Hash != want {
		t.Errorf("script hash %s, want %s", id.Hash, want)
	}
	fakeProc(t, root, "12", py, bin, []string{"python3", "-m", "keyring", "get"}, nil)
	if id, _ := Identify(12); id.Script != "-m keyring" || id.Hash != sum([]byte("python")) || id.Name() != "python3" {
		t.Errorf("-m: %+v", id)
	}

	// LD_PRELOAD: not to be trusted as it runs.
	fakeProc(t, root, "13", app, bin, []string{app}, []string{"LD_PRELOAD=/tmp/x.so"})
	if id, _ := Identify(13); id.Tampered != "LD_PRELOAD is set" {
		t.Errorf("tampered: %q", id.Tampered)
	}

	// Flatpak: the app ID.
	fakeProc(t, root, "14", app, bin, []string{app}, nil)
	os.WriteFile(filepath.Join(root, "14", "root", ".flatpak-info"), []byte("[Application]\nname=org.example.App\nruntime=x\n"), 0o644)
	if id, _ := Identify(14); id.Flatpak != "org.example.App" || id.Name() != "org.example.App" {
		t.Errorf("flatpak: %+v", id)
	}
}

func TestSystemProgram(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	sh, _ = filepath.EvalSymlinks(sh)
	if !rootOwnedDirs(filepath.Dir(sh)) {
		t.Skip(sh + " is not under root's directories")
	}
	if _, system, err := fileHash(sh, sh); err != nil || !system {
		t.Errorf("%s: system %v %v", sh, system, err)
	}
	if rootOwnedDirs(t.TempDir()) {
		t.Error("a temporary directory counted as root's")
	}
}

func TestTrust(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "index.json"), newMemory())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "trust.json")
	tr := NewTrust(path, store.TrustKey)
	gh := Identity{Exe: "/home/x/.local/bin/gh", Hash: "aaa"}
	chrome := Identity{Exe: "/opt/google/chrome/chrome", Hash: "c1", System: true}
	item := ItemKey(Login, "1")

	if v, _, checked := tr.Decide(item, gh); v != Unknown || !checked {
		t.Errorf("no file: %v %v", v, checked)
	}
	if err := tr.Add(item, Trusted{Identity: gh, Since: time.Now()}, false); err != nil {
		t.Fatal(err)
	}
	tr.Add(item, Trusted{Identity: chrome, Since: time.Now()}, false)
	if v, _, _ := tr.Decide(item, gh); v != Allowed {
		t.Errorf("gh trusted: %v", v)
	}
	if v, _, _ := tr.Decide(ItemKey(Login, "2"), gh); v != Unknown {
		t.Errorf("another item: %v", v)
	}
	// A user-writable program that changed is asked again; a system one
	// updated is not.
	gh2 := gh
	gh2.Hash = "bbb"
	if v, was, _ := tr.Decide(item, gh2); v != Changed || was == nil || was.Hash != "aaa" {
		t.Errorf("gh changed: %v %+v", v, was)
	}
	chrome2 := chrome
	chrome2.Hash = "c2"
	if v, _, _ := tr.Decide(item, chrome2); v != Allowed {
		t.Errorf("chrome updated: %v", v)
	}
	chrome2.System = false // now somewhere the user can write
	if v, _, _ := tr.Decide(item, chrome2); v != Changed {
		t.Errorf("chrome no longer root's: %v", v)
	}
	tampered := gh
	tampered.Tampered = "LD_PRELOAD is set"
	if v, _, _ := tr.Decide(item, tampered); v != Tampered {
		t.Errorf("tampered: %v", v)
	}

	// Trusting the new version replaces the old.
	tr.Add(item, Trusted{Identity: gh2}, false)
	if apps := ReadTrust(path)[item]; len(apps) != 2 {
		t.Errorf("%d apps after re-trusting gh", len(apps))
	}

	// Read again from the file, by another Trust: still sealed.
	again := NewTrust(path, store.TrustKey)
	if v, _, checked := again.Decide(item, gh2); v != Allowed || !checked || again.Problem() != "" {
		t.Errorf("read again: %v %v %q", v, checked, again.Problem())
	}

	// Edited outside: nothing is trusted, until resealed.
	b, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(strings.Replace(string(b), "/home/x/.local/bin/gh", "/tmp/evil", 1)), 0o600)
	evil := Identity{Exe: "/tmp/evil", Hash: "bbb"}
	if v, _, _ := tr.Decide(item, evil); v != Unknown {
		t.Errorf("edited: evil %v", v)
	}
	if v, _, _ := tr.Decide(item, chrome); v != Unknown || tr.Problem() == "" {
		t.Errorf("edited: chrome %v, problem %q", v, tr.Problem())
	}
	if err := tr.Add(item, Trusted{Identity: gh}, false); !errors.Is(err, ErrUntrusted) {
		t.Errorf("added to a list changed outside: %v", err)
	}
	// Removing from it trusts less, but must not seal what was added.
	if err := tr.Remove(item, chrome); err != nil {
		t.Fatal(err)
	}
	if tr.Problem() == "" {
		t.Error("removing from a list changed outside sealed it")
	}
	if v, _, _ := tr.Decide(item, evil); v != Unknown {
		t.Errorf("after removing: evil %v", v)
	}
	if apps := ReadTrust(path)[item]; len(apps) != 1 || apps[0].Exe != "/tmp/evil" {
		t.Errorf("after removing chrome: %+v", apps)
	}
	if err := tr.Reseal(); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := tr.Decide(item, evil); v != Allowed || tr.Problem() != "" {
		t.Errorf("resealed: %v %q", v, tr.Problem())
	}

	// Removed.
	tr.Remove(item, evil)
	if v, _, _ := tr.Decide(item, evil); v != Unknown {
		t.Errorf("removed: %v", v)
	}
	tr.Forget(item)
	if _, ok := ReadTrust(path)[item]; ok {
		t.Error("a deleted item kept its apps")
	}

	// Flatpak apps by their ID.
	fp := Identity{Exe: "/app/bin/x", Flatpak: "org.example.App"}
	tr.Add(item, Trusted{Identity: fp}, false)
	if v, _, _ := tr.Decide(item, Identity{Exe: "/other", Flatpak: "org.example.App"}); v != Allowed {
		t.Errorf("flatpak: %v", v)
	}
	if v, _, _ := tr.Decide(item, Identity{Exe: "/app/bin/x"}); v != Unknown {
		t.Errorf("the flatpak's program outside it: %v", v)
	}
}

// TestTrustLocked: while the key can't be read without the passphrase,
// the seal is not checked; Decide says so.
func TestTrustLocked(t *testing.T) {
	back := newCounting()
	store, _ := Open(filepath.Join(t.TempDir(), "index.json"), back)
	path := filepath.Join(t.TempDir(), "trust.json")
	gh := Identity{Exe: "/usr/bin/gh", Hash: "a", System: true}
	if err := NewTrust(path, store.TrustKey).Add(ItemKey(Login, "1"), Trusted{Identity: gh}, false); err != nil {
		t.Fatal(err)
	}
	back.locked = true
	tr := NewTrust(path, store.TrustKey)
	if v, _, checked := tr.Decide(ItemKey(Login, "1"), gh); v != Allowed || checked {
		t.Errorf("locked: %v, checked %v", v, checked)
	}
	back.locked = false
	if v, _, checked := tr.Decide(ItemKey(Login, "1"), gh); v != Allowed || !checked {
		t.Errorf("unlocked: %v, checked %v", v, checked)
	}
}

// A path rule lets any program at its path read every secret, whatever
// its hash; the process still must not be tampered with, an interpreter
// is matched by its script, and the rule is sealed with the rest.
func TestPathRules(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "index.json"), newMemory())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "trust.json")
	tr := NewTrust(path, store.TrustKey)
	item := ItemKey(Login, "1")
	gh := Identity{Exe: "/home/x/.local/bin/gh", Hash: "aaa"}
	tr.Add(item, Trusted{Identity: gh}, false)
	before, _ := os.ReadFile(path)

	claude := Identity{Exe: "/home/x/.local/share/claude/versions/2.1.300", Hash: "day1"}
	if v, _, _ := tr.Decide(item, claude); v != Unknown {
		t.Errorf("before the rule: %v", v)
	}
	if err := tr.AddPath("/home/x/.local/share/claude/versions/*", false); err != nil {
		t.Fatal(err)
	}
	claude2 := Identity{Exe: "/home/x/.local/share/claude/versions/2.1.301", Hash: "day2"}
	for _, id := range []Identity{claude, claude2} {
		if v, _, checked := tr.Decide(ItemKey(Login, "any"), id); v != Allowed || !checked {
			t.Errorf("%s: %v %v", id.Exe, v, checked)
		}
	}
	gh2 := gh
	gh2.Hash = "bbb"
	if v, _, _ := tr.Decide(item, gh2); v != Changed {
		t.Errorf("gh, no rule: %v", v)
	}
	tr.AddPath("/home/x/.local/bin/gh", false)
	if v, _, _ := tr.Decide(item, gh2); v != Allowed {
		t.Errorf("gh changed, with a rule: %v", v)
	}
	for _, id := range []Identity{
		{Exe: "/home/x/.local/share/claude/versions/a/b", Hash: "x"}, // "*" stays in its directory
		{Exe: "/home/x/.local/share/claude/versions/1", Hash: "x", Tampered: "LD_PRELOAD is set"},
		{Exe: "/home/x/.local/share/claude/versions/1", Flatpak: "org.example.App"},
	} {
		if v, _, _ := tr.Decide(item, id); v == Allowed {
			t.Errorf("%+v allowed", id)
		}
	}

	// An interpreter: the script is the app.
	tr.AddPath("/home/x/tools/*.py", false)
	if v, _, _ := tr.Decide(item, Identity{Exe: "/usr/bin/python3", Script: "/home/x/tools/sync.py"}); v != Allowed {
		t.Errorf("script: %v", v)
	}
	tr.AddPath("/usr/bin/python3", false)
	if v, _, _ := tr.Decide(item, Identity{Exe: "/usr/bin/python3", Script: "-c import os"}); v == Allowed {
		t.Error("an interpreter's own path let any code it runs through")
	}

	// Sealed: read again, still trusted; a rule added outside is not.
	if v, _, _ := NewTrust(path, store.TrustKey).Decide(item, claude); v != Allowed {
		t.Errorf("read again: %v", v)
	}
	if got := ReadPaths(path); len(got) != 4 || got[0].Pattern != "/home/x/.local/share/claude/versions/*" || got[0].Since.IsZero() {
		t.Errorf("paths %+v", got)
	}
	b, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(strings.Replace(string(b), `"pattern": "/home/x/tools/*.py"`, `"pattern": "/tmp/*"`, 1)), 0o600)
	if v, _, _ := tr.Decide(item, Identity{Exe: "/tmp/evil"}); v != Unknown || tr.Problem() == "" {
		t.Errorf("a rule added outside: %v %q", v, tr.Problem())
	}

	// Without rules, the list is sealed as before there were any.
	os.WriteFile(path, before, 0o600)
	if again := NewTrust(path, store.TrustKey); again.Problem() != "" {
		t.Errorf("an older list: %q", again.Problem())
	}
	tr.AddPath("/home/x/.local/bin/gh", false)
	stamp, _ := os.Stat(path)
	if err := tr.RemovePath("/nowhere/x/y"); err != nil {
		t.Fatal(err)
	}
	if now, _ := os.Stat(path); !now.ModTime().Equal(stamp.ModTime()) {
		t.Error("removing a rule the list does not hold wrote it")
	}
	tr.RemovePath("/home/x/.local/bin/gh")
	if again := NewTrust(path, store.TrustKey); again.Problem() != "" || len(ReadPaths(path)) != 0 {
		t.Errorf("after removing the rule: %q %+v", again.Problem(), ReadPaths(path))
	}
}

func TestCheckPattern(t *testing.T) {
	home := "/home/x"
	for in, want := range map[string]string{
		"~/.local/share/claude/versions/*": "/home/x/.local/share/claude/versions/*",
		"/opt/app/bin/app":                 "/opt/app/bin/app",
		"/home/x/.local/bin/":              "/home/x/.local/bin",
		"/home/x/tools/*.py":               "/home/x/tools/*.py",
	} {
		if got, err := CheckPattern(in, home); err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"*", "relative/*", "/*", "/usr/*", "/home/*/x", "~/*", "/home/x/*", "~", "/opt/[", "/opt/x/b[ad"} {
		if got, err := CheckPattern(in, home); err == nil {
			t.Errorf("%s: kept as %q", in, got)
		}
	}
}
