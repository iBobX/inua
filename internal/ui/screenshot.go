package ui

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/notify"
	"github.com/lyricat/inua/internal/rundir"
	"github.com/lyricat/inua/internal/shell"
)

// shotEntry marks the shell's own screenshot notifications, in their
// desktop-entry field, so clicking one works after a restart too.
const shotEntry = "inua-screenshot"

// shooter is the screenshot tool. Opening it captures every monitor first,
// then covers each with a layer surface showing that capture, so nothing
// moves while you choose; the choice is cut from the capture.
type shooter struct {
	open, busy bool
	mode       string // "area", "window", "screen", or "color" to pick a colour
	record     bool   // the choice is recorded, not taken as a picture
	surfaces   []*shotSurface
	refs       gtkx.Scope
	filling    bool        // setting the mode buttons from Go
	drawer     *shotDrawer // shown instead of the still once something is taken
	reopen     string      // the mode to open in once the drawer is off screen

	dragging *shotSurface // the area being dragged out is on this one
	x0, y0   float64
	sel      shell.Rect
}

// shotSurface is the tool on one monitor.
type shotSurface struct {
	name    string
	file    string  // its capture, a PNG
	w, h    float64 // the monitor in logical pixels
	scale   float64 // capture pixels per logical pixel
	bounds  image.Rectangle
	windows []shell.ShotWindow // top first
	img     image.Image        // the capture decoded, for picking a colour
	monX    float64            // the monitor's place, for recording an area
	monY    float64

	win                      gtk.ApplicationWindow
	marks                    gtk.Fixed
	dims                     [4]gtk.Box // above, below, left of and right of the choice
	frame                    gtk.Box
	size                     gtk.Label
	toolbar                  gtk.Box
	hint                     gtk.Label
	modes                    map[string]*gtk.ToggleButton
	record                   gtk.ToggleButton
	drawerBtn                gtk.Button
	recordSep                gtk.Separator
	modeSep                  gtk.Separator
	loupe                    gtk.Box
	loupePixels, loupeSwatch gtk.Picture
	loupeHex                 gtk.Label
	inside                   bool
	px, py                   float64 // the pointer, while inside
	hover                    int     // the window under the pointer, -1 for none
	focusedMon               bool
}

