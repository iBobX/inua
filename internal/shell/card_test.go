package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/system"
)

func TestCardSerial(t *testing.T) {
	a := CardSerial("0.0.30", "Framework Laptop 13|AMD Ryzen 7 7840U")
	if a != CardSerial("0.0.30", "Framework Laptop 13|AMD Ryzen 7 7840U") {
		t.Fatal("one machine, two serials")
	}
	if !strings.HasPrefix(a, "INUA-0.0.30-") || len(a) != len("INUA-0.0.30-")+4 {
		t.Errorf("serial %q", a)
	}
	if a == CardSerial("0.0.30", "Another Laptop|AMD Ryzen 7 7840U") {
		t.Error("two machines, one serial")
	}
	if got := CardSerial("", "x"); !strings.HasPrefix(got, "INUA-DEV-") {
		t.Errorf("no version: %q", got)
	}
}

func TestCardState(t *testing.T) {
	gib := uint64(1 << 30)
	ok := system.Usage{Used: 10 * gib, Total: 100 * gib}
	full := system.Usage{Used: 93 * gib, Total: 100 * gib}
	for _, tc := range []struct {
		mem, disk system.Usage
		bat       system.BatteryState
		stamp     string
		nominal   bool
	}{
		{ok, ok, system.BatteryState{}, "NOMINAL", true},
		{ok, full, system.BatteryState{}, "DISK 93%", false},
		{full, ok, system.BatteryState{}, "MEMORY 93%", false},
		{full, full, system.BatteryState{}, "DISK 93%", false}, // the disk first
		{ok, ok, system.BatteryState{Present: true, Percent: 9}, "BATTERY 9%", false},
		{ok, ok, system.BatteryState{Present: true, Percent: 9, Charging: true}, "NOMINAL", true},
		{ok, ok, system.BatteryState{Present: true, Percent: 40}, "NOMINAL", true},
		{system.Usage{}, system.Usage{}, system.BatteryState{}, "NOMINAL", true}, // nothing read
	} {
		got := CardState(tc.mem, tc.disk, tc.bat)
		if got.Stamp != tc.stamp || got.Nominal != tc.nominal || got.Headline == "" {
			t.Errorf("%+v %+v %+v: %+v, want %q", tc.mem, tc.disk, tc.bat, got, tc.stamp)
		}
	}
}

func TestCardUptime(t *testing.T) {
	for d, want := range map[time.Duration]string{
		46*time.Hour + 24*time.Minute: "1d 22h 24m",
		3*time.Hour + 5*time.Minute:   "3h 05m",
		7 * time.Minute:               "7m",
	} {
		if got := CardUptime(d); got != want {
			t.Errorf("%v: %q, want %q", d, got, want)
		}
	}
}

func TestThemeTitle(t *testing.T) {
	if got := ThemeTitle("/* 夜 Yoru — dark: night (the default).\n   Colour tokens only. */", "yoru"); got != "夜 Yoru" {
		t.Errorf("a theme's own name: %q", got)
	}
	if got := ThemeTitle("@define-color app #000;", "mine"); got != "mine" {
		t.Errorf("no comment: %q", got)
	}
}

func TestNewCard(t *testing.T) {
	gib := uint64(1 << 30)
	s := system.SysInfo{
		Host: "secret-host", OS: "Ubuntu 26.04.1 LTS", Arch: "x86_64", Kernel: "6.8.0-138-generic",
		Model: "Framework Laptop 13 (AMD Ryzen 7040Series)", CPU: "AMD Ryzen 7 7840U", Cores: 16, GPUs: []string{"AMD Radeon 780M"},
		Uptime: 46 * time.Hour, Packages: []system.Packages{{Manager: "dpkg", Count: 2390}},
		Memory: system.Usage{Used: 22 * gib, Total: 54 * gib}, Disk: system.Usage{Used: 114 * gib, Total: 234 * gib},
	}
	c := NewCard(s, "Hyprland 0.53.3", "0.0.30", []CardScreen{{3840, 2160, 60, 1.5}}, system.BatteryState{}, "夜 Yoru",
		"https://github.com/lyricat/inua/releases/tag/v0.0.30", time.Date(2026, 10, 20, 20, 22, 0, 0, time.UTC))
	if len(c.Stack) != 4 || c.Stack[0].Version != "6.8.0" || c.Stack[1].Note != "2,390 packages" ||
		c.Stack[2].Name != "Hyprland" || c.Stack[2].Version != "0.53.3" || c.Stack[3].Version != "0.0.30" {
		t.Errorf("stack %+v", c.Stack)
	}
	if c.Model != "Framework Laptop 13" || c.Time != "2026-10-20T20:22Z" || !c.Status.Nominal || c.Uptime != "1d 22h 00m" {
		t.Errorf("card %+v", c)
	}
	text := c.Text()
	for _, want := range []string{"Framework Laptop 13", c.Serial, "3840×2160 · 60 Hz · ×1.5", "22/54 GiB", "releases/tag/v0.0.30"} {
		if !strings.Contains(text, want) {
			t.Errorf("text without %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "secret-host") {
		t.Error("the host's name on the card")
	}
}
