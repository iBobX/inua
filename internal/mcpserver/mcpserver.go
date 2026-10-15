// Package mcpserver is inua as an MCP server (docs/feat/mcp.md): agents on
// this computer use the assistant's tools to operate the desktop, as
// mcp.policy lets them, and are asked before changes in the shell's
// dialog. `inua mcp` runs it on stdio, in inua-assistant.
package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/desk"
)

// Answer is how the user answered an agent's call.
type Answer int

const (
	Deny    Answer = iota
	Once           // this call
	Session        // this tool, until the agent's session ends
)

// Request is what the user is asked: which agent, what it runs under,
// and what the call does ("Closing Firefox").
type Request struct {
	Agent   string // the client's name in the MCP handshake: "claude-code"
	Chain   string // what started inua mcp: "claude ← zsh"
	Program string // its program's path
	Tool    string
	Doing   string
	// Assistant is inua's own assistant asking: once is all it gets.
	Assistant bool
}

// Asker asks the user; an error means nobody could be asked (it is then
// denied).
type Asker func(ctx context.Context, r Request) (Answer, error)

// Call is one call, as the record keeps it.
type Call struct {
	Time     time.Time `json:"time"`
	Agent    string    `json:"agent"`
	Chain    string    `json:"chain,omitempty"`
	Tool     string    `json:"tool"`
	Doing    string    `json:"doing,omitempty"`
	Decision string    `json:"decision"` // ran, allowed, denied, refused
	Error    string    `json:"error,omitempty"`
}

// Options set up a server.
type Options struct {
	Tools []desk.Tool
	// Config is mcp.* as it is now, read again for each call: turning it
	// off or tightening the policy takes effect at once.
	Config  func() config.MCP
	Ask     Asker
	Chain   string // what started this process
	Program string
	// Record keeps each call; nil keeps none.
	Record  func(Call)
	Version string
	// Assistant is inua's own assistant as the client, which started this
	// server: mcp.enable and mcp.policy are for agents; the assistant asks
	// as its settings say (AskBeforeChanges, read again for each call),
	// and its window shows a call it declined as declined.
	Assistant        bool
	AskBeforeChanges func() bool
	// Agent names the client in questions and the record instead of its
	// MCP handshake.
	Agent string
}

// left out: agents have their own web, files and shell.
var leftOut = []string{"web_search", "url_fetch", "read_file", "bash"}

// sensitive are reads that can hold secrets or private messages: they ask
// whatever the policy.
var sensitive = []string{"clipboard_read", "clipboard_history", "list_notifications", "notification_history"}

// actions are tools the assistant runs without asking, being harmless and
// asked for by the user typing to it, that still act: for an agent they
// are changes (not offered to look only, and not read-only).
var actions = []string{"focus_window", "switch_workspace", "open_app", "screenshot", "show_panel", "media_control"}

// risk is a tool's risk for an agent.
func risk(t desk.Tool) desk.Risk {
	if t.Risk == desk.Read && slices.Contains(actions, t.Name) {
		return desk.Change
	}
	return t.Risk
}

// server is one agent's session with inua.
type server struct {
	o       Options
	mu      sync.Mutex
	allowed map[string]bool // tools allowed for the session
}

// New makes the MCP server: the tools the policy offers, as of now.
func New(o Options) *mcp.Server {
	s := &server{o: o, allowed: map[string]bool{}}
	srv := mcp.NewServer(&mcp.Implementation{Name: "inua", Title: "inua desktop shell", Version: o.Version}, nil)
	for _, t := range o.Tools {
		if slices.Contains(leftOut, t.Name) || (!o.Assistant && !offered(o.Config().Policy, t)) {
			continue
		}
		srv.AddTool(mcpTool(t), s.handler(t))
	}
	return srv
}

// offered says whether a policy offers a tool at all.
func offered(policy string, t desk.Tool) bool {
	return policy != "read" || (risk(t) == desk.Read && !slices.Contains(sensitive, t.Name))
}

// mustAsk says whether a call needs the user's yes under a policy.
func mustAsk(policy string, t desk.Tool) bool {
	switch {
	case slices.Contains(sensitive, t.Name), risk(t) == desk.Always:
		return true
	case risk(t) == desk.Read:
		return false
	}
	return policy != "allow"
}

