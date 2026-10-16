// Package appui runs inua's light apps: Clock, Weather, Stocks,
// Calculator, Calendar, Color Picker, System Monitor, Tailscale, Pass,
// GPG, Music and Video, and those of other programs (Files, Viewer, the assistant, the
// games). Each runs as its own process with its own application ID, so
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
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/i18n"
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
// (as Viewer, registered by inua-viewer, does); the others bring their
// window forward when started again.
var Apps = map[string]struct {
	ID, Title string
	make      func() view
	many      bool
}{
	"clock":          {"Clock", i18n.M("Clock"), func() view { return &clockApp{} }, false},
	"weather":        {"Weather", i18n.M("Weather"), func() view { return &weatherApp{} }, false},
	"stocks":         {"Stocks", i18n.M("Stocks"), func() view { return &stocksApp{} }, false},
	"calculator":     {"Calculator", i18n.M("Calculator"), func() view { return &calcApp{} }, false},
	"calendar":       {"Calendar", i18n.M("Calendar"), func() view { return &calendarApp{} }, false},
	"color-picker":   {"ColorPicker", i18n.M("Color Picker"), func() view { return &colorApp{} }, false},
	"system-monitor": {"SystemMonitor", i18n.M("System Monitor"), func() view { return &sysmonApp{} }, false},
	"tailscale":      {"Tailscale", i18n.M("Tailscale"), func() view { return &tailscaleApp{} }, false},
	"pass":           {"Pass", i18n.M("Pass"), func() view { return &passApp{} }, false},
	"gpg":            {"GPG", i18n.M("GPG"), func() view { return &gpgApp{} }, false},
	"music":          {"Music", i18n.M("Music"), func() view { return &musicApp{} }, false},
	"video":          {"Video", i18n.M("Video"), func() view { return &videoApp{} }, false},
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

// Themed is a View that keeps a look of its own whatever the shell's
// theme: the name of a built-in theme ("washi"), or "" for the shell's.
type Themed interface{ Theme() string }

type external struct{ v View }

func (e external) theme() string {
	if t, ok := e.v.(Themed); ok {
		return t.Theme()
	}
	return ""
}

func (e external) build(c *Ctx) *gtk.ApplicationWindow { return e.v.Build(c) }
func (e external) themeChanged()                       { e.v.ThemeChanged() }
func (e external) open(args []string) {
	if o, ok := e.v.(Opener); ok {
		o.Open(args)
	}
}

// opensFiles are the apps (by name) that take files while they run:
// started again with files, the running one gets them (its Opener).
var opensFiles = map[string]bool{"music": true, "video": true}

// RegisterOpener is Register for a View that opens files (an Opener): a
// start with files hands them to the app already running.
func RegisterOpener(name, id, title string, make func() View) {
	Register(name, id, title, make)
	opensFiles[name] = true
}

// Register adds a light app from another package, for Run.
func Register(name, id, title string, make func() View) { register(name, id, title, make, false) }

// RegisterMany adds a light app whose every start runs on its own, as
// Viewer's do (inua-viewer); an app that wants one process keeps to it itself
// (inua-files does, through D-Bus).
func RegisterMany(name, id, title string, make func() View) { register(name, id, title, make, true) }

func register(name, id, title string, make func() View, many bool) {
	Apps[name] = struct {
		ID, Title string
		make      func() view
		many      bool
	}{id, title, func() view { return external{make()} }, many}
}

// Ctx is what the views share: the application, the layouts and the
// theme.
type Ctx struct {
	App *gtk.Application
	// StartHidden keeps a background app's first window out of the way.
	StartHidden bool
	UIDir       string // read layouts from here instead of the embedded copy
	Colors      map[string]string
	// DesktopIcons is the desktop's icon theme (the XDG settings portal's,
	// else GTK's), which the apps' own Phosphor theme inherits.
	DesktopIcons string
	refs         gtkx.Scope

	cfgPath   string
	cfgMod    time.Time
	css       *gtk.CssProvider
	userCSS   *gtk.CssProvider
	win       *gtk.ApplicationWindow
	v         view
	onTick    glib.SourceFunc
	open      func(gio.Application, uintptr, int32, string)
	activate  func(gio.Application)
	iconTheme string // the apps' icon theme in use, "" before loadIcons
}

// Run runs the named app until its window closes. A second start of the
// same app brings its window forward instead, unless the app has many.
// args are a page to open, or files for an opener.
func Run(name string, args []string, uiDir string) int {
	cfg, _, _ := config.Load(config.DefaultPath()) // a broken file leaves the defaults
	i18n.Init(cfg.Language)
	if name == "workshop" {
		return runWorkshop(args, uiDir)
	}
	a, ok := Apps[name]
	if !ok {
		names := make([]string, 0, len(Apps))
		for n := range Apps {
			names = append(names, n)
		}
		fmt.Fprintf(os.Stderr, "unknown app %q (%s)\n", name, strings.Join(names, ", "))
		return 2
	}
	return run(name, IDBase+"."+a.ID, a.Title, a.make(), a.many, args, uiDir)
}

// run runs view v as the app named name, with its application ID and
// title, until its window closes.
func run(name, appID, title string, v view, many bool, args []string, uiDir string) int {
	flags := gio.GApplicationDefaultFlagsValue
	if many {
		flags = gio.GApplicationNonUniqueValue
	}
	files := opensFiles[name]
	if files {
		flags |= gio.GApplicationHandlesOpenValue
	}
	app := gtk.NewApplication(appID, flags)
	c := &Ctx{App: app, UIDir: uiDir, cfgPath: config.DefaultPath(), v: v}
	opened := false // the files of the first start went to open already
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
		c.win.SetTitle(i18n.T(title))
		c.win.AddCssClass("app")
		c.win.AddCssClass("settings") // the settings window's controls and sections
		c.win.AddCssClass(name)
		if !c.StartHidden {
			c.win.Present()
		}
		if o, ok := c.v.(opener); ok && !opened {
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
	argv := []string{os.Args[0]}
	if files {
		// The files go through GApplication, so a second start's reach the
		// app already running: open, here or there.
		argv = append(argv, args...)
		c.open = func(_ gio.Application, list uintptr, n int32, _ string) {
			var paths []string
			// GFile *files[n], C memory.
			for _, ptr := range unsafe.Slice((*uintptr)(*(*unsafe.Pointer)(unsafe.Pointer(&list))), int(n)) {
				f := gio.FileBase{Ptr: ptr}
				if p := f.GetPath(); p != "" {
					paths = append(paths, p)
				}
			}
			first := c.win == nil
			opened = true
			c.activate(app.Application)
			if o, ok := c.v.(opener); ok {
				o.open(paths)
			}
			if !first {
				bringHere()
				c.win.Present()
			}
		}
		app.ConnectOpen(&c.open)
	}
	return int(app.Run(int32(len(argv)), argv))
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
	if c.css == nil {
		c.css, c.userCSS = gtk.NewCssProvider(), gtk.NewCssProvider()
		d := gdk.DisplayGetDefault()
		gtk.StyleContextAddProviderForDisplay(d, c.css, uint32(gtk.STYLE_PROVIDER_PRIORITY_APPLICATION))
		gtk.StyleContextAddProviderForDisplay(d, c.userCSS, uint32(gtk.STYLE_PROVIDER_PRIORITY_USER))
	}
	theme := cmp.Or(cfg.Style.Theme, "yoru")
	if t, ok := c.v.(interface{ theme() string }); ok && t.theme() != "" {
		theme = t.theme() // an app with a look of its own
	}
	themeCSS := cfg.ThemeCSS(theme, c.ReadUI)
	c.Colors = shell.ThemeColors(themeCSS)
	c.css.LoadFromString(themeCSS + shell.ScaleText(c.ReadUI("style.css"), cfg.Style.TextScale))
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
// (ui/icons), with the user's set over them when style.icons names one;
// "system" still means Phosphor here. The apps write the set as an icon
// theme of their own, which inherits the system's for app icons and any
// icon the set lacks.
func (c *Ctx) loadIcons() {
	cfg := c.config()
	if c.DesktopIcons == "" {
		var v gobject.Value
		v.Init(gobject.TypeStringVal)
		gtk.SettingsGetDefault().GetProperty("gtk-icon-theme-name", &v)
		c.DesktopIcons = cmp.Or(desktopIconTheme(), v.GetString(), "Adwaita")
		v.Unset()
		if strings.HasPrefix(c.DesktopIcons, "inua-") {
			c.DesktopIcons = "Adwaita"
		}
	}
	overlay := cfg.IconSetDir()
	name := appIconTheme
	if overlay != "" {
		name = "inua-apps-icons-" + cfg.Style.Icons
	}
	if overlay == "" && c.iconTheme == name {
		return // the built-in icons do not change
	}
	dir, err := rundir.Sub("app-icons")
	if err == nil {
		err = c.writeIconTheme(dir, name, overlay)
	}
	if err != nil {
		log.Printf("icons: %v", err)
		return
	}
	if c.iconTheme == "" {
		gtk.IconThemeGetForDisplay(gdk.DisplayGetDefault()).AddSearchPath(dir)
	}
	if c.iconTheme == name {
		// The set's files may have changed: make GTK read them again.
		gtkx.SetStringSetting("gtk-icon-theme-name", c.DesktopIcons)
	}
	gtkx.SetStringSetting("gtk-icon-theme-name", name)
	c.iconTheme = name
}

// desktopIconTheme is the icon theme the desktop's settings choose, read
// from the XDG settings portal (org.gnome.desktop.interface icon-theme),
// or "" without one.
func desktopIconTheme() string {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return ""
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	var v dbus.Variant
	err = conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop").
		CallWithContext(ctx, "org.freedesktop.portal.Settings.ReadOne", 0, "org.gnome.desktop.interface", "icon-theme").Store(&v)
	if err != nil {
		return ""
	}
	name, _ := v.Value().(string)
	return name
}

// appIconTheme names the apps' icon theme.
const appIconTheme = "inua-phosphor-apps"

func (c *Ctx) writeIconTheme(dir, name, overlay string) error {
	entries, err := layouts.FS.ReadDir("icons")
	if err != nil {
		return err
	}
	t := shell.IconTheme{
		Name:     name,
		Comment:  "Icons for inua's light apps (written by inua-apps)",
		Inherits: c.DesktopIcons,
		Read:     func(file string) string { return c.ReadUI("icons/" + file) },
		Overlay:  overlay,
	}
	for _, e := range entries {
		t.Builtin = append(t.Builtin, e.Name())
	}
	return t.Write(dir)
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

// sidePanel is a panel sliding in at a window's right, which widens the
// window. A floating window keeps the size and place it grew to, so once
// the panel has slid away, Hyprland is asked to put the window back: its
// size from before, and its place from before, moved by as much as it was
// moved while the panel showed.
type sidePanel struct {
	win    *gtk.ApplicationWindow
	rev    *gtk.Revealer
	want   bool
	before *hypr.Rect // the window before the panel, while it shows
	grown  *hypr.Rect // and once the panel showed
	// onRevealed outlives the window, like every signal handler.
	onRevealed func(uintptr, uintptr, uintptr)
}

func (p *sidePanel) init(win *gtk.ApplicationWindow, rev *gtk.Revealer) {
	p.win, p.rev = win, rev
	p.onRevealed = func(uintptr, uintptr, uintptr) {
		before := p.before
		if before == nil {
			return
		}
		shown, grown := p.rev.GetChildRevealed(), p.grown
		go func() {
			c, err := hyprClient()
			if err != nil {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			now, err := c.WindowOf(ctx, os.Getpid())
			if err != nil {
				log.Printf("panel: %v", err)
				return
			}
			if shown {
				gtkx.Post(func() {
					if p.before == before {
						p.grown = &now
					}
				})
				return
			}
			r := *before
			if grown != nil { // moved since it grew
				r.X += now.X - grown.X
				r.Y += now.Y - grown.Y
			}
			if err := c.PlaceWindow(ctx, os.Getpid(), r); err != nil {
				log.Printf("panel: %v", err)
			}
		}()
		if !shown {
			p.before, p.grown = nil, nil
		}
	}
	gtkx.ConnectNotify(rev, "child-revealed", &p.onRevealed)
}

// open shows the panel, once the window's place before it is known.
func (p *sidePanel) open() {
	if p.want {
		return
	}
	p.want = true
	go func() {
		var before *hypr.Rect
		if c, err := hyprClient(); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			if r, err := c.WindowOf(ctx, os.Getpid()); err == nil {
				before = &r
			}
			cancel()
		}
		gtkx.Post(func() {
			if !p.want {
				return
			}
			if p.before == nil {
				p.before, p.grown = before, nil
			}
			p.rev.SetRevealChild(true)
		})
	}()
}

func (p *sidePanel) close() {
	p.want = false
	p.rev.SetRevealChild(false)
}

func hyprClient() (*hypr.Client, error) {
	paths, err := hypr.PathsFromEnv()
	if err != nil {
		return nil, err // not in Hyprland
	}
	return hypr.NewClient(paths), nil
}
