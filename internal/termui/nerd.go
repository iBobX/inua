package termui

import (
	"cmp"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/sysmon"
	"github.com/lyricat/inua/internal/gtkx"
)

// The top of column 1 says where this is, as a prompt would: user@host,
// then the shell new panes start, how busy the CPU is, the memory in use
// and the uptime.

type nerdInfo struct {
	who, sys *gtk.Label
	// boot is when the machine started; cpu and mem are the shares busy
	// and in use, below 0 until read.
	boot     time.Time
	cpu, mem float64
}

// nerdEvery is how often the CPU and the memory are read again.
const nerdEvery = 2 * time.Second

// startNerd reads who and where, then the CPU and the memory every
// nerdEvery, off the main thread, and shows them.
func (a *App) startNerd() {
	a.nerd.cpu, a.nerd.mem = -1, -1
	go func() {
		name := os.Getenv("USER")
		if u, err := user.Current(); name == "" && err == nil {
			name = u.Username
		}
		host, _ := os.Hostname()
		var boot time.Time
		if up := sysmon.Uptime(); up > 0 {
			boot = time.Now().Add(-up)
		}
		mem := memUsed()
		gtkx.Post(func() {
			n := &a.nerd
			n.who.SetText(cmp.Or(name, "?") + "@" + cmp.Or(host, "localhost"))
			n.boot, n.mem = boot, mem
			a.updateNerd()
		})
		// The CPU's share busy is between two readings: none shows
		// until the second.
		prev, _ := sysmon.ParseCPU(sysmon.ReadFile("proc/stat"))
		for range time.Tick(nerdEvery) {
			cur, _ := sysmon.ParseCPU(sysmon.ReadFile("proc/stat"))
			cpu := -1.0
			if cur.Total > prev.Total {
				cpu = sysmon.Usage(prev, cur)
			}
			prev = cur
			mem := memUsed()
			gtkx.Post(func() {
				a.nerd.cpu, a.nerd.mem = cpu, mem
				a.updateNerd()
			})
		}
	}()
}

// memUsed is the share of the memory in use, 0 to 100; -1 when unknown.
func memUsed() float64 {
	m := sysmon.ParseMemory(sysmon.ReadFile("proc/meminfo"))
	if m.Total == 0 {
		return -1
	}
	return 100 * float64(m.Used()) / float64(m.Total)
}

// updateNerd writes the system line again: the shell may have changed,
// the readings and the uptime have.
func (a *App) updateNerd() {
	n := &a.nerd
	if n.who.GetText() == "" {
		return
	}
	parts := []string{filepath.Base(cmp.Or(a.set.Shell.Program, os.Getenv("SHELL"), "sh"))}
	if n.cpu >= 0 {
		parts = append(parts, fmt.Sprintf("cpu %.0f%%", n.cpu))
	}
	if n.mem >= 0 {
		parts = append(parts, fmt.Sprintf("mem %.0f%%", n.mem))
	}
	if !n.boot.IsZero() {
		parts = append(parts, "up "+uptime(time.Since(n.boot)))
	}
	if s := strings.Join(parts, " · "); n.sys.GetText() != s {
		n.sys.SetText(s)
	}
}

// uptime is a time up as uptime(1) would say it short: "3d 2h", "5h 12m",
// "12m".
func uptime(d time.Duration) string {
	m := int(d.Minutes())
	switch {
	case m >= 24*60:
		return fmt.Sprintf("%dd %dh", m/(24*60), m/60%24)
	case m >= 60:
		return fmt.Sprintf("%dh %dm", m/60, m%60)
	}
	return fmt.Sprintf("%dm", m)
}
