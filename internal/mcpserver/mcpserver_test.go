package mcpserver

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lyricat/inua/internal/assistant"
	"github.com/lyricat/inua/internal/config"
)

// tools are fakes of each kind: a read, a sensitive read, a change, a
// risky change, and one agents have their own of.
func tools(ran *[]string, mu *sync.Mutex) []assistant.Tool {
	mk := func(name string, risk assistant.Risk, out any) assistant.Tool {
		return assistant.Tool{Name: name, Description: name + " it", Schema: `{"type":"object","properties":{"window":{"type":"string"}}}`, Risk: risk,
			Describe: func(a map[string]any) string {
				return "Doing " + name + " to " + strings.TrimSpace(asString(a["window"]))
			},
			Run: func(_ context.Context, a map[string]any) (any, error) {
				mu.Lock()
				*ran = append(*ran, name)
				mu.Unlock()
				return out, nil
			}}
	}
	noArgs := mk("system_status", assistant.Read, "fine")
	noArgs.Schema = "" // as the assistant's tools without arguments have it
	return []assistant.Tool{
		noArgs,
		mk("list_windows", assistant.Read, []map[string]string{{"id": "1", "title": "Firefox"}}),
		mk("clipboard_read", assistant.Read, "secret text"),
		mk("focus_window", assistant.Read, "focused"), // as the assistant has it: an action for an agent
		mk("close_window", assistant.Always, "closed"),
		mk("set_volume", assistant.Change, "set"),
		mk("bash", assistant.Always, "no"),
	}
}

func asString(v any) string { s, _ := v.(string); return s }

type fixture struct {
	t      *testing.T
	cs     *mcp.ClientSession
	mu     sync.Mutex
	ran    []string
	asked  []Request
	answer Answer
	cfg    config.MCP
	calls  []Call
}

func start(t *testing.T, cfg config.MCP) *fixture {
	t.Helper()
	return startWith(t, cfg, nil)
}

// startWith starts a server with options changed by more.
func startWith(t *testing.T, cfg config.MCP, more func(*Options)) *fixture {
	t.Helper()
	f := &fixture{t: t, cfg: cfg, answer: Once}
	o := Options{
		Tools: tools(&f.ran, &f.mu),
		Config: func() config.MCP {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.cfg
		},
		Ask: func(_ context.Context, r Request) (Answer, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.asked = append(f.asked, r)
			return f.answer, nil
		},
		Chain: "claude ← zsh",
		Record: func(c Call) {
			f.mu.Lock()
			f.calls = append(f.calls, c)
			f.mu.Unlock()
		},
	}
	if more != nil {
		more(&o)
	}
	srv := New(o)
	st, ct := mcp.NewInMemoryTransports()
	ctx := t.Context()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	f.cs = cs
	return f
}

func (f *fixture) names() []string {
	res, err := f.cs.ListTools(context.Background(), nil)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []string
	for _, t := range res.Tools {
		out = append(out, t.Name)
	}
	slices.Sort(out)
	return out
}

