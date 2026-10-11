package hypr

import (
	"bufio"
	"context"
	"net"
	"strings"
)

// Event is one line from the event socket: "NAME>>DATA".
type Event struct {
	Name string
	Data string
}

// ParseEvent splits a raw event line. It returns false for malformed lines.
func ParseEvent(line string) (Event, bool) {
	name, data, ok := strings.Cut(strings.TrimRight(line, "\n"), ">>")
	if !ok || name == "" {
		return Event{}, false
	}
	return Event{Name: name, Data: data}, true
}

// Args splits Data into at most n comma-separated fields; the last field keeps
// any remaining commas (window titles may contain them). n <= 0 splits fully.
func (e Event) Args(n int) []string {
	if n <= 0 {
		return strings.Split(e.Data, ",")
	}
	return strings.SplitN(e.Data, ",", n)
}

// stateEvents are the events after which monitors, workspaces or toplevels
// may have changed. Others (openlayer, activelayout, submap, ...) are only
// forwarded as raw events.
var stateEvents = map[string]bool{
	"openwindow": true, "closewindow": true,
	"movewindow": true, "movewindowv2": true,
	"windowtitle": true, "windowtitlev2": true,
	"activewindow": true, "activewindowv2": true,
	"workspace": true, "workspacev2": true,
	"createworkspace": true, "createworkspacev2": true,
	"destroyworkspace": true, "destroyworkspacev2": true,
	"moveworkspace": true, "moveworkspacev2": true,
	"renameworkspace": true,
	"activespecial":   true, "activespecialv2": true,
	"focusedmon": true, "focusedmonv2": true,
	"monitoradded": true, "monitoraddedv2": true,
	"monitorremoved": true, "monitorremovedv2": true,
	"fullscreen": true, "changefloatingmode": true,
	"pin": true, "urgent": true,
}

// IsStateEvent reports whether the event may change the snapshot.
func IsStateEvent(name string) bool { return stateEvents[name] }

// readEvents connects to the event socket and sends parsed events to out until
// the connection fails or ctx ends. The returned error is never nil.
func readEvents(ctx context.Context, path string, connected func(), out chan<- Event) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	connected()

	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		if ev, ok := ParseEvent(sc.Text()); ok {
			select {
			case out <- ev:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return net.ErrClosed
}
