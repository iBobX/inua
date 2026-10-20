package shell

import (
	"fmt"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/system"
)

// Fact is a line of the About page's system summary: a key in the accent
// and its value, as fastfetch prints them.
type Fact struct{ Key, Value string }

// FetchExtra is what the shell knows that the files do not say.
type FetchExtra struct {
	Shell    string   // the login shell's name
	WM       string   // "Hyprland 0.53.3"
	Inua     string   // inua's version
	Theme    string   // the theme and icon set: "yoru · Phosphor"
	Displays []string // each as "2256×1504 @ 60 Hz"
	Battery  string   // "", or its level and state
	Locale   string
}

// FetchLogo is inua's name in blocks, shown beside the facts.
const FetchLogo = `██
     ██████    ██   ██   ██████
██   ██   ██   ██   ██        ██
██   ██   ██   ██   ██    ██████
██   ██   ██   ██   ██   ██   ██
██   ██   ██    ██████    ██████`

// FetchFacts are the facts in fastfetch's order; those not known are
// left out.
func FetchFacts(s system.SysInfo, x FetchExtra) []Fact {
	var out []Fact
	add := func(key, value string) {
		if value != "" {
			out = append(out, Fact{key, value})
		}
	}
	osName := s.OS
	if osName != "" && s.Arch != "" {
		osName += " " + s.Arch
	}
	add(i18n.T("OS"), osName)
	add(i18n.T("Host"), s.Model)
	add(i18n.T("Kernel"), s.Kernel)
	if s.Uptime > 0 {
		add(i18n.T("Uptime"), Uptime(s.Uptime))
	}
	var pkgs []string
	for _, p := range s.Packages {
		pkgs = append(pkgs, fmt.Sprintf("%d (%s)", p.Count, p.Manager))
	}
	add(i18n.T("Packages"), strings.Join(pkgs, ", "))
	add(i18n.T("Shell"), x.Shell)
	add(i18n.T("WM"), x.WM)
	add("inua", x.Inua)
	add(i18n.T("Theme"), x.Theme)
	for _, d := range x.Displays {
		add(i18n.T("Display"), d)
	}
	cpu := s.CPU
	if cpu != "" && s.Cores > 0 {
		cpu += fmt.Sprintf(" (%d)", s.Cores)
	}
	if cpu != "" && s.MHz > 0 {
		cpu += fmt.Sprintf(" @ %.2f GHz", float64(s.MHz)/1000)
	}
	add(i18n.T("CPU"), cpu)
	for _, g := range s.GPUs {
		add(i18n.T("GPU"), g)
	}
	add(i18n.T("Memory"), UsageText(s.Memory))
	add(i18n.T("Swap"), UsageText(s.Swap))
	disk := UsageText(s.Disk)
	if disk != "" && s.DiskFS != "" {
		disk += " - " + s.DiskFS
	}
	add(i18n.T("Disk (/)"), disk)
	add(i18n.T("Battery"), x.Battery)
	add(i18n.T("Locale"), x.Locale)
	return out
}

// Uptime is a duration as fastfetch says it: "1 day, 2 hours, 3 mins".
func Uptime(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	var parts []string
	if days > 0 {
		parts = append(parts, i18n.Nf("%d day", "%d days", days, days))
	}
	if hours > 0 {
		parts = append(parts, i18n.Nf("%d hour", "%d hours", hours, hours))
	}
	if mins > 0 || len(parts) == 0 {
		parts = append(parts, i18n.Nf("%d min", "%d mins", mins, mins))
	}
	return strings.Join(parts, ", ")
}

// UsageText is room used of all, in GiB, and the share: "22.3 GiB / 54.7
// GiB (41%)"; "" when there is none.
func UsageText(u system.Usage) string {
	if u.Total == 0 {
		return ""
	}
	gib := func(n uint64) string { return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30)) }
	return fmt.Sprintf("%s / %s (%.0f%%)", gib(u.Used), gib(u.Total), float64(u.Used)/float64(u.Total)*100)
}

// FetchText is the summary as text to paste: the title, a rule under it
// and a fact a line.
func FetchText(title string, facts []Fact) string {
	var b strings.Builder
	b.WriteString(title + "\n" + strings.Repeat("-", len([]rune(title))) + "\n")
	for _, f := range facts {
		b.WriteString(f.Key + ": " + f.Value + "\n")
	}
	return b.String()
}
