package ui

import (
	"context"
	"log"
	"time"

	"github.com/lyricat/inua/internal/apps/remotedesk"
)

// startSharing shares the desktop again at login when it was left on
// (Remote Desktop, This computer). The shell runs in the session, so
// Sunshine is started with its display. The virtual screen, when the user
// turned it on, is made first: Sunshine looks for its screens and
// encoders as it starts.
func (s *shellUI) startSharing() {
	s.virtual = make(chan struct{}, 1)
	go func() {
		sh := remotedesk.NewSharing()
		s.syncVirtual(sh)
		if sh.On() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := sh.AtLogin(ctx); err != nil {
				log.Printf("sharing: %v", err)
			}
			cancel()
		}
		for range s.virtual {
			s.syncVirtual(sh)
		}
	}()
}

// followMonitors asks for the virtual screen to follow the monitors, on
// Hyprland's events: a monitor came or went, or a reload dropped its mode.
func (s *shellUI) followMonitors() {
	if s.virtual == nil {
		return
	}
	select {
	case s.virtual <- struct{}{}:
	default:
	}
}

// syncVirtual adds or removes the virtual screen as its setting and the
// monitors say. Without the setting, a screen left from before is
// removed, and nothing else is done.
func (s *shellUI) syncVirtual(sh *remotedesk.Sharing) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := sh.SyncVirtual(ctx); err != nil {
		log.Printf("sharing: virtual screen: %v", err)
	}
}
