// Package appui runs inua's light apps: Clock, Weather, Stocks,
// Calculator, Calendar, Color Picker, System Monitor, Tailscale and
// Viewer. Each runs as its own process with its own application ID, so
// the launcher, the dock and the compositor see it as an app, but in the
// shell's look: the theme and style from inua's settings, followed as
// they change, and the shell's icons.
package appui

import (
	"cmp"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/rundir"
	"github.com/lyricat/inua/internal/shell"
	layouts "github.com/lyricat/inua/ui"
)

// IDBase prefixes the apps' application IDs; the shell's is IDBase itself.
const IDBase = "io.github.lyricat.Inua"

// view is one app's window and logic.
type view interface {
	// build makes the window from its layout; called once, on activate.
	build(c *Ctx) *gtk.ApplicationWindow
	// themeChanged runs after the theme's colours change.
	themeChanged()
}

// pager is a view with pages that `inua-apps NAME PAGE` can open.
type pager interface{ showPage(name string) }

// opener is a view that opens files: `inua-apps NAME FILE…`.
type opener interface{ open(args []string) }

// Apps are the light apps by command name, with their application ID
// suffix and window title. A many-windowed app runs once for each start
// (each image opened is its own window); the others bring their window
// forward when started again.
var Apps = map[string]struct {
	ID, Title string
	make      func() view
	many      bool
}{
	"clock":          {"Clock", "Clock", func() view { return &clockApp{} }, false},
	"weather":        {"Weather", "Weather", func() view { return &weatherApp{} }, false},
	"stocks":         {"Stocks", "Stocks", func() view { return &stocksApp{} }, false},
	"calculator":     {"Calculator", "Calculator", func() view { return &calcApp{} }, false},
	"calendar":       {"Calendar", "Calendar", func() view { return &calendarApp{} }, false},
	"color-picker":   {"ColorPicker", "Color Picker", func() view { return &colorApp{} }, false},
	"system-monitor": {"SystemMonitor", "System Monitor", func() view { return &sysmonApp{} }, false},
	"tailscale":      {"Tailscale", "Tailscale", func() view { return &tailscaleApp{} }, false},
	"viewer":         {"Viewer", "Viewer", func() view { return &viewerApp{} }, true},
}

// View is a light app's window from another package: cmd/inua-assistant
// registers one so that inua-apps never links what it needs.
type View interface {
	// Build makes the window from its layout; called once, on activate.
	Build(c *Ctx) *gtk.ApplicationWindow
	// ThemeChanged runs after the theme's colours change.
	ThemeChanged()
}

// Opener is a View that takes the arguments of its first start.
type Opener interface{ Open(args []string) }

type external struct{ v View }

func (e external) build(c *Ctx) *gtk.ApplicationWindow { return e.v.Build(c) }
func (e external) themeChanged()                       { e.v.ThemeChanged() }
func (e external) open(args []string) {
	if o, ok := e.v.(Opener); ok {
		o.Open(args)
	}
}

// Register adds a light app from another package, for Run.
func Register(name, id, title string, make func() View) {
	Apps[name] = struct {
		ID, Title string
		make      func() view
		many      bool
	}{id, title, func() view { return external{make()} }, false}
}

// Ctx is what the views share: the application, the layouts and the
// theme.
type Ctx struct {
	App    *gtk.Application
	UIDir  string // read layouts from here instead of the embedded copy
	Colors map[string]string
	refs   gtkx.Scope

	cfgPath  string
	cfgMod   time.Time
	css      *gtk.CssProvider
	userCSS  *gtk.CssProvider
	win      *gtk.ApplicationWindow
	v        view
	onTick   glib.SourceFunc
	activate func(gio.Application)
	iconPath bool // the apps' icon theme is in use
}

// Run runs the named app until its window closes. A second start of the
// same app brings its window forward instead, unless the app has many.
// args are a page to open, or files for an opener.
func Run(name string, args []string, uiDir string) int {
	a, ok := Apps[name]
	if !ok {
		names := make([]string, 0, len(Apps))
		for n := range Apps {
			names = append(names, n)
		}
		fmt.Fprintf(os.Stderr, "unknown app %q (%s)\n", name, strings.Join(names, ", "))
		return 2
	}
	flags := gio.GApplicationDefaultFlagsValue
	if a.many {
		flags = gio.GApplicationNonUniqueValue
	}
	app := gtk.NewApplication(IDBase+"."+a.ID, flags)
	c := &Ctx{App: app, UIDir: uiDir, cfgPath: config.DefaultPath(), v: a.make()}
	c.activate = func(gio.Application) {
		if c.win != nil { // started again: the window comes to the user
			bringHere()
			c.win.Present()
			return
		}
		c.loadStyle()
		c.loadIcons()
		c.win = c.v.build(c)
		c.win.SetApplication(app)
		c.win.SetTitle(a.Title)
		c.win.AddCssClass("app")
		c.win.AddCssClass("settings") // the settings window's controls and sections
		c.win.AddCssClass(name)
		c.win.Present()
		if o, ok := c.v.(opener); ok {
			o.open(args)
		} else if p, ok := c.v.(pager); ok && len(args) > 0 {
			p.showPage(args[0])
		}
		c.onTick = func(uintptr) bool {
			c.followConfig()
			return true
		}
		glib.TimeoutAdd(2000, &c.onTick, 0)
	}
	app.ConnectActivate(&c.activate)
	return int(app.Run(1, []string{os.Args[0]}))
}

