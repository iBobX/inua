package system

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/lyricat/inua/internal/assistant/record"
)

// Dictation is dictation's AI engine, the other to voxtype: it records the
// microphone (pw-cat), has inua-assistant turn the recording into text
// with the profile for voice input, and hands the words to Type. It
// reports the same states as Voice: "idle", "recording", "transcribing".
type Dictation struct {
	// Profile is the voice input profile's name, shown as the model.
	Profile string
	// Type puts the words into the focused window.
	Type func(text string) error
	// Failed hears why a recording came to nothing.
	Failed func(error)
	// Assistant is the program that transcribes ("inua-assistant").
	Assistant string

	mu       sync.Mutex
	ctx      context.Context
	onChange func(VoiceState)
	state    VoiceState
	rec      *record.Recorder
}

// Run reports the state until ctx ends, and drops a recording then.
func (d *Dictation) Run(ctx context.Context, onChange func(VoiceState)) {
	d.mu.Lock()
	d.ctx, d.onChange = ctx, onChange
	st := VoiceState{Available: true, Status: "idle", Model: d.Profile}
	switch {
	case d.Profile == "":
		st = VoiceState{Status: VoiceNoProfile}
	case !record.Available():
		st = VoiceState{Status: VoiceMissing} // pw-cat, here
	}
	d.state = st
	d.mu.Unlock()
	onChange(st)
	<-ctx.Done()
	d.Cancel()
}

// set changes the status and says so; d.mu is held.
func (d *Dictation) set(status string) {
	d.state.Status = status
	if d.onChange != nil && d.ctx.Err() == nil {
		d.onChange(d.state)
	}
}

// Start records.
func (d *Dictation) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ctx == nil || !d.state.Available || d.state.Status != "idle" {
		return nil
	}
	r, err := record.Start()
	if err != nil {
		return err
	}
	d.rec = r
	d.set("recording")
	go func() { // five minutes, or the microphone went away
		t := time.NewTicker(200 * time.Millisecond)
		defer t.Stop()
		for range t.C {
			d.mu.Lock()
			mine := d.rec == r
			d.mu.Unlock()
			if !mine {
				return
			}
			if r.Full() {
				d.Stop()
				return
			}
		}
	}()
	return nil
}

// Stop ends the recording; its words are typed once they are text.
func (d *Dictation) Stop() error {
	d.mu.Lock()
	r := d.rec
	if r == nil {
		d.mu.Unlock()
		return nil
	}
	d.rec = nil
	d.set("transcribing")
	ctx := d.ctx
	d.mu.Unlock()
	go func() {
		wav, err := r.Stop()
		var text string
		if err == nil {
			text, err = d.transcribe(ctx, wav)
		}
		if err == nil && text != "" && d.Type != nil {
			err = d.Type(text)
		}
		d.mu.Lock()
		d.set("idle")
		d.mu.Unlock()
		if err != nil && !errors.Is(err, record.ErrEmpty) && ctx.Err() == nil && d.Failed != nil {
			d.Failed(err)
		}
	}()
	return nil
}

// Toggle starts a recording, or ends it.
func (d *Dictation) Toggle() error {
	d.mu.Lock()
	recording := d.rec != nil
	d.mu.Unlock()
	if recording {
		return d.Stop()
	}
	return d.Start()
}

// Cancel drops the recording.
func (d *Dictation) Cancel() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.rec == nil {
		return nil
	}
	d.rec.Cancel()
	d.rec = nil
	d.set("idle")
	return nil
}

// transcribe has the assistant turn wav into text; its last line on
// stderr is the error when it cannot.
func (d *Dictation) transcribe(ctx context.Context, wav []byte) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, d.Assistant, "--transcribe")
	cmd.Stdin = bytes.NewReader(wav)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		if msg := lastLine(stderr.String()); msg != "" {
			return "", errors.New(msg)
		}
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}
