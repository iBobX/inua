package ui

import "testing"

// A dictation ends with a space unless it has one or is written without
// them.
func TestSpaced(t *testing.T) {
	for in, want := range map[string]string{
		"Turn on focus.": "Turn on focus. ",
		"done ":          "done ",
		"打开专注模式。":        "打开专注模式。",
		"音量を上げて":         "音量を上げて",
		"볼륨을 올려줘":        "볼륨을 올려줘 ",
		"":               "",
	} {
		if got := spaced(in); got != want {
			t.Errorf("spaced(%q) = %q, want %q", in, got, want)
		}
	}
}
