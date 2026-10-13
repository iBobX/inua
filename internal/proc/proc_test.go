package proc

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// fake writes a process into a fake /proc.
func fake(t *testing.T, pid, ppid int, comm string, args ...string) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmdline := ""
	for _, a := range args {
		cmdline += a + "\x00"
	}
	files := map[string]string{
		"cmdline": cmdline,
		"comm":    comm + "\n",
		"stat":    strconv.Itoa(pid) + " (" + comm + ") S " + strconv.Itoa(ppid) + " 1 1 0",
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestChain(t *testing.T) {
	old := root
	root = t.TempDir()
	defer func() { root = old }()
	fake(t, 2, 1, "systemd", "/usr/lib/systemd/systemd", "--user")
	fake(t, 10, 2, "Hyprland", "Hyprland")
	fake(t, 20, 10, "ghostty", "/usr/bin/ghostty")
	fake(t, 30, 20, "zsh", "-zsh")
	fake(t, 40, 30, "bash", "bash", "/usr/bin/pass", "show", "work")
	fake(t, 50, 40, "gpg", "gpg", "-d", "--quiet")
	fake(t, 60, 20, "ghostty", "/usr/bin/ghostty", "+child")
	fake(t, 70, 60, "kworker (x) y", "") // no command line: the kernel's name
	fake(t, 80, 30, "sh", "sh", "-c", "sleep 30")
	fake(t, 81, 30, "bash", "/bin/bash", "-ec", "make test")
	fake(t, 82, 30, "python3", "python3", "-m", "http.server")

	if got := Chain(50, 8); got != "gpg ← pass ← zsh ← ghostty" {
		t.Errorf("chain: %q", got)
	}
	if got := Chain(50, 2); got != "gpg ← pass" {
		t.Errorf("chain of 2: %q", got)
	}
	if got := Chain(60, 8); got != "ghostty" {
		t.Errorf("a run of one name: %q", got)
	}
	if Parent(70) != 60 || Name(70) != "kworker (x) y" {
		t.Errorf("comm with spaces and parentheses: %d %q", Parent(70), Name(70))
	}
	if Describe(20) != "ghostty (pid 20)" || Describe(99) != "pid 99" {
		t.Errorf("describe: %q %q", Describe(20), Describe(99))
	}
	if Name(80) != "sh" || Name(81) != "bash" || Name(82) != "http.server" {
		t.Errorf("code on the command line: %q %q %q", Name(80), Name(81), Name(82))
	}
	if Chain(99, 8) != "" {
		t.Error("a gone process has a chain")
	}
}

// The detail of a process: its program's path and its command line,
// quoted so that arguments with spaces stay apart, and cut when long.
func TestExeAndCommand(t *testing.T) {
	old := root
	root = t.TempDir()
	defer func() { root = old }()
	fake(t, 40, 1, "gh", "gh", "pr", "", "view", "8", "--json", "a b")
	if err := os.Symlink("/usr/bin/gh", filepath.Join(root, "40", "exe")); err != nil {
		t.Fatal(err)
	}
	if got := Exe(40); got != "/usr/bin/gh" {
		t.Errorf("exe: %q", got)
	}
	if got := Command(40); got != `gh pr "" view 8 --json "a b"` {
		t.Errorf("command: %q", got)
	}
	fake(t, 41, 1, "cat", "cat", strings.Repeat("é", maxCommand))
	if got := Command(41); len(got) > maxCommand+len("…") || !strings.HasSuffix(got, "…") || !utf8.ValidString(got) {
		t.Errorf("long command: %d bytes", len(got))
	}
	// Chrome rewrites its command line as one string.
	fake(t, 42, 1, "chrome", "/opt/google/chrome/chrome --user-data-dir=/tmp/x/chrome-profile --no-first-run")
	if Name(42) != "chrome" || Command(42) != "/opt/google/chrome/chrome --user-data-dir=/tmp/x/chrome-profile --no-first-run" {
		t.Errorf("rewritten command line: %q %q", Name(42), Command(42))
	}
	if Exe(99) != "" || Command(99) != "" || Exe(0) != "" {
		t.Error("a gone process has a program")
	}
}
