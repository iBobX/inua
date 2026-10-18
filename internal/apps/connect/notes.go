package connect

import (
	"context"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	notesName  = "org.freedesktop.Notifications"
	notesPath  = dbus.ObjectPath("/org/freedesktop/Notifications")
	notesIface = "org.freedesktop.Notifications"
)

// Note is a notification to post.
type Note struct {
	App, Icon   string
	Title, Body string
	// Actions are the buttons, by key: "default" is a click on it.
	Actions []Action
	Image   string // a picture's path
	Urgent  bool
	Replace uint32 // a note posted before, to update
	// Acted is called with the action's key; Closed when it is closed,
	// with whether the user closed it.
	Acted  func(key string)
	Closed func(byUser bool)
}

// Action is a button on a note.
type Action struct{ Key, Label string }

// notes posts notifications and passes on what is done with them.
type notes struct {
	conn *dbus.Conn

	mu   sync.Mutex
	live map[uint32]*Note
}

func newNotes(conn *dbus.Conn) *notes {
	n := &notes{conn: conn, live: map[uint32]*Note{}}
	conn.AddMatchSignal(dbus.WithMatchInterface(notesIface), dbus.WithMatchObjectPath(notesPath))
	return n
}

// signal takes the notification server's signals.
func (n *notes) signal(s *dbus.Signal) {
	if len(s.Body) < 2 {
		return
	}
	id, _ := s.Body[0].(uint32)
	n.mu.Lock()
	note := n.live[id]
	if s.Name == notesIface+".NotificationClosed" {
		delete(n.live, id)
	}
	n.mu.Unlock()
	if note == nil {
		return
	}
	switch s.Name {
	case notesIface + ".ActionInvoked":
		if key, _ := s.Body[1].(string); note.Acted != nil {
			go note.Acted(key)
		}
	case notesIface + ".NotificationClosed":
		reason, _ := s.Body[1].(uint32)
		if note.Closed != nil {
			go note.Closed(reason == 2) // dismissed by the user
		}
	}
}

// Post shows the note, and is its ID.
func (n *notes) Post(note Note) uint32 {
	var actions []string
	for _, a := range note.Actions {
		actions = append(actions, a.Key, a.Label)
	}
	hints := map[string]dbus.Variant{"desktop-entry": dbus.MakeVariant(DesktopID)}
	if note.Image != "" {
		hints["image-path"] = dbus.MakeVariant(note.Image)
	}
	if note.Urgent {
		hints["urgency"] = dbus.MakeVariant(byte(2))
	}
	icon := note.Icon
	if icon == "" {
		icon = DesktopID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var id uint32
	if err := n.conn.Object(notesName, notesPath).CallWithContext(ctx, notesIface+".Notify", 0,
		note.App, note.Replace, icon, note.Title, note.Body, actions, hints, int32(-1)).Store(&id); err != nil {
		return 0
	}
	n.mu.Lock()
	n.live[id] = &note
	n.mu.Unlock()
	return id
}

// Close takes a note away.
func (n *notes) Close(id uint32) {
	if id == 0 {
		return
	}
	n.mu.Lock()
	delete(n.live, id)
	n.mu.Unlock()
	n.conn.Object(notesName, notesPath).Call(notesIface+".CloseNotification", 0, id)
}
