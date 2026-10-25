package portal

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
)

// The states of an app, as GetAppState says them.
const (
	AppBackground uint32 = 0 // running without a window
	AppRunning    uint32 = 1 // with a window
	AppActive     uint32 = 2 // with the window in focus
)

// What the user lets an app running without a window do.
const (
	BackgroundForbid    uint32 = 0 // the frontend stops it
	BackgroundAllow     uint32 = 1
	BackgroundAllowOnce uint32 = 2
)

// BackgroundHandler asks whether an app may keep running with no window;
// it waits, and stops when done is closed.
type BackgroundHandler interface {
	Notify(appID, name string, done <-chan struct{}) uint32
}

// Background serves org.freedesktop.impl.portal.Background: which apps
// have a window (the frontend takes the sandboxed apps it knows that
// have none for running in the background), and the question when one
// keeps running without.
type Background struct {
	conn      *dbus.Conn
	h         BackgroundHandler
	autostart string // ~/.config/autostart

	mu   sync.Mutex
	apps map[string]uint32 // by app ID
}

// NewBackground hands questions to h, and writes autostart entries in
// autostart.
func NewBackground(conn *dbus.Conn, h BackgroundHandler, autostart string) *Background {
	return &Background{conn: conn, h: h, autostart: autostart, apps: map[string]uint32{}}
}

// Interface is what Export serves.
func (b *Background) Interface() Interface {
	return Interface{Name: "Background", Version: 1, Methods: backgroundObj{b}, XML: `
<method name="GetAppState"><arg type="a{sv}" name="apps" direction="out"/></method>
<method name="NotifyBackground"><arg type="o" name="handle" direction="in"/><arg type="s" name="app_id" direction="in"/><arg type="s" name="name" direction="in"/><arg type="u" name="response" direction="out"/><arg type="a{sv}" name="results" direction="out"/></method>
<method name="EnableAutostart"><arg type="s" name="app_id" direction="in"/><arg type="b" name="enable" direction="in"/><arg type="as" name="commandline" direction="in"/><arg type="u" name="flags" direction="in"/><arg type="b" name="result" direction="out"/></method>
<signal name="RunningApplicationsChanged"/>`}
}

// SetApps is the apps with windows now, by app ID; apps are told when
// that changes.
func (b *Background) SetApps(apps map[string]uint32) {
	b.mu.Lock()
	changed := !maps.Equal(b.apps, apps)
	b.apps = apps
	b.mu.Unlock()
	if changed {
		b.conn.Emit(Path, Prefix+"Background.RunningApplicationsChanged")
	}
}

type backgroundObj struct{ b *Background }

func (o backgroundObj) GetAppState() (map[string]dbus.Variant, *dbus.Error) {
	o.b.mu.Lock()
	defer o.b.mu.Unlock()
	out := map[string]dbus.Variant{}
	for id, st := range o.b.apps {
		out[id] = dbus.MakeVariant(st)
	}
	return out, nil
}

func (o backgroundObj) NotifyBackground(handle dbus.ObjectPath, appID, name string) (uint32, map[string]dbus.Variant, *dbus.Error) {
	req, err := NewRequest(o.b.conn, handle)
	if err != nil {
		return Ended, nil, nil
	}
	defer req.End()
	result := o.b.h.Notify(appID, name, req.Done())
	return Success, map[string]dbus.Variant{"result": dbus.MakeVariant(result)}, nil
}

// EnableAutostart writes the app's autostart entry, or removes it. The
// frontend writes them itself in newer versions; this is for the older.
func (o backgroundObj) EnableAutostart(appID string, enable bool, commandline []string, flags uint32) (bool, *dbus.Error) {
	if appID == "" || strings.ContainsAny(appID, "/\x00") || o.b.autostart == "" {
		return false, nil
	}
	path := filepath.Join(o.b.autostart, appID+".desktop")
	if !enable {
		os.Remove(path)
		return false, nil
	}
	var quoted []string
	for _, a := range commandline {
		quoted = append(quoted, execQuote(a))
	}
	entry := fmt.Sprintf("[Desktop Entry]\nType=Application\nName=%s\nExec=%s\nX-Flatpak=%s\n", appID, strings.Join(quoted, " "), appID)
	if flags&1 != 0 {
		entry += "DBusActivatable=true\n"
	}
	if err := os.MkdirAll(o.b.autostart, 0o755); err != nil {
		return false, nil
	}
	if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
		return false, nil
	}
	return true, nil
}

// execQuote quotes an argument for a desktop entry's Exec, as the
// specification asks.
func execQuote(a string) string {
	if a != "" && !strings.ContainsAny(a, " \t\n\"'\\><~|&;$*?#()`%") {
		return a
	}
	r := strings.NewReplacer(`\`, `\\\\`, `"`, `\\"`, "`", "\\\\`", `$`, `\\$`, `%`, `%%`)
	return `"` + r.Replace(a) + `"`
}
