package ui

import (
	"context"
	"log"
	"time"

	"github.com/lyricat/inua/internal/apps/remotedesk"
)

// startSharing shares the desktop again at login when it was left on
// (Remote Desktop, This computer). The shell runs in the session, so
// Sunshine is started with its display.
func (s *shellUI) startSharing() {
	go func() {
		sh := remotedesk.NewSharing()
		if !sh.On() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := sh.AtLogin(ctx); err != nil {
			log.Printf("sharing: %v", err)
		}
	}()
}
