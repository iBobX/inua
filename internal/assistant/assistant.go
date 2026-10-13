// Package assistant is inua's agent, without GTK: Mister Morph's runtime
// (github.com/quailyquaily/mistermorph/integration) with inua's tools, a
// conversation, and approvals for what changes the system. The window is
// cmd/inua-assistant; docs/feat/assistant.md has the plan.
package assistant

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/integration"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"

	"github.com/lyricat/inua/internal/assistant/keys"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/i18n"
)

// ErrNoKey is a configuration without an API key in the environment.
var ErrNoKey = errors.New("assistant: no API key")

// Profile is a model with its key, as Mister Morph's llm.profiles has it.
type Profile struct {
	Name, Provider, Model, Endpoint, APIKey, Reasoning string
}

// Settings are what the runtime needs, resolved from config.toml, the
// saved keys and the environment.
type Settings struct {
	Profiles         []Profile // the first answers; all have a key
	Fallback         []string  // profiles tried in turn when it fails
	Missing          []string  // profiles left out for want of a key
	Tools            []string  // Mister Morph's built-ins to add
	AskBeforeChanges bool
	StateDir         string // Mister Morph's files; empty for inua's defaults
	CacheDir         string
	MaxSteps         int
	ServeListen      string
	ServeToken       string
}

// keyless are the providers for servers that may want no key (a model
// served on this computer).
var keyless = []string{"openai_chat_compatible", "openai_response_compatible", "anthropic_compatible"}

// SettingsFrom resolves the [assistant] section. A profile's key is the
// one saved in the settings window (saved, by profile), else the
// variable it names; profiles without one are left out, and ErrNoKey is
// the answering profile without one.
func SettingsFrom(c config.Assistant, saved map[string]string) (Settings, error) {
	s := Settings{Tools: c.Tools, AskBeforeChanges: c.AskBeforeChanges}
	if c.ServeEnable {
		s.ServeListen = strings.TrimSpace(c.ServeListen)
		if _, _, err := net.SplitHostPort(s.ServeListen); err != nil {
			return s, fmt.Errorf("assistant.serve_listen: want host:port: %w", err)
		}
		s.ServeToken = strings.TrimSpace(saved[keys.ServeTokenKey])
		if s.ServeToken == "" {
			return s, errors.New("assistant: create the Console token in Settings → AI or with inua-assistant --serve-token before enabling the endpoint")
		}
	}
	primary, ok := c.ProfileNamed(c.Profile)
	if !ok {
		return s, fmt.Errorf("assistant: no profile %q", c.Profile)
	}
	resolve := func(p config.AIProfile) (Profile, bool) {
		key := saved[p.Name]
		if key == "" && slices.Contains(keyless, p.Provider) {
			key = "none"
		}
		return Profile{Name: p.Name, Provider: p.Provider, Model: p.Model, Endpoint: p.Endpoint, APIKey: key, Reasoning: p.Reasoning}, key != ""
	}
	first, ok := resolve(primary)
	if !ok {
		return s, fmt.Errorf("%w for %s: type one in Settings → AI", ErrNoKey, primary.Name)
	}
	s.Profiles = []Profile{first}
	for _, p := range c.Profiles {
		if p.Name == primary.Name {
			continue
		}
		if r, ok := resolve(p); ok {
			s.Profiles = append(s.Profiles, r)
		} else {
			s.Missing = append(s.Missing, p.Name)
		}
	}
	for _, name := range c.Fallback {
		if slices.ContainsFunc(s.Profiles, func(p Profile) bool { return p.Name == name }) {
			s.Fallback = append(s.Fallback, name)
		}
	}
	return s, nil
}

// Risk says whether a tool asks before it runs.
type Risk int

const (
	Read   Risk = iota // only looks
	Change             // changes something easily undone; asks when AskBeforeChanges
	Always             // changes something hard to undo, or reaches past inua; always asks
)

// Tool is one of inua's tools.
type Tool struct {
	Name, Description string
	Schema            string // JSON schema of the arguments
	Risk              Risk
	// Describe says what a call is about to do, for the window: "Turning
	// on Focus". Nil uses the tool's name.
	Describe func(args map[string]any) string
	Run      func(ctx context.Context, args map[string]any) (any, error)
}

// Events reach the window while a question is answered. They come from the
// run's goroutines.
type Events struct {
	Answer func(text string)            // the answer so far, as it streams
	Tool   func(name, doing string)     // a tool starts
	Denied func(name, doing string)     // the user declined one
	Error  func(name string, err error) // a tool failed
}

// Approver asks the user whether to go ahead; false declines. It blocks
// until they answer or ctx ends.
type Approver func(ctx context.Context, tool, doing string) (bool, error)

// Assistant is one conversation with the agent.
type Assistant struct {
	set     Settings
	profile string // the profile answering this conversation
	rt      *integration.Runtime
	reg     *tools.Registry
	approve Approver

	mu      sync.Mutex
	history []llm.Message
	busy    bool
}

