package sni

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// Item is a tray item as the host last read it.
type Item struct {
	Key               string // "service/path", unique while the item lives
	ID                string
	Title             string
	Status            string // Active, Passive (hide it) or NeedsAttention
	IconName          string
	IconThemePath     string
	Pixmaps           []Pixmap
	AttentionIconName string
	AttentionPixmaps  []Pixmap
	Tooltip           string
	ItemIsMenu        bool // primary click should open the menu
	HasMenu           bool
}

// Pixmap is one size of an icon: ARGB32 pixels in network byte order.
type Pixmap struct {
	Width, Height int32
	ARGB          []byte
}

// callTimeout bounds every call to an item; a hung app must not stall the
// host.
const callTimeout = 2 * time.Second

type entry struct {
	service  string
	path     dbus.ObjectPath
	owner    string // unique name, matched against signal senders
	menu     dbus.ObjectPath
	item     *Item // nil until the first successful read
	fetching bool
	dirty    bool // a change arrived while fetching
}

type fetched struct {
	key   string
	owner string
	item  *Item
	menu  dbus.ObjectPath
	err   error
}

// Host tracks the items registered with the watcher and reports the list
// whenever it changes.
type Host struct {
	conn     *dbus.Conn
	onChange func([]Item)
	results  chan fetched

	mu      sync.Mutex
	keys    []string // registration order
	entries map[string]*entry
	last    []Item
}

// NewHost returns a host on conn. onChange is called from the host's
// goroutine with a fresh slice.
func NewHost(conn *dbus.Conn, onChange func([]Item)) *Host {
	return &Host{conn: conn, onChange: onChange, results: make(chan fetched, 16), entries: map[string]*entry{}}
}

// Run serves the host until ctx ends.
func (h *Host) Run(ctx context.Context) error {
	sigs := make(chan *dbus.Signal, 64)
	h.conn.Signal(sigs)
	defer h.conn.RemoveSignal(sigs)
	for _, m := range [][]dbus.MatchOption{
		{dbus.WithMatchSender(busName), dbus.WithMatchMember("NameOwnerChanged")},
		{dbus.WithMatchInterface(WatcherName)},
		{dbus.WithMatchInterface(itemIface)},
	} {
		if err := h.conn.AddMatchSignal(m...); err != nil {
			return err
		}
	}
	h.connectWatcher()

	for {
		select {
		case <-ctx.Done():
			return nil
		case r := <-h.results:
			h.applyFetch(r)
		case sig, ok := <-sigs:
			if !ok {
				return nil
			}
			h.signal(sig)
		}
	}
}

func (h *Host) signal(sig *dbus.Signal) {
	switch {
	case sig.Name == ownerChanged && len(sig.Body) == 3:
		name, _ := sig.Body[0].(string)
		newOwner, _ := sig.Body[2].(string)
		if name == WatcherName {
			h.clear()
			if newOwner != "" {
				h.connectWatcher()
			}
		} else if newOwner == "" {
			h.removeService(name)
		}
	case sig.Name == WatcherName+".StatusNotifierItemRegistered" && len(sig.Body) == 1:
		if key, ok := sig.Body[0].(string); ok {
			h.add(key)
		}
	case sig.Name == WatcherName+".StatusNotifierItemUnregistered" && len(sig.Body) == 1:
		if key, ok := sig.Body[0].(string); ok {
			h.remove(key)
		}
	case strings.HasPrefix(sig.Name, itemIface+".New"):
		h.mu.Lock()
		var keys []string
		for _, k := range h.keys {
			if e := h.entries[k]; e.owner == sig.Sender && e.path == sig.Path {
				keys = append(keys, k)
			}
		}
		h.mu.Unlock()
		for _, k := range keys {
			h.fetch(k)
		}
	}
}

