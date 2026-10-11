package ui

import (
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/shell"
)

// settings is the settings window (settings.ui), a normal toplevel built
// on open and destroyed on close. Every change applies to the running
// shell at once (applyConfig) and is saved to the settings file shortly
// after the last one.
type settings struct {
	win  *gtk.ApplicationWindow // nil while closed
	refs gtkx.Scope

	status, clockPreview gtk.Label
	pages                gtk.Stack
	themes               gtk.FlowBox
	swatches             map[string]*gtk.Button // by theme, owned by the flow box

	bgEnable, title, tray, tpAccel, notifyOn, critFocus gtk.Switch
	barOn, dockOn, dockHide, dockDrawer                 gtk.Switch
	dockSize, dockPosition                              gtk.DropDown
	dockPinned                                          gtk.Box
	bgImage, bgColor, clock, terminal, lock, screenshot gtk.Entry
	bgFit, ws, launchMode, swap, caps, icons, banner    gtk.DropDown
	tpSpeed                                             gtk.Scale

	// Keyboard and touchpad: where the settings come from, and why they
	// would do nothing.
	kbState, kbReadout, kbWarn, tpState, tpValue, tpWarn gtk.Label
	notifyState                                          gtk.Label
	kbReset, tpReset                                     gtk.Button
	// hyprKb is Hyprland's kb_options while inua sets none, so the
	// controls start from what is in effect.
	hyprKb string

	sc shortcutState // the Shortcuts page

	sticky map[uintptr]*stickyHead // by the pages' vertical adjustments

	// Signal sources by pointer: entries and drop-downs share one handler.
	entries map[uintptr]*gtk.Entry
	drops   map[uintptr]*gtk.DropDown

	filling bool    // controls are being set from the settings; not an edit
	saveGen uintptr // the pending save; see onSettingsSave
	dirty   bool    // a save is pending
	dialog  *gtk.FileDialog
}

// Theme names as shown on the swatches.
var themeKanji = map[string]string{
	"yoru": "夜", "washi": "和紙", "aojashin": "青写真", "suimen": "水面",
	"rinko": "燐光", "bunko": "分光", "denko": "電光",
}

var (
	fitChoices  = config.Fits
	wsChoices   = []string{"monitor", "all"}
	modeChoices = []string{"apps", "windows"}
	swapChoices = []string{"", "left", "both"}
	capsChoices = []string{"", "escape", "ctrl"}
)

