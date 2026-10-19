package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const userClaude = `{
  "model": "opus",
  "hooks": {
    "Stop": [
      {"hooks": [{"type": "command", "command": "notify-send done"}]}
    ],
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "audit"}]}
    ]
  },
  "permissions": {"allow": ["Bash(go test:*)"]},
  "cleanupPeriodDays": 30
}
`

func TestClaude(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(userClaude), 0o600)
	changed, err := InstallClaude(path, "/usr/bin/inua-term")
	if err != nil || !changed {
		t.Fatalf("install: %v %v", changed, err)
	}
	b, _ := os.ReadFile(path)
	s := string(b)
	for _, want := range []string{`"notify-send done"`, `"audit"`, `"Bash(go test:*)"`, `"/usr/bin/inua-term report --hook claude"`, `"UserPromptSubmit"`, `"Notification"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in\n%s", want, s)
		}
	}
	// The user's keys keep their order; numbers stay as written.
	if strings.Index(s, `"model"`) > strings.Index(s, `"hooks"`) || strings.Index(s, `"permissions"`) > strings.Index(s, `"cleanupPeriodDays": 30`) {
		t.Errorf("order changed:\n%s", s)
	}
	if bak, _ := os.ReadFile(path + ".bak"); string(bak) != userClaude {
		t.Error("no backup of the user's file")
	}
	if strings.Count(s, "report --hook claude") != 3 {
		t.Errorf("hooks %d", strings.Count(s, "report --hook claude"))
	}
	if changed, _ := InstallClaude(path, "/usr/bin/inua-term"); changed {
		t.Error("installed twice")
	}
	changed, err = RemoveClaude(path)
	if err != nil || !changed {
		t.Fatalf("remove: %v %v", changed, err)
	}
	b, _ = os.ReadFile(path)
	s = string(b)
	if strings.Contains(s, "inua-term") || !strings.Contains(s, "notify-send done") || !strings.Contains(s, `"audit"`) || strings.Contains(s, "UserPromptSubmit") {
		t.Errorf("after removing:\n%s", s)
	}
}

func TestClaudeNoFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude", "settings.json")
	if changed, err := InstallClaude(path, "inua-term"); err != nil || !changed {
		t.Fatalf("install: %v %v", changed, err)
	}
	if _, err := os.Stat(path + ".bak"); err == nil {
		t.Error("a backup of no file")
	}
	if changed, _ := RemoveClaude(path); !changed {
		t.Error("not removed")
	}
	if b, _ := os.ReadFile(path); strings.TrimSpace(string(b)) != "{}" {
		t.Errorf("left %s", b)
	}
}

func TestCodex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	user := "model = \"gpt-6\"\n\n[mcp_servers.x]\ncommand = \"x\"\n"
	os.WriteFile(path, []byte(user), 0o600)
	if changed, err := InstallCodex(path, "/usr/bin/inua-term"); err != nil || !changed {
		t.Fatalf("install: %v %v", changed, err)
	}
	b, _ := os.ReadFile(path)
	s := string(b)
	if i, j := strings.Index(s, "notify = "), strings.Index(s, "[mcp_servers.x]"); i < 0 || i > j {
		t.Errorf("notify not at the top level:\n%s", s)
	}
	if changed, _ := InstallCodex(path, "/usr/bin/inua-term"); changed {
		t.Error("installed twice")
	}
	if changed, _ := RemoveCodex(path); !changed {
		t.Error("not removed")
	}
	if b, _ := os.ReadFile(path); string(b) != user {
		t.Errorf("after removing:\n%q", b)
	}
	// The user's own notify stays.
	os.WriteFile(path, []byte("notify = [\"my-notifier\"]\n"), 0o600)
	if changed, _ := InstallCodex(path, "inua-term"); changed {
		t.Error("the user's notify replaced")
	}
}

func TestCodexNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	InstallCodex(path, "inua-term")
	if b, _ := os.ReadFile(path); !strings.HasPrefix(string(b), "notify = ") || !strings.HasSuffix(string(b), "\n") || strings.Count(string(b), "\n") != 1 {
		t.Errorf("new file %q", b)
	}
	os.WriteFile(path, []byte("model = \"x\"\n"), 0o600)
	InstallCodex(path, "inua-term")
	if b, _ := os.ReadFile(path); !strings.HasPrefix(string(b), "model = \"x\"\nnotify = ") || !strings.HasSuffix(string(b), "\n") {
		t.Errorf("no table %q", b)
	}
}
