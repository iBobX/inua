package portal

import (
	"image/color"
	"maps"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
)

// Settings serves org.freedesktop.impl.portal.Settings: the values apps
// read for the theme. The frontend asks every backend the config lists
// for Settings and takes each key from the first that has it, so the keys
// inua has no value for come from the next one (gtk).
type Settings struct {
	conn *dbus.Conn

	mu   sync.Mutex
	vals map[string]map[string]dbus.Variant // namespace → key → value
}

// NewSettings serves no values until Set.
func NewSettings(conn *dbus.Conn) *Settings {
	return &Settings{conn: conn, vals: map[string]map[string]dbus.Variant{}}
}

// Interface is what Export serves.
func (s *Settings) Interface() Interface {
	return Interface{Name: "Settings", Version: 1, Methods: settingsObj{s}, XML: `
<method name="ReadAll"><arg type="as" name="namespaces" direction="in"/><arg type="a{sa{sv}}" name="value" direction="out"/></method>
<method name="Read"><arg type="s" name="namespace" direction="in"/><arg type="s" name="key" direction="in"/><arg type="v" name="value" direction="out"/></method>
<signal name="SettingChanged"><arg type="s" name="namespace"/><arg type="s" name="key"/><arg type="v" name="value"/></signal>`}
}

// Set replaces the values, and tells apps of each one that changed. A key
// taken away is not signalled: apps keep its last value until they read
// it again.
func (s *Settings) Set(vals map[string]map[string]dbus.Variant) {
	s.mu.Lock()
	old := s.vals
	s.vals = vals
	s.mu.Unlock()
	for ns, keys := range vals {
		for key, v := range keys {
			if was, ok := old[ns][key]; ok && was.String() == v.String() {
				continue
			}
			s.conn.Emit(Path, Prefix+"Settings.SettingChanged", ns, key, v)
		}
	}
}

type settingsObj struct{ s *Settings }

// ReadAll answers the namespaces asked for: names, or prefixes ending in
// "*"; none, or "", is all of them.
func (o settingsObj) ReadAll(namespaces []string) (map[string]map[string]dbus.Variant, *dbus.Error) {
	o.s.mu.Lock()
	defer o.s.mu.Unlock()
	out := map[string]map[string]dbus.Variant{}
	for ns, keys := range o.s.vals {
		if matchNamespace(namespaces, ns) {
			out[ns] = maps.Clone(keys)
		}
	}
	return out, nil
}

func (o settingsObj) Read(namespace, key string) (dbus.Variant, *dbus.Error) {
	o.s.mu.Lock()
	defer o.s.mu.Unlock()
	if v, ok := o.s.vals[namespace][key]; ok {
		return v, nil
	}
	return dbus.Variant{}, dbus.NewError("org.freedesktop.portal.Error.NotFound", []any{"Requested setting not found"})
}

func matchNamespace(patterns []string, ns string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if p == "" || p == ns || strings.HasSuffix(p, "*") && strings.HasPrefix(ns, strings.TrimSuffix(p, "*")) {
			return true
		}
	}
	return false
}

// Appearance is what the shell's settings say about how apps look.
type Appearance struct {
	Dark      bool
	Accent    color.NRGBA
	IconTheme string  // "" leaves the desktop's (the next backend's)
	TextScale float64 // 1 is 100%
}

// Values are the Settings values for the appearance.
func (a Appearance) Values() map[string]map[string]dbus.Variant {
	scheme := uint32(2) // prefer light
	if a.Dark {
		scheme = 1
	}
	accent := struct{ R, G, B float64 }{float64(a.Accent.R) / 255, float64(a.Accent.G) / 255, float64(a.Accent.B) / 255}
	iface := map[string]dbus.Variant{"text-scaling-factor": dbus.MakeVariant(a.TextScale)}
	if a.IconTheme != "" {
		iface["icon-theme"] = dbus.MakeVariant(a.IconTheme)
	}
	return map[string]map[string]dbus.Variant{
		"org.freedesktop.appearance": {
			"color-scheme": dbus.MakeVariant(scheme),
			"accent-color": dbus.MakeVariant(accent),
			"contrast":     dbus.MakeVariant(uint32(0)),
		},
		"org.gnome.desktop.interface": iface,
	}
}
