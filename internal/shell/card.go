package shell

import (
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/system"
)

// Card is the About page's system card, the manifest a ship prints of
// itself: the unit, its silicon, the stack from the kernel to inua, the
// screens, the uptime as the total, a barcode of its serial and a QR code
// of inua's release. Nothing of the user's: no user or host name, no
// paths. The view draws it; this is what it says.
type Card struct {
	Serial  string // "INUA-0.0.30-7A3F": the version and a mark of the machine
	Time    string // when it was read, ISO 8601 to the minute
	Model   string
	Arch    string
	CPU     string
	Threads int
	GPU     string
	Memory  system.Usage
	Disk    system.Usage
	Stack   []CardLayer // the kernel first, inua last
	Screens []CardScreen
	Uptime  string
	Theme   string // "夜 Yoru", the theme's own name
	Colors  []string
	Link    string // what the QR code opens: this release of inua's
	Status  CardStatus
}

// CardLayer is one layer of the software: its name and version; Note is
// what it adds ("2,390 packages").
type CardLayer struct{ Name, Version, Note string }

// CardScreen is a display: its size, its rate and scale.
type CardScreen struct {
	Width, Height int
	Hz            float64
	Scale         float64
}

// CardStatus is the stamp: NOMINAL, or the first reading that is not.
type CardStatus struct {
	Stamp    string // "NOMINAL", "DISK 93%" …
	Nominal  bool
	Headline string // "All systems nominal."
}

// CardColors are the theme's tokens the card shows, as squares.
var CardColors = []string{"t0", "t1", "t2", "line", "accent", "ok", "warn", "danger"}

// NewCard makes the card from what was read: wm is the compositor and its
// version ("Hyprland 0.53.3"), inua inua's version, link the release's page.
func NewCard(s system.SysInfo, wm, inua string, screens []CardScreen, bat system.BatteryState, theme, link string, now time.Time) Card {
	c := Card{
		Model:   s.Model,
		Arch:    s.Arch,
		CPU:     s.CPU,
		Threads: s.Cores,
		Memory:  s.Memory,
		Disk:    s.Disk,
		Screens: screens,
		Theme:   theme,
		Link:    link,
		Time:    now.Format("2006-01-02T15:04Z07:00"),
	}
	// "Framework Laptop 13 (AMD Ryzen 7040Series)": the brackets repeat
	// what the CPU line says.
	if i := strings.Index(c.Model, " ("); i > 0 && strings.HasSuffix(c.Model, ")") {
		c.Model = c.Model[:i]
	}
	if c.Model == "" {
		c.Model = i18n.T("This computer")
	}
	if len(s.GPUs) > 0 {
		c.GPU = s.GPUs[0]
	}
	if s.Uptime > 0 {
		c.Uptime = CardUptime(s.Uptime)
	}
	if s.Kernel != "" {
		k, _, _ := strings.Cut(s.Kernel, "-") // "6.8.0" of "6.8.0-138-generic"
		c.Stack = append(c.Stack, CardLayer{Name: "Linux", Version: k})
	}
	if s.OS != "" {
		pkgs := 0
		for _, p := range s.Packages {
			pkgs += p.Count
		}
		note := ""
		if pkgs > 0 {
			note = i18n.Nf("%s package", "%s packages", pkgs, groupDigits(pkgs))
		}
		c.Stack = append(c.Stack, CardLayer{Name: s.OS, Note: note})
	}
	if name, version, ok := strings.Cut(wm, " "); ok {
		c.Stack = append(c.Stack, CardLayer{Name: name, Version: version})
	} else if wm != "" {
		c.Stack = append(c.Stack, CardLayer{Name: wm})
	}
	c.Stack = append(c.Stack, CardLayer{Name: "inua", Version: inua})
	c.Serial = CardSerial(inua, s.Model+"|"+s.CPU)
	c.Status = CardState(s.Memory, s.Disk, bat)
	return c
}

// CardSerial is the slip's number and the barcode's text: inua's version
// and four hex digits of a hash of the machine's model, the same on every
// card of one machine and telling nothing about its owner.
func CardSerial(version, machine string) string {
	h := fnv.New32a()
	h.Write([]byte(machine))
	if version == "" {
		version = "DEV"
	}
	return fmt.Sprintf("INUA-%s-%04X", strings.ToUpper(version), h.Sum32()&0xffff)
}

