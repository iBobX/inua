package ui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/notify"
)

// The control socket's `system` and `notifications list`, for the
// assistant's tools (internal/assistant) and scripts: the state the bar and
// the control center show, and the changes they make.

// systemRequest answers `call system status|set KEY VALUE|power ACTION`.
func (s *shellUI) systemRequest(fn string, args []string) ipc.Response {
	switch fn {
	case "status":
		return jsonResponse(s.systemStatus())
	case "set":
		if len(args) != 2 {
			return ipc.Response{Error: "system set: want a setting and a value"}
		}
		if err := s.systemSet(args[0], args[1]); err != nil {
			return ipc.Response{Error: err.Error()}
		}
		return ipc.Response{OK: true}
	case "power":
		if len(args) != 1 {
			return ipc.Response{Error: "system power: want suspend, reboot, poweroff or logout"}
		}
		switch args[0] {
		case "suspend", "reboot", "poweroff", "logout":
			act := args[0]
			s.runAfter(150, func() { s.doPower(act) }) // after the reply
			return ipc.Response{OK: true}
		}
		return ipc.Response{Error: "system power: want suspend, reboot, poweroff or logout"}
	}
	return ipc.Response{Error: "unknown function system " + fn}
}

func jsonResponse(v any) ipc.Response {
	b, err := json.Marshal(v)
	if err != nil {
		return ipc.Response{Error: err.Error()}
	}
	return ipc.Response{OK: true, Result: string(b)}
}

func (s *shellUI) systemStatus() map[string]any {
	st := s.sys
	now := time.Now()
	out := map[string]any{
		"time":        now.Format("Monday, 2 January 2006, 15:04 MST"),
		"focus":       s.focusOn(),
		"power_saver": st.Prof.Active == "power-saver",
		"wifi":        map[string]any{"available": st.Net.Available && st.Net.WifiDevice, "on": st.Net.WifiEnabled, "connection": st.Net.Kind, "network": st.Net.Name},
		"bluetooth":   map[string]any{"available": st.BT.Available, "on": st.BT.Powered, "connected": st.BT.Connected},
	}
	if st.Bat.Present {
		out["battery"] = map[string]any{"percent": int(st.Bat.Percent), "charging": st.Bat.Charging}
	}
	if st.Audio.Available {
		out["volume"] = map[string]any{"percent": int(st.Audio.Volume*100 + 0.5), "muted": st.Audio.Muted}
	}
	if b := s.svc.backlight; b != nil {
		if p, err := b.Percent(); err == nil {
			out["brightness_percent"] = int(p + 0.5)
		}
	}
	return out
}

// focusOn is Focus as the control center shows it: inua's own while it
// shows notifications, else mako's.
func (s *shellUI) focusOn() bool {
	if s.notesNative() {
		return s.notes.c.Focus
	}
	return s.sys.DND
}

func onOff(v string) (bool, error) {
	switch strings.ToLower(v) {
	case "on", "true", "yes", "1":
		return true, nil
	case "off", "false", "no", "0":
		return false, nil
	}
	return false, fmt.Errorf("want on or off, not %q", v)
}

func percent(v string) (float64, error) {
	n, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(v), "%"), 64)
	if err != nil || n < 0 || n > 100 {
		return 0, fmt.Errorf("want 0 to 100, not %q", v)
	}
	return n, nil
}

// systemSet changes one setting as the control center would.
func (s *shellUI) systemSet(key, value string) error {
	v := &s.svc
	st := s.sys
	switch key {
	case "wifi", "bluetooth", "mute", "focus", "saver":
		on, err := onOff(value)
		if err != nil {
			return err
		}
		switch key {
		case "wifi":
			go func() { logErr("wifi", v.net.SetWifi(on)) }()
		case "bluetooth":
			go func() { logErr("bluetooth", v.bt.SetPowered(on)) }()
		case "mute":
			if on != st.Audio.Muted {
				go func() { logErr("mute", v.audio.ToggleMute()) }()
			}
		case "focus":
			if on != s.focusOn() {
				s.controlAction("dnd")
			}
		case "saver":
			target := "balanced"
			if on {
				target = "power-saver"
			}
			go func() { logErr("power profile", v.prof.SetProfile(target)) }()
		}
	case "volume":
		p, err := percent(value)
		if err != nil {
			return err
		}
		v.volume.Set(p / 100)
	case "brightness":
		if v.brightness == nil {
			return fmt.Errorf("this screen's brightness cannot be set")
		}
		p, err := percent(value)
		if err != nil {
			return err
		}
		v.brightness.Set(p)
	default:
		return fmt.Errorf("no setting %q (wifi, bluetooth, volume, mute, brightness, focus, saver)", key)
	}
	return nil
}

