package system

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/lyricat/inua/internal/dbustest"
)

// fakeBlueZ is BlueZ with one adapter, hci0, and the devices it knows.
type fakeBlueZ struct {
	t       *testing.T
	conn    *dbus.Conn
	adapter *prop.Properties

	mu      sync.Mutex
	devices map[dbus.ObjectPath]*fakeDevice
	// The agent registered: its bus name, path and capability, and
	// whether it was made the default.
	agentName, capability string
	agentPath             dbus.ObjectPath
	defaultAgent          bool
	// pair is what Pair does before the device is paired: ask the agent.
	pair func(agent dbus.BusObject, device dbus.ObjectPath) error
}

type fakeDevice struct {
	props   *prop.Properties
	battery bool
}

const hci0 = dbus.ObjectPath("/org/bluez/hci0")

type objectManager struct{ f *fakeBlueZ }

func (m objectManager) GetManagedObjects() (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, *dbus.Error) {
	m.f.mu.Lock()
	defer m.f.mu.Unlock()
	out := map[dbus.ObjectPath]map[string]map[string]dbus.Variant{}
	a, _ := m.f.adapter.GetAll(bluezName + ".Adapter1")
	out[hci0] = map[string]map[string]dbus.Variant{bluezName + ".Adapter1": a}
	for path, d := range m.f.devices {
		props, _ := d.props.GetAll(bluezName + ".Device1")
		out[path] = map[string]map[string]dbus.Variant{bluezName + ".Device1": props}
		if d.battery {
			b, _ := d.props.GetAll(bluezName + ".Battery1")
			out[path][bluezName+".Battery1"] = b
		}
	}
	return out, nil
}

type fakeAgentManager struct{ f *fakeBlueZ }

func (m fakeAgentManager) RegisterAgent(sender dbus.Sender, path dbus.ObjectPath, capability string) *dbus.Error {
	m.f.mu.Lock()
	defer m.f.mu.Unlock()
	m.f.agentName, m.f.agentPath, m.f.capability = string(sender), path, capability
	return nil
}

func (m fakeAgentManager) RequestDefaultAgent(sender dbus.Sender, path dbus.ObjectPath) *dbus.Error {
	m.f.mu.Lock()
	defer m.f.mu.Unlock()
	m.f.defaultAgent = string(sender) == m.f.agentName && path == m.f.agentPath
	return nil
}

type fakeAdapter struct{ f *fakeBlueZ }

func (a fakeAdapter) StartDiscovery() *dbus.Error {
	a.f.adapter.SetMust(bluezName+".Adapter1", "Discovering", true)
	return nil
}

func (a fakeAdapter) StopDiscovery() *dbus.Error {
	a.f.adapter.SetMust(bluezName+".Adapter1", "Discovering", false)
	return nil
}

func (a fakeAdapter) RemoveDevice(path dbus.ObjectPath) *dbus.Error {
	a.f.mu.Lock()
	delete(a.f.devices, path)
	a.f.mu.Unlock()
	a.f.conn.Emit("/", "org.freedesktop.DBus.ObjectManager.InterfacesRemoved", path, []string{bluezName + ".Device1"})
	return nil
}

// fakeDeviceMethods are a device's Device1 methods.
type fakeDeviceMethods struct {
	f    *fakeBlueZ
	path dbus.ObjectPath
}

func (d fakeDeviceMethods) set(name string, v any) {
	d.f.mu.Lock()
	dev := d.f.devices[d.path]
	d.f.mu.Unlock()
	dev.props.SetMust(bluezName+".Device1", name, v)
}

func (d fakeDeviceMethods) Connect() *dbus.Error    { d.set("Connected", true); return nil }
func (d fakeDeviceMethods) Disconnect() *dbus.Error { d.set("Connected", false); return nil }

func (d fakeDeviceMethods) Pair() *dbus.Error {
	d.f.mu.Lock()
	agent := d.f.conn.Object(d.f.agentName, d.f.agentPath)
	pair := d.f.pair
	d.f.mu.Unlock()
	if pair != nil {
		if err := pair(agent, d.path); err != nil {
			return dbus.NewError(bluezName+".Error.AuthenticationRejected", []any{"Authentication Rejected"})
		}
	}
	d.set("Paired", true)
	return nil
}

