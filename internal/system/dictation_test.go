package system

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A recording goes to the assistant, and its words to Type; why it came to
// nothing goes to Failed.
func TestDictation(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	write := func(name, script string) {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("pw-cat", "head -c 32000 /dev/zero; exec sleep 30\n")
	write("inua-assistant", `[ "$1" = --transcribe ] || exit 2
n=$(wc -c)
[ "$n" -gt 44 ] || { echo "empty: $n" >&2; exit 1; }
echo "turn on focus"
`)
	typed := make(chan string, 1)
	failed := make(chan error, 1)
	d := &Dictation{Profile: "ears", Assistant: "inua-assistant",
		Type:   func(text string) error { typed <- text; return nil },
		Failed: func(err error) { failed <- err }}
	states := make(chan VoiceState, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx, func(st VoiceState) { states <- st })
	next := func() VoiceState {
		select {
		case st := <-states:
			return st
		case <-time.After(5 * time.Second):
			t.Fatal("no state")
		}
		return VoiceState{}
	}
	if st := next(); !st.Available || st.Status != "idle" || st.Model != "ears" {
		t.Fatalf("first: %+v", st)
	}
	if err := d.Toggle(); err != nil {
		t.Fatal(err)
	}
	if st := next(); st.Status != "recording" {
		t.Fatalf("recording: %+v", st)
	}
	time.Sleep(200 * time.Millisecond)
	d.Toggle()
	if st := next(); st.Status != "transcribing" {
		t.Fatalf("transcribing: %+v", st)
	}
	select {
	case text := <-typed:
		if text != "turn on focus" {
			t.Errorf("typed %q", text)
		}
	case err := <-failed:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("nothing typed")
	}
	if st := next(); st.Status != "idle" {
		t.Fatalf("after: %+v", st)
	}

	// The assistant's own words when it cannot.
	write("inua-assistant", "echo 'No profile turns speech into text.' >&2\nexit 1\n")
	d.Start()
	next()
	time.Sleep(200 * time.Millisecond)
	d.Stop()
	next()
	select {
	case err := <-failed:
		if err.Error() != "No profile turns speech into text." {
			t.Errorf("failed with %q", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no failure")
	}
}
