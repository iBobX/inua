package ui

import (
	"log"

	"github.com/lyricat/inua/internal/desktop"
)

// startAssistant keeps the assistant available to Console without a window.
// The app's D-Bus action makes repeated starts harmless on shell restart.
func (s *shellUI) startAssistant() {
	if !s.cfg.Assistant.Enable {
		return
	}
	go func() {
		if err := desktop.Spawn([]string{"inua-assistant", "--background"}, ""); err != nil {
			log.Printf("starting assistant: %v", err)
		}
	}()
}
