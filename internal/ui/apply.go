package ui

import (
	"cmp"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/rundir"
	"github.com/lyricat/inua/internal/shell"
)

// styles are the CSS providers the shell keeps, so settings can change
// them while it runs.
type styles struct {
	base      *gtk.CssProvider // theme tokens + style.css
	user      *gtk.CssProvider // style.css from the config directory
	wallpaper *gtk.CssProvider // window.wallpaper
}

func (s *shellUI) theme() string { return cmp.Or(s.cfg.Style.Theme, "yoru") }

// loadStyles (re)loads the theme, the built-in style and the user's
// stylesheet.
func (s *shellUI) loadStyles() {
	st := &s.styles
	display := gdk.DisplayGetDefault()
	if st.base == nil {
		st.base, st.user, st.wallpaper = gtk.NewCssProvider(), gtk.NewCssProvider(), gtk.NewCssProvider()
		gtk.StyleContextAddProviderForDisplay(display, st.base, uint32(gtk.STYLE_PROVIDER_PRIORITY_APPLICATION))
		gtk.StyleContextAddProviderForDisplay(display, st.wallpaper, uint32(gtk.STYLE_PROVIDER_PRIORITY_APPLICATION))
		gtk.StyleContextAddProviderForDisplay(display, st.user, uint32(gtk.STYLE_PROVIDER_PRIORITY_USER))
	}
	// The theme defines the colour tokens style.css uses.
	st.base.LoadFromString(s.readUI("themes/"+s.theme()+".css") + s.readUI("style.css") + s.swatchCSS())
	user := ""
	if path := s.cfg.CSSPath(); path != "" {
		if css, err := os.ReadFile(path); err == nil {
			user = string(css)
			log.Printf("style: %s", path)
		} else if !os.IsNotExist(err) {
			log.Printf("style: %v", err)
		}
	}
	st.user.LoadFromString(user)
	s.loadIcons()
}

// loadWallpaper styles the wallpaper windows from the settings. Without an
// image they show the theme's ground with the texture, whose tile is
// written to the runtime directory.
func (s *shellUI) loadWallpaper() {
	bg := s.cfg.Background
	w := shell.Wallpaper{Image: s.cfg.ImagePath(), Fit: bg.Fit, Color: bg.Color}
	if w.Image == "" {
		colors := shellThemeColors(s, s.theme())
		path, err := writeTile(colors["field"])
		if err != nil {
			log.Printf("wallpaper: %v", err)
		}
		w.Tile = path
	}
	s.styles.wallpaper.LoadFromString(w.CSS())
}

// shellThemeColors reads a theme's colour tokens.
func shellThemeColors(s *shellUI, name string) map[string]string {
	return shell.ThemeColors(s.readUI("themes/" + name + ".css"))
}

func writeTile(field string) (string, error) {
	data, err := shell.FieldTile(field, 0.55)
	if err != nil {
		return "", err
	}
	dir, err := rundir.Dir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "field-"+strings.TrimPrefix(field, "#")+".png")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	return path, os.WriteFile(path, data, 0o644)
}

// syncWallpaper gives a bar's monitor a wallpaper window, or takes it away,
// following background.enable.
func (s *shellUI) syncWallpaper(b *bar) {
	want := s.cfg.Background.Enable && s.layer
	switch {
	case want && b.wallpaper == nil:
		win := gtk.NewApplicationWindow(s.app)
		win.AddCssClass("wallpaper")
		win.SetDecorated(false)
		w := win.GoPointer()
		layershell.InitForWindow(w)
		layershell.SetNamespace(w, "inua-background")
		layershell.SetLayer(w, layershell.LayerBackground)
		layershell.SetMonitor(w, b.mon.GoPointer())
		for _, e := range []layershell.Edge{layershell.EdgeTop, layershell.EdgeBottom, layershell.EdgeLeft, layershell.EdgeRight} {
			layershell.SetAnchor(w, e, true)
		}
		layershell.SetExclusiveZone(w, -1) // under the bar too
		win.Present()
		b.wallpaper = win // the window list holds it; ours goes in dropWallpaper
	case !want && b.wallpaper != nil:
		dropWallpaper(b)
	}
}

func dropWallpaper(b *bar) {
	if b.wallpaper != nil {
		b.wallpaper.Destroy()
		b.wallpaper.Unref()
		b.wallpaper = nil
	}
}

// applyConfig switches to new settings while running: styles, wallpaper,
// bars and the clock follow at once; the launcher and control center read
// their settings when used.
func (s *shellUI) applyConfig(c config.Config) {
	old := s.cfg
	s.cfg = c
	if old.Style != c.Style {
		s.loadStyles()
	}
	if old.Style.Theme != c.Style.Theme || old.Background != c.Background || old.Dir != c.Dir {
		s.loadWallpaper()
	}
	for _, b := range s.bars {
		if old.Dock.FlushBar != c.Dock.FlushBar {
			b.setEdgeGap(c.Dock.FlushBar)
		}
		if old.Bar.Enable != c.Bar.Enable {
			b.win.SetVisible(c.Bar.Enable)
		}
		s.syncDock(b)
		b.title.SetVisible(c.Bar.Title)
		b.tray.SetVisible(c.Bar.Tray)
		b.setHypr(s, s.snap)
		s.syncWallpaper(b)
	}
	if c.Bar.Tray && s.tray.host == nil {
		s.startTray()
	}
	if old.Notify.Enable != c.Notify.Enable {
		s.syncNotify()
	}
	if old.Bar.Clock != c.Bar.Clock {
		s.restartClock()
	}
	s.applyLauncherWeb()
	if s.settings.win != nil {
		s.showSettings()
	}
}

// reload reads the settings file again and applies it.
func (s *shellUI) reload() ipc.Response {
	c, warnings, err := config.Load(s.opts.ConfigPath)
	if err != nil {
		return ipc.Response{Error: err.Error()}
	}
	s.applyConfig(c)
	s.applyHyprland()
	msg := fmt.Sprintf("reloaded %s", s.opts.ConfigPath)
	for _, w := range warnings {
		msg += "\n" + w
	}
	return ipc.Response{OK: true, Result: msg}
}
