package portal

import (
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/notify"
)

// NotificationHandler shows the portal's notifications as the shell's own;
// it is called on D-Bus goroutines.
type NotificationHandler interface {
	Notify(n notify.Notification) // new, or replacing the one of its ID
	Close(id uint32)              // the app withdrew it
}

// Notifications serves org.freedesktop.impl.portal.Notification. Apps
// name their notifications with strings of their own; the shell's center
// knows them by the numbers its server hands out, so each gets one.
type Notifications struct {
	conn  *dbus.Conn
	h     NotificationHandler
	newID func() uint32 // 0: notifications are off
	dir   string        // where icons sent as bytes are written

	mu    sync.Mutex
	byKey map[noteKey]*portalNote
	byID  map[uint32]*portalNote
}

type noteKey struct{ app, id string }

type portalNote struct {
	key     noteKey
	num     uint32
	actions map[string]noteAction // by the key the shell's button has
}

// noteAction is what a button, or the notification itself ("default"),
// sends back to the app.
type noteAction struct {
	name   string
	target *dbus.Variant
}

// NewNotifications takes IDs from newID and writes icons sent as bytes
// to dir.
func NewNotifications(conn *dbus.Conn, h NotificationHandler, newID func() uint32, dir string) *Notifications {
	return &Notifications{conn: conn, h: h, newID: newID, dir: dir, byKey: map[noteKey]*portalNote{}, byID: map[uint32]*portalNote{}}
}

// Interface is what Export serves.
func (n *Notifications) Interface() Interface {
	return Interface{Name: "Notification", Version: 1, Methods: notificationObj{n}, XML: `
<method name="AddNotification"><arg type="s" name="app_id" direction="in"/><arg type="s" name="id" direction="in"/><arg type="a{sv}" name="notification" direction="in"/></method>
<method name="RemoveNotification"><arg type="s" name="app_id" direction="in"/><arg type="s" name="id" direction="in"/></method>
<signal name="ActionInvoked"><arg type="s" name="app_id"/><arg type="s" name="id"/><arg type="s" name="action"/><arg type="av" name="parameter"/></signal>`}
}

// Owns reports whether the shell's notification id came from the portal.
func (n *Notifications) Owns(id uint32) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.byID[id] != nil
}

// Invoke sends the app the action of the button with key, or of the
// notification itself for "default", and reports whether the app has
// one to receive; without one, the shell brings the app forward itself.
func (n *Notifications) Invoke(id uint32, key string) bool {
	n.mu.Lock()
	p := n.byID[id]
	n.mu.Unlock()
	if p == nil {
		return false
	}
	a, ok := p.actions[key]
	if !ok {
		return false
	}
	var param []dbus.Variant
	if a.target != nil {
		param = []dbus.Variant{*a.target}
	}
	n.conn.Emit(Path, Prefix+"Notification.ActionInvoked", p.key.app, p.key.id, a.name, param)
	return true
}

// Forget is the shell's notification id gone from the screen and the
// center.
func (n *Notifications) Forget(id uint32) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if p := n.byID[id]; p != nil {
		delete(n.byID, id)
		delete(n.byKey, p.key)
	}
}

type notificationObj struct{ n *Notifications }

func (o notificationObj) AddNotification(appID, id string, opts map[string]dbus.Variant) *dbus.Error {
	n := o.n
	key := noteKey{appID, id}
	n.mu.Lock()
	p := n.byKey[key]
	n.mu.Unlock()
	if p == nil {
		// Not under the lock: the shell numbers it on its main thread,
		// which may be asking Owns meanwhile.
		num := n.newID()
		if num == 0 {
			return nil
		}
		n.mu.Lock()
		if p = n.byKey[key]; p == nil {
			p = &portalNote{key: key, num: num}
			n.byKey[key], n.byID[num] = p, p
		}
		n.mu.Unlock()
	}
	note, actions := n.note(p.num, appID, Options(opts))
	n.mu.Lock()
	p.actions = actions
	n.mu.Unlock()
	n.h.Notify(note)
	return nil
}

func (o notificationObj) RemoveNotification(appID, id string) *dbus.Error {
	n := o.n
	n.mu.Lock()
	p := n.byKey[noteKey{appID, id}]
	n.mu.Unlock()
	if p != nil {
		n.Forget(p.num)
		n.h.Close(p.num)
	}
	return nil
}

// note turns the portal's notification into the shell's.
func (n *Notifications) note(num uint32, appID string, o Options) (notify.Notification, map[string]noteAction) {
	note := notify.Notification{ID: num, App: appID, Entry: appID, Summary: o.String("title"),
		Urgency: notify.Normal, Time: time.Now(), Default: true}
	if m := o.String("markup-body"); m != "" {
		note.Body = notify.Markup(m)
	} else {
		note.Body = html.EscapeString(o.String("body"))
	}
	switch o.String("priority") {
	case "low":
		note.Urgency = notify.Low
	case "urgent":
		note.Urgency = notify.Critical
	}
	note.Transient = strings.Contains(o.String("display-hint"), "transient")
	note.Icon = n.icon(num, o["icon"])

	actions := map[string]noteAction{}
	if name := o.String("default-action"); name != "" {
		a := noteAction{name: name}
		if t, ok := o["default-action-target"]; ok {
			a.target = &t
		}
		actions["default"] = a
	}
	buttons, _ := o["buttons"].Value().([]map[string]dbus.Variant)
	for i, b := range buttons {
		bo := Options(b)
		key := fmt.Sprintf("button-%d", i)
		a := noteAction{name: bo.String("action")}
		if t, ok := bo["target"]; ok {
			a.target = &t
		}
		actions[key] = a
		note.Actions = append(note.Actions, notify.Action{Key: key, Label: bo.String("label")})
	}
	return note, actions
}

// icon is the notification's icon as the shell takes one: a themed
// icon's first name, or a file holding the bytes sent.
func (n *Notifications) icon(num uint32, v dbus.Variant) string {
	var kind string
	var val dbus.Variant
	switch t := v.Value().(type) {
	case []any: // (sv)
		if len(t) == 2 {
			kind, _ = t[0].(string)
			val, _ = t[1].(dbus.Variant)
		}
	default:
		return ""
	}
	var data []byte
	switch kind {
	case "themed":
		if names, _ := val.Value().([]string); len(names) > 0 {
			return names[0]
		}
		return ""
	case "bytes":
		data, _ = val.Value().([]byte)
	case "file-descriptor":
		fd, ok := val.Value().(dbus.UnixFD)
		if !ok {
			return ""
		}
		f := os.NewFile(uintptr(fd), "icon")
		data, _ = io.ReadAll(io.LimitReader(f, 8<<20))
		f.Close()
	}
	if len(data) == 0 || n.dir == "" {
		return ""
	}
	path := filepath.Join(n.dir, fmt.Sprintf("portal-note-%d.img", num))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return ""
	}
	return path
}