// prompt is inua's policy for the agent, added to Mister Morph's prompt.
const prompt = `[[ inua ]]
You are the assistant of inua, the desktop shell on this Linux computer (Hyprland). The user
asks from the launcher or your window; answer in their language, briefly, in plain text.
- Use the tools to look at and act on this computer: windows, apps, Wi-Fi, Bluetooth, volume,
  brightness, Focus, notifications, the clipboard, processes, Tailscale, the weather.
- Never say you did something unless a tool did it. Do one thing at a time when acting.
- Some tools ask the user first. If they decline, say so and do not try another way.
- Read only what the question needs; say which device or window you mean by its name.`

// New makes a conversation with inua's tools (own) added to the built-ins
// the settings name.
func New(set Settings, own []Tool, approve Approver) (*Assistant, error) {
	data, _ := os.UserHomeDir()
	stateDir := cmp.Or(set.StateDir, filepath.Join(cmp.Or(os.Getenv("XDG_DATA_HOME"), filepath.Join(data, ".local", "share")), "inua", "assistant"))
	cacheDir := set.CacheDir
	if cacheDir == "" {
		c, err := os.UserCacheDir()
		if err != nil {
			c = stateDir
		}
		cacheDir = filepath.Join(c, "inua", "assistant")
	}
	for _, d := range []string{stateDir, cacheDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
	}
	cfg := integration.DefaultConfig()
	cfg.Features = integration.Features{Guard: true} // no skills from disk, no plans
	// An empty list means every built-in; a name no tool has means none.
	cfg.BuiltinToolNames = set.Tools
	if len(cfg.BuiltinToolNames) == 0 {
		cfg.BuiltinToolNames = []string{"-"}
	}
	if len(set.Profiles) == 0 {
		return nil, fmt.Errorf("assistant: no profile")
	}
	// The first profile is also Mister Morph's default (llm.*); every one
	// is a named profile, and the main loop's route falls back in turn.
	first := set.Profiles[0]
	for k, v := range profileValues(first) {
		cfg.Set("llm."+k, v)
	}
	profiles := map[string]any{}
	for _, p := range set.Profiles {
		profiles[p.Name] = profileValues(p)
	}
	cfg.Set("llm.profiles", profiles)
	route := map[string]any{"profile": first.Name}
	if len(set.Fallback) > 0 {
		route["fallback_profiles"] = set.Fallback
	}
	cfg.Set("llm.routes", map[string]any{"main_loop": route})
	cfg.Set("file_state_dir", stateDir)
	cfg.Set("file_cache_dir", cacheDir)
	cfg.Set("max_steps", cmp.Or(set.MaxSteps, 24))
	cfg.Set("tool_repeat_limit", 3)
	cfg.Set("server.listen", set.ServeListen)
	cfg.Set("server.auth_token", set.ServeToken)
	cfg.AddPromptBlock(prompt)
	rt, err := integration.NewChecked(cfg)
	if err != nil {
		return nil, fmt.Errorf("assistant: %w", err)
	}
	a := &Assistant{set: set, rt: rt, approve: approve, profile: first.Name}
	a.reg = rt.NewRegistry()
	// Every tool, built-in or inua's, goes through wrap: the window sees it
	// start, and changes wait for the user.
	for _, t := range a.reg.All() {
		if err := a.reg.Replace(&wrapped{a: a, name: t.Name(), risk: builtinRisk(t.Name()), inner: t}); err != nil {
			return nil, err
		}
	}
	for _, t := range own {
		if err := a.reg.Register(&wrapped{a: a, name: t.Name, risk: t.Risk, inner: inuaTool{t}, describe: t.Describe}); err != nil {
			return nil, fmt.Errorf("assistant: tool %s: %w", t.Name, err)
		}
	}
	return a, nil
}

// profileValues are a profile's keys under llm.* or llm.profiles.NAME.
func profileValues(p Profile) map[string]any {
	v := map[string]any{"inference_provider": p.Provider, "model": p.Model, "api_key": p.APIKey}
	if p.Endpoint != "" {
		v["endpoint"] = p.Endpoint
	}
	if p.Reasoning != "" {
		v["reasoning_effort"] = p.Reasoning
	}
	return v
}

// Profiles names the profiles the window can switch to, the first
// answering unless switched.
func (a *Assistant) Profiles() []string {
	names := make([]string, len(a.set.Profiles))
	for i, p := range a.set.Profiles {
		names[i] = p.Name
	}
	return names
}

// UseProfile switches this conversation to another profile; the route's
// fallbacks still apply.
func (a *Assistant) UseProfile(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.busy {
		return ErrBusy
	}
	if !slices.ContainsFunc(a.set.Profiles, func(p Profile) bool { return p.Name == name }) {
		return fmt.Errorf("assistant: no profile %q", name)
	}
	a.profile = name
	return nil
}

// builtinRisk: reading files and running commands reach past inua.
func builtinRisk(name string) Risk {
	if slices.Contains([]string{"read_file", "write_file", "bash", "powershell"}, name) {
		return Always
	}
	return Read
}