func (s *shellUI) bindShotHandlers() {
	sh := &s.shot
	s.onShotDragBegin = func(g gtk.GestureDrag, x, y float64) {
		sf := s.shotSurfaceOf(g.GetWidget())
		if sf == nil || sh.mode != "area" {
			return
		}
		sh.dragging, sh.x0, sh.y0, sh.sel = sf, x, y, shell.Rect{}
	}
	s.onShotDragUpdate = func(g gtk.GestureDrag, dx, dy float64) {
		sf := sh.dragging
		if sf == nil {
			return
		}
		sh.sel = shell.SpanRect(sh.x0, sh.y0, sh.x0+dx, sh.y0+dy, sf.w, sf.h)
		s.drawShot(sf)
	}
	s.onShotDragEnd = func(g gtk.GestureDrag, dx, dy float64) {
		sf := s.shotSurfaceOf(g.GetWidget())
		if sf == nil {
			return
		}
		if sh.mode == "color" {
			if math.Hypot(dx, dy) < 6 {
				s.pickColor(sf)
			}
			return
		}
		if sh.mode == "area" {
			sel := sh.sel
			sh.dragging, sh.sel = nil, shell.Rect{}
			if sf == nil || sel.Empty() {
				s.drawShot(sf)
				return
			}
			s.takeShot(sf, sel)
			return
		}
		if math.Hypot(dx, dy) < 6 { // a click, not a drag
			s.takeHovered(sf)
		}
	}
	s.onShotMotion = func(c gtk.EventControllerMotion, x, y float64) {
		sf := s.shotSurfaceOf(c.GetWidget())
		if sf == nil {
			return
		}
		sf.inside, sf.px, sf.py = true, x, y
		if sh.mode == "color" {
			s.moveLoupe(sf, x, y)
			return
		}
		hover := shell.WindowAt(sf.windows, x, y)
		if sh.mode == "window" && hover == sf.hover {
			return
		}
		sf.hover = hover
		if sh.mode != "area" {
			s.drawShot(sf)
		}
	}
	s.onShotLeave = func(c gtk.EventControllerMotion) {
		if sf := s.shotSurfaceOf(c.GetWidget()); sf != nil {
			sf.inside, sf.hover = false, -1
			sf.loupe.SetVisible(false)
			s.drawShot(sf)
		}
	}
	s.onShotKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		switch keyval {
		case keyEscape:
			s.closeShot("")
		case keySpace:
			if sh.mode == "color" {
				return true
			}
			s.setShotMode(map[string]string{"area": "window", "window": "area", "screen": "area"}[sh.mode])
		case keyReturn, keyKPEnter:
			for _, sf := range sh.surfaces {
				if sf.inside && sh.mode == "color" {
					s.pickColor(sf)
					break
				}
				if sf.inside {
					s.takeHovered(sf)
					break
				}
			}
		default:
			return false
		}
		return true
	}
	s.onShotMode = func(b gtk.ToggleButton) {
		if !sh.filling && b.GetActive() {
			s.setShotMode(b.GetName())
		}
	}
	s.onShotCancel = func(gtk.Button) { s.closeShot("") }
	s.onShotRecord = func(b gtk.ToggleButton) {
		if !sh.filling {
			sh.record = b.GetActive()
			s.setShotMode(sh.mode)
		}
	}
}

// screenshotRequest answers `inua call screenshot area|window|screen`:
// the tool, or the focused screen or window at once; `color` picks a
// colour, `record` opens the tool to record, `stop` ends a recording.
func (s *shellUI) screenshotRequest(fn string) bool {
	switch fn {
	case "area", "open":
		s.shot.record = s.shot.open && s.shot.record
		s.openShot("area")
	case "color":
		s.shot.record = false
		s.openShot("color")
	case "record":
		if s.recordingNow() {
			s.stopScreenRecording()
		} else {
			s.shot.record = true
			s.openShot("area")
		}
	case "stop":
		s.stopScreenRecording()
	case "pick-window":
		s.openShot("window")
	case "pick-screen":
		s.openShot("screen")
	case "screen", "window":
		s.shootNow(fn)
	case "close":
		s.closeShot("")
	default:
		return false
	}
	return true
}

// openShot captures the monitors, then shows the tool over them.
func (s *shellUI) openShot(mode string) {
	sh := &s.shot
	if sh.open {
		s.setShotMode(mode)
		return
	}
	if sh.busy {
		return
	}
	if sh.drawer != nil { // off screen before the capture
		s.closeShotDrawer()
		sh.reopen = mode
		glib.TimeoutAdd(150, &s.onShotReopen, 0)
		return
	}
	sh.busy = true
	mons := s.snap.Monitors
	focused := s.snap.FocusedMonitor
	decode := mode == "color"
	go func() {
		dir, err := shotTempDir()
		var clients []byte
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			clients, err = s.client.Request(ctx, "j/clients")
			cancel()
		}
		var surfaces []*shotSurface
		for _, m := range mons {
			if err != nil {
				break
			}
			sf := &shotSurface{name: m.Name, file: filepath.Join(dir, "frozen-"+m.Name+".png"), hover: -1, focusedMon: m.Name == focused}
			if out, e := exec.Command("grim", "-o", m.Name, sf.file).CombinedOutput(); e != nil {
				err = fmt.Errorf("grim: %v %s", e, strings.TrimSpace(string(out)))
				break
			}
			sf.bounds, err = pngBounds(sf.file)
			if err != nil {
				break
			}
			sf.monX, sf.monY = float64(m.X), float64(m.Y)
			if decode {
				if sf.img, err = readPNG(sf.file); err != nil {
					break
				}
			}
			sf.w, sf.h = logicalSize(m)
			sf.scale = float64(sf.bounds.Dx()) / sf.w
			sf.windows, err = shell.ShotWindows(clients, float64(m.X), float64(m.Y), shownWorkspaces(m))
			surfaces = append(surfaces, sf)
		}
		gtkx.Post(func() {
			sh.busy = false
			if err != nil {
				log.Printf("screenshot: %v", err)
				removeShotFiles(surfaces, "")
				return
			}
			s.showShot(surfaces, mode)
		})
	}()
}

