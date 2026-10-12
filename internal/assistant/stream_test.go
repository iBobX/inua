package assistant

import "testing"

func TestAnswerStream(t *testing.T) {
	final := `{"type":"final","output":"It is 21°C in Kyoto,\nclear. \"Nice\" 😀"}`
	var s answerStream
	var last string
	for i := 0; i < len(final); i++ { // a byte at a time: every cut a stream can make
		if got, ok := s.add(final[i : i+1]); ok {
			if len(got) < len(last) {
				t.Fatalf("the answer shrank at %d: %q after %q", i, got, last)
			}
			last = got
		}
	}
	if want := "It is 21°C in Kyoto,\nclear. \"Nice\" 😀"; last != want {
		t.Errorf("answer %q, want %q", last, want)
	}

	for name, text := range map[string]string{
		"a plan":     `{"type":"plan","steps":[{"output":"no"}]}`,
		"tool calls": `{"type":"tool_call","output":"no"}`,
		"no output":  `{"type":"final"`,
		"not JSON":   `Thinking about it`,
	} {
		if got, ok := partialOutput(text); ok {
			t.Errorf("%s: %q", name, got)
		}
	}
	if got, ok := partialOutput(`{"output": "first", "type": "final"}`); !ok || got != "first" {
		t.Errorf("output before type: %q %v", got, ok)
	}
}
