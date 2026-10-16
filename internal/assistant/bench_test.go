package assistant

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// A chat profile runs the text and tool benchmarks, alone; the tool one
// passes only when the agent calls the tool.
func TestBenchmark(t *testing.T) {
	model := &fakeModel{t: t, replies: []reply{
		{final: "OK"},                            // text
		{tool: "ping", args: `{"message":"hi"}`}, // tools: called…
		{final: "done"},
		{final: "OK"},
		{final: "done"}, // …and not
	}}
	url := newModelServer(t, model)
	set := Settings{Profiles: []Profile{
		{Name: "chat", Provider: "openai_chat_compatible", Model: "fake", Endpoint: url + "/v1", APIKey: "k", Abilities: []string{"text"}},
	}, StateDir: t.TempDir(), CacheDir: t.TempDir()}
	if got := fmt.Sprint(BenchTests(set.Profiles[0])); got != "[text_reply tool_calling]" {
		t.Errorf("tests %s", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	run := func() []BenchResult {
		var got []BenchResult
		if err := Benchmark(ctx, set, "chat", func(r BenchResult) { got = append(got, r) }); err != nil {
			t.Fatal(err)
		}
		return got
	}
	got := run()
	if len(got) != 2 || !got[0].OK || got[0].Detail != "OK" || !got[1].OK || got[1].Detail != "called ping" {
		t.Errorf("first run %+v", got)
	}
	if got := run(); len(got) != 2 || !got[0].OK || got[1].OK || got[1].Error == "" {
		t.Errorf("without the tool call %+v", got)
	}
	if err := Benchmark(ctx, set, "nope", func(BenchResult) {}); err == nil {
		t.Error("no such profile")
	}
	if got := BenchTests(Profile{Abilities: []string{"decision"}}); fmt.Sprint(got) != "[quick_choice]" {
		t.Errorf("decision only: %v", got)
	}
}
