package proc

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
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
