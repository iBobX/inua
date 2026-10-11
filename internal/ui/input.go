package ui

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/bootstrap"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/hyprconf"
	"github.com/lyricat/inua/internal/shell"
)

// The keyboard and touchpad settings live in config.toml like the rest,
// but Hyprland applies them: applyHyprland writes them to hypr/inua.conf,
// which hyprland.conf sources, and Hyprland reloads.

// hyprApply serializes writing inua.conf; only the latest request runs.
type hyprApply struct {
	mu     sync.Mutex
	latest atomic.Uint64
}

// applyHyprland writes inua.conf from the input settings in the
// background, and reloads Hyprland if the file changed.
func (s *shellUI) applyHyprland() {
	c, client := s.cfg, s.client
	path := hyprconf.Path(s.opts.ConfigPath)
	gen := s.hyprApply.latest.Add(1)
	go func() {
		s.hyprApply.mu.Lock()
		defer s.hyprApply.mu.Unlock()
		if s.hyprApply.latest.Load() != gen {
			return // a newer request follows
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		changed, err := hyprconf.Apply(ctx, c, path, client)
		gtkx.Post(func() {
			switch {
			case err != nil:
				log.Printf("hyprland settings: %v", err)
				s.setStatus("not applied to Hyprland: "+err.Error(), true)
			case changed:
				log.Printf("hyprland settings: wrote %s", path)
			}
		})
	}()
}

// refreshHyprInput rereads what Hyprland has in effect after it reloads
// its config, for the settings window.
func (s *shellUI) refreshHyprInput() {
	if s.settings.win == nil {
		return
	}
	s.settings.hyprKb = s.hyprKbOptions()
	s.settings.sc.binds = s.hyprBinds()
	s.showSourceWarning()
	s.showSettings()
}

// kbOptions is the kb_options the keyboard controls change: inua's, or
// Hyprland's while inua sets none.
func (s *shellUI) kbOptions() string {
	if o := s.cfg.Input.KbOptions; o != nil {
		return *o
	}
	return s.settings.hyprKb
}

// hyprKbOptions asks Hyprland for input:kb_options, "" if it cannot say.
func (s *shellUI) hyprKbOptions() string {
	if s.client == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	reply, err := s.client.Request(ctx, "j/getoption input:kb_options")
	if err != nil {
		log.Printf("hyprland: kb_options: %v", err)
		return ""
	}
	var opt struct {
		Str string `json:"str"`
	}
	if err := json.Unmarshal(reply, &opt); err != nil {
		log.Printf("hyprland: kb_options: %v", err)
	}
	return opt.Str
}

// showInput sets the keyboard and touchpad controls.
func (s *shellUI) showInput() {
	st := &s.settings
	in := s.cfg.Input
	opts := s.kbOptions()
	st.swap.SetSelected(uint32(max(0, slices.Index(swapChoices, shell.SwapCtrlAlt(opts)))))
	st.caps.SetSelected(uint32(max(0, slices.Index(capsChoices, shell.CapsLock(opts)))))
	if in.KbOptions != nil {
		st.kbState.SetText("Set by inua")
	} else {
		st.kbState.SetText("Set in hyprland.conf")
	}
	st.kbReadout.SetText(cmp.Or(opts, "no XKB options"))
	st.kbReset.SetSensitive(in.KbOptions != nil)

	speed := 0.0
	if in.TouchpadSpeed != nil {
		speed = *in.TouchpadSpeed
		st.tpValue.SetText(fmt.Sprintf("%+.2f", speed))
	} else {
		st.tpValue.SetText("hyprland.conf's")
	}
	st.tpSpeed.SetValue(speed)
	st.tpAccel.SetActive(in.TouchpadAccel != "flat")
	if in.TouchpadSpeed != nil || in.TouchpadAccel != "" {
		st.tpState.SetText("Set by inua")
	} else {
		st.tpState.SetText("Set in hyprland.conf")
	}
	st.tpReset.SetSensitive(in.TouchpadSpeed != nil || in.TouchpadAccel != "")
}

// showSourceWarning explains on the keyboard and touchpad pages when
// hyprland.conf does not source inua.conf, or overrides it.
func (s *shellUI) showSourceWarning() {
	st := &s.settings
	target := hyprconf.Path(s.opts.ConfigPath)
	conf := filepath.Join(filepath.Dir(target), "hyprland.conf")
	_, overrides, err := bootstrap.FindSource(conf, target)
	text := ""
	switch {
	case errors.Is(err, bootstrap.ErrNoSource):
		text = fmt.Sprintf("hyprland.conf does not load inua.conf, so these settings do nothing yet. "+
			"Add this as its last line:  source = ./%s", hyprconf.Name)
	case err != nil:
		text = err.Error()
	case len(overrides) > 0:
		text = "hyprland.conf sets these again after loading inua.conf, which wins over inua: " +
			strings.Join(overrides, ", ") + ". Move the source line below them."
	}
	for _, l := range []*gtk.Label{&st.kbWarn, &st.tpWarn, &st.sc.warn} {
		l.SetText(text)
		l.SetVisible(text != "")
	}
}