func (d fakeDeviceMethods) CancelPairing() *dbus.Error { return nil }

func newFakeBlueZ(t *testing.T, addr string) *fakeBlueZ {
	f := &fakeBlueZ{t: t, conn: service(t, addr, bluezName), devices: map[dbus.ObjectPath]*fakeDevice{}}
	f.adapter = export(t, f.conn, hci0, prop.Map{bluezName + ".Adapter1": {
		"Powered":      {Value: true, Writable: true, Emit: prop.EmitTrue},
		"Discoverable": {Value: false, Writable: true, Emit: prop.EmitTrue},
		"Discovering":  emitted(false),
		"Alias":        {Value: "laptop"},
	}})
	if err := f.conn.Export(fakeAdapter{f}, hci0, bluezName+".Adapter1"); err != nil {
		t.Fatal(err)
	}
	if err := f.conn.Export(fakeAgentManager{f}, "/org/bluez", bluezName+".AgentManager1"); err != nil {
		t.Fatal(err)
	}
	f.add("/org/bluez/hci0/dev_AA", map[string]any{"Alias": "Headphones", "Name": "Headphones", "Icon": "audio-headphones", "Paired": true, "Connected": true}, -1)
	f.add("/org/bluez/hci0/dev_BB", map[string]any{"Alias": "Keyboard", "Name": "Keyboard", "Icon": "input-keyboard", "Paired": true, "Connected": false}, 70)
	if err := f.conn.Export(objectManager{f}, "/", "org.freedesktop.DBus.ObjectManager"); err != nil {
		t.Fatal(err)
	}
	return f
}

// add makes a device with Device1 properties, and a battery level unless
// battery is -1; a nearby one is announced.
func (f *fakeBlueZ) add(path dbus.ObjectPath, props map[string]any, battery int) {
	device := map[string]*prop.Prop{
		"Address":   {Value: string(path[len(path)-2:])},
		"Adapter":   {Value: hci0},
		"Paired":    emitted(false),
		"Connected": emitted(false),
		"Trusted":   {Value: false, Writable: true, Emit: prop.EmitTrue},
		"Icon":      {Value: ""},
	}
	for k, v := range props {
		device[k] = emitted(v)
		if k == "Trusted" {
			device[k].Writable = true
		}
	}
	m := prop.Map{bluezName + ".Device1": device}
	if battery >= 0 {
		m[bluezName+".Battery1"] = map[string]*prop.Prop{"Percentage": {Value: byte(battery)}}
	}
	d := &fakeDevice{props: export(f.t, f.conn, path, m), battery: battery >= 0}
	if err := f.conn.Export(fakeDeviceMethods{f, path}, path, bluezName+".Device1"); err != nil {
		f.t.Fatal(err)
	}
	f.mu.Lock()
	f.devices[path] = d
	f.mu.Unlock()
	f.conn.Emit("/", "org.freedesktop.DBus.ObjectManager.InterfacesAdded", path, map[string]map[string]dbus.Variant{})
}

func startBluetooth(t *testing.T) (*fakeBlueZ, *Bluetooth, *states[BluetoothState]) {
	addr := dbustest.Start(t)
	f := newFakeBlueZ(t, addr)
	b := NewBluetooth(dialer(addr))
	got := newStates[BluetoothState]()
	run(t, func(ctx context.Context) { b.Run(ctx, got.add) })
	got.wait(t, "bluetooth", func(s BluetoothState) bool { return s.Available })
	return f, b, got
}

