package shell

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/system"
)

func TestFetchFacts(t *testing.T) {
	s := system.SysInfo{
		OS: "Ubuntu 26.04.1 LTS", Arch: "x86_64", Kernel: "6.8.0", Model: "Framework Laptop 13",
		Uptime:   26*time.Hour + 3*time.Minute,
		Packages: []system.Packages{{Manager: "dpkg", Count: 2390}, {Manager: "snap", Count: 30}},
		CPU:      "AMD Ryzen 7 7840U", Cores: 16, MHz: 5132,
		GPUs:   []string{"AMD Phoenix1"},
		Memory: system.Usage{Used: 22 << 30, Total: 55 << 30},
		Disk:   system.Usage{Used: 100 << 30, Total: 200 << 30}, DiskFS: "ext4",
	}
	facts := FetchFacts(s, FetchExtra{Shell: "zsh", WM: "Hyprland 0.53.3", Displays: []string{"2256×1504 @ 60 Hz", "3840×2160 @ 60 Hz"}})
	var lines []string
	for _, f := range facts {
		lines = append(lines, f.Key+": "+f.Value)
	}
	want := []string{
		"OS: Ubuntu 26.04.1 LTS x86_64",
		"Host: Framework Laptop 13",
		"Kernel: 6.8.0",
		"Uptime: 1 day, 2 hours, 3 mins",
		"Packages: 2390 (dpkg), 30 (snap)",
		"Shell: zsh",
		"WM: Hyprland 0.53.3",
		"Display: 2256×1504 @ 60 Hz",
		"Display: 3840×2160 @ 60 Hz",
		"CPU: AMD Ryzen 7 7840U (16) @ 5.13 GHz",
		"GPU: AMD Phoenix1",
		"Memory: 22.0 GiB / 55.0 GiB (40%)",
		"Disk (/): 100.0 GiB / 200.0 GiB (50%) - ext4",
	}
	if !slices.Equal(lines, want) {
		t.Errorf("facts\n got %q\nwant %q", lines, want)
	}
	if text := FetchText("me@laptop", facts[:1]); text != "me@laptop\n---------\nOS: Ubuntu 26.04.1 LTS x86_64\n" {
		t.Errorf("text %q", text)
	}
}

func TestUptime(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Second:             "0 mins",
		time.Hour + time.Minute:      "1 hour, 1 min",
		48 * time.Hour:               "2 days",
		49*time.Hour + 5*time.Minute: "2 days, 1 hour, 5 mins",
	} {
		if got := Uptime(d); got != want {
			t.Errorf("Uptime(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestFetchLogo(t *testing.T) {
	// The block letters keep their columns: no line ends in spaces.
	for _, line := range strings.Split(FetchLogo, "\n") {
		if strings.TrimRight(line, " ") != line {
			t.Errorf("trailing space in %q", line)
		}
	}
}
