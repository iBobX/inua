package system

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const sink = "@DEFAULT_AUDIO_SINK@"

// AudioState is the default output's volume.
type AudioState struct {
	Available bool
	Volume    float64 // 0-1; wpctl's scale, as the volume keys use
	Muted     bool
}

// Audio follows PipeWire. `pw-dump -m` says when the graph changes and wpctl
// reads and sets the volume, so the numbers match the volume keys exactly.
type Audio struct{}

func NewAudio() *Audio { return &Audio{} }

// Run reports the state until ctx ends.
func (a *Audio) Run(ctx context.Context, onChange func(AudioState)) {
	e := &emitter[AudioState]{f: onChange}
	read := func() {
		s, err := readVolume(ctx)
		if err != nil {
			s = AudioState{}
		}
		e.emit(s)
	}
	backoff := time.Second
	for ctx.Err() == nil {
		start := time.Now()
		err := a.monitor(ctx, read)
		if ctx.Err() != nil {
			return
		}
		log.Printf("system: pw-dump: %v", err)
		e.emit(AudioState{})
		if time.Since(start) > time.Minute {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
			backoff = min(backoff*2, 30*time.Second)
		}
	}
}

// monitor runs pw-dump -m and calls read, coalesced, after relevant changes.
func (a *Audio) monitor(ctx context.Context, read func()) error {
	cmd := exec.CommandContext(ctx, "pw-dump", "-m", "-N")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	changes := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- watchDump(bufio.NewReader(out), func() {
			select {
			case changes <- struct{}{}:
			default:
			}
		})
	}()
	for {
		select {
		case <-changes:
			time.Sleep(settle) // let a burst finish
			select {
			case <-changes:
			default:
			}
			read()
		case err := <-done:
			cmd.Wait()
			if err == nil {
				err = fmt.Errorf("exited")
			}
			return err
		}
	}
}

type pwObject struct {
	ID   int    `json:"id"`
	Type string `json:"type"`
}

// watchDump decodes pw-dump's stream of JSON arrays and calls changed for
// each array touching a node, device or metadata object, including their
// removal. Other objects are ignored: wpctl itself adds and removes a client
// on every call, so following everything would loop.
func watchDump(r io.Reader, changed func()) error {
	dec := json.NewDecoder(r)
	known := map[int]bool{}
	for {
		var chunk []pwObject
		if err := dec.Decode(&chunk); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		relevant := false
		for _, o := range chunk {
			switch o.Type {
			case "PipeWire:Interface:Node", "PipeWire:Interface:Device", "PipeWire:Interface:Metadata":
				known[o.ID] = true
				relevant = true
			case "": // removed
				if known[o.ID] {
					delete(known, o.ID)
					relevant = true
				}
			}
		}
		if relevant {
			changed()
		}
	}
}

func readVolume(ctx context.Context) (AudioState, error) {
	ctx, cancel := context.WithTimeout(ctx, callLimit)
	defer cancel()
	out, err := exec.CommandContext(ctx, "wpctl", "get-volume", sink).Output()
	if err != nil {
		return AudioState{}, err
	}
	return parseVolume(string(out))
}

// parseVolume parses `wpctl get-volume`: "Volume: 0.40" or "Volume: 0.40 [MUTED]".
func parseVolume(s string) (AudioState, error) {
	f := strings.Fields(s)
	if len(f) < 2 || f[0] != "Volume:" {
		return AudioState{}, fmt.Errorf("system: unexpected wpctl output %q", s)
	}
	v, err := strconv.ParseFloat(f[1], 64)
	if err != nil {
		return AudioState{}, err
	}
	return AudioState{Available: true, Volume: v, Muted: strings.Contains(s, "[MUTED]")}, nil
}

// SetVolume sets the output volume, 0-1, and unmutes.
func (a *Audio) SetVolume(v float64) error {
	v = max(0, min(v, 1))
	if err := exec.Command("wpctl", "set-volume", sink, strconv.FormatFloat(v, 'f', 3, 64)).Run(); err != nil {
		return err
	}
	return exec.Command("wpctl", "set-mute", sink, "0").Run()
}

// ToggleMute mutes or unmutes the output.
func (a *Audio) ToggleMute() error {
	return exec.Command("wpctl", "set-mute", sink, "toggle").Run()
}