func TestBluetooth(t *testing.T) {
	f, b, got := startBluetooth(t)
	// Nearby: one with a name, one without.
	f.add("/org/bluez/hci0/dev_CC", map[string]any{"Alias": "Phone", "Name": "Phone", "Icon": "phone"}, -1)
	f.add("/org/bluez/hci0/dev_DD", map[string]any{"Alias": "DD"}, -1)

	want := BluetoothState{Available: true, Powered: true, Name: "laptop", Connected: []string{"Headphones"}, Devices: []BluetoothDevice{
		{Path: "/org/bluez/hci0/dev_AA", Address: "AA", Name: "Headphones", Icon: "audio-headphones", Paired: true, Connected: true, Battery: -1},
		{Path: "/org/bluez/hci0/dev_BB", Address: "BB", Name: "Keyboard", Icon: "input-keyboard", Paired: true, Battery: 70},
		{Path: "/org/bluez/hci0/dev_DD", Address: "DD", Battery: -1}, // no name: sorted first, the page leaves it out
		{Path: "/org/bluez/hci0/dev_CC", Address: "CC", Name: "Phone", Icon: "phone", Battery: -1},
	}}
	s := got.wait(t, "nearby devices", func(s BluetoothState) bool { return len(s.Devices) == 4 })
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("state\n got %+v\nwant %+v", s, want)
	}

	f.devices["/org/bluez/hci0/dev_BB"].props.SetMust(bluezName+".Device1", "Connected", true)
	got.wait(t, "two connected", func(s BluetoothState) bool {
		return reflect.DeepEqual(s.Connected, []string{"Headphones", "Keyboard"})
	})

	if err := b.SetPowered(false); err != nil {
		t.Fatal(err)
	}
	got.wait(t, "powered off", func(s BluetoothState) bool { return !s.Powered })
}

func TestBluetoothDevices(t *testing.T) {
	f, b, got := startBluetooth(t)
	if err := b.SetDiscovering(true); err != nil {
		t.Fatal(err)
	}
	got.wait(t, "searching", func(s BluetoothState) bool { return s.Discovering })
	if v, _ := f.adapter.Get(bluezName+".Adapter1", "Discoverable"); v.Value() != true {
		t.Error("not visible while searching")
	}
	if err := b.SetDiscovering(false); err != nil {
		t.Fatal(err)
	}
	got.wait(t, "not searching", func(s BluetoothState) bool { return !s.Discovering })

	if err := b.Connect("/org/bluez/hci0/dev_BB"); err != nil {
		t.Fatal(err)
	}
	got.wait(t, "keyboard connected", func(s BluetoothState) bool { return len(s.Connected) == 2 })
	if err := b.Disconnect("/org/bluez/hci0/dev_AA"); err != nil {
		t.Fatal(err)
	}
	got.wait(t, "headphones disconnected", func(s BluetoothState) bool { return reflect.DeepEqual(s.Connected, []string{"Keyboard"}) })
	if err := b.Forget("/org/bluez/hci0/dev_AA"); err != nil {
		t.Fatal(err)
	}
	got.wait(t, "headphones forgotten", func(s BluetoothState) bool { return len(s.Devices) == 1 })
	if err := b.Connect("/org/bluez/hci0/dev_AA"); err == nil {
		t.Error("connected a forgotten device")
	}
}

// The shell's agent is BlueZ's default, and answers through the Asker.
func TestBluetoothPairConfirm(t *testing.T) {
	f, b, got := startBluetooth(t)
	f.mu.Lock()
	registered := f.capability == "KeyboardDisplay" && f.defaultAgent
	f.pair = func(agent dbus.BusObject, device dbus.ObjectPath) error {
		return agent.Call(bluezName+".Agent1.RequestConfirmation", 0, device, uint32(4321)).Err
	}
	f.mu.Unlock()
	if !registered {
		t.Fatalf("agent not registered as the default KeyboardDisplay one")
	}
	f.add("/org/bluez/hci0/dev_CC", map[string]any{"Alias": "Phone", "Name": "Phone"}, -1)
	got.wait(t, "the phone", func(s BluetoothState) bool { return len(s.Devices) == 3 })

	var asked []BluetoothPrompt
	var mu sync.Mutex
	confirm := true
	b.SetAsker(func(ctx context.Context, p BluetoothPrompt) BluetoothAnswer {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, p)
		return BluetoothAnswer{OK: confirm}
	})
	if err := b.Pair("/org/bluez/hci0/dev_CC"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(asked) != 1 || asked[0].Kind != PromptConfirm || asked[0].Code != "004321" || asked[0].Device.Name != "Phone" {
		t.Errorf("asked %+v", asked)
	}
	mu.Unlock()
	got.wait(t, "paired, trusted and connected", func(s BluetoothState) bool {
		for _, d := range s.Devices {
			if d.Name == "Phone" {
				return d.Paired && d.Trusted && d.Connected
			}
		}
		return false
	})

	// Rejected: BlueZ's error comes back, by name.
	f.add("/org/bluez/hci0/dev_EE", map[string]any{"Alias": "Other", "Name": "Other"}, -1)
	got.wait(t, "another", func(s BluetoothState) bool { return len(s.Devices) == 4 })
	mu.Lock()
	confirm = false
	mu.Unlock()
	err := b.Pair("/org/bluez/hci0/dev_EE")
	var bt *BluetoothError
	if !errors.As(err, &bt) || bt.Name != bluezName+".Error.AuthenticationRejected" {
		t.Errorf("rejected pairing: %v", err)
	}
}

