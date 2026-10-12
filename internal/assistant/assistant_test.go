package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lyricat/inua/internal/config"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeModel speaks the OpenAI chat API, replying from a script: each reply
// either calls tools or is the agent's final JSON. It records what it was
// sent.
type fakeModel struct {
	t       *testing.T
	mu      sync.Mutex
	replies []reply
	got     []map[string]any
}

type reply struct {
	tool, args string // a tool call
	final      string // or a final answer
}

func (f *fakeModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	f.got = append(f.got, req)
	if len(f.replies) == 0 {
		f.mu.Unlock()
		http.Error(w, "no more replies", http.StatusInternalServerError)
		return
	}
	rep := f.replies[0]
	f.replies = f.replies[1:]
	n := len(f.got)
	f.mu.Unlock()

	content := ""
	var calls []map[string]any
	if rep.tool != "" {
		calls = []map[string]any{{"index": 0, "id": fmt.Sprintf("call_%d", n), "type": "function",
			"function": map[string]any{"name": rep.tool, "arguments": rep.args}}}
	} else {
		b, _ := json.Marshal(map[string]any{"type": "final", "output": rep.final})
		content = string(b)
	}
	finish := "stop"
	if calls != nil {
		finish = "tool_calls"
	}
	if stream, _ := req["stream"].(bool); stream {
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(v any) {
			b, _ := json.Marshal(v)
			fmt.Fprintf(w, "data: %s\n\n", b)
			w.(http.Flusher).Flush()
		}
		chunk := func(delta map[string]any, finish any) map[string]any {
			return map[string]any{"id": "x", "object": "chat.completion.chunk", "created": 1, "model": "fake",
				"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}}}
		}
		send(chunk(map[string]any{"role": "assistant"}, nil))
		if calls != nil {
			send(chunk(map[string]any{"tool_calls": calls}, nil))
		}
		for i := 0; i < len(content); i += 7 { // in pieces, as a model streams
			send(chunk(map[string]any{"content": content[i:min(i+7, len(content))]}, nil))
		}
		send(chunk(map[string]any{}, finish))
		fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}
	msg := map[string]any{"role": "assistant", "content": content}
	if calls != nil {
		msg["tool_calls"] = calls
	}
	json.NewEncoder(w).Encode(map[string]any{"id": "x", "object": "chat.completion", "created": 1, "model": "fake",
		"choices": []map[string]any{{"index": 0, "message": msg, "finish_reason": finish}},
		"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}})
}

func newTest(t *testing.T, model *fakeModel, own []Tool, approve Approver) *Assistant {
	t.Helper()
	srv := httptest.NewServer(model)
	t.Cleanup(srv.Close)
	a, err := New(Settings{Profiles: []Profile{{Name: "test", Provider: "openai_chat_compatible", Model: "gpt-4.1-mini", Endpoint: srv.URL + "/v1", APIKey: "test"}},
		AskBeforeChanges: true, StateDir: t.TempDir(), CacheDir: t.TempDir(), MaxSteps: 6}, own, approve)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAsk(t *testing.T) {
	focus := false
	setFocus := Tool{
		Name: "set_focus", Description: "Turns Focus (quiet notifications) on or off.",
		Schema: `{"type":"object","properties":{"on":{"type":"boolean"}},"required":["on"]}`,
		Risk:   Change,
		Describe: func(args map[string]any) string {
			if on, _ := args["on"].(bool); on {
				return "Turning on Focus"
			}
			return "Turning off Focus"
		},
		Run: func(_ context.Context, args map[string]any) (any, error) {
			focus, _ = args["on"].(bool)
			return map[string]any{"focus": focus}, nil
		},
	}
	model := &fakeModel{t: t, replies: []reply{
		{tool: "set_focus", args: `{"on":true}`},
		{final: "Focus is on: notifications stay quiet."},
		{final: "Yes, it is still on."},
	}}
	var asked []string
	a := newTest(t, model, []Tool{setFocus}, func(_ context.Context, tool, doing string) (bool, error) {
		asked = append(asked, doing)
		return true, nil
	})

	var mu sync.Mutex
	var lines, answers []string
	ev := Events{
		Tool:   func(name, doing string) { mu.Lock(); lines = append(lines, doing); mu.Unlock() },
		Answer: func(text string) { mu.Lock(); answers = append(answers, text); mu.Unlock() },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	answer, err := a.Ask(ctx, "Turn on Focus", ev)
	if err != nil {
		t.Fatal(err)
	}
	if answer != "Focus is on: notifications stay quiet." || !focus {
		t.Errorf("answer %q, focus %v", answer, focus)
	}
	if len(asked) != 1 || asked[0] != "Turning on Focus" || len(lines) != 1 {
		t.Errorf("asked %q, lines %q", asked, lines)
	}
	if len(answers) == 0 || answers[len(answers)-1] != answer {
		t.Errorf("streamed %q", answers)
	}
	for _, a := range answers {
		if strings.Contains(a, `"type"`) {
			t.Errorf("the window saw JSON: %q", a)
		}
	}

	// The second question carries the first: the model is sent both.
	if _, err := a.Ask(ctx, "Is it still on?", Events{}); err != nil {
		t.Fatal(err)
	}
	last, _ := json.Marshal(model.got[len(model.got)-1]["messages"])
	if !strings.Contains(string(last), "Turn on Focus") || !strings.Contains(string(last), "Focus is on") {
		t.Errorf("the conversation was not carried: %s", last)
	}
	// inua's tool and policy reached the model.
	first, _ := json.Marshal(model.got[0])
	if !strings.Contains(string(first), "set_focus") || !strings.Contains(string(first), "assistant of inua") {
		t.Errorf("the first request lacks inua's tool or prompt")
	}
}

func TestDeclined(t *testing.T) {
	ran := false
	power := Tool{Name: "power", Description: "Shuts down.", Risk: Always,
		Run: func(context.Context, map[string]any) (any, error) { ran = true; return "ok", nil }}
	model := &fakeModel{t: t, replies: []reply{
		{tool: "power", args: `{}`},
		{final: "I did not shut down: you declined."},
	}}
	var denied []string
	a := newTest(t, model, []Tool{power}, func(context.Context, string, string) (bool, error) { return false, nil })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	answer, err := a.Ask(ctx, "Shut down", Events{Denied: func(name, _ string) { denied = append(denied, name) }})
	if err != nil {
		t.Fatal(err)
	}
	if ran || len(denied) != 1 || !strings.Contains(answer, "declined") {
		t.Errorf("ran %v, denied %q, answer %q", ran, denied, answer)
	}
	// The model was told it was declined, not that it failed.
	if last, _ := json.Marshal(model.got[len(model.got)-1]); !strings.Contains(string(last), "declined") {
		t.Error("the model was not told")
	}
}

func TestSettings(t *testing.T) {
	t.Setenv("INUA_TEST_KEY", "")
	c := config.Default().Assistant
	c.Profiles = []config.AIProfile{
		{Name: "main", Provider: "openai", Model: "gpt-5.2", APIKeyEnv: "INUA_TEST_KEY"},
		{Name: "cheap", Provider: "openai", Model: "gpt-5-mini", APIKeyEnv: "INUA_TEST_KEY"},
		{Name: "claude", Provider: "anthropic", Model: "claude-sonnet-5-5"},
		{Name: "local", Provider: "openai_chat_compatible", Model: "qwen3", Endpoint: "http://127.0.0.1:11434/v1"},
	}
	c.Profile, c.Fallback = "main", []string{"claude", "local"}
	if _, err := SettingsFrom(c, nil); !errors.Is(err, ErrNoKey) {
		t.Errorf("no key for the answering profile: %v", err)
	}
	t.Setenv("INUA_TEST_KEY", "sk-env")
	s, err := SettingsFrom(c, map[string]string{"main": "sk-saved"})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range s.Profiles {
		names = append(names, p.Name)
	}
	// The saved key wins; claude has none and is left out, also as a
	// fallback; a local server needs none.
	if s.Profiles[0].APIKey != "sk-saved" || s.Profiles[1].APIKey != "sk-env" || fmt.Sprint(names) != "[main cheap local]" ||
		fmt.Sprint(s.Missing) != "[claude]" || fmt.Sprint(s.Fallback) != "[local]" {
		t.Errorf("%+v", s)
	}
}

func TestProfiles(t *testing.T) {
	var models []string
	var mu sync.Mutex
	model := &fakeModel{t: t, replies: []reply{{final: "from backup"}, {final: "from other"}}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		json.Unmarshal(body, &req)
		mu.Lock()
		models = append(models, fmt.Sprint(req["model"]))
		mu.Unlock()
		if req["model"] == "broken" { // the answering profile fails: the fallback answers
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		model.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	p := func(name, model string) Profile {
		return Profile{Name: name, Provider: "openai_chat_compatible", Model: model, Endpoint: srv.URL + "/v1", APIKey: "k"}
	}
	a, err := New(Settings{Profiles: []Profile{p("main", "broken"), p("backup", "backup-model"), p("other", "other-model")},
		Fallback: []string{"backup"}, StateDir: t.TempDir(), CacheDir: t.TempDir(), MaxSteps: 4}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if answer, err := a.Ask(ctx, "hi", Events{}); err != nil || answer != "from backup" {
		t.Errorf("fallback: %q %v (asked %q)", answer, err, models)
	}
	if err := a.UseProfile("other"); err != nil {
		t.Fatal(err)
	}
	if answer, err := a.Ask(ctx, "hi", Events{}); err != nil || answer != "from other" || a.Model() != "other-model · openai_chat_compatible" {
		t.Errorf("switched: %q %v %q", answer, err, a.Model())
	}
	if a.UseProfile("nope") == nil {
		t.Error("no such profile")
	}
}