// ToolNames lists the tools the agent has.
func (a *Assistant) ToolNames() []string {
	var names []string
	for _, t := range a.reg.All() {
		names = append(names, t.Name())
	}
	slices.Sort(names)
	return names
}

// Model names who answers, for the window: "gpt-5.2 · openai".
func (a *Assistant) Model() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range a.set.Profiles {
		if p.Name == a.profile {
			return p.Model + " · " + p.Provider
		}
	}
	return a.profile
}

// Profile is the profile answering this conversation.
func (a *Assistant) Profile() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.profile
}

// ErrBusy is a question asked while another is being answered.
var ErrBusy = errors.New("assistant: still answering")

// Ask answers a question in the conversation, streaming through ev, and
// returns the answer. Cancelling ctx stops it.
func (a *Assistant) Ask(ctx context.Context, question string, ev Events) (string, error) {
	a.mu.Lock()
	if a.busy {
		a.mu.Unlock()
		return "", ErrBusy
	}
	a.busy = true
	history := slices.Clone(a.history)
	profile := a.profile
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.busy = false
		a.mu.Unlock()
	}()

	var stream answerStream
	ctx = context.WithValue(ctx, eventsKey{}, ev)
	res, err := a.rt.RunTaskWithOptions(ctx, question, integration.RunTaskOptions{
		Registry:    a.reg,
		LLMProfile:  profile,
		PersistTask: a.set.ServeListen != "",
		Agent: agent.RunOptions{
			History: history,
			OnStream: func(e llm.StreamEvent) error {
				if e.Delta == "" {
					return nil
				}
				if text, ok := stream.add(e.Delta); ok && ev.Answer != nil {
					ev.Answer(text)
				}
				return nil
			},
		},
	})
	if err != nil {
		return "", err
	}
	answer := finalText(res.Final)
	if ev.Answer != nil {
		ev.Answer(answer) // the whole of it, whatever the stream showed
	}
	a.mu.Lock()
	a.history = append(a.history, llm.Message{Role: "user", Content: question}, llm.Message{Role: "assistant", Content: answer})
	a.mu.Unlock()
	return answer, nil
}

// Carry takes over another assistant's conversation, as when the
// settings change in the middle of one.
func (a *Assistant) Carry(from *Assistant) {
	from.mu.Lock()
	h := slices.Clone(from.history)
	from.mu.Unlock()
	a.mu.Lock()
	a.history = h
	a.mu.Unlock()
}

// Reset starts the conversation over.
func (a *Assistant) Reset() {
	a.mu.Lock()
	a.history = nil
	a.mu.Unlock()
}

func finalText(f *agent.Final) string {
	if f == nil {
		return ""
	}
	switch o := f.Output.(type) {
	case string:
		return strings.TrimSpace(o)
	case nil:
		return ""
	default:
		b, _ := json.MarshalIndent(o, "", "  ")
		return string(b)
	}
}

// Tool events belong to the local question, not to concurrent Console tasks.
type eventsKey struct{}

// wrapped is a tool the window sees run, which asks first when it must.
type wrapped struct {
	a        *Assistant
	name     string
	risk     Risk
	inner    tools.Tool
	describe func(map[string]any) string
}

func (w *wrapped) Name() string            { return w.name }
func (w *wrapped) Description() string     { return w.inner.Description() }
func (w *wrapped) ParameterSchema() string { return w.inner.ParameterSchema() }

func (w *wrapped) Execute(ctx context.Context, args map[string]any) (string, error) {
	doing := w.name
	if w.describe != nil {
		doing = w.describe(args)
	}
	ev, local := ctx.Value(eventsKey{}).(Events)
	if ev.Tool != nil {
		ev.Tool(w.name, doing)
	}
	if w.risk == Always || (w.risk == Change && w.a.set.AskBeforeChanges) {
		ok := false
		if w.a.approve != nil {
			var err error
			prompt := doing
			if !local {
				prompt = i18n.Tf("Remote Console: %s", prompt)
			}
			if ok, err = w.a.approve(ctx, w.name, prompt); err != nil {
				return "", err
			}
		}
		if !ok {
			if ev.Denied != nil {
				ev.Denied(w.name, doing)
			}
			// Not an error: the agent tells the user rather than retrying.
			return `{"declined": true, "note": "The user declined this. Do not try it another way."}`, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	out, err := w.inner.Execute(ctx, args)
	if err != nil && ev.Error != nil {
		ev.Error(w.name, err)
	}
	return out, err
}

// inuaTool adapts a Tool to Mister Morph's interface: the result as JSON.
type inuaTool struct{ t Tool }

func (i inuaTool) Name() string        { return i.t.Name }
func (i inuaTool) Description() string { return i.t.Description }
func (i inuaTool) ParameterSchema() string {
	return cmp.Or(i.t.Schema, `{"type":"object","properties":{}}`)
}

func (i inuaTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	v, err := i.t.Run(ctx, args)
	if err != nil {
		return "", err
	}
	if s, ok := v.(string); ok {
		return s, nil
	}
	b, err := json.Marshal(v)
	return string(b), err
}
