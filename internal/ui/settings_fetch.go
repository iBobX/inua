package ui

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

// fetchSwatches are the theme's colours shown under the facts, as
// fastfetch shows the terminal's.
var fetchSwatches = []string{"t0", "t1", "t2", "line", "accent", "primary", "ok", "warn", "danger"}

// fetchSystem reads the facts off the main thread each time the About
// page is shown (the uptime and memory move), and shows them.
func (s *shellUI) fetchSystem() {
	a := &s.settings.about
	if a.fetching {
		return
	}
	a.fetching = true
	client := s.client
	extra := shell.FetchExtra{
		Shell:  filepath.Base(os.Getenv("SHELL")),
		Inua:   strings.TrimPrefix(s.opts.Version, "v"),
		Theme:  s.theme() + " · " + cmp.Or(s.cfg.Style.Icons, "phosphor"),
		Locale: cmp.Or(os.Getenv("LC_ALL"), os.Getenv("LANG")),
	}
	if extra.Shell == "." {
		extra.Shell = ""
	}
	for _, m := range s.snap.Monitors {
		d := fmt.Sprintf("%d×%d", m.Width, m.Height)
		if hz, ok := m.Raw["refreshRate"].(float64); ok && hz > 0 {
			d += fmt.Sprintf(" @ %.0f Hz", math.Round(hz))
		}
		if m.Scale > 0 && m.Scale != 1 {
			d += fmt.Sprintf(" (×%g)", m.Scale)
		}
		extra.Displays = append(extra.Displays, d)
	}
	if b := s.sys.Bat; b.Present {
		extra.Battery = shell.BatteryText(b)
	}
	user, _ := os.LookupEnv("USER")
	go func() {
		info := system.ReadSysInfo("/")
		extra.WM = "Hyprland"
		if client != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			if b, err := client.Request(ctx, "j/version"); err == nil {
				var v struct{ Tag, Version string }
				if json.Unmarshal(b, &v) == nil {
					extra.WM += " " + strings.TrimPrefix(cmp.Or(v.Tag, v.Version), "v")
				}
			} else {
				log.Printf("about: hyprland version: %v", err)
			}
			cancel()
		}
		facts := shell.FetchFacts(info, extra)
		title := info.Host
		if user != "" {
			title = user + "@" + info.Host
		}
		gtkx.Post(func() {
			a.fetching = false
			a.facts, a.fetchTitle = facts, title
			s.showFetch()
		})
	}()
}

// showFetch lays the facts out: the logo in the accent, the title over a
// rule, the facts in two columns, the theme's colours.
func (s *shellUI) showFetch() {
	a := &s.settings.about
	if s.settings.win == nil {
		return
	}
	gtkx.Clear(&a.fetch)
	a.fetchCopy.SetLabel(i18n.T("Copy"))
	a.fetchCopy.SetSensitive(len(a.facts) > 0)
	if len(a.facts) == 0 {
		l := gtk.NewLabel(i18n.T("Reading…"))
		l.AddCssClass("sub")
		gtkx.Append(&a.fetch, &l.Widget)
		return
	}
	logo := gtk.NewLabel(shell.FetchLogo)
	logo.AddCssClass("fetch-logo")
	logo.SetValign(gtk.AlignStartValue)
	logo.SetXalign(0)
	gtkx.Append(&a.fetch, &logo.Widget)

	col := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	col.SetHexpand(true)
	title := gtk.NewLabel(a.fetchTitle)
	title.AddCssClass("fetch-title")
	title.SetXalign(0)
	gtkx.Append(col, &title.Widget)
	rule := gtk.NewLabel(strings.Repeat("-", len([]rune(a.fetchTitle))))
	rule.AddCssClass("fetch-rule")
	rule.SetXalign(0)
	gtkx.Append(col, &rule.Widget)

	grid := gtk.NewGrid()
	grid.SetColumnSpacing(12)
	grid.SetRowSpacing(2)
	for i, f := range a.facts {
		k := gtk.NewLabel(f.Key)
		k.AddCssClass("fetch-key")
		k.SetXalign(0)
		k.SetValign(gtk.AlignStartValue)
		grid.Attach(&k.Widget, 0, int32(i), 1, 1)
		v := gtk.NewLabel(f.Value)
		v.AddCssClass("fetch-value")
		v.SetXalign(0)
		v.SetWrap(true)
		v.SetSelectable(true)
		v.SetHexpand(true)
		grid.Attach(&v.Widget, 1, int32(i), 1, 1)
	}
	gtkx.Append(col, &grid.Widget)

	swatches := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	swatches.AddCssClass("fetch-swatches")
	for _, name := range fetchSwatches {
		sw := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		sw.AddCssClass("fetch-swatch")
		sw.AddCssClass("c-" + name)
		sw.SetTooltipText(name)
		gtkx.Append(swatches, &sw.Widget)
	}
	gtkx.Append(col, &swatches.Widget)
	gtkx.Append(&a.fetch, &col.Widget)
}

// copyFetch copies the facts as text.
func (s *shellUI) copyFetch() {
	a := &s.settings.about
	if len(a.facts) == 0 {
		return
	}
	text := shell.FetchText(a.fetchTitle, a.facts)
	go func() {
		if err := copyText(text); err != nil {
			log.Printf("about: copy: %v", err)
			gtkx.Post(func() { s.setStatus(i18n.Tf("Not copied: %s", err.Error()), true) })
			return
		}
		gtkx.Post(func() {
			if s.settings.win != nil {
				a.fetchCopy.SetLabel(i18n.T("Copied"))
			}
		})
	}()
}