// connectWatcher registers as a host and loads the watcher's item list.
func (h *Host) connectWatcher() {
	w := h.conn.Object(WatcherName, WatcherPath)
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	// Registering only tells items a host exists. Some watchers (GNOME's
	// AppIndicator extension) refuse a second host but still list items.
	if call := w.CallWithContext(ctx, WatcherName+".RegisterStatusNotifierHost", 0, h.conn.Names()[0]); call.Err != nil {
		log.Printf("sni: register host: %v", call.Err)
	}
	v, err := w.GetProperty(WatcherName + ".RegisteredStatusNotifierItems")
	if err != nil {
		log.Printf("sni: no watcher yet: %v", err)
		return
	}
	keys, _ := v.Value().([]string)
	for _, k := range keys {
		h.add(k)
	}
}

func (h *Host) add(key string) {
	service, path, ok := parseKey(key)
	if !ok {
		log.Printf("sni: ignoring item %q", key)
		return
	}
	h.mu.Lock()
	if _, dup := h.entries[key]; dup {
		h.mu.Unlock()
		return
	}
	h.keys = append(h.keys, key)
	h.entries[key] = &entry{service: service, path: path}
	h.mu.Unlock()
	h.fetch(key)
}

// parseKey splits a watcher's item string. Watchers disagree on the format:
// "service/path" (KDE, this package), "service@/path" (GNOME) or a bare
// service name at the default path.
func parseKey(key string) (string, dbus.ObjectPath, bool) {
	service, path, found := strings.Cut(key, "/")
	service = strings.TrimSuffix(service, "@")
	if service == "" {
		return "", "", false
	}
	if !found {
		return service, defaultPath, true
	}
	return service, dbus.ObjectPath("/" + path), true
}

func (h *Host) remove(key string) {
	h.mu.Lock()
	if _, ok := h.entries[key]; ok {
		delete(h.entries, key)
		h.keys = deleteKey(h.keys, key)
	}
	h.mu.Unlock()
	h.emit()
}

func (h *Host) removeService(name string) {
	h.mu.Lock()
	for _, k := range append([]string(nil), h.keys...) {
		if e := h.entries[k]; e.service == name || e.owner == name {
			delete(h.entries, k)
			h.keys = deleteKey(h.keys, k)
		}
	}
	h.mu.Unlock()
	h.emit()
}

func (h *Host) clear() {
	h.mu.Lock()
	h.keys = nil
	h.entries = map[string]*entry{}
	h.mu.Unlock()
	h.emit()
}

func deleteKey(keys []string, key string) []string {
	out := keys[:0]
	for _, k := range keys {
		if k != key {
			out = append(out, k)
		}
	}
	return out
}

// fetch reads an item's properties in the background; changes that arrive
// meanwhile cause one more read when it finishes.
func (h *Host) fetch(key string) {
	h.mu.Lock()
	e := h.entries[key]
	if e == nil {
		h.mu.Unlock()
		return
	}
	if e.fetching {
		e.dirty = true
		h.mu.Unlock()
		return
	}
	e.fetching = true
	service, path := e.service, e.path
	h.mu.Unlock()

	go func() {
		r := fetched{key: key}
		r.owner, r.item, r.menu, r.err = h.read(service, path)
		h.results <- r
	}()
}

func (h *Host) applyFetch(r fetched) {
	h.mu.Lock()
	e := h.entries[r.key]
	if e == nil { // removed meanwhile
		h.mu.Unlock()
		return
	}
	e.fetching = false
	if r.err != nil {
		log.Printf("sni: %s: %v", r.key, r.err)
	} else {
		r.item.Key = r.key
		e.owner, e.item, e.menu = r.owner, r.item, r.menu
	}
	again := e.dirty
	e.dirty = false
	h.mu.Unlock()
	if again {
		h.fetch(r.key)
	}
	h.emit()
}