func (s *shellUI) bindSettingsHandlers() {
	s.onSettingsEntry = func(ptr, _ uintptr) {
		if e := s.settings.entries[ptr]; e != nil && !s.settings.filling {
			s.settingsEntryChanged(e)
		}
	}
	s.onSettingsSwitch = func(sw gtk.Switch, on bool) bool {
		if s.settings.filling {
			return false
		}
		c := s.cfg
		switch sw.GetName() {
		case "bg_enable":
			c.Background.Enable = on
		case "title":
			c.Bar.Title = on
		case "tray":
			c.Bar.Tray = on
		case "bar_enable":
			c.Bar.Enable = on
		case "dock_enable":
			c.Dock.Enable = on
		case "dock_drawer":
			c.Dock.Drawer = on
		case "dock_autohide":
			c.Dock.Autohide = on
		case "notify_enable":
			c.Notify.Enable = on
		case "critical_focus":
			c.Notify.CriticalInFocus = on
		case "tp_accel":
			c.Input.TouchpadAccel = "flat"
			if on {
				c.Input.TouchpadAccel = "adaptive"
			}
		}
		s.changeSettings(c)
		return false // let the switch move
	}
	s.onSettingsDrop = func(ptr, _, _ uintptr) {
		d := s.settings.drops[ptr]
		if d == nil || s.settings.filling {
			return
		}
		i := int(d.GetSelected())
		c := s.cfg
		switch d.GetName() {
		case "bg_fit":
			c.Background.Fit = pick(fitChoices, i, c.Background.Fit)
		case "ws":
			c.Bar.Workspaces = pick(wsChoices, i, c.Bar.Workspaces)
		case "launch_mode":
			c.Launcher.Mode = pick(modeChoices, i, c.Launcher.Mode)
		case "dock_size":
			if i >= 0 && i < len(config.DockSizes) {
				c.Dock.Size = config.DockSizes[i]
			}
		case "dock_position":
			c.Dock.Position = pick(config.DockPositions, i, c.Dock.Position)
		case "banner":
			if i >= 0 && i < len(config.BannerChoices) {
				c.Notify.Banner = config.BannerChoices[i]
			}
		case "icons":
			c.Style.Icons = pick(config.IconSets, i, c.Style.Icons)
		case "swap":
			opts := shell.SetSwapCtrlAlt(s.kbOptions(), pick(swapChoices, i, ""))
			c.Input.KbOptions = &opts
		case "caps":
			opts := shell.SetCapsLock(s.kbOptions(), pick(capsChoices, i, ""))
			c.Input.KbOptions = &opts
		}
		s.changeSettings(c)
	}
	s.onSettingsButton = func(b gtk.Button) {
		name := b.GetName()
		if s.shortcutButton(name) {
			return
		}
		c := s.cfg
		switch {
		case strings.HasPrefix(name, "theme:"):
			c.Style.Theme = strings.TrimPrefix(name, "theme:")
			s.changeSettings(c)
		case name == "bg_choose":
			s.chooseImage()
		case name == "bg_clear":
			c.Background.Image = ""
			s.changeSettings(c)
		case strings.HasPrefix(name, "pin-"):
			action, id, _ := strings.Cut(strings.TrimPrefix(name, "pin-"), ":")
			c.Dock.Pinned = movePinned(c.Dock.Pinned, id, action)
			s.changeSettings(c)
		case name == "kb_reset":
			c.Input.KbOptions = nil
			s.changeSettings(c)
		case name == "tp_reset":
			c.Input.TouchpadSpeed, c.Input.TouchpadAccel = nil, ""
			s.changeSettings(c)
		}
	}
	s.onSettingsScale = func(r gtk.Range, _ gtk.ScrollType, v float64) bool {
		if s.settings.filling {
			return false
		}
		v = math.Round(max(-1, min(v, 1))*20) / 20 // the steps of 0.05
		c := s.cfg
		c.Input.TouchpadSpeed = &v
		s.changeSettings(c)
		return false // let the scale move
	}
	s.onSettingsScroll = s.updateSticky
	s.onSettingsClose = func(gtk.Window) bool {
		s.closeSettings()
		return true
	}
	s.onSettingsKey = func(_ gtk.EventControllerKey, keyval, keycode uint32, state gdk.ModifierType) bool {
		if s.recordKey(keyval, keycode, state) {
			return true
		}
		if keyval == keyEscape {
			s.closeSettings()
			return true
		}
		return false
	}
	s.onRecordTimeout = func(gen uintptr) bool {
		if gen == s.settings.sc.recordGen {
			s.stopRecording()
		}
		return false
	}
	s.onSettingsSave = func(gen uintptr) bool {
		if gen == s.settings.saveGen {
			s.saveSettings()
		}
		return false
	}
	s.onImageChosen = func(_, result, _ uintptr) {
		st := &s.settings
		d := st.dialog
		st.dialog = nil
		if d == nil {
			return
		}
		defer d.Unref()
		file, err := d.OpenFinish(&gio.AsyncResultBase{Ptr: result})
		if err != nil || file == nil || st.win == nil {
			return // cancelled, or the window closed meanwhile
		}
		path := file.GetPath()
		unref(file)
		c := s.cfg
		c.Background.Image = shortenHome(path)
		s.changeSettings(c)
	}
}

func pick(choices []string, i int, current string) string {
	if i >= 0 && i < len(choices) {
		return choices[i]
	}
	return current
}

