package system

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/godbus/dbus/v5"
)

const bluezName = "org.bluez"

// BluetoothState is the first adapter's state.
type BluetoothState struct {
	Available bool // BlueZ is running and has an adapter
	Powered   bool
	Connected []string // aliases of connected devices, sorted
}

// Bluetooth follows BlueZ.
type Bluetooth struct {
	src source

	mu      sync.Mutex
	adapter dbus.ObjectPath
}

func NewBluetooth(dial Dialer) *Bluetooth {
	return &Bluetooth{src: source{dial: dial, name: bluezName, matches: [][]dbus.MatchOption{
		{dbus.WithMatchSender(bluezName), dbus.WithMatchInterface(propsIface)},
		{dbus.WithMatchSender(bluezName), dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager")},
	}}}
}

// Run reports the state until ctx ends.
func (b *Bluetooth) Run(ctx context.Context, onChange func(BluetoothState)) {
	e := &emitter[BluetoothState]{f: onChange}
	b.src.run(ctx, func(ctx context.Context, conn *dbus.Conn) {
		s, adapter := readBluetooth(ctx, conn)
		b.mu.Lock()
		b.adapter = adapter
		b.mu.Unlock()
		e.emit(s)
	})
}

func readBluetooth(ctx context.Context, conn *dbus.Conn) (BluetoothState, dbus.ObjectPath) {
	ctx, cancel := context.WithTimeout(ctx, callLimit)
	defer cancel()
	var objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	err := conn.Object(bluezName, "/").CallWithContext(ctx, "org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objects)
	if err != nil {
		return BluetoothState{}, ""
	}
	paths := make([]dbus.ObjectPath, 0, len(objects))
	for p := range objects {
		paths = append(paths, p)
	}
	slices.Sort(paths)

	var s BluetoothState
	var adapter dbus.ObjectPath
	for _, p := range paths {
		if a, ok := objects[p][bluezName+".Adapter1"]; ok && adapter == "" {
			adapter = p
			s.Available = true
			s.Powered = value[bool](a, "Powered")
		}
	}
	for _, p := range paths {
		if d, ok := objects[p][bluezName+".Device1"]; ok && value[bool](d, "Connected") {
			s.Connected = append(s.Connected, value[string](d, "Alias"))
		}
	}
	slices.Sort(s.Connected)
	return s, adapter
}

// SetPowered turns the adapter on or off.
func (b *Bluetooth) SetPowered(on bool) error {
	b.mu.Lock()
	adapter := b.adapter
	b.mu.Unlock()
	if adapter == "" {
		return errors.New("system: no Bluetooth adapter")
	}
	return b.src.setProp(bluezName, adapter, bluezName+".Adapter1", "Powered", on)
}