// A passkey to type on a keyboard is shown until pairing ends.
func TestBluetoothPairShow(t *testing.T) {
	f, b, got := startBluetooth(t)
	shown := make(chan BluetoothPrompt, 4)
	ended := make(chan struct{}, 4)
	b.SetAsker(func(ctx context.Context, p BluetoothPrompt) BluetoothAnswer {
		shown <- p
		<-ctx.Done()
		ended <- struct{}{}
		return BluetoothAnswer{}
	})
	f.mu.Lock()
	f.pair = func(agent dbus.BusObject, device dbus.ObjectPath) error {
		if err := agent.Call(bluezName+".Agent1.DisplayPasskey", 0, device, uint32(12345), uint16(0)).Err; err != nil {
			return err
		}
		time.Sleep(50 * time.Millisecond) // the user types it
		return nil
	}
	f.mu.Unlock()
	f.add("/org/bluez/hci0/dev_CC", map[string]any{"Alias": "K380", "Name": "K380", "Icon": "input-keyboard"}, -1)
	got.wait(t, "the keyboard", func(s BluetoothState) bool { return len(s.Devices) == 3 })

	if err := b.Pair("/org/bluez/hci0/dev_CC"); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-shown:
		if p.Kind != PromptShow || p.Code != "012345" || p.Typed != 0 || p.Device.Name != "K380" {
			t.Errorf("shown %+v", p)
		}
	default:
		t.Fatal("no passkey shown")
	}
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("the passkey stays shown after pairing")
	}
}

// Without an Asker, a request is rejected rather than left waiting.
func TestBluetoothNoAsker(t *testing.T) {
	f, b, got := startBluetooth(t)
	f.mu.Lock()
	f.pair = func(agent dbus.BusObject, device dbus.ObjectPath) error {
		return agent.Call(bluezName+".Agent1.RequestPinCode", 0, device).Err
	}
	f.mu.Unlock()
	f.add("/org/bluez/hci0/dev_CC", map[string]any{"Alias": "Old", "Name": "Old"}, -1)
	got.wait(t, "the device", func(s BluetoothState) bool { return len(s.Devices) == 3 })
	if err := b.Pair("/org/bluez/hci0/dev_CC"); err == nil {
		t.Error("paired with nobody to ask")
	}
}

func TestBluetoothWithoutAdapter(t *testing.T) {
	addr := dbustest.Start(t)
	conn := service(t, addr, bluezName)
	if err := conn.Export(emptyManager{}, "/", "org.freedesktop.DBus.ObjectManager"); err != nil {
		t.Fatal(err)
	}
	b := NewBluetooth(dialer(addr))
	got := newStates[BluetoothState]()
	run(t, func(ctx context.Context) { b.Run(ctx, got.add) })
	if s := got.wait(t, "a state", func(BluetoothState) bool { return true }); s.Available {
		t.Fatalf("available without an adapter: %+v", s)
	}
	if err := b.SetPowered(true); err == nil {
		t.Fatal("SetPowered without an adapter succeeded")
	}
}

type emptyManager struct{}

func (emptyManager) GetManagedObjects() (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, *dbus.Error) {
	return map[dbus.ObjectPath]map[string]map[string]dbus.Variant{}, nil
}

func TestSignalOnly(t *testing.T) {
	changed := func(props map[string]dbus.Variant) *dbus.Signal {
		return &dbus.Signal{Name: propsIface + ".PropertiesChanged", Body: []any{bluezName + ".Device1", props, []string{}}}
	}
	if !signalOnly(changed(map[string]dbus.Variant{"RSSI": dbus.MakeVariant(int16(-60))})) {
		t.Error("a signal strength change is read")
	}
	if signalOnly(changed(map[string]dbus.Variant{"RSSI": dbus.MakeVariant(int16(-60)), "Name": dbus.MakeVariant("Phone")})) {
		t.Error("a name change is ignored")
	}
}