// CardState is the stamp: the first reading out of bounds (a disk or the
// memory at 90%, a battery under 15% and not charging), else NOMINAL.
func CardState(mem, disk system.Usage, bat system.BatteryState) CardStatus {
	share := func(u system.Usage) int {
		if u.Total == 0 {
			return 0
		}
		return int(u.Used * 100 / u.Total)
	}
	switch {
	case share(disk) >= 90:
		return CardStatus{Stamp: i18n.Tf("DISK %d%%", share(disk)), Headline: i18n.T("The disk is nearly full.")}
	case share(mem) >= 90:
		return CardStatus{Stamp: i18n.Tf("MEMORY %d%%", share(mem)), Headline: i18n.T("Memory is running short.")}
	case bat.Present && !bat.Charging && !bat.Full && bat.Percent < 15:
		return CardStatus{Stamp: i18n.Tf("BATTERY %.0f%%", bat.Percent), Headline: i18n.T("The battery is running low.")}
	}
	return CardStatus{Stamp: i18n.T("NOMINAL"), Nominal: true, Headline: i18n.T("All systems nominal.")}
}

// CardUptime is the uptime as the total line says it: "1d 22h 24m".
func CardUptime(d time.Duration) string {
	days, hours, mins := int(d.Hours())/24, int(d.Hours())%24, int(d.Minutes())%60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %02dh %02dm", days, hours, mins)
	case hours > 0:
		return fmt.Sprintf("%dh %02dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

// CardGiB is a usage as the card prints it: "22/54 GiB".
func CardGiB(u system.Usage) string {
	return fmt.Sprintf("%.0f/%.0f GiB", float64(u.Used)/(1<<30), float64(u.Total)/(1<<30))
}

// Text is the card's facts as Markdown, to paste where an image will not
// do (an issue, a forum).
func (c Card) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "**%s** · `%s` · %s\n\n", c.Model, c.Serial, c.Time)
	line := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "- **%s** %s\n", k, v)
		}
	}
	cpu := c.CPU
	if c.Threads > 0 {
		cpu += fmt.Sprintf(" · %d threads", c.Threads)
	}
	line("CPU", cpu)
	line("GPU", c.GPU)
	if c.Memory.Total > 0 {
		line(i18n.T("Memory"), CardGiB(c.Memory))
	}
	if c.Disk.Total > 0 {
		line(i18n.T("Disk"), CardGiB(c.Disk))
	}
	for _, l := range c.Stack {
		v := strings.TrimSpace(l.Version + " " + l.Note)
		if l.Version != "" && l.Note != "" {
			v = l.Version + " · " + l.Note
		}
		line(l.Name, v)
	}
	for _, s := range c.Screens {
		line(i18n.T("Screen"), s.String())
	}
	line(i18n.T("Theme"), c.Theme)
	line(i18n.T("Uptime"), c.Uptime)
	if c.Link != "" {
		fmt.Fprintf(&b, "\n%s\n", c.Link)
	}
	return b.String()
}

// String is a screen as "3840×2160 · 60 Hz · ×1.5".
func (s CardScreen) String() string {
	out := fmt.Sprintf("%d×%d", s.Width, s.Height)
	if s.Hz > 0 {
		out += fmt.Sprintf(" · %.0f Hz", s.Hz)
	}
	if s.Scale > 0 && s.Scale != 1 {
		out += fmt.Sprintf(" · ×%g", s.Scale)
	}
	return out
}

// ThemeTitle is a theme's own name, from its file's first comment
// ("/* 夜 Yoru — dark: night"): "夜 Yoru"; else its name.
func ThemeTitle(css, name string) string {
	css = strings.TrimSpace(css)
	if rest, ok := strings.CutPrefix(css, "/*"); ok {
		line, _, _ := strings.Cut(rest, "\n")
		if title, _, ok := strings.Cut(line, " — "); ok && strings.TrimSpace(title) != "" {
			return strings.TrimSpace(title)
		}
	}
	return name
}

// groupDigits writes a count with thousands apart: "2,390".
func groupDigits(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
