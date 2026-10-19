package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func load(t *testing.T, dir string) *Set {
	t.Helper()
	s, errs := Load(dir)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	return s
}

func TestDetect(t *testing.T) {
	s := load(t, "")
	for _, c := range []struct {
		argv     []string
		override string
		want     string
	}{
		{[]string{"/home/me/.local/bin/claude"}, "", "claude"},
		{[]string{"node", "/usr/lib/node_modules/.bin/codex", "--full-auto"}, "", "codex"},
		{[]string{"python3", "-m", "aider"}, "", "aider"}, // python -m aider runs aider
		{[]string{"cursor-agent"}, "", "cursor"},
		{[]string{"vim", "claude"}, "", ""},
		{[]string{"bash"}, "", ""},
		{[]string{"bash"}, "mybot", "mybot"},
		{nil, "", ""},
	} {
		got := ""
		if m := s.Detect(c.argv, c.override); m != nil {
			got = m.Type
		}
		if got != c.want {
			t.Errorf("Detect(%q, %q) = %q, want %q", c.argv, c.override, got, c.want)
		}
	}
}

func TestScreenState(t *testing.T) {
	m := load(t, "").ByType("claude")
	working := []string{"● Update(host.go)", "", "✻ Writing the attach test… (2m 14s · esc to interrupt)", "", "╭───╮", "│ > │", "╰───╯"}
	if st, _ := m.State(working); st != Working {
		t.Errorf("working screen: %q", st)
	}
	blocked := []string{"╭──────────────────────────╮", "│ Bash command              │", "│   rm -rf build            │", "│ Do you want to proceed?   │", "│ ❯ 1. Yes                  │", "│   2. No                   │", "╰──────────────────────────╯"}
	st, ask := m.State(blocked)
	if st != Blocked || ask != "Do you want to proceed?" {
		t.Errorf("blocked screen: %q %q", st, ask)
	}
	if st, _ := m.State([]string{"> ", ""}); st != "" {
		t.Errorf("prompt: %q", st)
	}
}

func TestOverride(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "claude.toml"), []byte(`type = "claude"
name = "Mine"
process = ["claude", "cc"]
`), 0o600)
	os.WriteFile(filepath.Join(dir, "bot.toml"), []byte(`type = "bot"
process = ["bot"]
[[state]]
state = "blocked"
any = ["approve?"]
`), 0o600)
	s := load(t, dir)
	if m := s.Detect([]string{"cc"}, ""); m == nil || m.Name != "Mine" {
		t.Errorf("override %+v", m)
	}
	if m := s.Detect([]string{"bot"}, ""); m == nil || m.Name != "bot" {
		t.Fatalf("new agent %+v", m)
	} else if st, _ := m.State([]string{"approve?"}); st != Blocked {
		t.Errorf("its rule: %q", st)
	}
	os.WriteFile(filepath.Join(dir, "bad.toml"), []byte("type = "), 0o600)
	if _, errs := Load(dir); len(errs) != 1 {
		t.Errorf("errors %v", errs)
	}
	os.WriteFile(filepath.Join(dir, "bad.toml"), []byte("type = \"x\"\ncolor = \"orange\"\n"), 0o600)
	if _, errs := Load(dir); len(errs) != 1 {
		t.Errorf("a colour not #rrggbb: %v", errs)
	}
	if m := load(t, "").ByType("claude"); m == nil || m.Color != "#d97757" {
		t.Errorf("claude's colour %+v", m)
	}
}

func TestTitle(t *testing.T) {
	for in, want := range map[string]string{
		"⠙ Move the PTY reader": "Move the PTY reader",
		"✳ Claude Code":         "Claude Code",
		"vim main.go":           "vim main.go",
	} {
		if got, _ := Title(in); got != want {
			t.Errorf("Title(%q) = %q", in, got)
		}
	}
}

func TestTracker(t *testing.T) {
	var tr Tracker
	t0 := time.Unix(1000, 0)
	tr.Set(Idle, "", t0)
	if !tr.Set(Working, "", t0.Add(time.Second)) || tr.Since != t0.Add(time.Second) {
		t.Errorf("working %+v", tr)
	}
	tr.Set(Idle, "", t0.Add(time.Minute))
	if tr.State != Done {
		t.Errorf("after working, unseen: %q", tr.State)
	}
	if tr.Set(Idle, "", t0.Add(2*time.Minute)) || tr.State != Done {
		t.Errorf("done stays: %+v", tr)
	}
	if !tr.Seen(t0.Add(3*time.Minute)) || tr.State != Idle {
		t.Errorf("seen: %q", tr.State)
	}
	tr.Set(Blocked, "Allow?", t0)
	if tr.State != Blocked || tr.Ask != "Allow?" {
		t.Errorf("blocked %+v", tr)
	}
}

func TestStateOnShortScreen(t *testing.T) {
	m := load(t, "").ByType("claude")
	screen := append([]string{"● Done thinking", "✻ Working (esc to interrupt)"}, make([]string, 30)...)
	if st, _ := m.State(screen); st != Working {
		t.Errorf("inline near the top: %q", st)
	}
}