// logicalSize is a monitor's size in logical pixels, turned if the
// monitor is.
func logicalSize(m hypr.Monitor) (w, h float64) {
	w, h = math.Round(float64(m.Width)/m.Scale), math.Round(float64(m.Height)/m.Scale)
	if t, _ := m.Raw["transform"].(float64); int(t)%2 == 1 {
		w, h = h, w
	}
	return w, h
}

// shownWorkspaces are the workspaces a monitor shows: its active one, and
// a special workspace open over it.
func shownWorkspaces(m hypr.Monitor) []int {
	ws := []int{m.ActiveWorkspaceID}
	if sp, ok := m.Raw["specialWorkspace"].(map[string]any); ok {
		if id, _ := sp["id"].(float64); id != 0 {
			ws = append(ws, int(id))
		}
	}
	return ws
}

func (s *shellUI) showShot(surfaces []*shotSurface, mode string) {
	sh := &s.shot
	for _, sf := range surfaces {
		mon := s.monitorByName(sf.name)
		if mon == nil {
			continue
		}
		s.buildShotSurface(sf, mon)
		sh.surfaces = append(sh.surfaces, sf)
	}
	if len(sh.surfaces) == 0 {
		removeShotFiles(surfaces, "")
		return
	}
	sh.open = true
	s.setShotMode(mode)
	for _, sf := range sh.surfaces {
		sf.win.Present()
	}
}

func (s *shellUI) monitorByName(name string) *gdk.Monitor {
	for _, b := range s.bars {
		if b.connector() == name {
			return b.mon
		}
	}
	return nil
}

