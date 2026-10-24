package system

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The state file says what the daemon does; the model is asked once, when
// the daemon comes up, not at every poll.
func TestVoice(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	asked := filepath.Join(dir, "asked")
	os.Mkdir(bin, 0o755)
	t.Setenv("PATH", bin)
	state := filepath.Join(dir, "state")
	v := NewVoice(state)

	if st := v.poll(); st.Available || st.Status != VoiceMissing {
		t.Fatalf("no voxtype: %+v", st)
	}
	script := "#!/bin/sh\necho >>" + asked + "\necho '{\"model\": \"small-int8\"}'\n"
	if err := os.WriteFile(filepath.Join(bin, "voxtype"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if st := v.poll(); st.Available || st.Status != VoiceStopped {
		t.Fatalf("no daemon: %+v", st)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan VoiceState, 16)
	go v.Run(ctx, func(st VoiceState) { got <- st })
	next := func() VoiceState {
		select {
		case st := <-got:
			return st
		case <-time.After(5 * time.Second):
			t.Fatal("no state")
		}
		return VoiceState{}
	}
	if st := next(); st.Available {
		t.Fatalf("first: %+v", st)
	}
	os.WriteFile(state, []byte("idle\n"), 0o644)
	if st := next(); !st.Available || st.Status != "idle" || st.Model != "small-int8" {
		t.Fatalf("up: %+v", st)
	}
	os.WriteFile(state, []byte("recording\n"), 0o644)
	if st := next(); st.Status != "recording" || st.Model != "small-int8" {
		t.Fatalf("recording: %+v", st)
	}
	if b, _ := os.ReadFile(asked); len(b) != 1 {
		t.Errorf("the model was asked %d times, want once", len(b))
	}
}

// Starting the daemon passes systemctl's own words back.
func TestStartDaemon(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	script := "#!/bin/sh\necho 'Failed to start voxtype.service: Unit voxtype.service not found.' >&2\nexit 5\n"
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := NewVoice(filepath.Join(bin, "state")).StartDaemon(); err == nil || err.Error() != "Failed to start voxtype.service: Unit voxtype.service not found." {
		t.Errorf("got %v", err)
	}
}
