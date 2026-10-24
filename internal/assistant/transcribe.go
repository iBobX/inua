package assistant

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/llm"
	muniai "github.com/quailyquaily/mistermorph/providers/uniai"

	"github.com/lyricat/inua/internal/config"
)

// Voice input: a recording goes to the transcription profile's
// speech-to-text model, OpenAI's /audio/transcriptions through Mister
// Morph's uniai client, as its Console's microphone does, and the text
// comes back to the question field. Mister Morph's own client for it is
// internal; its transcription route is set too, for Console.

// MaxAudio is the largest recording sent, OpenAI's upload limit.
const MaxAudio = 25 << 20

// transcribeTimeout bounds a transcription, Mister Morph's default.
const transcribeTimeout = 60 * time.Second

// ErrNoTranscription is an assistant without a profile for voice input.
var ErrNoTranscription = errors.New("assistant: no profile transcribes voice input")

// Transcribes reports whether voice input has a profile to go to.
func (a *Assistant) Transcribes() bool { return a.set.Transcription != "" }

// Transcribe returns the words spoken in audio, a recording in a format
// the name says ("voice.wav").
func (a *Assistant) Transcribe(ctx context.Context, audio []byte, name string) (string, error) {
	p, ok := a.set.profile(a.set.Transcription)
	if !ok {
		return "", ErrNoTranscription
	}
	return transcribe(ctx, p, llm.AudioInput{Filename: name, Data: audio}, "")
}

// transcribe asks p's model for the words in audio; language is a hint
// ("en"), "" to let the model tell.
func transcribe(ctx context.Context, p Profile, audio llm.AudioInput, language string) (string, error) {
	if !p.Can(config.AbilityTranscription) {
		return "", fmt.Errorf("assistant: %s does not transcribe", p.Name)
	}
	switch {
	case len(audio.Data) == 0:
		return "", errors.New("the recording is empty")
	case len(audio.Data) > MaxAudio:
		return "", fmt.Errorf("the recording is over %d MB", MaxAudio>>20)
	}
	endpoint := cmp.Or(p.Endpoint, config.TranscriptionEndpoints[p.Provider])
	if endpoint == "" {
		return "", fmt.Errorf("assistant: %s has no endpoint", p.Name)
	}
	c, err := muniai.New(muniai.Config{Provider: "openai", Endpoint: endpoint, APIKey: p.APIKey, Model: p.Model,
		RequestTimeout: transcribeTimeout})
	if err != nil {
		return "", err
	}
	res, err := c.Transcribe(ctx, llm.TranscriptionRequest{Audio: audio, Language: language, Scene: "inua.voice"})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Text), nil
}