func (h *Host) emit() {
	h.mu.Lock()
	items := []Item{}
	for _, k := range h.keys {
		if it := h.entries[k].item; it != nil {
			items = append(items, *it)
		}
	}
	if reflect.DeepEqual(items, h.last) {
		h.mu.Unlock()
		return
	}
	h.last = items
	h.mu.Unlock()
	h.onChange(append([]Item(nil), items...))
}

type rawPixmap struct {
	W, H int32
	Data []byte
}

type rawTooltip struct {
	Icon   string
	Pixmap []rawPixmap
	Title  string
	Text   string
}

func (h *Host) read(service string, path dbus.ObjectPath) (string, *Item, dbus.ObjectPath, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	var owner string
	if err := h.conn.BusObject().CallWithContext(ctx, busName+".GetNameOwner", 0, service).Store(&owner); err != nil {
		return "", nil, "", err
	}
	var props map[string]dbus.Variant
	if err := h.conn.Object(service, path).CallWithContext(ctx, "org.freedesktop.DBus.Properties.GetAll", 0, itemIface).Store(&props); err != nil {
		return "", nil, "", err
	}
	it := &Item{Status: "Active"}
	str := func(name string, dst *string) {
		if v, ok := props[name]; ok {
			if s, ok := v.Value().(string); ok {
				*dst = s
			}
		}
	}
	str("Id", &it.ID)
	str("Title", &it.Title)
	str("Status", &it.Status)
	str("IconName", &it.IconName)
	str("IconThemePath", &it.IconThemePath)
	str("AttentionIconName", &it.AttentionIconName)
	it.Pixmaps = pixmaps(props["IconPixmap"])
	it.AttentionPixmaps = pixmaps(props["AttentionIconPixmap"])
	if v, ok := props["ItemIsMenu"]; ok {
		it.ItemIsMenu, _ = v.Value().(bool)
	}
	var tip rawTooltip
	if v, ok := props["ToolTip"]; ok && v.Store(&tip) == nil {
		it.Tooltip = tip.Title
		if it.Tooltip == "" {
			it.Tooltip = tip.Text
		}
	}
	var menu dbus.ObjectPath
	if v, ok := props["Menu"]; ok {
		menu, _ = v.Value().(dbus.ObjectPath)
	}
	it.HasMenu = menu != "" && menu != "/"
	return owner, it, menu, nil
}

func pixmaps(v dbus.Variant) []Pixmap {
	var raw []rawPixmap
	if v.Signature().String() == "" || v.Store(&raw) != nil {
		return nil
	}
	var out []Pixmap
	for _, p := range raw {
		if p.W > 0 && p.H > 0 && len(p.Data) == int(p.W*p.H*4) {
			out = append(out, Pixmap{Width: p.W, Height: p.H, ARGB: p.Data})
		}
	}
	return out
}

// --- actions ---

func (h *Host) lookup(key string) (*entry, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.entries[key]
	if e == nil || e.item == nil {
		return nil, fmt.Errorf("sni: no item %q", key)
	}
	cp := *e
	return &cp, nil
}

func (h *Host) callItem(key, method string, args ...any) error {
	e, err := h.lookup(key)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return h.conn.Object(e.service, e.path).CallWithContext(ctx, itemIface+"."+method, 0, args...).Err
}

// Activate is the primary (left) click. x and y would be the screen
// position; Wayland gives the shell none, so they are 0.
func (h *Host) Activate(key string) error { return h.callItem(key, "Activate", int32(0), int32(0)) }

// SecondaryActivate is the middle click.
func (h *Host) SecondaryActivate(key string) error {
	return h.callItem(key, "SecondaryActivate", int32(0), int32(0))
}

// ContextMenu asks an item without a dbusmenu to show its own menu.
func (h *Host) ContextMenu(key string) error {
	return h.callItem(key, "ContextMenu", int32(0), int32(0))
}

// Scroll forwards a wheel step; orientation is "vertical" or "horizontal".
func (h *Host) Scroll(key string, delta int32, orientation string) error {
	return h.callItem(key, "Scroll", delta, orientation)
}