func (f *fixture) call(name string) (string, bool) {
	f.t.Helper()
	res, err := f.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{"window": "Firefox"}})
	if err != nil {
		f.t.Fatal(err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String(), res.IsError
}

func TestPolicies(t *testing.T) {
	f := start(t, config.MCP{Enable: true, Policy: "read"})
	if got := strings.Join(f.names(), " "); got != "list_windows system_status" {
		t.Errorf("read offers %q", got)
	}
	f = start(t, config.MCP{Enable: true, Policy: "ask"})
	if got := strings.Join(f.names(), " "); got != "clipboard_read close_window focus_window list_windows set_volume system_status" {
		t.Errorf("ask offers %q (bash is the agent's own)", got)
	}
	// The hints.
	res, _ := f.cs.ListTools(context.Background(), nil)
	for _, tl := range res.Tools {
		ro, destructive := tl.Annotations.ReadOnlyHint, tl.Annotations.DestructiveHint != nil && *tl.Annotations.DestructiveHint
		isRead := tl.Name == "list_windows" || tl.Name == "system_status" || tl.Name == "clipboard_read" // it asks, but only reads
		if isRead != ro || (tl.Name == "close_window") != destructive {
			t.Errorf("%s: read-only %v, destructive %v", tl.Name, ro, destructive)
		}
	}
}

func TestAsk(t *testing.T) {
	f := start(t, config.MCP{Enable: true, Policy: "ask"})
	if out, isErr := f.call("system_status"); isErr || out != "fine" {
		t.Errorf("a tool without arguments: %q %v", out, isErr)
	}
	f.calls = nil
	if out, isErr := f.call("list_windows"); isErr || !strings.Contains(out, "Firefox") || len(f.asked) != 0 {
		t.Errorf("a read: %q %v, asked %d", out, isErr, len(f.asked))
	}
	// A change asks; once, then again.
	if out, isErr := f.call("focus_window"); isErr || out != "focused" {
		t.Errorf("allowed once: %q %v", out, isErr)
	}
	f.call("focus_window")
	if len(f.asked) != 2 {
		t.Errorf("asked %d times for two calls allowed once", len(f.asked))
	}
	r := f.asked[0]
	if r.Agent != "test-agent" || r.Chain != "claude ← zsh" || r.Tool != "focus_window" || r.Doing != "Doing focus_window to Firefox" {
		t.Errorf("the question %+v", r)
	}
	// Denied: it does not run.
	f.mu.Lock()
	f.answer = Deny
	f.mu.Unlock()
	if out, isErr := f.call("close_window"); !isErr || !strings.Contains(out, "did not allow") || slices.Contains(f.ran, "close_window") {
		t.Errorf("denied: %q %v, ran %v", out, isErr, f.ran)
	}
	// For the session: asked once.
	f.mu.Lock()
	f.answer = Session
	f.mu.Unlock()
	n := len(f.asked)
	f.call("clipboard_read")
	f.call("clipboard_read")
	if len(f.asked) != n+1 {
		t.Errorf("allowed for the session, asked %d times", len(f.asked)-n)
	}
	// The record.
	var decisions []string
	for _, c := range f.calls {
		decisions = append(decisions, c.Tool+":"+c.Decision)
	}
	want := "list_windows:ran focus_window:allowed focus_window:allowed close_window:denied clipboard_read:allowed clipboard_read:ran"
	if strings.Join(decisions, " ") != want {
		t.Errorf("record %v", decisions)
	}
}

func TestAllowAndOff(t *testing.T) {
	f := start(t, config.MCP{Enable: true, Policy: "allow"})
	f.call("focus_window")
	if len(f.asked) != 0 {
		t.Errorf("allow asked for a change")
	}
	f.call("close_window")
	f.call("clipboard_read")
	if len(f.asked) != 2 {
		t.Errorf("allow did not ask for the riskiest: %d", len(f.asked))
	}
	// Tightened while connected: refused.
	f.mu.Lock()
	f.cfg.Policy = "read"
	f.mu.Unlock()
	if out, isErr := f.call("focus_window"); !isErr || !strings.Contains(out, "only look") {
		t.Errorf("read now: %q %v", out, isErr)
	}
	// Turned off: refused.
	f.mu.Lock()
	f.cfg.Enable = false
	f.mu.Unlock()
	if out, isErr := f.call("list_windows"); !isErr || !strings.Contains(out, "turned off") {
		t.Errorf("off: %q %v", out, isErr)
	}
}

func TestRecordFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inua", "mcp-calls.jsonl")
	for i := range 2*recordKeep + 1 {
		if err := Record(path, Call{Tool: "t", Agent: string(rune('a' + i%26))}); err != nil {
			t.Fatal(err)
		}
	}
	calls, err := ReadRecord(path)
	if err != nil || len(calls) != recordKeep {
		t.Errorf("%d calls kept, %v", len(calls), err)
	}
}

// The assistant's real tools all make MCP tools (their schemas are
// objects), but those agents have their own.
func TestRealTools(t *testing.T) {
	srv := New(Options{Tools: assistant.Tools(assistant.Desktop{}), Config: func() config.MCP { return config.MCP{Enable: true, Policy: "ask"} }})
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(t.Context(), st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t"}, nil).Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil || len(res.Tools) < 15 {
		t.Fatalf("%v tools, %v", len(res.Tools), err)
	}
	for _, tl := range res.Tools {
		if slices.Contains(leftOut, tl.Name) {
			t.Errorf("offered %s", tl.Name)
		}
	}
}

// inua's assistant as the client: on while mcp.enable is off, everything
// offered, asking as the assistant's settings say, and a declined call
// marked declined for its window.
func TestAssistantClient(t *testing.T) {
	ask := true
	f := startWith(t, config.MCP{Enable: false, Policy: "read"}, func(o *Options) {
		o.Assistant = true
		o.Agent = "inua's assistant"
		o.AskBeforeChanges = func() bool { return ask }
	})
	if got := strings.Join(f.names(), " "); got != "clipboard_read close_window focus_window list_windows set_volume system_status" {
		t.Errorf("offered %q", got)
	}
	// Reads, the sensitive ones and the harmless actions included, run.
	for _, name := range []string{"list_windows", "clipboard_read", "focus_window"} {
		if _, isErr := f.call(name); isErr {
			t.Errorf("%s refused", name)
		}
	}
	if len(f.asked) != 0 {
		t.Errorf("asked for reads: %+v", f.asked)
	}
	f.call("set_volume")
	if len(f.asked) != 1 || f.asked[0].Agent != "inua's assistant" {
		t.Errorf("a change, asking before changes: %+v", f.asked)
	}
	ask = false
	f.call("set_volume")
	if len(f.asked) != 1 {
		t.Errorf("a change, not asking before changes: asked %d", len(f.asked))
	}
	f.mu.Lock()
	f.answer = Deny
	f.mu.Unlock()
	out, isErr := f.call("close_window")
	if !isErr || !strings.HasPrefix(out, assistant.DeclinedPrefix) || len(f.asked) != 2 {
		t.Errorf("declined: %q %v, asked %d", out, isErr, len(f.asked))
	}
}
