package assistant

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/lyricat/inua/internal/config"
)

// fakeEars speaks OpenAI's /audio/transcriptions, answering text; it
// records the model and the file it was sent.
type fakeEars struct {
	mu          sync.Mutex
	text        string
	model, file string
	audio       []byte
	auth        string
}

func (f *fakeEars) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/audio/transcriptions" {
		http.NotFound(w, r)
		return
	}
	file, head, err := r.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	data, _ := io.ReadAll(file)
	f.mu.Lock()
	f.model, f.file, f.audio, f.auth = r.FormValue("model"), head.Filename, data, r.Header.Get("Authorization")
	text := f.text
	f.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]any{"text": text})
}

// Voice input goes to the profile for it, with its key, and only with
// one: a profile without a key, or of a provider that cannot transcribe,
// leaves the window without a microphone.
func TestTranscribe(t *testing.T) {
	ears := &fakeEars{text: " turn on Focus \n"}
	srv := httptest.NewServer(ears)
	t.Cleanup(srv.Close)
	c := config.Default().Assistant
	c.Profiles = []config.AIProfile{
		{Name: "main", Provider: "openai_chat_compatible", Model: "qwen3", Endpoint: srv.URL + "/v1"},
		{Name: "claude", Provider: "anthropic", Model: "claude-sonnet-5-5", Abilities: []string{"text", "transcription"}},
		{Name: "ears", Provider: "groq", Model: "whisper-1", Endpoint: srv.URL, Abilities: []string{"transcription"}},
	}
	c.Profile = "main"
	s, err := SettingsFrom(c, map[string]string{"claude": "sk-ant", "ears": "sk-ears"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Transcription != "ears" {
		t.Fatalf("transcription %q", s.Transcription)
	}
	s.StateDir, s.CacheDir = t.TempDir(), t.TempDir()
	a, err := New(s, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Transcribes() || len(a.Profiles()) != 2 { // ears does not answer
		t.Errorf("transcribes %v, answering %v", a.Transcribes(), a.Profiles())
	}
	text, err := a.Transcribe(context.Background(), []byte("RIFF…"), "voice.wav")
	if err != nil {
		t.Fatal(err)
	}
	if text != "turn on Focus" || ears.model != "whisper-1" || ears.file != "voice.wav" || string(ears.audio) != "RIFF…" || ears.auth != "Bearer sk-ears" {
		t.Errorf("text %q, sent %q %q %q %q", text, ears.model, ears.file, ears.audio, ears.auth)
	}
	if _, err := a.Transcribe(context.Background(), nil, "voice.wav"); err == nil {
		t.Error("an empty recording was sent")
	}

	c.Transcription = "ears"
	s, err = SettingsFrom(c, map[string]string{"claude": "sk-ant"})
	if err != nil || s.Transcription != "" {
		t.Errorf("without a key: %q %v", s.Transcription, err)
	}
}

// The transcription benchmark passes when the sample's words come back.
func TestBenchVoice(t *testing.T) {
	ears := &fakeEars{text: "The quick brown fox jumps over the lazy dog."}
	srv := httptest.NewServer(ears)
	t.Cleanup(srv.Close)
	p := Profile{Name: "ears", Provider: "groq", Model: "whisper-large-v3-turbo", Endpoint: srv.URL + "/v1", APIKey: "gsk",
		Abilities: []string{"transcription"}}
	if got := BenchTests(p); len(got) != 1 || got[0] != BenchVoice {
		t.Fatalf("tests %v", got)
	}
	var got []BenchResult
	set := Settings{Profiles: []Profile{p}, StateDir: t.TempDir(), CacheDir: t.TempDir()}
	if err := Benchmark(context.Background(), set, "ears", func(r BenchResult) { got = append(got, r) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].OK || ears.file != "sample.mp3" {
		t.Errorf("%+v", got)
	}
	ears.text = "Hello."
	got = nil
	Benchmark(context.Background(), set, "ears", func(r BenchResult) { got = append(got, r) })
	if len(got) != 1 || got[0].OK {
		t.Errorf("a wrong transcript passed: %+v", got)
	}
}
