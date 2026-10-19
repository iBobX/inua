package termui

import (
	"bufio"
	"cmp"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
)

// The top of column 1 says where this is, as a prompt would: user@host,
// then the system, the kernel, the shell new panes start and the uptime.

type nerdInfo struct {
	who, sys *gtk.Label
	// system and kernel are read once; boot is when the machine started.
	system, kernel string
	boot           time.Time
}

// startNerd reads who and where off the main thread, and shows them.
func (a *App) startNerd() {
	go func() {
		name := os.Getenv("USER")
		if u, err := user.Current(); name == "" && err == nil {
			name = u.Username
		}
		host, _ := os.Hostname()
		system, kernel, boot := osID(), kernelRelease(), bootTime()
		gtkx.Post(func() {
			n := &a.nerd
			n.who.SetText(cmp.Or(name, "?") + "@" + cmp.Or(host, "localhost"))
			n.system, n.kernel, n.boot = system, kernel, boot
			a.updateNerd()
		})
	}()
}

// updateNerd writes the system line again: the shell may have changed,
// the uptime has.
func (a *App) updateNerd() {
	n := &a.nerd
	if n.system == "" {
		return
	}
	parts := []string{n.system}
	if n.kernel != "" {
		parts = append(parts, n.kernel)
	}
	parts = append(parts, filepath.Base(cmp.Or(a.set.Shell.Program, os.Getenv("SHELL"), "sh")))
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

// osID is the system's ID in /etc/os-release ("arch", "debian"), else
// "linux".
func osID() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return "linux"
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "ID="); ok {
			return cmp.Or(strings.Trim(v, `"'`), "linux")
		}
	}
	return "linux"
}

// kernelRelease is the kernel's version without its build: "6.8.0".
func kernelRelease() string {
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return ""
	}
	v, _, _ := strings.Cut(strings.TrimSpace(string(b)), "-")
	return v
}

// bootTime is when the machine started, from /proc/uptime; zero when it
// cannot say.
func bootTime() time.Time {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return time.Time{}
	}
	f, _, _ := strings.Cut(string(b), " ")
	secs, err := strconv.ParseFloat(f, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Now().Add(-time.Duration(secs * float64(time.Second)))
}
