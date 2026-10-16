package assistant

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/desk"
)

// The benchmarks Settings → AI runs on a profile, as Mister Morph's Console
// does, but as inua uses the profile: through the agent and the decision
// route, not with bare model calls.
const (
	BenchText   = "text_reply"   // a short answer
	BenchTools  = "tool_calling" // the agent calls a tool
	BenchChoice = "quick_choice" // a choice from a list (Choose)
)

// BenchResult is how one benchmark went.
type BenchResult struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Millis int64  `json:"ms"`
	Detail string `json:"detail,omitempty"` // what it answered, when it passed
	Error  string `json:"error,omitempty"`  // why it failed
	Raw    string `json:"raw,omitempty"`    // the answer as it came
}

// BenchTests are the benchmarks a profile runs: the chat ones when it
// chats, the quick choice when it makes them.
func BenchTests(p Profile) []string {
	var ids []string
	if p.Can(config.AbilityText) {
		ids = append(ids, BenchText, BenchTools)
	}
	if p.Can(config.AbilityDecision) {
		ids = append(ids, BenchChoice)
	}
	return ids
}

// benchTimeout bounds each benchmark.
const benchTimeout = 90 * time.Second

// Benchmark runs the benchmarks of the profile named, alone: no fallbacks,
// no tools but a ping, so a failing profile fails. Each result goes to
// each as it ends.
func Benchmark(ctx context.Context, set Settings, name string, each func(BenchResult)) error {
	p, ok := set.profile(name)
	if !ok {
		return fmt.Errorf("assistant: no profile %q with a key", name)
	}
	one := set
	one.Profiles, one.Fallback, one.Decision = []Profile{p}, nil, ""
	if p.Can(config.AbilityDecision) {
		one.Decision = p.Name
	}
	one.Tools, one.MCPServer, one.ServeListen, one.ServeToken = nil, nil, "", ""
	one.MaxSteps = 4
	var pinged atomic.Bool
	ping := desk.Tool{
		Name: "ping", Description: "Connectivity check tool.",
		Schema: `{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}`,
		Risk:   desk.Read,
		Run: func(context.Context, map[string]any) (any, error) {
			pinged.Store(true)
			return "pong", nil
		},
	}
	a, err := New(one, []desk.Tool{ping}, nil)
	if err != nil {
		return err
	}
	for _, id := range BenchTests(p) {
		tctx, cancel := context.WithTimeout(ctx, benchTimeout)
		start := time.Now()
		r := BenchResult{ID: id}
		switch id {
		case BenchText:
			a.Reset()
			text, err := a.Ask(tctx, "Reply with the single word: OK", Events{})
			r.Raw = strings.TrimSpace(text)
			switch {
			case err != nil:
				r.Error = err.Error()
			case r.Raw == "":
				r.Error = "received an empty reply"
			default:
				r.OK, r.Detail = true, summary(r.Raw)
			}
		case BenchTools:
			a.Reset()
			pinged.Store(false)
			text, err := a.Ask(tctx, `Call the ping tool now, with the message "hi". Then reply: done.`, Events{})
			r.Raw = strings.TrimSpace(text)
			switch {
			case pinged.Load():
				r.OK, r.Detail = true, "called ping"
			case err != nil:
				r.Error = err.Error()
			default:
				r.Error = "replied without calling the tool"
			}
		case BenchChoice:
			options := []string{"a hammer", "an apple", "a cloud"}
			i, err := a.Choose(tctx, p.Name, "Pick the option that is a fruit.", "Nothing else to judge.", options)
			switch {
			case errors.Is(err, ErrNoChoice):
				r.Error = "this profile cannot make quick choices (Evaluate)"
			case err != nil:
				r.Error = err.Error()
			case i != 1:
				r.Error, r.Raw = "picked the wrong option", options[i]
			default:
				r.OK, r.Detail, r.Raw = true, "picked "+options[i], options[i]
			}
		}
		r.Millis = time.Since(start).Milliseconds()
		cancel()
		each(r)
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}

// summary is an answer shortened to a line.
func summary(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return s
}
