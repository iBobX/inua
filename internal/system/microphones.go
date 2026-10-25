package system

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
)

// Microphone is an audio input PipeWire has: its node id (wpctl's) and its
// description, "DJI MIC MINI Analog Stereo".
type Microphone struct {
	ID          int
	Name, Title string
}

// Microphones lists PipeWire's audio inputs (`pw-dump`) and the one that is
// the default, which dictation records from, whichever engine.
func Microphones(ctx context.Context) ([]Microphone, string, error) {
	out, err := exec.CommandContext(ctx, "pw-dump", "-N").Output()
	if err != nil {
		return nil, "", err
	}
	return parseMicrophones(out)
}

// parseMicrophones reads pw-dump's objects: the Audio/Source nodes, and the
// "default" metadata's default.audio.source, the default's node name.
func parseMicrophones(dump []byte) ([]Microphone, string, error) {
	var objs []struct {
		ID    int    `json:"id"`
		Type  string `json:"type"`
		Props struct {
			Name string `json:"metadata.name"`
		} `json:"props"`
		Info *struct {
			Props map[string]any `json:"props"`
		} `json:"info"`
		Metadata []struct {
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(dump, &objs); err != nil {
		return nil, "", err
	}
	var mics []Microphone
	var def string
	for _, o := range objs {
		if o.Type == "PipeWire:Interface:Metadata" && o.Props.Name == "default" {
			for _, m := range o.Metadata {
				var v struct{ Name string }
				if m.Key == "default.audio.source" && json.Unmarshal(m.Value, &v) == nil {
					def = v.Name
				}
			}
		}
		if o.Info == nil {
			continue
		}
		p := o.Info.Props
		if class, _ := p["media.class"].(string); class != "Audio/Source" {
			continue
		}
		name, _ := p["node.name"].(string)
		title, _ := p["node.description"].(string)
		mics = append(mics, Microphone{ID: o.ID, Name: name, Title: strings.TrimSpace(title)})
	}
	return mics, def, nil
}

// SetMicrophone makes a microphone PipeWire's default input.
func SetMicrophone(ctx context.Context, id int) error {
	return runStep(ctx, "wpctl", "set-default", strconv.Itoa(id))
}
