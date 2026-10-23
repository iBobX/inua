package desktop

import (
	"context"
	"log"
	"os/exec"
	"regexp"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/host"
)

// appID is the window's: its desktop entry and its Hyprland class.
const appID = "io.github.lyricat.Inua.Term"

// New is the desktop of the session: notifications through any
// notification server (org.freedesktop.Notifications), the window raised
// through Hyprland.
func New() host.Desktop { return &freedesktop{} }

type freedesktop struct {
	once sync.Once
	conn *dbus.Conn
	mu   sync.Mutex
	live map[uint32]func() // notification to its Show
}

func (d *freedesktop) Notify(summary, body string, show func()) error {
	d.once.Do(d.connect)
	if d.conn == nil {
		return nil // no session bus: said once, when connecting
	}
	var id uint32
	err := d.conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications").Call(
		"org.freedesktop.Notifications.Notify", 0,
		"inua-term", uint32(0), "utilities-terminal", summary, body,
		[]string{"default", i18n.T("Show")}, map[string]dbus.Variant{"desktop-entry": dbus.MakeVariant(appID)}, int32(-1),
	).Store(&id)
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.live[id] = show
	d.mu.Unlock()
	return nil
}

// connect reaches the session bus and listens for the notifications'
// Show.
func (d *freedesktop) connect() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Printf("desktop: notifications: %v", err)
		return
	}
	d.conn, d.live = conn, map[uint32]func(){}
	conn.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.Notifications"), dbus.WithMatchObjectPath("/org/freedesktop/Notifications"))
	sigs := make(chan *dbus.Signal, 16)
	conn.Signal(sigs)
	go func() {
		for s := range sigs {
			if s.Name != "org.freedesktop.Notifications.ActionInvoked" || len(s.Body) < 1 {
				continue
			}
			id, _ := s.Body[0].(uint32)
			d.mu.Lock()
			show := d.live[id]
			d.mu.Unlock()
			if show != nil {
				show()
			}
		}
	}()
}

// Raise focuses the window open, on its own workspace, else opens one
// (inua-term again).
func (d *freedesktop) Raise() {
	// Started again, the window would come to the workspace shown, as
	// it does from the dock; from a notification it is gone to instead.
	if paths, err := hypr.PathsFromEnv(); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err = hypr.NewClient(paths).Focus(ctx, "class:^("+regexp.QuoteMeta(appID)+")$")
		cancel()
		if err == nil {
			return
		}
	}
	if exe, err := host.Program("inua-term"); err == nil {
		cmd := exec.Command(exe)
		if err := cmd.Start(); err == nil {
			go cmd.Wait()
		}
	}
}
