package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// claudeHooked is a user's settings with the hooks an earlier inua-term
// added beside the user's own.
const claudeHooked = `{
  "model": "opus",
  "hooks": {
    "Stop": [
      {"hooks": [{"type": "command", "command": "notify-send done"}]},
      {"hooks": [{"type": "command", "command": "/usr/bin/inua-term report --hook claude"}]}
    ],
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "audit"}]}
    ],
    "UserPromptSubmit": [
      {"hooks": [{"type": "command", "command": "/usr/bin/inua-term report --hook claude"}]}
    ],
    "Notification": [
      {"hooks": [{"type": "command", "command": "/usr/bin/inua-term report --hook claude"}, {"type": "command", "command": "chime"}]}
    ]
  },
  "permissions": {"allow": ["Bash(go test:*)"]},
  "cleanupPeriodDays": 30
}
`

func TestClaude(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(claudeHooked), 0o600)
	changed, err := RemoveClaude(path)
	if err != nil || !changed {
		t.Fatalf("remove: %v %v", changed, err)
	}
	b, _ := os.ReadFile(path)
	s := string(b)
	if strings.Contains(s, "inua-term") || strings.Contains(s, "UserPromptSubmit") {
		t.Errorf("hooks left:\n%s", s)
	}
	for _, want := range []string{`"notify-send done"`, `"audit"`, `"chime"`, `"Bash(go test:*)"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in\n%s", want, s)
		}
	}
	// The user's keys keep their order; numbers stay as written.
	if strings.Index(s, `"model"`) > strings.Index(s, `"hooks"`) || strings.Index(s, `"permissions"`) > strings.Index(s, `"cleanupPeriodDays": 30`) {
		t.Errorf("order changed:\n%s", s)
	}
	if bak, _ := os.ReadFile(path + ".bak"); string(bak) != claudeHooked {
		t.Error("no backup of the user's file")
	}
	if changed, _ := RemoveClaude(path); changed {
		t.Error("removed twice")
	}
}

func TestClaudeOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "inua-term report --hook claude"}]}]}}`), 0o600)
	if changed, _ := RemoveClaude(path); !changed {
		t.Error("not removed")
	}
	if b, _ := os.ReadFile(path); strings.TrimSpace(string(b)) != "{}" {
		t.Errorf("left %s", b)
	}
	if changed, err := RemoveClaude(filepath.Join(t.TempDir(), "none.json")); changed || err != nil {
		t.Errorf("no file: %v %v", changed, err)
	}
}

func TestCodex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	user := "model = \"gpt-6\"\n\n[mcp_servers.x]\ncommand = \"x\"\n"
	hooked := "model = \"gpt-6\"\nnotify = [\"/usr/bin/inua-term\", \"report\", \"--hook\", \"codex\"] # inua-term report: added by inua-term\n\n[mcp_servers.x]\ncommand = \"x\"\n"
	os.WriteFile(path, []byte(hooked), 0o600)
	if changed, err := RemoveCodex(path); err != nil || !changed {
		t.Fatalf("remove: %v %v", changed, err)
	}
	if b, _ := os.ReadFile(path); string(b) != user {
		t.Errorf("after removing:\n%q", b)
	}
	// The user's own notify stays.
	os.WriteFile(path, []byte("notify = [\"my-notifier\"]\n"), 0o600)
	if changed, _ := RemoveCodex(path); changed {
		t.Error("the user's notify removed")
	}
}