func (s *shellUI) buildShotSurface(sf *shotSurface, mon *gdk.Monitor) {
	sh := &s.shot
	ui := s.load("screenshot.ui", &sh.refs)
	var frozen gtk.Picture
	var cancel gtk.Button
	ui.Get("screenshot", &sf.win)
	ui.Get("frozen", &frozen)
	ui.Get("marks", &sf.marks)
	ui.Get("dim_top", &sf.dims[0])
	ui.Get("dim_bottom", &sf.dims[1])
	ui.Get("dim_left", &sf.dims[2])
	ui.Get("dim_right", &sf.dims[3])
	ui.Get("frame", &sf.frame)
	ui.Get("size", &sf.size)
	ui.Get("toolbar", &sf.toolbar)
	ui.Get("hint", &sf.hint)
	ui.Get("cancel", &cancel)
	ui.Get("record", &sf.record)
	ui.Get("drawer", &sf.drawerBtn)
	ui.Get("record_sep", &sf.recordSep)
	ui.Get("mode_sep", &sf.modeSep)
	ui.Get("loupe", &sf.loupe)
	ui.Get("loupe_pixels", &sf.loupePixels)
	ui.Get("loupe_swatch", &sf.loupeSwatch)
	ui.Get("loupe_hex", &sf.loupeHex)
	sf.modes = map[string]*gtk.ToggleButton{}
	for _, m := range []string{"area", "window", "screen"} {
		b := &gtk.ToggleButton{}
		ui.Get("mode_"+m, b)
		b.ConnectToggled(&s.onShotMode)
		sf.modes[m] = b
	}
	ui.Close()

	frozen.SetFilename(sf.file)
	sf.toolbar.SetVisible(sf.focusedMon)
	cancel.ConnectClicked(&s.onShotCancel)
	sf.record.ConnectToggled(&s.onShotRecord)
	sf.drawerBtn.ConnectClicked(&s.onShotDrawer)
	sf.marks.SetCursorFromName("crosshair")

	drag := gtk.NewGestureDrag()
	drag.SetButton(1)
	drag.ConnectDragBegin(&s.onShotDragBegin)
	drag.ConnectDragUpdate(&s.onShotDragUpdate)
	drag.ConnectDragEnd(&s.onShotDragEnd)
	sf.marks.AddController(&drag.EventController)
	motion := gtk.NewEventControllerMotion()
	motion.ConnectMotion(&s.onShotMotion)
	motion.ConnectLeave(&s.onShotLeave)
	sf.marks.AddController(&motion.EventController)
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue)
	keys.ConnectKeyPressed(&s.onShotKey)
	sf.win.AddController(&keys.EventController)

	sf.win.SetApplication(s.app)
	w := sf.win.GoPointer()
	if s.layer {
		layershell.InitForWindow(w)
		layershell.SetNamespace(w, "inua-screenshot")
		layershell.SetLayer(w, layershell.LayerOverlay)
		if sf.focusedMon {
			layershell.SetKeyboardMode(w, layershell.KeyboardExclusive)
		} else {
			layershell.SetKeyboardMode(w, layershell.KeyboardOnDemand)
		}
		for _, e := range []layershell.Edge{layershell.EdgeTop, layershell.EdgeBottom, layershell.EdgeLeft, layershell.EdgeRight} {
			layershell.SetAnchor(w, e, true)
		}
		layershell.SetExclusiveZone(w, -1)
		layershell.SetMonitor(w, mon.GoPointer())
	} else {
		sf.win.SetTitle(i18n.T("inua screenshot"))
		sf.win.SetDefaultSize(int32(sf.w), int32(sf.h))
	}
}

func (s *shellUI) shotSurfaceOf(w *gtk.Widget) *shotSurface {
	if w == nil {
		return nil
	}
	for _, sf := range s.shot.surfaces {
		if sf.marks.GoPointer() == w.GoPointer() {
			return sf
		}
	}
	return nil
}

var shotHints = map[string]string{
	"area":   i18n.M("Drag to take an area"),
	"window": i18n.M("Click a window"),
	"screen": i18n.M("Click a screen"),
	"color":  i18n.M("Click to copy a colour"),
}

var recordHints = map[string]string{
	"area":   i18n.M("Drag to record an area"),
	"window": i18n.M("Click a window to record"),
	"screen": i18n.M("Click a screen to record"),
}

func (s *shellUI) setShotMode(mode string) {
	sh := &s.shot
	if _, ok := shotHints[mode]; !ok {
		return
	}
	sh.mode = mode
	sh.dragging, sh.sel = nil, shell.Rect{}
	sh.filling = true
	color := mode == "color"
	for _, sf := range sh.surfaces {
		for _, b := range sf.modes {
			b.SetVisible(!color)
		}
		sf.record.SetVisible(!color)
		sf.drawerBtn.SetVisible(!color)
		sf.recordSep.SetVisible(!color)
		sf.modeSep.SetVisible(!color)
		sf.record.SetActive(sh.record)
		if b := sf.modes[mode]; b != nil {
			b.SetActive(true)
		}
		hint := shotHints[mode]
		if sh.record && !color {
			hint = recordHints[mode]
		}
		sf.hint.SetText(i18n.T(hint))
		toggle(&sf.toolbar.Widget, "recording", sh.record && !color)
		sf.hover = shell.WindowAt(sf.windows, sf.px, sf.py)
		s.drawShot(sf)
	}
	sh.filling = false
}

