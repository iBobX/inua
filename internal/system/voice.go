package system

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// VoiceState is what the voice daemon is doing, for the bar and the
// settings.
type VoiceState struct {
	Available bool // voxtype is installed and the daemon answers
	Status    string
	Model     string // the loaded model, as the daemon reports it
	Error     string // why it is not available, in a word
}

// Voice follows voxtype: the daemon's state file for what it is doing, and
// its CLI to start and stop recordings. When voxtype is not there, every
// call degrades to "off".
type Voice struct {
	statePath string
}

// NewVoice follows voxtype. statePath is where the daemon writes its
// state; "" reads the daemon's own default, under $XDG_RUNTIME_DIR.
func NewVoice(statePath string) *Voice {
	if statePath == "" {
		dir := os.Getenv("XDG_RUNTIME_DIR")
		if dir == "" {
			dir = fmt.Sprintf("/run/user/%d", os.Getuid())
		}
		statePath = filepath.Join(dir, "voxtype", "state")
	}
	return &Voice{statePath: statePath}
}

// voicePoll is how often Run reads the state file: often enough for the
// bar's microphone to follow a recording that a key started.
const voicePoll = 500 * time.Millisecond

// Run reports the state until ctx ends. Each poll reads the state file
// alone; the daemon is asked for its model only when it comes up.
func (v *Voice) Run(ctx context.Context, onChange func(VoiceState)) {
	var st VoiceState
	first := true
	tick := time.NewTicker(voicePoll)
	defer tick.Stop()
	for {
		next := v.poll()
		if next.Available {
			if st.Available {
				next.Model = st.Model
			} else {
				next.Model = loadedModel(ctx)
			}
		}
		if ctx.Err() != nil {
			return
		}
		if first || next != st {
			st, first = next, false
			onChange(st)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// poll reads the daemon's state once, without the model.
func (v *Voice) poll() VoiceState {
	data, err := os.ReadFile(v.statePath)
	if err != nil {
		if _, err := exec.LookPath("voxtype"); err != nil {
			return VoiceState{Status: "unavailable", Error: "voxtype is not installed"}
		}
		return VoiceState{Status: "stopped", Error: "the daemon is not running"}
	}
	st := strings.TrimSpace(string(data))
	if st == "" {
		st = "idle"
	}
	return VoiceState{Available: true, Status: st}
}

// loadedModel asks the daemon's status for the model it has. It fails
// quietly: the state file is the source of truth for what it is doing.
func loadedModel(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "voxtype", "status", "--format", "json", "--extended").Output()
	if err != nil {
		return ""
	}
	var st struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(out, &st) != nil {
		return ""
	}
	return st.Model
}

// Start asks the daemon to record.
func (v *Voice) Start() error { return v.record("start") }

// Stop ends the recording and transcribes it.
func (v *Voice) Stop() error { return v.record("stop") }

// Toggle starts a recording, or ends it.
func (v *Voice) Toggle() error { return v.record("toggle") }

// Cancel throws the recording away.
func (v *Voice) Cancel() error { return v.record("cancel") }

func (v *Voice) record(what string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "voxtype", "record", what).Run()
}

// SetupModelCommand is the command line that opens voxtype's interactive
// model setup, which lists and downloads its models, in the terminal the
// settings give; the view runs it.
func SetupModelCommand(term []string, dashE bool) *exec.Cmd {
	args := slices.Clone(term[1:])
	if dashE {
		args = append(args, "-e")
	}
	return exec.Command(term[0], append(args, "voxtype", "setup", "model")...)
}
