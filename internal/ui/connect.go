package ui

import (
	"context"
	"log"
	"time"

	"github.com/lyricat/inua/internal/apps/connect"
)

// startConnect starts Connect's service at login when it was left on. The
// shell runs in the session, so the service gets its display and bus.
func (s *shellUI) startConnect() {
	if !connect.On() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := connect.AtLogin(ctx); err != nil {
			log.Printf("connect: %v", err)
		}
	}()
}