// shotChoice is what a click on the surface would take now, if anything.
func (s *shellUI) shotChoice(sf *shotSurface) (shell.Rect, bool) {
	sh := &s.shot
	switch sh.mode {
	case "area":
		if sh.dragging == sf && !sh.sel.Empty() {
			return sh.sel, true
		}
	case "window":
		if sf.inside && sf.hover >= 0 {
			r := sf.windows[sf.hover].Rect
			return shell.SpanRect(r.X, r.Y, r.X+r.W, r.Y+r.H, sf.w, sf.h), true
		}
	case "screen":
		if sf.inside {
			return shell.Rect{W: sf.w, H: sf.h}, true
		}
	}
	return shell.Rect{}, false
}

// drawShot dims everything but the choice, and frames it.
func (s *shellUI) drawShot(sf *shotSurface) {
	r, ok := s.shotChoice(sf)
	place := func(b *gtk.Widget, x0, y0, x1, y1 int) {
		b.SetVisible(x1 > x0 && y1 > y0)
		sf.marks.Move(b, float64(x0), float64(y0))
		b.SetSizeRequest(int32(max(0, x1-x0)), int32(max(0, y1-y0)))
	}
	W, H := int(sf.w), int(sf.h)
	if s.shot.mode == "color" { // the still as it is, for true colours
		for _, d := range sf.dims {
			d.SetVisible(false)
		}
		sf.frame.SetVisible(false)
		sf.size.SetVisible(false)
		return
	}
	if !ok {
		place(&sf.dims[0].Widget, 0, 0, W, H)
		for _, d := range sf.dims[1:] {
			d.SetVisible(false)
		}
		sf.frame.SetVisible(false)
		sf.size.SetVisible(false)
		return
	}
	x0, y0 := int(math.Floor(r.X)), int(math.Floor(r.Y))
	x1, y1 := int(math.Ceil(r.X+r.W)), int(math.Ceil(r.Y+r.H))
	place(&sf.dims[0].Widget, 0, 0, W, y0)
	place(&sf.dims[1].Widget, 0, y1, W, H)
	place(&sf.dims[2].Widget, 0, y0, x0, y1)
	place(&sf.dims[3].Widget, x1, y0, W, y1)
	place(&sf.frame.Widget, x0, y0, x1, y1)
	px := r.Pixels(sf.scale, sf.bounds)
	sf.size.SetText(fmt.Sprintf("%d × %d", px.Dx(), px.Dy()))
	sf.size.SetVisible(true)
	ly := y1 + 8 // below the choice, or inside it at the bottom
	if ly+24 > H {
		ly = y1 - 32
	}
	sf.marks.Move(&sf.size.Widget, float64(x0), float64(max(0, ly)))
}

// takeHovered takes what a click on the surface would take.
func (s *shellUI) takeHovered(sf *shotSurface) {
	if r, ok := s.shotChoice(sf); ok {
		s.takeShot(sf, r)
	}
}

// takeShot puts the still away and cuts r out of its capture, then shows
// the drawer with it first; or with Record on, starts recording r.
func (s *shellUI) takeShot(sf *shotSurface, r shell.Rect) {
	sh := &s.shot
	if sh.record {
		args := recordArgs(sf, r)
		s.closeShot("")
		glib.TimeoutAdd(250, &s.onRecordStart, 0) // the tool off screen first
		s.recording.pending = args
		return
	}
	px := r.Pixels(sf.scale, sf.bounds)
	// The capture stays the tool's; cut from a link to it, which closing
	// the tool meanwhile does not take away.
	file := sf.file + "." + strconv.FormatInt(time.Now().UnixNano(), 36) + ".png"
	if err := os.Link(sf.file, file); err != nil {
		log.Printf("screenshot: %v", err)
		return
	}
	s.closeCapture("")
	s.openShotDrawer() // at once; the shot joins it when written
	go func() {
		defer os.Remove(file)
		img, err := readPNG(file)
		if err == nil {
			sub, ok := img.(interface {
				SubImage(image.Rectangle) image.Image
			})
			if !ok {
				err = fmt.Errorf("cannot crop a %T", img)
			} else {
				err = s.deliverShot(func(path string) error { return writePNG(path, sub.SubImage(px)) })
			}
		}
		if err != nil {
			log.Printf("screenshot: %v", err)
			return
		}
		gtkx.Post(s.shotTaken)
	}()
}

