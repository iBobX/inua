package assistant

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"

	"github.com/lyricat/inua/internal/desk"
	"github.com/lyricat/inua/internal/i18n"
)

// inua's tools reach the assistant through inua's MCP server, as they
// reach agents (docs/feat/mcp.md): Mister Morph's MCP client starts
// `inua-assistant --mcp`, names its tools mcp_inua__<tool>, and its
// tool_search keeps them out of requests until the model finds them.
// The server asks before changes, by the assistant's settings.

// mcpServerName is inua's server among Mister Morph's MCP servers.
const mcpServerName = "inua"

// MCPClientEnv tells the server it was started by the assistant (with its
// parent's program: an agent cannot claim it by setting the variable).
const MCPClientEnv = "INUA_MCP_CLIENT"

// mcpToolPrefix is how Mister Morph names inua's tools.
const mcpToolPrefix = "mcp_" + mcpServerName + "__"

// newTopic names a conversation: Mister Morph keeps what tool_search found
// for it, so later questions see those tools without searching again.
func newTopic() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "inua-" + hex.EncodeToString(b)
}

// describers say what inua's tools are doing, for the window's lines.
var describers = func() map[string]func(map[string]any) string {
	m := map[string]func(map[string]any) string{}
	for _, t := range desk.Tools(desk.Desktop{}) {
		if t.Describe != nil {
			m[t.Name] = t.Describe
		}
	}
	return m
}()

// mcpEvents shows in the window the calls that do not go through wrapped:
// inua's tools over MCP, tool_search and codemode.
func mcpEvents(ev Events) agent.EventSink {
	return agent.EventSinkFunc(func(_ context.Context, e agent.Event) {
		name, ok := strings.CutPrefix(e.ToolName, mcpToolPrefix)
		doing := name
		if d := describers[name]; ok && d != nil {
			doing = cmp.Or(d(e.Args), name)
		}
		switch {
		case e.Kind == agent.EventKindToolStart && ev.Tool != nil:
			switch {
			case ok:
				ev.Tool(name, doing)
			case e.ToolName == "tool_search":
				q, _ := e.Args["query"].(string)
				ev.Tool(e.ToolName, i18n.Tf("Looking for tools: %s", q))
			case e.ToolName == "codemode":
				ev.Tool(e.ToolName, i18n.T("Running a script with the tools"))
			}
		case e.Kind == agent.EventKindToolDone && ok && e.Error != "":
			if strings.Contains(e.Error, desk.DeclinedPrefix) {
				if ev.Denied != nil {
					ev.Denied(name, doing)
				}
			} else if ev.Error != nil {
				ev.Error(name, errString(e.Error))
			}
		}
	})
}

type errString string

func (e errString) Error() string { return string(e) }
