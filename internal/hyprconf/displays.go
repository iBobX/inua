package hyprconf

import (
	"cmp"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/display"
)

// LidCommand runs when the lid opens or closes: it writes the file again,
// with the built-in display off or on as the lid now says, and Hyprland
// reloads it. It works without inua-shell running.
const LidCommand = "inua config apply"

// panelsOff lists the built-in displays to turn off now; tests replace it.
var panelsOff = display.Local.PanelsOff

var (
	lidRe      = regexp.MustCompile(`(?i)\blid\b`)
	switchRe   = regexp.MustCompile(`^[^,"\\\n]+$`)
	switchLine = regexp.MustCompile(`switch:([^,"\\\n]+)`)
)

// renderDisplays writes the monitor rules from [displays], then turns off
// the panels d.PanelsOff names (the last rule for a monitor wins), and
// binds the lid switches to LidCommand.
func renderDisplays(b *strings.Builder, c config.Config, d Devices) {
	off := panelsFor(c, d)
	if len(c.Displays.Monitors) > 0 {
		b.WriteString("\n# Displays from inua's settings window.\n")
	}
	for _, m := range c.Displays.Monitors {
		if slices.Contains(off, m.Output) {
			continue // turned off below
		}
		if m.Disabled {
			fmt.Fprintf(b, "monitor = %s, disable\n", m.Output)
			continue
		}
		fmt.Fprintf(b, "monitor = %s, %s, %s, %s", m.Output, monitorMode(m), monitorPosition(m), monitorScale(m))
		if m.Mirror != "" {
			fmt.Fprintf(b, ", mirror, %s", m.Mirror)
		}
		b.WriteString("\n")
	}
	if len(off) > 0 {
		b.WriteString("\n# The lid is closed and another display is connected: the built-in\n# display is off until the lid opens.\n")
		for _, name := range off {
			fmt.Fprintf(b, "monitor = %s, disable\n", name)
		}
	}
	if c.Displays.LidOff && len(d.Lids) > 0 {
		b.WriteString("\n# The lid turns the built-in display off and on ([displays] lid_off).\n")
		for _, lid := range d.Lids {
			fmt.Fprintf(b, "bindl = , switch:%s, exec, %s\n", lid, LidCommand)
		}
	}
}

// renderDisplaysLua is renderDisplays for inua.lua.
func renderDisplaysLua(b *strings.Builder, c config.Config, d Devices) {
	off := panelsFor(c, d)
	if len(c.Displays.Monitors) > 0 {
		b.WriteString("\n-- Displays from inua's settings window.\n")
	}
	for _, m := range c.Displays.Monitors {
		if slices.Contains(off, m.Output) {
			continue // turned off below
		}
		if m.Disabled {
			fmt.Fprintf(b, "hl.monitor({ output = %s, disabled = true })\n", luaString(m.Output))
			continue
		}
		scale := monitorScale(m)
		if scale == "auto" {
			scale = luaString(scale)
		}
		fmt.Fprintf(b, "hl.monitor({ output = %s, mode = %s, position = %s, scale = %s", luaString(m.Output), luaString(monitorMode(m)), luaString(monitorPosition(m)), scale)
		if m.Mirror != "" {
			fmt.Fprintf(b, ", mirror = %s", luaString(m.Mirror))
		}
		b.WriteString(" })\n")
	}
	if len(off) > 0 {
		b.WriteString("\n-- The lid is closed and another display is connected: the built-in\n-- display is off until the lid opens.\n")
		for _, name := range off {
			fmt.Fprintf(b, "hl.monitor({ output = %s, disabled = true })\n", luaString(name))
		}
	}
	if c.Displays.LidOff && len(d.Lids) > 0 {
		b.WriteString("\n-- The lid turns the built-in display off and on ([displays] lid_off).\n")
		for _, lid := range d.Lids {
			fmt.Fprintf(b, "hl.bind(%s, hl.dsp.exec_cmd(%s), { locked = true })\n", luaString("switch:"+lid), luaString(LidCommand))
		}
	}
}

// writesDisplays reports whether renderDisplays writes anything.
func writesDisplays(c config.Config, d Devices) bool {
	return len(c.Displays.Monitors) > 0 || (c.Displays.LidOff && (len(d.Lids) > 0 || len(d.PanelsOff) > 0))
}

// panelsFor is the panels to turn off, if the settings say to.
func panelsFor(c config.Config, d Devices) []string {
	if !c.Displays.LidOff {
		return nil
	}
	return d.PanelsOff
}

func monitorMode(m config.Monitor) string     { return cmp.Or(m.Mode, "preferred") }
func monitorPosition(m config.Monitor) string { return cmp.Or(m.Position, "auto") }

func monitorScale(m config.Monitor) string {
	if m.Scale == 0 {
		return "auto"
	}
	return strconv.FormatFloat(m.Scale, 'f', -1, 64)
}

// Lids lists the lid switches in a j/devices reply, by Hyprland's name.
func Lids(devices []byte) ([]string, error) {
	var d struct {
		Switches []struct {
			Name string `json:"name"`
		} `json:"switches"`
	}
	if err := json.Unmarshal(devices, &d); err != nil {
		return nil, err
	}
	var names []string
	for _, s := range d.Switches {
		if lidRe.MatchString(s.Name) && switchRe.MatchString(s.Name) {
			names = append(names, s.Name)
		}
	}
	return names, nil
}

// knownLids lists the lid switches a file Render or RenderLua wrote binds,
// so the binds stay when the file is written without Hyprland running.
func knownLids(file string) []string {
	var names []string
	for _, m := range switchLine.FindAllStringSubmatch(file, -1) {
		names = append(names, strings.TrimSpace(m[1]))
	}
	return names
}