// noteRequest answers `call notifications dismiss|invoke ID [KEY]`: as
// the notification's × or its button (KEY "default" is a click on it).
func (s *shellUI) noteRequest(fn string, args []string) ipc.Response {
	if len(args) == 0 {
		return ipc.Response{Error: "notifications " + fn + ": want an id"}
	}
	id, err := strconv.ParseUint(args[0], 10, 32)
	if err != nil {
		return ipc.Response{Error: "notifications " + fn + ": want an id, not " + strconv.Quote(args[0])}
	}
	n, ok := s.noteByID(uint32(id))
	if !ok {
		return ipc.Response{Error: fmt.Sprintf("no notification %d", id)}
	}
	if fn == "dismiss" {
		s.dismissNote(n.ID)
		return ipc.Response{OK: true}
	}
	key := "default"
	if len(args) > 1 {
		key = args[1]
	}
	if key == "default" && !n.Default || key != "default" && !slices.ContainsFunc(n.Actions, func(a notify.Action) bool { return a.Key == key }) {
		return ipc.Response{Error: fmt.Sprintf("notification %d has no action %q", id, key)}
	}
	s.invokeNote(n, key)
	return ipc.Response{OK: true}
}

// mediaRequest answers `call media status|play-pause|next|previous`: the
// player the control center shows.
func (s *shellUI) mediaRequest(fn string) ipc.Response {
	m := s.sys.Media
	if fn == "status" {
		if !m.Available {
			return jsonResponse(map[string]any{"available": false})
		}
		return jsonResponse(map[string]any{"available": true, "player": m.Identity, "title": m.Title, "artist": m.Artist,
			"playing": m.Playing, "can_play_pause": m.CanPlayPause, "can_next": m.CanNext, "can_previous": m.CanPrev})
	}
	v := &s.svc
	var do func() error
	switch {
	case fn == "play-pause" && m.CanPlayPause:
		do = v.media.PlayPause
	case fn == "next" && m.CanNext:
		do = v.media.Next
	case fn == "previous" && m.CanPrev:
		do = v.media.Previous
	case fn == "play-pause" || fn == "next" || fn == "previous":
		return ipc.Response{Error: "the player cannot do " + fn + " now"}
	default:
		return ipc.Response{Error: "unknown function media " + fn}
	}
	if err := do(); err != nil {
		return ipc.Response{Error: err.Error()}
	}
	return ipc.Response{OK: true}
}

// notificationList answers `call notifications list`: the center's
// notifications, newest first, as plain text.
func (s *shellUI) notificationList() ipc.Response {
	type action struct {
		Key   string `json:"key"`
		Label string `json:"label"`
	}
	type note struct {
		ID      uint32   `json:"id"`
		App     string   `json:"app"`
		Summary string   `json:"summary"`
		Body    string   `json:"body,omitempty"`
		Time    string   `json:"time"`
		Actions []action `json:"actions,omitempty"`
	}
	var out []note
	for _, n := range s.notes.c.Items { // newest first
		if len(out) == 50 {
			break
		}
		app, _ := s.noteApp(n)
		o := note{ID: n.ID, App: app, Summary: n.Summary, Body: notify.PlainText(n.Body), Time: n.Time.Format("Mon 15:04")}
		if n.Default {
			o.Actions = append(o.Actions, action{"default", i18n.T("Open")})
		}
		for _, a := range n.Actions {
			o.Actions = append(o.Actions, action{a.Key, a.Label})
		}
		out = append(out, o)
	}
	return jsonResponse(map[string]any{"focus": s.focusOn(), "notifications": out})
}