func mcpTool(t desk.Tool) *mcp.Tool {
	ann := &mcp.ToolAnnotations{ReadOnlyHint: risk(t) == desk.Read}
	destructive := risk(t) == desk.Always
	ann.DestructiveHint = &destructive
	schema := t.Schema
	if strings.TrimSpace(schema) == "" { // a tool without arguments
		schema = `{"type":"object","properties":{}}`
	}
	return &mcp.Tool{Name: t.Name, Description: t.Description, InputSchema: json.RawMessage(schema), Annotations: ann}
}

func failure(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}
}

func (s *server) handler(t desk.Tool) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		agent := "an agent"
		if p := req.Session.InitializeParams(); p != nil && p.ClientInfo != nil {
			agent = cmpOr(p.ClientInfo.Title, p.ClientInfo.Name, agent)
		}
		agent = cmpOr(s.o.Agent, agent)
		args := map[string]any{}
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return failure("arguments: " + err.Error()), nil
			}
		}
		doing := t.Name
		if t.Describe != nil {
			doing = t.Describe(args)
		}
		call := Call{Time: time.Now(), Agent: agent, Chain: s.o.Chain, Tool: t.Name, Doing: doing}
		done := func(decision string, err error) {
			call.Decision = decision
			if err != nil {
				call.Error = err.Error()
			}
			if s.o.Record != nil {
				s.o.Record(call)
			}
		}
		ask := false
		if s.o.Assistant {
			// As the assistant's own tools ask: the riskiest always,
			// changes as its settings say.
			ask = t.Risk == desk.Always || (t.Risk == desk.Change && s.o.AskBeforeChanges != nil && s.o.AskBeforeChanges())
		} else {
			cfg := s.o.Config()
			switch {
			case !cfg.Enable:
				done("refused", nil)
				return failure("inua's MCP server is turned off (Settings → AI → MCP)."), nil
			case !offered(cfg.Policy, t):
				done("refused", nil)
				return failure("inua's settings let agents only look now (Settings → AI → MCP)."), nil
			}
			ask = mustAsk(cfg.Policy, t)
		}
		decision := "ran"
		s.mu.Lock()
		ok := s.allowed[t.Name]
		s.mu.Unlock()
		if ask && !ok {
			ans := Deny
			if s.o.Ask != nil {
				var err error
				if ans, err = s.o.Ask(ctx, Request{Agent: agent, Chain: s.o.Chain, Program: s.o.Program, Tool: t.Name, Doing: doing, Assistant: s.o.Assistant}); err != nil {
					ans = Deny
				}
			}
			switch ans {
			case Deny:
				done("denied", nil)
				if s.o.Assistant {
					return failure(desk.DeclinedPrefix + "The user declined this. Do not try it another way."), nil
				}
				return failure("The user did not allow it."), nil
			case Session:
				s.mu.Lock()
				s.allowed[t.Name] = true
				s.mu.Unlock()
			}
			decision = "allowed"
		}
		out, err := t.Run(ctx, args)
		if err != nil {
			done(decision, err)
			return failure(err.Error()), nil
		}
		done(decision, nil)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text(out)}}}, nil
	}
}

// text is a tool's result for the agent: a string as it is, else JSON.
func text(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func cmpOr(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// --- the record ---

// RecordPath is the record of calls: $XDG_STATE_HOME/inua/mcp-calls.jsonl.
func RecordPath() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "inua", "mcp-calls.jsonl")
}

// recordKeep is how many calls the record keeps; it is trimmed to them
// once it holds twice as many.
const recordKeep = 300

// Record appends a call to the record at path.
func Record(path string, c Call) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return trim(path)
}

func trim(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	f.Close()
	if len(lines) <= 2*recordKeep {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines[len(lines)-recordKeep:], "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadRecord reads the record, newest first.
func ReadRecord(path string) ([]Call, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Call
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var c Call
		if json.Unmarshal([]byte(line), &c) == nil && c.Tool != "" {
			out = append(out, c)
		}
	}
	slices.Reverse(out)
	return out, nil
}
