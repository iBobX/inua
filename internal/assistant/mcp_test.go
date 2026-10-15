package assistant

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/desk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMain lets the test binary be inua's MCP server, as the assistant
// starts it: list_windows runs, close_window is declined (as the server
// answers when the user declines).
func TestMain(m *testing.M) {
	if os.Getenv(MCPClientEnv) == "assistant" {
		srv := mcp.NewServer(&mcp.Implementation{Name: "inua"}, nil)
		obj := map[string]any{"type": "object", "properties": map[string]any{"window": map[string]any{"type": "string"}}}
		srv.AddTool(&mcp.Tool{Name: "list_windows", Description: "The open windows.", InputSchema: obj},
			func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: `[{"id":"1","title":"Firefox"}]`}}}, nil
			})
		srv.AddTool(&mcp.Tool{Name: "close_window", Description: "Closes a window.", InputSchema: obj},
			func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: desk.DeclinedPrefix + "The user declined this."}}}, nil
			})
		srv.Run(context.Background(), &mcp.StdioTransport{})
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func requestTools(req map[string]any) []string {
	var names []string
	ts, _ := req["tools"].([]any)
	for _, x := range ts {
		b, _ := json.Marshal(x)
		var v struct{ Function struct{ Name string } }
		json.Unmarshal(b, &v)
		names = append(names, v.Function.Name)
	}
	return names
}

// inua's tools through its MCP server: hidden until tool_search finds
// them, then called, each shown in the window; a declined call is shown
// declined; what was found stays for the conversation, until Reset.
func TestMCPServerTools(t *testing.T) {
	self, _ := os.Executable()
	model := &fakeModel{t: t, replies: []reply{
		{tool: "tool_search", args: `{"query":"window"}`},
		{tool: "mcp_inua__list_windows", args: `{}`},
		{tool: "mcp_inua__close_window", args: `{"window":"Firefox"}`},
		{final: "I could not close Firefox: you declined."},
	}}
	srv := newModelServer(t, model)
	a, err := New(Settings{Profiles: []Profile{{Name: "test", Provider: "openai_chat_compatible", Model: "gpt-4.1-mini", Endpoint: srv + "/v1", APIKey: "test"}},
		AskBeforeChanges: true, StateDir: t.TempDir(), CacheDir: t.TempDir(), MaxSteps: 8, MCPServer: []string{self}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var lines, denied, failed []string
	ev := Events{
		Tool:   func(_, doing string) { mu.Lock(); lines = append(lines, doing); mu.Unlock() },
		Denied: func(name, doing string) { mu.Lock(); denied = append(denied, name+": "+doing); mu.Unlock() },
		Error:  func(name string, err error) { mu.Lock(); failed = append(failed, name); mu.Unlock() },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := a.Ask(ctx, "Close Firefox", ev); err != nil {
		t.Fatal(err)
	}
	first := requestTools(model.got[0])
	if !slices.Contains(first, "tool_search") || slices.ContainsFunc(first, func(n string) bool { return strings.HasPrefix(n, mcpToolPrefix) }) {
		t.Errorf("the first request's tools: %v", first)
	}
	if after := requestTools(model.got[1]); !slices.Contains(after, "mcp_inua__list_windows") {
		t.Errorf("found, not offered: %v", after)
	}
	if got := strings.Join(lines, " | "); got != "Looking for tools: window | Looking at the windows | Closing Firefox" {
		t.Errorf("window lines %q", got)
	}
	if len(denied) != 1 || denied[0] != "close_window: Closing Firefox" || len(failed) != 0 {
		t.Errorf("denied %q, failed %q", denied, failed)
	}
	if after, _ := json.Marshal(model.got[2]); !strings.Contains(string(after), "title") {
		t.Error("list_windows's result did not reach the model")
	}

	// The next question offers what was found; after Reset, a new
	// conversation, it is hidden again.
	model.replies = append(model.replies, reply{final: "Still open."}, reply{final: "New topic."})
	if _, err := a.Ask(ctx, "And now?", Events{}); err != nil {
		t.Fatal(err)
	}
	if next := requestTools(model.got[4]); !slices.Contains(next, "mcp_inua__list_windows") {
		t.Errorf("found tools forgotten on the next question: %v", next)
	}
	a.Reset()
	if _, err := a.Ask(ctx, "What is open?", Events{}); err != nil {
		t.Fatal(err)
	}
	if fresh := requestTools(model.got[5]); slices.Contains(fresh, "mcp_inua__list_windows") {
		t.Errorf("found tools kept after Reset: %v", fresh)
	}
}

// Every tool's description of a call works without a desktop, as the
// window's lines call them.
func TestDescribers(t *testing.T) {
	if len(describers) < 15 {
		t.Fatalf("%d describers", len(describers))
	}
	for _, d := range describers {
		d(map[string]any{})
		d(map[string]any{"window": "Firefox", "action": "suspend", "on": true})
	}
}
