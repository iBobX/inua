package main

import (
	"strings"
	"testing"
)

func TestClaudeHook(t *testing.T) {
	for _, c := range []struct {
		in                    string
		state, title, summary string
	}{
		{`{"hook_event_name":"UserPromptSubmit","prompt":"Fix the flaky pty test\nand rerun it"}`, "working", "Fix the flaky pty test", ""},
		{`{"hook_event_name":"Notification","message":"Claude needs your permission to use Bash"}`, "blocked", "", "Claude needs your permission to use Bash"},
		{`{"hook_event_name":"Notification","message":"Claude is waiting for your input"}`, "idle", "", ""},
		{`{"hook_event_name":"Stop"}`, "done", "", ""},
		{`{"hook_event_name":"PreToolUse"}`, "", "", ""},
		{`not json`, "", "", ""},
	} {
		s, ti, su := claudeHook(strings.NewReader(c.in))
		if s != c.state || ti != c.title || su != c.summary {
			t.Errorf("%s: %q %q %q", c.in, s, ti, su)
		}
	}
}

func TestCodexHook(t *testing.T) {
	s, ti, su := codexHook([]string{`{"type":"agent-turn-complete","input-messages":["rename the flag"],"last-assistant-message":"Renamed it.\nTests pass."}`})
	if s != "done" || ti != "rename the flag" || su != "Renamed it." {
		t.Errorf("%q %q %q", s, ti, su)
	}
	if s, _, _ := codexHook([]string{`{"type":"other"}`}); s != "" {
		t.Errorf("other: %q", s)
	}
	if long := firstLine(strings.Repeat("x", 100)); len([]rune(long)) != 80 {
		t.Errorf("first line %d", len([]rune(long)))
	}
}
