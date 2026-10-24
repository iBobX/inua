package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/lyricat/inua/internal/term/agent"
	"github.com/lyricat/inua/internal/term/hooks"
	"github.com/lyricat/inua/internal/term/host"
)

// The commands inua-term takes besides the window: report, agents,
// integration. The host and serve are inua-termd's.

// report tells the host what the agent in this pane does. Run from an
// agent's hooks (--hook claude, --hook codex), it reads what the agent
// passes; outside inua-term it does nothing, so a hook never fails.
func report(args []string) int {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	state := fs.String("state", "", "working, blocked, done or idle")
	title := fs.String("title", "", "what the agent works on")
	summary := fs.String("summary", "", "what it asks, or did")
	kind := fs.String("agent", os.Getenv("INUA_TERM_AGENT"), "the agent's type (claude, codex…)")
	hook := fs.String("hook", "", "read an agent's hook: claude (its JSON on stdin) or codex (its JSON argument)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: inua-term report [--state S] [--title T] [--summary S] [--agent TYPE] [--hook claude|codex]")
		fs.PrintDefaults()
	}
	fs.Parse(args)
	pane, _ := strconv.Atoi(os.Getenv("INUA_TERM_PANE"))
	sock := os.Getenv("INUA_TERM_SOCK")
	if pane == 0 || sock == "" {
		if *hook != "" {
			return 0 // an agent run outside inua-term
		}
		fmt.Fprintln(os.Stderr, "inua-term report: not in an inua-term pane")
		return 1
	}
	switch *hook {
	case "":
	case "claude":
		*kind = "claude"
		*state, *title, *summary = claudeHook(os.Stdin)
	case "codex":
		*kind = "codex"
		*state, *title, *summary = codexHook(fs.Args())
	default:
		fmt.Fprintf(os.Stderr, "inua-term report: no hook %q\n", *hook)
		return 2
	}
	c, err := host.Dial(sock, nil, nil)
	if err == nil {
		err = c.Report(pane, *kind, *state, *title, *summary)
		c.Close()
	}
	if err != nil && *hook == "" {
		fmt.Fprintf(os.Stderr, "inua-term report: %v\n", err)
		return 1
	}
	return 0
}

// claudeHook reads a Claude Code hook's JSON: a prompt sent is working
// (on that prompt), a notification asks (or says it waits for input), a
// stop is done.
func claudeHook(r io.Reader) (state, title, summary string) {
	var in struct {
		Event   string `json:"hook_event_name"`
		Prompt  string `json:"prompt"`
		Message string `json:"message"`
	}
	if json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(&in) != nil {
		return "", "", ""
	}
	switch in.Event {
	case "UserPromptSubmit":
		return string(agent.Working), firstLine(in.Prompt), ""
	case "Notification":
		if strings.Contains(strings.ToLower(in.Message), "waiting for your input") {
			// Also sent a while after a turn already seen: idle, which is
			// done only if it was still working.
			return string(agent.Idle), "", ""
		}
		return string(agent.Blocked), "", in.Message
	case "Stop":
		return string(agent.Done), "", ""
	}
	return "", "", ""
}

// codexHook reads Codex's notify argument: a turn complete is done.
func codexHook(args []string) (state, title, summary string) {
	if len(args) == 0 {
		return "", "", ""
	}
	var in struct {
		Type   string   `json:"type"`
		Inputs []string `json:"input-messages"`
		Last   string   `json:"last-assistant-message"`
	}
	if json.Unmarshal([]byte(args[len(args)-1]), &in) != nil || in.Type != "agent-turn-complete" {
		return "", "", ""
	}
	if len(in.Inputs) > 0 {
		title = firstLine(in.Inputs[len(in.Inputs)-1])
	}
	return string(agent.Done), title, firstLine(in.Last)
}

// firstLine is text's first line, at most 80 characters.
func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if r := []rune(s); len(r) > 80 {
		s = string(r[:79]) + "…"
	}
	return s
}

// agents prints the agents in every pane (--json for scripts).
func agents(args []string) int {
	fs := flag.NewFlagSet("agents", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	fs.Parse(args)
	type row struct {
		Pane      int    `json:"pane"`
		Workspace string `json:"workspace"`
		Session   string `json:"session"`
		Agent     string `json:"agent"`
		Name      string `json:"name"`
		State     string `json:"state"`
		Since     int64  `json:"since"`
		Title     string `json:"title"`
		Ask       string `json:"ask,omitempty"`
	}
	rows := []row{}
	if c, err := host.Dial(host.SocketPath(), nil, nil); err == nil {
		st, err := c.State()
		c.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "inua-term agents: %v\n", err)
			return 1
		}
		info := map[int]host.PaneInfo{}
		for _, p := range st.Panes {
			info[p.ID] = p
		}
		for _, w := range st.Model.Workspaces {
			for _, s := range w.Sessions {
				for _, id := range s.Tree.Panes() {
					p := info[id]
					if p.Agent == "" {
						continue
					}
					t, _ := agent.Title(p.Title)
					rows = append(rows, row{id, w.Name, s.Name, p.Agent, p.AgentName, p.State, p.Since, t, p.Ask})
				}
			}
		}
	}
	if *asJSON {
		e := json.NewEncoder(os.Stdout)
		e.SetIndent("", "  ")
		e.Encode(rows)
		return 0
	}
	for _, r := range rows {
		fmt.Printf("%-10s %-8s %s / %s: %s\n", r.Name, strings.ToUpper(r.State), r.Workspace, r.Session, r.Title)
	}
	return 0
}

// integration removes inua-term's hooks from an agent's settings.
func integration(args []string) int {
	if len(args) != 2 || args[0] != "remove" {
		fmt.Fprintln(os.Stderr, "usage: inua-term integration remove claude|codex|all")
		return 2
	}
	which := args[1]
	ok := true
	for _, a := range []struct {
		name   string
		path   string
		remove func(string) (bool, error)
	}{{"claude", hooks.ClaudePath(), hooks.RemoveClaude}, {"codex", hooks.CodexPath(), hooks.RemoveCodex}} {
		if which != a.name && which != "all" {
			continue
		}
		changed, err := a.remove(a.path)
		switch {
		case err != nil:
			fmt.Fprintf(os.Stderr, "inua-term: %s: %v\n", a.path, err)
			ok = false
		case changed:
			fmt.Printf("inua-term's hooks are out of %s (the file before is %s.bak)\n", a.path, a.path)
		default:
			fmt.Printf("%s has no hooks of inua-term's\n", a.path)
		}
	}
	if which != "claude" && which != "codex" && which != "all" {
		fmt.Fprintf(os.Stderr, "inua-term: no agent %q\n", which)
		return 2
	}
	if !ok {
		return 1
	}
	return 0
}