// shotTaken shows a new screenshot or recording in the drawer, if it is
// open.
func (s *shellUI) shotTaken() {
	if s.shot.drawer != nil {
		s.fillShotDrawer()
	}
}

// closeShot puts the tool away, the still and the drawer, and deletes its
// captures but keep.
func (s *shellUI) closeShot(keep string) {
	s.closeShotDrawer()
	s.closeCapture(keep)
}

// closeCapture puts the still away and deletes its captures but keep.
func (s *shellUI) closeCapture(keep string) {
	sh := &s.shot
	if !sh.open {
		return
	}
	for _, sf := range sh.surfaces {
		sf.win.Destroy()
	}
	sh.refs.Release()
	removeShotFiles(sh.surfaces, keep)
	sh.open, sh.surfaces, sh.dragging, sh.record = false, nil, nil, false
}

// recordArgs are wf-recorder's arguments for recording r on a surface's
// monitor: the monitor itself, or the area in global logical pixels, its
// size a multiple of 4 so that scaled to pixels it stays even, as the
// encoder needs.
func recordArgs(sf *shotSurface, r shell.Rect) []string {
	if r.X <= 0 && r.Y <= 0 && r.W >= sf.w && r.H >= sf.h {
		return []string{"-o", sf.name}
	}
	w, h := int(r.W)/4*4, int(r.H)/4*4
	return []string{"-g", fmt.Sprintf("%d,%d %dx%d", int(sf.monX+r.X), int(sf.monY+r.Y), max(w, 4), max(h, 4))}
}

func removeShotFiles(surfaces []*shotSurface, keep string) {
	for _, sf := range surfaces {
		if sf.file != keep {
			os.Remove(sf.file)
		}
	}
}

// shootNow takes the focused screen or window without the tool.
func (s *shellUI) shootNow(what string) {
	focused := s.snap.FocusedMonitor
	go func() {
		args := []string{"-o", focused}
		if what == "window" {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			reply, err := s.client.Request(ctx, "j/activewindow")
			cancel()
			g, gerr := activeGeometry(reply)
			if err != nil || gerr != nil {
				log.Printf("screenshot: no focused window: %v %v", err, gerr)
				return
			}
			args = []string{"-g", g}
		}
		err := s.deliverShot(func(path string) error {
			if out, err := exec.Command("grim", append(args, path)...).CombinedOutput(); err != nil {
				return fmt.Errorf("grim: %v %s", err, strings.TrimSpace(string(out)))
			}
			return nil
		})
		if err != nil {
			log.Printf("screenshot: %v", err)
		}
	}()
}

// activeGeometry is the focused window as grim's -g wants it: "X,Y WxH".
func activeGeometry(reply []byte) (string, error) {
	ws, err := shell.ShotWindows(append(append([]byte("["), reply...), ']'), 0, 0, nil)
	if err == nil && len(ws) == 0 {
		err = fmt.Errorf("none")
	}
	if err != nil {
		return "", err
	}
	r := ws[0].Rect
	return fmt.Sprintf("%d,%d %dx%d", int(r.X), int(r.Y), int(r.W), int(r.H)), nil
}