func (s *shellUI) openSettings() {
	st := &s.settings
	if st.win != nil {
		st.win.Present()
		return
	}
	st.win = &gtk.ApplicationWindow{}
	ui := s.load("settings.ui", &st.refs)
	ui.Get("settings", st.win)
	ui.Get("status", &st.status)
	ui.Get("pages", &st.pages)
	ui.Get("themes", &st.themes)
	ui.Get("clock_preview", &st.clockPreview)
	ui.Get("bg_enable", &st.bgEnable)
	ui.Get("title", &st.title)
	ui.Get("tray", &st.tray)
	ui.Get("bg_image", &st.bgImage)
	ui.Get("bg_color", &st.bgColor)
	ui.Get("clock", &st.clock)
	ui.Get("terminal", &st.terminal)
	ui.Get("lock", &st.lock)
	ui.Get("screenshot", &st.screenshot)
	ui.Get("bg_fit", &st.bgFit)
	ui.Get("ws", &st.ws)
	ui.Get("launch_mode", &st.launchMode)
	ui.Get("icons", &st.icons)
	ui.Get("banner", &st.banner)
	ui.Get("bar_enable", &st.barOn)
	ui.Get("dock_enable", &st.dockOn)
	ui.Get("dock_autohide", &st.dockHide)
	ui.Get("dock_drawer", &st.dockDrawer)
	ui.Get("dock_size", &st.dockSize)
	ui.Get("dock_position", &st.dockPosition)
	ui.Get("dock_pinned", &st.dockPinned)
	ui.Get("sc_inua", &st.sc.inua)
	ui.Get("sc_shots", &st.sc.shots)
	ui.Get("sc_custom", &st.sc.custom)
	ui.Get("sc_hypr", &st.sc.hypr)
	ui.Get("sc_recording", &st.sc.recordingNote)
	ui.Get("sc_warn", &st.sc.warn)
	ui.Get("sc_new_name", &st.sc.newName)
	ui.Get("sc_new_cmd", &st.sc.newCmd)
	var scAdd gtk.Button
	ui.Get("sc_add", &scAdd)
	scAdd.ConnectClicked(&s.onSettingsButton)
	ui.Get("notify_enable", &st.notifyOn)
	ui.Get("critical_focus", &st.critFocus)
	ui.Get("notify_state", &st.notifyState)
	ui.Get("swap", &st.swap)
	ui.Get("caps", &st.caps)
	ui.Get("tp_speed", &st.tpSpeed)
	ui.Get("tp_accel", &st.tpAccel)
	ui.Get("kb_state", &st.kbState)
	ui.Get("kb_readout", &st.kbReadout)
	ui.Get("kb_warn", &st.kbWarn)
	ui.Get("tp_state", &st.tpState)
	ui.Get("tp_value", &st.tpValue)
	ui.Get("tp_warn", &st.tpWarn)
	ui.Get("kb_reset", &st.kbReset)
	ui.Get("tp_reset", &st.tpReset)
	var choose, clearImage gtk.Button
	ui.Get("bg_choose", &choose)
	ui.Get("bg_clear", &clearImage)
	s.buildStickyHeads(ui)
	ui.Close()

	st.entries = map[uintptr]*gtk.Entry{}
	for _, e := range []*gtk.Entry{&st.bgImage, &st.bgColor, &st.clock, &st.terminal, &st.lock, &st.screenshot} {
		st.entries[e.GoPointer()] = e
		gtkx.ConnectChanged(e, &s.onSettingsEntry)
	}
	st.drops = map[uintptr]*gtk.DropDown{}
	for _, d := range []*gtk.DropDown{&st.bgFit, &st.ws, &st.launchMode, &st.swap, &st.caps, &st.icons, &st.banner, &st.dockSize, &st.dockPosition} {
		st.drops[d.GoPointer()] = d
		gtkx.ConnectNotify(d, "selected", &s.onSettingsDrop)
	}
	for _, sw := range []*gtk.Switch{&st.bgEnable, &st.title, &st.tray, &st.tpAccel, &st.notifyOn, &st.critFocus, &st.barOn, &st.dockOn, &st.dockHide, &st.dockDrawer} {
		sw.ConnectStateSet(&s.onSettingsSwitch)
	}
	for _, b := range []*gtk.Button{&choose, &clearImage, &st.kbReset, &st.tpReset} {
		b.ConnectClicked(&s.onSettingsButton)
	}
	st.tpSpeed.ConnectChangeValue(&s.onSettingsScale)
	s.buildSwatches()
	st.hyprKb = s.hyprKbOptions()
	st.sc.binds = s.hyprBinds()
	s.showSourceWarning()

	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue) // before entries and buttons, for recording
	keys.ConnectKeyPressed(&s.onSettingsKey)
	st.win.AddController(&keys.EventController)
	st.win.ConnectCloseRequest(&s.onSettingsClose)

	st.win.SetApplication(s.app)
	s.showSettings()
	s.setStatus("", false)
	st.win.Present()
}

