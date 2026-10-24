package assistant

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/quailyquaily/mistermorph/llm"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/desk"
)

// The benchmarks Settings → AI runs on a profile, as Mister Morph's Console
// does, but as inua uses the profile: through the agent and the decision
// route, not with bare model calls.
const (
	BenchText   = "text_reply"    // a short answer
	BenchTools  = "tool_calling"  // the agent calls a tool
	BenchChoice = "quick_choice"  // a choice from a list (Choose)
	BenchVoice  = "transcription" // a spoken sentence to text (Transcribe)
)

// speechSample is "The quick brown fox jumps over the lazy dog." spoken by
// espeak-ng, Mister Morph's sample for the same benchmark (Apache-2.0):
// 3.7 seconds of 16 kHz mono MP3. Synthetic speech is heard imperfectly,
// so four of speechWords are enough.
//
//go:embed speech_sample.mp3
var speechSample []byte

var speechWords = []string{"quick", "brown", "fox", "jumps", "lazy", "dog"}

var wordRe = regexp.MustCompile(`[a-z]+`)

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
// chats, the quick choice when it makes them, the transcription when it
// turns speech into text.
func BenchTests(p Profile) []string {
	var ids []string
	if p.Can(config.AbilityText) {
		ids = append(ids, BenchText, BenchTools)
	}
	if p.Can(config.AbilityDecision) {
		ids = append(ids, BenchChoice)
	}
	if p.Can(config.AbilityTranscription) {
		ids = append(ids, BenchVoice)
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
	// Transcription alone needs no agent: Mister Morph would not run one
	// on a profile that only transcribes.
	var a *Assistant
	if p.Can(config.AbilityText) || p.Can(config.AbilityDecision) {
		var err error
		if a, err = New(one, []desk.Tool{ping}, nil); err != nil {
			return err
		}
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
		case BenchVoice:
			text, err := transcribe(tctx, p, llm.AudioInput{Filename: "sample.mp3", MIMEType: "audio/mpeg", Data: speechSample}, "en")
			r.Raw = text
			heard := 0
			for _, w := range wordRe.FindAllString(strings.ToLower(text), -1) {
				if slices.Contains(speechWords, w) {
					heard++
				}
			}
			switch {
			case err != nil:
				r.Error = err.Error()
			case text == "":
				r.Error = "the transcript is empty"
			case heard < 4:
				r.Error = fmt.Sprintf("the transcript is not the sentence spoken (%d of %d words)", heard, len(speechWords))
			default:
				r.OK, r.Detail = true, summary(text)
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
