package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// VoiceState is what the voice daemon is doing, for the bar and the
// settings.
type VoiceState struct {
	Available bool // voxtype is installed and the daemon answers
	// Status is VoiceMissing or VoiceStopped when it is not available,
	// else the daemon's own: "idle", "recording", "transcribing" …
	Status string
	Model  string // the loaded model, as the daemon reports it
}

// Why voice input is not available.
const (
	VoiceMissing = "missing" // voxtype is not installed
	VoiceStopped = "stopped" // installed, its daemon not running
	// VoiceNoProfile is the AI engine (Dictation) with no profile for
	// voice input.
	VoiceNoProfile = "no-profile"
)

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

// VoxtypePath is the voxtype program: on PATH, else where InstallVoxtype
// puts it (~/.local/bin, which a session's PATH may lack); "" for none.
func VoxtypePath() string {
	if p, err := exec.LookPath("voxtype"); err == nil {
		return p
	}
	if p := voxtypeInstallPath(); p != "" {
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
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
		if VoxtypePath() == "" {
			return VoiceState{Status: VoiceMissing}
		}
		return VoiceState{Status: VoiceStopped}
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
	out, err := exec.CommandContext(ctx, VoxtypePath(), "status", "--format", "json", "--extended").Output()
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
	return exec.CommandContext(ctx, VoxtypePath(), "record", what).Run()
}

// StartDaemon starts voxtype's user service, with its error when it does
// not start.
func (v *Voice) StartDaemon() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "--user", "start", "voxtype.service").CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	return nil
}