// settingsPages are the pages' names in settings.ui, in order.
var settingsPages = []string{"appearance", "bar", "launcher", "notifications", "shortcuts", "keyboard", "mouse"}

// settingsPageAliases name the page that now holds a section that was once
// a page of its own.
var settingsPageAliases = map[string]string{
	"wallpaper": "appearance", "dock": "bar", "control": "bar", "touchpad": "mouse",
}

// showSettingsPage turns the open settings window to a page, reporting
// whether it exists.
func (s *shellUI) showSettingsPage(name string) bool {
	st := &s.settings
	if a, ok := settingsPageAliases[name]; ok {
		name = a
	}
	if st.win == nil || !slices.Contains(settingsPages, name) {
		return false
	}
	st.pages.SetVisibleChildName(name)
	return true
}

func (s *shellUI) closeSettings() {
	st := &s.settings
	if st.win == nil {
		return
	}
	s.stopRecording()
	if st.dirty {
		s.saveSettings()
	}
	st.win.Destroy()
	st.refs.Release()
	st.win, st.swatches, st.entries, st.drops, st.sticky = nil, nil, nil, nil, nil
}

// buildSwatches adds one button per theme: its ground with its accent and
// text colours, and its name.
func (s *shellUI) buildSwatches() {
	st := &s.settings
	st.swatches = map[string]*gtk.Button{}
	for _, name := range config.Themes {
		btn := gtk.NewButton()
		btn.SetName("theme:" + name)
		btn.AddCssClass("swatch")
		btn.AddCssClass("sw-" + name)
		btn.SetTooltipText(name)

		box := gtk.NewBox(gtk.OrientationVerticalValue, 6)
		ground := gtk.NewBox(gtk.OrientationHorizontalValue, 4)
		ground.AddCssClass("ground")
		ground.SetValign(gtk.AlignStartValue)
		for _, chip := range []string{"chip-accent", "chip-text"} {
			c := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
			c.AddCssClass("chip")
			c.AddCssClass(chip)
			c.SetValign(gtk.AlignEndValue)
			gtkx.Append(ground, &c.Widget)
		}
		gtkx.Append(box, &ground.Widget)
		label := gtk.NewLabel(themeKanji[name] + "  " + name)
		label.SetXalign(0)
		gtkx.Append(box, &label.Widget)

		btn.SetChild(&box.Widget)
		box.Unref()
		btn.ConnectClicked(&s.onSettingsButton)
		st.themes.Append(&btn.Widget)
		btn.Unref() // the flow box holds it
		st.swatches[name] = btn
	}
}

// swatchCSS colours the theme swatches from the theme files, whatever the
// current theme is.
func (s *shellUI) swatchCSS() string {
	var b strings.Builder
	for _, name := range config.Themes {
		c := shellThemeColors(s, name)
		fmt.Fprintf(&b, ".swatch.sw-%[1]s .ground { background-color: %[2]s; border: 1px solid %[3]s; }\n"+
			".swatch.sw-%[1]s .chip-accent { background-color: %[4]s; }\n"+
			".swatch.sw-%[1]s .chip-text { background-color: %[5]s; }\n",
			name, c["app"], c["line"], c["accent"], c["t0"])
	}
	return b.String()
}

