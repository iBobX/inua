// Package system reads and changes the state the control center shows:
// network (NetworkManager), Bluetooth (BlueZ), battery and power profile
// (UPower), media (MPRIS), audio (PipeWire), brightness (sysfs + logind) and
// notifications (mako).
//
// The D-Bus services share one pattern: read the whole state, read it again
// after any signal from the service (coalesced), and report it only when it
// changed. Each service has its own connection and reconnects on its own.
package system

import (
	"context"
	"errors"
	"log"
	"reflect"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// Dialer opens a private D-Bus connection. Tests dial a private bus.
type Dialer func() (*dbus.Conn, error)

// SystemBus and SessionBus open private connections to the real buses.
var (
	SystemBus  Dialer = func() (*dbus.Conn, error) { return private(dbus.SystemBusPrivate) }
	SessionBus Dialer = func() (*dbus.Conn, error) { return private(dbus.SessionBusPrivate) }
)

func private(open func(...dbus.ConnOption) (*dbus.Conn, error)) (*dbus.Conn, error) {
	conn, err := open()
	if err != nil {
		return nil, err
	}
	if err := conn.Auth(nil); err != nil {
		conn.Close()
		return nil, err
	}
	if err := conn.Hello(); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

const (
	propsIface = "org.freedesktop.DBus.Properties"
	callLimit  = 2 * time.Second
	settle     = 40 * time.Millisecond // coalesces a burst of signals into one read
)

var errNotConnected = errors.New("system: not connected")

// source keeps one service's state current.
type source struct {
	dial Dialer
	// name is the service's bus name; a restart triggers a new read. Empty
	// for sources that follow many names (MPRIS).
	name    string
	matches [][]dbus.MatchOption

	mu   sync.Mutex
	conn *dbus.Conn
}

// run calls read after connecting, after matching signals and after the
// service restarts, until ctx ends. read reports state itself.
func (s *source) run(ctx context.Context, read func(context.Context, *dbus.Conn)) {
	backoff := 500 * time.Millisecond
	for ctx.Err() == nil {
		conn, err := s.dial()
		if err != nil {
			log.Printf("system: %s: %v", s.label(), err)
		} else {
			backoff = 500 * time.Millisecond
			s.serve(ctx, conn, read)
			conn.Close()
		}
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
			backoff = min(backoff*2, 30*time.Second)
		}
	}
}

func (s *source) label() string {
	if s.name == "" {
		return "bus"
	}
	return s.name
}

func (s *source) serve(ctx context.Context, conn *dbus.Conn, read func(context.Context, *dbus.Conn)) {
	sigs := make(chan *dbus.Signal, 64)
	conn.Signal(sigs)
	matches := s.matches
	if s.name != "" {
		matches = append(matches, []dbus.MatchOption{
			dbus.WithMatchSender("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, s.name),
		})
	}
	for _, m := range matches {
		if err := conn.AddMatchSignal(m...); err != nil {
			log.Printf("system: %s: match: %v", s.label(), err)
			return
		}
	}
	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.conn = nil
		s.mu.Unlock()
	}()

	read(ctx, conn)
	var timer <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case sig, ok := <-sigs:
			if !ok { // connection lost
				return
			}
			if sig.Name == "org.freedesktop.DBus.NameAcquired" || sig.Name == "org.freedesktop.DBus.NameLost" {
				continue
			}
			if timer == nil {
				timer = time.After(settle)
			}
		case <-timer:
			timer = nil
			read(ctx, conn)
		}
	}
}

// object returns a proxy on the current connection.
func (s *source) object(dest string, path dbus.ObjectPath) (dbus.BusObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return nil, errNotConnected
	}
	return s.conn.Object(dest, path), nil
}

// setProp sets a property on the current connection.
func (s *source) setProp(dest string, path dbus.ObjectPath, iface, name string, value any) error {
	obj, err := s.object(dest, path)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), callLimit)
	defer cancel()
	return obj.CallWithContext(ctx, propsIface+".Set", 0, iface, name, dbus.MakeVariant(value)).Err
}

// call invokes a method on the current connection.
func (s *source) call(dest string, path dbus.ObjectPath, method string, args ...any) error {
	obj, err := s.object(dest, path)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), callLimit)
	defer cancel()
	return obj.CallWithContext(ctx, method, 0, args...).Err
}

// getAll reads all properties of one interface.
func getAll(ctx context.Context, obj dbus.BusObject, iface string) (map[string]dbus.Variant, error) {
	ctx, cancel := context.WithTimeout(ctx, callLimit)
	defer cancel()
	var props map[string]dbus.Variant
	err := obj.CallWithContext(ctx, propsIface+".GetAll", 0, iface).Store(&props)
	return props, err
}

// value returns a property as T, or the zero value.
func value[T any](props map[string]dbus.Variant, name string) T {
	v, _ := props[name].Value().(T)
	return v
}

// emitter reports values that differ from the last one reported.
type emitter[T any] struct {
	f    func(T)
	last T
	sent bool
}

func (e *emitter[T]) emit(v T) {
	if e.sent && reflect.DeepEqual(v, e.last) {
		return
	}
	e.last, e.sent = v, true
	e.f(v)
}
