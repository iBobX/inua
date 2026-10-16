package main

import (
	"cmp"
	"context"
	"errors"
	"log"
	"os"
	"slices"

	"github.com/lyricat/inua/internal/assistant"
	"github.com/lyricat/inua/internal/assistant/answer"
	"github.com/lyricat/inua/internal/assistant/keys"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/i18n"
)

// runAnswer answers questions on stdio with the profiles of Settings → AI
// and no tools, each on its own, for the games' AI players
// (internal/assistant/answer). It offers the profiles that make quick
// choices, the one for them first.
func runAnswer(ctx context.Context) int {
	out := os.Stdout
	os.Stdout = os.Stderr // the protocol's alone
	log.SetOutput(os.Stderr)
	log.SetPrefix("inua-assistant --answer: ")
	cfg, _, err := config.Load(config.DefaultPath())
	if err != nil {
		cfg = config.Default()
	}
	i18n.Init(cfg.Language)
	var as *assistant.Assistant
	var profiles []answer.Profile
	var failed error
	if !cfg.Assistant.Enable {
		failed = errors.New(i18n.T("The assistant is turned off. Turn it on in Settings → AI."))
	} else {
		c := cfg.Assistant
		c.ServeEnable = false
		var set assistant.Settings
		if set, failed = assistant.Load(c, keys.New()); failed == nil {
			set.Tools = nil // none: it only answers
			if as, failed = assistant.New(set, nil, nil); failed == nil {
				models := map[string]string{}
				for _, p := range set.Profiles {
					models[p.Name] = p.Model
				}
				for _, name := range as.Choosers() {
					profiles = append(profiles, answer.Profile{Name: name, Model: models[name]})
				}
				if len(profiles) == 0 {
					failed = errors.New(i18n.T("No profile makes quick choices. Turn on Decision for one in Settings → AI."))
				}
			}
		}
	}
	err = answer.Serve(ctx, os.Stdin, out, profiles, failed, answerer{as, profiles})
	if failed != nil || err != nil && !errors.Is(err, context.Canceled) {
		return 1
	}
	return 0
}

// answerer answers with the assistant, each question on its own.
type answerer struct {
	as       *assistant.Assistant
	profiles []answer.Profile
}

// Ask asks profile, "" for the one that answers.
func (a answerer) Ask(ctx context.Context, profile, prompt string) (string, error) {
	a.as.Reset()
	if err := a.as.UseProfile(cmp.Or(profile, a.as.Profiles()[0])); err != nil {
		return "", err
	}
	return a.as.Ask(ctx, prompt, assistant.Events{})
}

// Choose asks Mister Morph's Evaluate; a profile that cannot, Serve asks
// instead, if it answers questions.
func (a answerer) Choose(ctx context.Context, profile, instructions, state string, options []string) (int, error) {
	profile = cmp.Or(profile, a.profiles[0].Name)
	i, err := a.as.Choose(ctx, profile, instructions, state, options)
	if errors.Is(err, assistant.ErrNoChoice) && slices.Contains(a.as.Profiles(), profile) {
		log.Printf("%s: %v; asking instead", profile, err)
		return -1, answer.ErrCannotChoose
	}
	return i, err
}