// ReadUI is a layout or stylesheet from ui/.
func (c *Ctx) ReadUI(name string) string {
	if c.UIDir != "" {
		if b, err := os.ReadFile(filepath.Join(c.UIDir, name)); err == nil {
			return string(b)
		}
	}
	b, err := layouts.FS.ReadFile(name)
	if err != nil {
		log.Fatalf("ui: %v", err)
	}
	return string(b)
}

// Load opens a layout; objects taken with Get live as long as the app.
func (c *Ctx) Load(name string) *gtkx.Builder {
	b, err := gtkx.Load(c.ReadUI(name), &c.refs)
	if err != nil {
		log.Fatalf("%s: %v", name, err)
	}
	return b
}

func (c *Ctx) config() config.Config {
	cfg, _, err := config.Load(c.cfgPath)
	if err != nil {
		return config.Default()
	}
	return cfg
}

// loadStyle loads the theme, the shell's style and the user's.
func (c *Ctx) loadStyle() {
	cfg := c.config()
	theme := cmp.Or(cfg.Style.Theme, "yoru")
	if c.css == nil {
		c.css, c.userCSS = gtk.NewCssProvider(), gtk.NewCssProvider()
		d := gdk.DisplayGetDefault()
		gtk.StyleContextAddProviderForDisplay(d, c.css, uint32(gtk.STYLE_PROVIDER_PRIORITY_APPLICATION))
		gtk.StyleContextAddProviderForDisplay(d, c.userCSS, uint32(gtk.STYLE_PROVIDER_PRIORITY_USER))
	}
	themeCSS := c.ReadUI("themes/" + theme + ".css")
	c.Colors = shell.ThemeColors(themeCSS)
	c.css.LoadFromString(themeCSS + c.ReadUI("style.css"))
	if p := cfg.CSSPath(); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			c.userCSS.LoadFromString(string(b))
		}
	}
	if st, err := os.Stat(c.cfgPath); err == nil {
		c.cfgMod = st.ModTime()
	}
}

// followConfig reloads the style when inua's settings change.
func (c *Ctx) followConfig() {
	st, err := os.Stat(c.cfgPath)
	if err != nil || st.ModTime().Equal(c.cfgMod) {
		return
	}
	c.loadStyle()
	c.loadIcons()
	c.v.themeChanged()
}

// loadIcons draws the apps' own icons from the Phosphor set inua ships
// (ui/icons), whatever the shell's style.icons: the apps write the set as
// an icon theme of their own, which inherits the system's for app icons
// and any icon the set lacks.
func (c *Ctx) loadIcons() {
	if c.iconPath {
		return
	}
	settings := gtk.SettingsGetDefault()
	var v gobject.Value
	v.Init(gobject.TypeStringVal)
	settings.GetProperty("gtk-icon-theme-name", &v)
	system := cmp.Or(v.GetString(), "Adwaita")
	v.Unset()
	if system == appIconTheme {
		system = "Adwaita"
	}
	dir, err := rundir.Sub("app-icons")
	if err == nil {
		err = c.writeIconTheme(dir, system)
	}
	if err != nil {
		log.Printf("icons: %v", err)
		return
	}
	gtk.IconThemeGetForDisplay(gdk.DisplayGetDefault()).AddSearchPath(dir)
	gtkx.SetStringSetting("gtk-icon-theme-name", appIconTheme)
	c.iconPath = true
}

// appIconTheme names the apps' icon theme.
const appIconTheme = "inua-phosphor-apps"

func (c *Ctx) writeIconTheme(dir, inherits string) error {
	theme := filepath.Join(dir, appIconTheme)
	if err := os.MkdirAll(filepath.Join(theme, "symbolic"), 0o700); err != nil {
		return err
	}
	index := fmt.Sprintf(`[Icon Theme]
Name=%s
Comment=Phosphor icons for inua's light apps (written by inua-apps)
Inherits=%s,hicolor
Directories=symbolic

[symbolic]
Size=16
MinSize=8
MaxSize=512
Type=Scalable
Context=Status
`, appIconTheme, inherits)
	if err := os.WriteFile(filepath.Join(theme, "index.theme"), []byte(index), 0o644); err != nil {
		return err
	}
	entries, err := layouts.FS.ReadDir("icons")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".svg") {
			continue
		}
		if err := os.WriteFile(filepath.Join(theme, "symbolic", e.Name()), []byte(c.ReadUI("icons/"+e.Name())), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// bringHere moves the app's window to the workspace in view, on Hyprland,
// rather than leaving it where it was opened.
func bringHere() {
	paths, err := hypr.PathsFromEnv()
	if err != nil {
		return // not on Hyprland
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := hypr.NewClient(paths).BringHere(ctx, os.Getpid()); err != nil {
		log.Printf("bringing the window here: %v", err)
	}
}
