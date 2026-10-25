package system

import "testing"

// PipeWire's inputs, and its default.
func TestParseMicrophones(t *testing.T) {
	dump := `[
 {"id": 30, "type": "PipeWire:Interface:Node", "info": {"props": {"media.class": "Audio/Sink", "node.name": "out", "node.description": "Speakers"}}},
 {"id": 59, "type": "PipeWire:Interface:Node", "info": {"props": {"media.class": "Audio/Source", "node.name": "alsa_input.pci", "node.description": "Built-in Microphone"}}},
 {"id": 77, "type": "PipeWire:Interface:Node", "info": {"props": {"media.class": "Audio/Source", "node.name": "alsa_input.usb", "node.description": "DJI MIC MINI"}}},
 {"id": 40, "type": "PipeWire:Interface:Metadata", "props": {"metadata.name": "default"}, "metadata": [
  {"subject": 0, "key": "default.audio.sink", "value": {"name": "out"}},
  {"subject": 0, "key": "default.audio.source", "value": {"name": "alsa_input.usb"}}]}
]`
	mics, def, err := parseMicrophones([]byte(dump))
	if err != nil || def != "alsa_input.usb" || len(mics) != 2 || mics[1] != (Microphone{ID: 77, Name: "alsa_input.usb", Title: "DJI MIC MINI"}) {
		t.Errorf("%+v %q %v", mics, def, err)
	}
}