// deliverShot has write make the screenshot's file, in the screenshot
// folder or (not saving) a temporary one, puts it on the clipboard, and
// says so with its thumbnail.
func (s *shellUI) deliverShot(write func(path string) error) error {
	cfg := s.cfg
	folder := cfg.ShotFolder()
	saved := folder != ""
	if !saved {
		var err error
		if folder, err = shotTempDir(); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return err
	}
	path := filepath.Join(folder, shell.ShotName(time.Now()))
	if err := write(path); err != nil {
		return err
	}
	copied := false
	if cfg.Screenshot.Copy {
		if err := copyImage(path); err != nil {
			log.Printf("screenshot: copy: %v", err)
		} else {
			copied = true
		}
	}
	gtkx.Post(func() { s.shotNote(path, saved, copied) })
	return nil
}

// copyText puts text on the clipboard.
func copyText(text string) error {
	return wlCopy(nil, "--", text)
}

func copyImage(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return wlCopy(f, "--type", "image/png")
}

// wlCopy runs wl-copy. Its output goes to a file, not a pipe: wl-copy
// leaves a process behind to serve the clipboard, and a pipe it holds
// would keep the wait from returning until the clipboard changes.
func wlCopy(stdin *os.File, args ...string) error {
	errs, err := os.CreateTemp("", "inua-wl-copy-*")
	if err != nil {
		return err
	}
	defer os.Remove(errs.Name())
	defer errs.Close()
	cmd := exec.Command("wl-copy", args...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	cmd.Stdout, cmd.Stderr = errs, errs
	if err := cmd.Run(); err != nil {
		out, _ := os.ReadFile(errs.Name())
		return fmt.Errorf("wl-copy: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// shotNote is the notification a screenshot leaves: its thumbnail; a
// click opens it, and "Show in Folder" shows where it is.
func (s *shellUI) shotNote(path string, saved, copied bool) {
	summary := map[[2]bool]string{
		{true, true}:   i18n.T("Screenshot saved and copied"),
		{true, false}:  i18n.T("Screenshot saved"),
		{false, true}:  i18n.T("Screenshot copied"),
		{false, false}: i18n.T("Screenshot taken"),
	}[[2]bool{saved, copied}]
	n := notify.Notification{
		App: i18n.T("Screenshot"), Entry: shotEntry, Icon: "applets-screenshooter-symbolic",
		Summary: summary, ImgPath: path, Default: true, Time: time.Now(),
		Transient: !saved, // nothing to come back to
	}
	if saved {
		n.Body = notify.Markup(filepath.Base(path))
		n.Actions = []notify.Action{{Key: "folder", Label: i18n.T("Show in Folder")}}
	}
	if s.notes.srv == nil { // another notification daemon shows them
		go run(fmt.Sprintf("notify-send -a Screenshot -i %s %s", shellQuote(path), shellQuote(summary)))
		return
	}
	n.ID = s.notes.srv.NewID()
	s.postNote(n)
}

// shotNoteAction is a click on a screenshot's notification.
func (s *shellUI) shotNoteAction(n notify.Notification, key string) {
	path := n.ImgPath
	go func() {
		if key == "folder" {
			uri := "file://" + path
			err := exec.Command("gdbus", "call", "--session", "--dest", "org.freedesktop.FileManager1",
				"--object-path", "/org/freedesktop/FileManager1",
				"--method", "org.freedesktop.FileManager1.ShowItems", "['"+strings.ReplaceAll(uri, "'", "\\'")+"']", "").Run()
			if err == nil {
				return
			}
			path = filepath.Dir(path)
		}
		if err := exec.Command("xdg-open", path).Run(); err != nil {
			log.Printf("screenshot: open %s: %v", path, err)
		}
	}()
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// shotTempDir is a private directory for captures, in the runtime
// directory.
func shotTempDir() (string, error) { return rundir.Sub("shots") }

func pngBounds(path string) (image.Rectangle, error) {
	f, err := os.Open(path)
	if err != nil {
		return image.Rectangle{}, err
	}
	defer f.Close()
	c, err := png.DecodeConfig(f)
	return image.Rect(0, 0, c.Width, c.Height), err
}

func readPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