// showSettings sets every control from the current settings.
func (s *shellUI) showSettings() {
	st := &s.settings
	if st.win == nil {
		return
	}
	st.filling = true
	defer func() { st.filling = false }()
	c := s.cfg
	for name, btn := range st.swatches {
		toggle(&btn.Widget, "on", name == s.theme())
	}
	st.bgEnable.SetActive(c.Background.Enable)
	st.title.SetActive(c.Bar.Title)
	st.tray.SetActive(c.Bar.Tray)
	setEntry(&st.bgImage, c.Background.Image)
	setEntry(&st.bgColor, c.Background.Color)
	setEntry(&st.clock, c.Bar.Clock)
	setEntry(&st.terminal, c.Launcher.Terminal)
	setEntry(&st.lock, c.Control.Lock)
	setEntry(&st.screenshot, c.Control.Screenshot)
	st.bgFit.SetSelected(uint32(max(0, slices.Index(fitChoices, c.Background.Fit))))
	st.ws.SetSelected(uint32(max(0, slices.Index(wsChoices, c.Bar.Workspaces))))
	st.launchMode.SetSelected(uint32(max(0, slices.Index(modeChoices, c.Launcher.Mode))))
	st.icons.SetSelected(uint32(max(0, slices.Index(config.IconSets, c.Style.Icons))))
	s.showInput()
	s.showNotifySettings()
	s.showDockSettings()
	s.showShortcuts()
	st.clockPreview.SetText(config.Strftime(c.Bar.Clock, time.Now()))
	for _, e := range st.entries {
		e.RemoveCssClass("invalid")
	}
}

// setEntry changes an entry's text only if it differs, so the caret stays
// where it is while the user types.
func setEntry(e *gtk.Entry, text string) {
	if e.GetText() != text {
		e.SetText(text)
	}
}

// settingsEntryChanged applies a text setting. Text that is not valid yet
// (a half-typed colour, a path to nothing) is marked and not applied.
func (s *shellUI) settingsEntryChanged(e *gtk.Entry) {
	st := &s.settings
	text := e.GetText()
	c := s.cfg
	valid := true
	switch e.GetName() {
	case "clock":
		valid = strings.TrimSpace(text) != ""
		c.Bar.Clock = text
		st.clockPreview.SetText(config.Strftime(text, time.Now()))
	case "terminal":
		c.Launcher.Terminal = text
	case "lock":
		c.Control.Lock = text
	case "screenshot":
		c.Control.Screenshot = text
	case "bg_color":
		valid = config.ValidColor(text)
		c.Background.Color = text
	case "bg_image":
		c.Background.Image = text
		if p := c.ImagePath(); p != "" {
			info, err := os.Stat(p)
			valid = err == nil && !info.IsDir()
		}
	}
	toggle(&e.Widget, "invalid", !valid)
	if valid {
		s.changeSettings(c)
	}
}

// changeSettings applies c and schedules saving it.
func (s *shellUI) changeSettings(c config.Config) {
	s.applyConfig(c)
	st := &s.settings
	st.dirty = true
	st.saveGen++
	glib.TimeoutAdd(400, &s.onSettingsSave, st.saveGen)
}

func (s *shellUI) saveSettings() {
	st := &s.settings
	st.dirty = false
	if err := config.Save(s.opts.ConfigPath, s.cfg); err != nil {
		log.Printf("settings: %v", err)
		s.setStatus("not saved: "+err.Error(), true)
		return
	}
	s.setStatus("", false)
	s.applyHyprland()
}

func (s *shellUI) setStatus(text string, failed bool) {
	st := &s.settings
	if st.win == nil {
		return
	}
	st.status.SetText(text)
	st.status.SetVisible(text != "")
	toggle(&st.status.Widget, "error", failed)
}

// chooseImage asks for a wallpaper with the portal's (or GTK's) file
// chooser; onImageChosen takes the answer.
func (s *shellUI) chooseImage() {
	st := &s.settings
	if st.dialog != nil {
		return
	}
	d := gtk.NewFileDialog()
	d.SetTitle("Choose a wallpaper")
	filter := gtk.NewFileFilter()
	filter.SetName("Images")
	filter.AddMimeType("image/*")
	d.SetDefaultFilter(filter)
	filter.Unref()
	if p := s.cfg.ImagePath(); p != "" {
		f := gio.FileNewForPath(p)
		d.SetInitialFile(f)
		unref(f)
	} else if home, err := os.UserHomeDir(); err == nil {
		f := gio.FileNewForPath(filepath.Join(home, "Pictures"))
		d.SetInitialFolder(f)
		unref(f)
	}
	st.dialog = d
	d.Open(&st.win.Window, nil, &s.onImageChosen, 0)
}

