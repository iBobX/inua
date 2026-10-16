package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"

	"github.com/lyricat/inua/internal/assistant"
	"github.com/lyricat/inua/internal/assistant/keys"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/i18n"
)

// benchHead is the first line --benchmark prints: the profile and the
// benchmarks to come, or why none can run.
type benchHead struct {
	Profile  string   `json:"profile,omitempty"`
	Provider string   `json:"provider,omitempty"`
	Model    string   `json:"model,omitempty"`
	Endpoint string   `json:"endpoint,omitempty"`
	Tests    []string `json:"tests,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// runBenchmark runs a profile's benchmarks (assistant.Benchmark) for
// Settings → AI, a JSON object a line on stdout: the head, then each
// result as it ends.
func runBenchmark(ctx context.Context, name string) int {
	out := json.NewEncoder(os.Stdout)
	os.Stdout = os.Stderr // the results' alone
	log.SetOutput(os.Stderr)
	log.SetPrefix("inua-assistant --benchmark: ")
	cfg, _, err := config.Load(config.DefaultPath())
	if err != nil {
		cfg = config.Default()
	}
	i18n.Init(cfg.Language)
	c := cfg.Assistant
	c.ServeEnable = false
	fail := func(err error) int {
		out.Encode(benchHead{Profile: name, Error: err.Error()})
		return 1
	}
	if _, ok := c.ProfileNamed(name); !ok {
		return fail(errors.New(i18n.Tf("No profile %s", name)))
	}
	set, err := assistant.Load(c, keys.New())
	if err != nil && !errors.Is(err, assistant.ErrNoKey) {
		return fail(err)
	}
	head := benchHead{Profile: name}
	for _, p := range set.Profiles {
		if p.Name == name {
			head.Provider, head.Model, head.Endpoint, head.Tests = p.Provider, p.Model, p.Endpoint, assistant.BenchTests(p)
		}
	}
	if head.Tests == nil {
		return fail(errors.New(i18n.Tf("No API key for %s: type one and Save first.", name)))
	}
	out.Encode(head)
	err = assistant.Benchmark(ctx, set, name, func(r assistant.BenchResult) { out.Encode(r) })
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Print(err)
		return 1
	}
	return 0
}
