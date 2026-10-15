package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lyricat/inua/internal/assistant"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/mcpserver"
	"github.com/lyricat/inua/internal/pinentry"
	"github.com/lyricat/inua/internal/proc"
)

// version is the release, set by the release build (-X main.version).
var version = "dev"

// runMCP is inua as an MCP server on stdio (`inua mcp`, docs/feat/mcp.md):
// the agent that started it is this process's parent.
func runMCP(ctx context.Context) int {
	log.SetOutput(os.Stderr) // stdout is the protocol's
	log.SetPrefix("inua mcp: ")
	cfg, _, _ := config.Load(config.DefaultPath())
	i18n.Init(cfg.Language)
	parent := proc.Parent(os.Getpid())
	// inua's assistant starts this server for its own tools: its settings
	// apply, not those for agents. The variable alone is not enough: the
	// parent must be this program too.
	own, _ := os.Executable()
	// (An assistant running since inua was installed again has its program
	// " (deleted)".)
	byAssistant := os.Getenv(assistant.MCPClientEnv) == "assistant" && own != "" &&
		strings.TrimSuffix(proc.Exe(parent), " (deleted)") == own
	if !cfg.MCP.Enable && !byAssistant {
		fmt.Fprintln(os.Stderr, "inua: the MCP server is off; turn it on in Settings → AI → MCP (mcp.enable in config.toml)")
		return 1
	}
	now := func() config.Config {
		c, _, err := config.Load(config.DefaultPath())
		if err != nil {
			return cfg
		}
		return c
	}
	agent := ""
	if byAssistant {
		agent = i18n.T("inua's assistant")
	}
	srv := mcpserver.New(mcpserver.Options{
		Tools:            assistant.Tools(assistant.LocalDesktop(cfg.TerminalCommand())),
		Config:           func() config.MCP { return now().MCP },
		Assistant:        byAssistant,
		AskBeforeChanges: func() bool { return now().Assistant.AskBeforeChanges },
		Agent:            agent,
		Ask:              askShell,
		Chain:            proc.Chain(parent, 8),
		Program:          proc.Exe(parent),
		Record: func(c mcpserver.Call) {
			if err := mcpserver.Record(mcpserver.RecordPath(), c); err != nil {
				log.Printf("record: %v", err)
			}
		},
		Version: version,
	})
	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		log.Print(err)
		return 1
	}
	return 0
}

// askShell asks in the shell's dialog whether the agent may make a call;
// an error when no shell listens (the call is then denied).
func askShell(ctx context.Context, r mcpserver.Request) (mcpserver.Answer, error) {
	req := pinentry.Request{Mode: pinentry.Approve, App: r.Agent, Chain: r.Chain, Entry: r.Doing,
		Approval: &pinentry.Approval{Agent: true, Action: r.Doing, Program: r.Program, Assistant: r.Assistant}}
	arg, err := json.Marshal(req)
	if err != nil {
		return mcpserver.Deny, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	sock := ipc.SocketPath(cmp.Or(os.Getenv("INUA_INSTANCE"), "default"))
	resp, err := ipc.Call(ctx, sock, ipc.Request{Op: "call", Target: "password", Function: string(pinentry.Approve), Args: []string{string(arg)}})
	switch {
	case err == nil && resp.Result == "always":
		return mcpserver.Session, nil
	case err == nil && resp.Result == "once":
		return mcpserver.Once, nil
	case resp.Error == "cancelled":
		return mcpserver.Deny, nil
	case err == nil:
		return mcpserver.Deny, fmt.Errorf("the shell answered %q", resp.Result)
	}
	return mcpserver.Deny, err
}
