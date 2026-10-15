package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Topic is something a workshop app can follow (events.subscribe): the
// tool that reads it and the use it needs.
type Topic struct{ Tool, Use string }

// Topics are what Watch follows.
var Topics = map[string]Topic{
	"window":       {"list_windows", "windows"},                // the focused window changed
	"clipboard":    {"clipboard_history", "clipboard-history"}, // something was copied
	"notification": {"list_notifications", "notifications"},    // a notification came
	"system":       {"system_status", "system"},                // the system's state changed
	"media":        {"media_status", "media"},                  // what plays changed
}

// Watch reads topic every so often until ctx ends, and gives emit what
// changed since the last read: the focused window, the newest clipboard
// entry, each new notification, the system's or the player's state. The
// first read only sets where it starts; failed reads are skipped.
func Watch(ctx context.Context, d Desktop, topic string, every time.Duration, emit func(any)) error {
	tp, ok := Topics[topic]
	if !ok {
		return fmt.Errorf("no topic %q", topic)
	}
	for _, t := range Tools(d) {
		if t.Name == tp.Tool {
			WatchTool(ctx, t, topic, every, emit)
		}
	}
	return nil
}

// WatchTool is Watch with the topic's tool at hand.
func WatchTool(ctx context.Context, t Tool, topic string, every time.Duration, emit func(any)) {
	read := t.Run
	w := watcher{topic: topic, emit: emit}
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		args := map[string]any{}
		if topic == "clipboard" {
			args["limit"] = float64(1)
		}
		if v, err := read(ctx, args); err == nil {
			if b, err := json.Marshal(v); err == nil {
				w.saw(b)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// watcher compares each read with the last.
type watcher struct {
	topic   string
	emit    func(any)
	started bool
	last    string
	ids     map[uint32]bool // the notifications seen
}

func (w *watcher) saw(b []byte) {
	first := !w.started
	w.started = true
	switch w.topic {
	case "notification":
		var v struct {
			Notifications []map[string]any `json:"notifications"`
		}
		if json.Unmarshal(b, &v) != nil {
			return
		}
		seen := map[uint32]bool{}
		for i := len(v.Notifications) - 1; i >= 0; i-- { // oldest first
			n := v.Notifications[i]
			id, _ := n["id"].(float64)
			seen[uint32(id)] = true
			if !first && !w.ids[uint32(id)] {
				w.emit(n)
			}
		}
		w.ids = seen
		return
	case "window": // the focused one
		var ws []window
		if json.Unmarshal(b, &ws) != nil {
			return
		}
		var focused any
		for _, x := range ws {
			if x.Focused {
				focused = x
			}
		}
		b, _ = json.Marshal(focused)
	case "clipboard": // the newest entry
		var es []json.RawMessage
		if json.Unmarshal(b, &es) != nil {
			return
		}
		b = []byte("null")
		if len(es) > 0 {
			b = es[0]
		}
	case "system": // not the clock
		var st map[string]any
		if json.Unmarshal(b, &st) != nil {
			return
		}
		delete(st, "time")
		b, _ = json.Marshal(st)
	}
	if s := string(b); first || s != w.last {
		changed := !first && s != w.last
		w.last = s
		if changed {
			var v any
			json.Unmarshal(b, &v)
			w.emit(v)
		}
	}
}