// unref drops a reference to an object puregotk gives no Unref method,
// such as a GFile.
func unref(o gobject.Ptr) { (&gobject.Object{Ptr: o.GoPointer()}).Unref() }

// shortenHome writes a path under the home directory as "~/...".
func shortenHome(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home+"/") {
		return "~/" + strings.TrimPrefix(p, home+"/")
	}
	return p
}

// showNotifySettings sets the notification controls, and says whether inua
// shows notifications now.
func (s *shellUI) showNotifySettings() {
	st := &s.settings
	c := s.cfg.Notify
	st.notifyOn.SetActive(c.Enable)
	st.critFocus.SetActive(c.CriticalInFocus)
	st.banner.SetSelected(uint32(max(0, slices.Index(config.BannerChoices, c.Banner))))
	switch {
	case !c.Enable:
		st.notifyState.SetText("Off: another notification daemon can show them")
	case s.notesNative():
		st.notifyState.SetText("Banners, then the notification center (the bell in the bar, or Super+N)")
	default:
		st.notifyState.SetText("Waiting: another notification daemon is running")
	}
}

// showDockSettings sets the bar and dock controls and lists the pinned
// apps, each with buttons to move or remove it.
func (s *shellUI) showDockSettings() {
	st := &s.settings
	c := s.cfg
	st.barOn.SetActive(c.Bar.Enable)
	st.dockOn.SetActive(c.Dock.Enable)
	st.dockHide.SetActive(c.Dock.Autohide)
	st.dockDrawer.SetActive(c.Dock.Drawer)
	st.dockSize.SetSelected(uint32(max(0, slices.Index(config.DockSizes, c.Dock.Size))))
	st.dockPosition.SetSelected(uint32(max(0, slices.Index(config.DockPositions, c.Dock.Position))))
	gtkx.Clear(&st.dockPinned)
	if len(c.Dock.Pinned) == 0 {
		l := gtk.NewLabel("Nothing pinned")
		l.AddCssClass("sub")
		l.SetXalign(0)
		l.AddCssClass("row")
		gtkx.Append(&st.dockPinned, &l.Widget)
	}
	for i, id := range c.Dock.Pinned {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		row.AddCssClass("row")
		img := gtk.NewImage()
		name := id
		if e := s.dockState.apps.ByID(id); e != nil {
			setIcon(img, e.Icon)
			name = e.Name
		} else {
			img.SetFromIconName("application-x-executable-symbolic")
			name = id + " (not installed)"
		}
		img.SetPixelSize(24)
		gtkx.Append(row, &img.Widget)
		l := gtk.NewLabel(name)
		l.AddCssClass("title")
		l.SetXalign(0)
		l.SetHexpand(true)
		gtkx.Append(row, &l.Widget)
		for _, b := range []struct {
			action, icon, tip string
			ok                bool
		}{
			{"up", "pan-up-symbolic", "Move earlier", i > 0},
			{"down", "pan-down-symbolic", "Move later", i < len(c.Dock.Pinned)-1},
			{"remove", "window-close-symbolic", "Remove from the dock", true},
		} {
			btn := gtk.NewButtonFromIconName(b.icon)
			btn.AddCssClass("plain")
			btn.SetName("pin-" + b.action + ":" + id)
			btn.SetTooltipText(b.tip)
			btn.SetSensitive(b.ok)
			btn.SetValign(gtk.AlignCenterValue)
			btn.ConnectClicked(&s.onSettingsButton)
			gtkx.Append(row, &btn.Widget)
		}
		gtkx.Append(&st.dockPinned, &row.Widget)
	}
}

// movePinned moves a pinned app left ("up"), right ("down"), or removes it.
func movePinned(pinned []string, id, action string) []string {
	out := slices.Clone(pinned)
	i := slices.Index(out, id)
	switch {
	case i < 0:
	case action == "remove":
		out = slices.Delete(out, i, i+1)
	case action == "up" && i > 0:
		out[i-1], out[i] = out[i], out[i-1]
	case action == "down" && i < len(out)-1:
		out[i], out[i+1] = out[i+1], out[i]
	}
	return out
}
