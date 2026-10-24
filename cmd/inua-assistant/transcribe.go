package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/lyricat/inua/internal/assistant"
	"github.com/lyricat/inua/internal/assistant/keys"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/i18n"
)

// runTranscribe prints the words in the recording on stdin (a WAV) with
// the profile for voice input (Settings → AI), for dictation's AI engine:
// the shell records, and the keys stay with the assistant. Why it failed
// goes to stderr, its last line for the user.
func runTranscribe(ctx context.Context) int {
	log.SetOutput(os.Stderr)
	log.SetPrefix("inua-assistant --transcribe: ")
	text, err := transcribeStdin(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(text)
	return 0
}

func transcribeStdin(ctx context.Context) (string, error) {
	cfg, _, err := config.Load(config.DefaultPath())
	if err != nil {
		cfg = config.Default()
	}
	i18n.Init(cfg.Language)
	if !cfg.Assistant.Enable {
		return "", errors.New(i18n.T("The assistant is turned off. Turn it on in Settings → AI."))
	}
	c := cfg.Assistant
	c.ServeEnable = false // no Console token to read
	set, err := assistant.Load(c, keys.New())
	if err != nil {
		return "", err
	}
	if set.Transcription == "" {
		return "", errors.New(i18n.T("No profile turns speech into text. Turn on Voice for one in Settings → AI."))
	}
	audio, err := io.ReadAll(io.LimitReader(os.Stdin, assistant.MaxAudio+1))
	if err != nil {
		return "", err
	}
	return set.Transcribe(ctx, audio, "dictation.wav")
}
