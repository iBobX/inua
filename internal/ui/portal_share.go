package ui

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/shell"
)

// Sharing the screen (docs/portals.md): xdg-desktop-portal-hyprland
// captures, and inua-share-picker, its picker, asks the shell which
// screen, window or area to share: `inua call share pick`, answered when
// the user has chosen in the screenshot tool.

// askShare opens the tool for sharing and waits for the choice, off the
// main loop: "screen:DP-1", "window:0x55d1c0" or "region:DP-1@x,y,w,h"
// (logical pixels on the monitor).
func (s *shellUI) askShare(ctx context.Context, req ipc.Request) ipc.Response {
	if req.Function != "pick" {
		return ipc.Response{Error: "share: unknown function " + req.Function}
	}
	p := &portalShot{share: make(chan string, 1), gone: make(chan struct{})}
	ok, _ := gtkx.Call(time.Second, func() bool {
		sh := &s.shot
		if sh.open || sh.busy || sh.portal != nil {
			return false
		}
		sh.portal = p
		sh.record, sh.silentNext = false, true
		s.openShot("screen")
		return true
	})
	if !ok {
		return ipc.Response{Error: i18n.T("The screenshot tool is in use.")}
	}
	select {
	case sel := <-p.share:
		return ipc.Response{OK: true, Result: sel}
	case <-p.gone:
		return ipc.Response{Error: "cancelled"}
	case <-ctx.Done(): // the picker went
		gtkx.Post(func() { s.closeShot("") })
		return ipc.Response{Error: "cancelled"}
	}
}

// portalShare answers the picker waiting for the tool with what the user
// took, r on sf, and reports whether one was waiting.
func (s *shellUI) portalShare(sf *shotSurface, r shell.Rect) bool {
	p := s.shot.portal
	if p == nil || p.share == nil {
		return false
	}
	s.shot.portal = nil
	sel := "screen:" + sf.name
	switch s.shot.mode {
	case "window":
		if sf.hover >= 0 {
			sel = "window:" + sf.windows[sf.hover].Address
		}
	case "area":
		x, y := int(math.Round(r.X)), int(math.Round(r.Y))
		sel = fmt.Sprintf("region:%s@%d,%d,%d,%d", sf.name, x, y, int(math.Round(r.W)), int(math.Round(r.H)))
	}
	p.share <- sel
	return true
}
