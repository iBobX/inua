package display

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/lyricat/inua/internal/config"
)

// Output is a display as Hyprland has it (j/monitors all), on or off.
type Output struct {
	Name        string   `json:"name"` // the connector, "DP-2"
	Description string   `json:"description"`
	Make        string   `json:"make"`
	Model       string   `json:"model"`
	Width       int      `json:"width"`
	Height      int      `json:"height"`
	Refresh     float64  `json:"refreshRate"`
	X           int      `json:"x"`
	Y           int      `json:"y"`
	Scale       float64  `json:"scale"`
	Disabled    bool     `json:"disabled"`
	MirrorOf    string   `json:"mirrorOf"`       // "none" for none
	Modes       []string `json:"availableModes"` // "3840x2160@60.00Hz"
}

// Outputs reads a j/monitors all reply, leaving out the virtual screens
// (inua's, Hyprland's placeholder), which nobody plugs in.
func Outputs(reply []byte) ([]Output, error) {
	var all []Output
	if err := json.Unmarshal(reply, &all); err != nil {
		return nil, err
	}
	return slices.DeleteFunc(all, func(o Output) bool {
		return o.Name == "INUA-VIRTUAL" || o.Name == "FALLBACK" || strings.HasPrefix(o.Name, "HEADLESS-")
	}), nil
}

// Internal reports whether the output is a built-in panel.
func (o Output) Internal() bool { return Internal(o.Name) }

// Rule is the output a rule names for o: a built-in panel by its
// connector, which never changes; another display by its description,
// so the rule follows it from one port to another.
func (o Output) Rule() string {
	if o.Internal() || o.Description == "" {
		return o.Name
	}
	return "desc:" + o.Description
}

// RuleIn is the rule in d for o, by any name it goes by, and the output
// that rule names.
func (o Output) RuleIn(d config.Displays) (config.Monitor, bool) {
	for _, name := range []string{o.Rule(), o.Name, "desc:" + o.Description} {
		if m, ok := d.MonitorFor(name); ok {
			return m, true
		}
	}
	return config.Monitor{}, false
}

// Mode is the mode o shows now, as a rule writes it: one of its
// modes ("2256x1504@60.00") when one is that size and rate. A rate under
// 1 Hz (a headless output's) is left out.
func (o Output) Mode() string {
	for _, m := range o.ModeNames() {
		if w, h, r, ok := ParseMode(m); ok && w == o.Width && h == o.Height && math.Abs(r-o.Refresh) < 0.5 {
			return m
		}
	}
	if o.Refresh < 1 {
		return fmt.Sprintf("%dx%d", o.Width, o.Height)
	}
	return fmt.Sprintf("%dx%d@%s", o.Width, o.Height, strconv.FormatFloat(math.Round(o.Refresh*100)/100, 'f', -1, 64))
}

// ModeNames are o's modes as rules write them, without "Hz".
func (o Output) ModeNames() []string {
	var names []string
	for _, m := range o.Modes {
		names = append(names, strings.TrimSuffix(m, "Hz"))
	}
	return names
}

// ParseMode reads "3840x2160@60.00" (or without the rate).
func ParseMode(mode string) (width, height int, refresh float64, ok bool) {
	size, rate, hasRate := strings.Cut(mode, "@")
	ws, hs, found := strings.Cut(size, "x")
	if !found {
		return 0, 0, 0, false
	}
	var err1, err2, err3 error
	width, err1 = strconv.Atoi(ws)
	height, err2 = strconv.Atoi(hs)
	if hasRate {
		refresh, err3 = strconv.ParseFloat(rate, 64)
	}
	return width, height, refresh, err1 == nil && err2 == nil && err3 == nil && width > 0 && height > 0
}

// Seed is the rule that keeps o as it is now, for the settings window to
// change one thing of: its mode, position and scale. off is whether o is
// off only because the lid is closed, which the rule does not keep.
func (o Output) Seed(off bool) config.Monitor {
	m := config.Monitor{Output: o.Rule(), Disabled: o.Disabled && !off}
	if o.Width > 0 && o.Height > 0 {
		m.Mode = o.Mode()
		m.Position = fmt.Sprintf("%dx%d", o.X, o.Y)
		m.Scale = o.Scale
	}
	if o.MirrorOf != "" && o.MirrorOf != "none" {
		m.Mirror = o.MirrorOf
	}
	return m
}

// Scales are the scales the settings window offers, before ValidScale
// moves them to ones the mode takes; 0 is automatic.
var Scales = []float64{0, 1, 1.25, 1.5, 1.75, 2}

// ValidScale is the scale nearest want that Hyprland takes for a mode of
// width × height: one in steps of 1/120 (Wayland's fractional scale) that
// gives whole logical pixels both ways. Hyprland moves another to such a
// scale itself, warning; offering only these shows what it will use.
func ValidScale(width, height int, want float64) float64 {
	if want <= 0 || width <= 0 || height <= 0 {
		return want
	}
	whole := func(v float64) bool {
		w, h := float64(width)/v, float64(height)/v
		return math.Abs(w-math.Round(w)) < 1e-6 && math.Abs(h-math.Round(h)) < 1e-6
	}
	base := math.Round(want * 120)
	for i := 0.0; i <= 240; i++ {
		for _, k := range []float64{base - i, base + i} {
			if v := k / 120; k > 0 && whole(v) {
				return math.Round(v*1e6) / 1e6
			}
		}
	}
	return want
}

// ScalesFor are Scales as a mode of width × height takes them, each once.
func ScalesFor(width, height int) []float64 {
	var out []float64
	for _, f := range Scales {
		if v := ValidScale(width, height, f); !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}
