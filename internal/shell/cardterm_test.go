package shell

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/system"
)

func termCard(model string, disk uint64) Card {
	gib := uint64(1 << 30)
	s := system.SysInfo{
		OS: "Ubuntu 26.04.1 LTS", Arch: "x86_64", Kernel: "6.8.0-138-generic", Model: model,
		CPU: "AMD Ryzen 7 7840U w/ Radeon 780M Graphics and a name long enough to be cut", Cores: 16, GPUs: []string{"AMD Phoenix1"},
		Uptime: 46 * time.Hour, Packages: []system.Packages{{Manager: "dpkg", Count: 2390}},
		Memory: system.Usage{Used: 22 * gib, Total: 54 * gib}, Disk: system.Usage{Used: disk * gib, Total: 234 * gib},
	}
	return NewCard(s, "Hyprland 0.53.3", "0.0.30", []CardScreen{{3840, 2160, 60, 1.5}}, system.BatteryState{}, "青写真 Aojashin",
		"https://github.com/lyricat/inua/releases/tag/v0.0.30", time.Date(2026, 10, 20, 20, 22, 0, 0, time.UTC))
}

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

var theme = map[string]string{"panel": "#eef2f8", "t0": "#112233", "accent": "#2266bb", "warn": "#aa6611", "line": "#445566"}

func TestCardTerminal(t *testing.T) {
	c := termCard("Framework Laptop 13", 114)
	plain := c.Terminal(nil, false)
	if ansi.MatchString(plain) {
		t.Fatalf("colours without ink:\n%s", plain)
	}
	lines := strings.Split(strings.TrimSuffix(plain, "\n"), "\n")
	for i, mark := range cardLogo {
		if !strings.HasPrefix(lines[i], strings.TrimRight(mark, " ")) {
			t.Errorf("line %d without the mark: %q", i, lines[i])
		}
	}
	// The facts start in one column, beside the mark.
	at := termWidth(cardLogo[0]) + 3
	for _, want := range []string{"INUA // SYSTEM MANIFEST  [ NOMINAL ]", "NO. " + c.Serial + " · 2026-10-20T20:22Z",
		"Model    Framework Laptop 13 · x86_64", "CPU      AMD Ryzen 7 7840U w/ Radeon 780M Graph… · 16 thr",
		"Memory   22/54 GiB    ━━━━", "Disk     114/234 GiB  ━━━━━", "Kernel   Linux 6.8.0", "OS       Ubuntu 26.04.1 LTS · 2,390 packages",
		"WM       Hyprland 0.53.3", "Desktop  inua 0.0.30", "Screen   3840×2160 · 60 Hz · ×1.5", "Theme    青写真 Aojashin", "Uptime   1d 22h 00m"} {
		found := false
		for _, l := range lines {
			if i := strings.Index(l, want); i >= 0 {
				found = true
				if termWidth(l[:i]) != at {
					t.Errorf("%q at column %d, want %d", want, termWidth(l[:i]), at)
				}
			}
		}
		if !found {
			t.Errorf("without %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "All systems nominal.") {
		t.Error("the headline when nominal")
	}

	// Coloured on the terminal's ground: the accent for the mark and the
	// labels, the values in the terminal's own colour, no background but
	// the swatches'.
	coloured := c.Terminal(ANSIInk(theme), false)
	if !strings.Contains(coloured, "\x1b[38;2;34;102;187m"+cardLogo[0]) {
		t.Error("the mark not in the accent")
	}
	if strings.Contains(coloured, "\x1b[38;2;17;34;51m") {
		t.Error("the theme's text colour off its ground")
	}
	if strings.Contains(coloured, "\x1b[48;2;238;242;248m") || !strings.Contains(coloured, "\x1b[48;2;17;34;51m   ") {
		t.Error("the panel colour as ground, or no swatch")
	}
	// The same text, but for the swatches, only coloured: blank beside the
	// mark once the colours are gone.
	stripped := strings.Split(ansi.ReplaceAllString(coloured, ""), "\n")
	for i := range lines {
		if strings.TrimRight(stripped[i], " ") != strings.TrimRight(lines[i], " ") {
			t.Errorf("coloured differs from plain at line %d:\n%s", i, ansi.ReplaceAllString(coloured, ""))
			break
		}
	}
}

// With ground, the card is a block on the theme's panel colour: every line
// starts on it and is as wide as the others.
func TestCardTerminalGround(t *testing.T) {
	out := termCard("ThinkPad 很长的型号名称很长的型号名称很长的型号名称很长的型号名称", 114).Terminal(ANSIInk(theme), true)
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	width := termWidth(ansi.ReplaceAllString(lines[0], ""))
	for _, l := range lines {
		if !strings.HasPrefix(l, "\x1b[48;2;238;242;248m") || !strings.HasSuffix(l, "\x1b[0m") {
			t.Errorf("not on the ground: %q", l)
		}
		if got := termWidth(ansi.ReplaceAllString(l, "")); got != width {
			t.Errorf("%q: %d columns, want %d", ansi.ReplaceAllString(l, ""), got, width)
		}
		if strings.Contains(strings.TrimSuffix(l, "\x1b[0m"), "\x1b[0m") {
			t.Errorf("the ground reset inside a line: %q", l)
		}
	}
	if !strings.Contains(out, "\x1b[38;2;17;34;51m") {
		t.Error("the theme's text colour on its ground")
	}
}

// A reading out of bounds: the stamp and the headline in the warning colour.
func TestCardTerminalWarn(t *testing.T) {
	out := termCard("Framework Laptop 13", 220).Terminal(ANSIInk(theme), false)
	if !strings.Contains(out, "\x1b[38;2;170;102;17m[ DISK 94% ]") || !strings.Contains(out, "The disk is nearly full.") {
		t.Errorf("no warning:\n%q", out)
	}
}

func TestTermWidth(t *testing.T) {
	for s, want := range map[string]int{"abc": 3, "青写真": 6, "×1.5": 4, "é": 1, "e\u0301": 1, "ｱ": 1, "ＡＢ": 4} {
		if got := termWidth(s); got != want {
			t.Errorf("termWidth(%q) = %d, want %d", s, got, want)
		}
	}
	if got := termCut("青写真 Aojashin", 5); got != "青写…" || termWidth(got) != 5 {
		t.Errorf("termCut = %q", got)
	}
	if got := termCut("short", 10); got != "short" {
		t.Errorf("termCut = %q", got)
	}
}
