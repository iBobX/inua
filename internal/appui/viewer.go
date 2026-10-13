package appui

import (
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gdkpixbuf"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gsk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/viewer"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// viewerApp is the Viewer: one window per start, showing an image
// and stepping through its folder.
type viewerApp struct {
	c   *Ctx
	win *gtk.ApplicationWindow

	toolbar                                    gtk.Box
	name, meta, status, trashLabel, emptyLabel gtk.Label
	zoomLevel, prev, next, fullscreenB         gtk.Button
	scroller                                   gtk.ScrolledWindow
	canvas                                     gtk.Fixed
	picture                                    gtk.Picture
	empty, details                             gtk.Box
	detailsReveal                              gtk.Revealer
	detailsToggle                              gtk.ToggleButton
	hadj, vadj                                 *gtk.Adjustment
	dialog                                     *gtk.FileDialog

	files []string
	index int

	// The images loaded: the one shown, and those beside it (viewerload.go).
	cache      map[string]*shot
	work       chan struct{} // a place for each load running
	cur        *shot
	prefetched bool // the images beside the shown one are loading

	// What is drawn.
	gen        uintptr // bumps on every image shown: an old animation's frames stop
	iter       *gdkpixbuf.PixbufAnimationIter
	texW, texH int // the texture drawn, which may be a smaller preview
	w, h       int // the image's pixels as shown (oriented, turned)
	turns      int // quarter turns clockwise asked for

	fit          bool    // follow the window, never above actual size
	zoom         float64 // screen pixels per image pixel
	viewW, viewH int     // the stage's size when last fitted
	px, py       float64 // the pointer over the stage
	dragX, dragY float64 // the scroll position when a drag began
	pinchZoom    float64

	trashArmed bool
	trashGen   uintptr
	statusGen  uintptr

	onButton                  func(gtk.Button)
	onDetails                 func(gtk.ToggleButton)
	onKey                     func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onScroll                  func(gtk.EventControllerScroll, float64, float64) bool
	onMotion                  func(gtk.EventControllerMotion, float64, float64)
	onDragBegin, onDragUpdate func(gtk.GestureDrag, float64, float64)
	onPinchBegin              func(gtk.Gesture, uintptr)
	onPinch                   func(gtk.GestureZoom, float64)
	onDouble                  func(gtk.GestureClick, int32, float64, float64)
	onViewChanged             func(gtk.Adjustment)
	onFullscreened            func(uintptr, uintptr, uintptr)
	onFrame, onDisarm, onCalm glib.SourceFunc
	onRefit                   glib.SourceFunc
	refitting                 bool
	onChosen                  gio.AsyncReadyCallback
}

func (a *viewerApp) build(c *Ctx) *gtk.ApplicationWindow {
	a.c = c
	a.win = &gtk.ApplicationWindow{}
	ui := c.Load("viewer.ui")
	ui.Get("viewer", a.win)
	ui.Get("toolbar", &a.toolbar)
	ui.Get("name", &a.name)
	ui.Get("meta", &a.meta)
	ui.Get("status", &a.status)
	ui.Get("zoom_level", &a.zoomLevel)
	ui.Get("scroller", &a.scroller)
	ui.Get("picture", &a.picture)
	ui.Get("canvas", &a.canvas)
	ui.Get("prev", &a.prev)
	ui.Get("next", &a.next)
	ui.Get("empty", &a.empty)
	ui.Get("empty_label", &a.emptyLabel)
	ui.Get("details_reveal", &a.detailsReveal)
	ui.Get("details", &a.details)
	ui.Get("details_toggle", &a.detailsToggle)
	ui.Get("trash_label", &a.trashLabel)
	ui.Get("fullscreen", &a.fullscreenB)
	buttons := map[string]*gtk.Button{}
	for _, n := range []string{"open", "zoom_out", "zoom_in", "rotate", "copy", "wallpaper", "folder", "trash", "open_empty"} {
		b := &gtk.Button{}
		ui.Get(n, b)
		buttons[n] = b
	}
	ui.Close()
	buttons["zoom_level"], buttons["prev"], buttons["next"], buttons["fullscreen"] = &a.zoomLevel, &a.prev, &a.next, &a.fullscreenB

	a.onButton = func(b gtk.Button) { a.action(b.GetName()) }
	for n, b := range buttons {
		b.SetName(n)
		b.ConnectClicked(&a.onButton)
	}
	a.onDetails = func(t gtk.ToggleButton) { a.detailsReveal.SetRevealChild(t.GetActive()) }
	a.detailsToggle.ConnectToggled(&a.onDetails)

	a.hadj, a.vadj = a.scroller.GetHadjustment(), a.scroller.GetVadjustment()
	// The stage's size changes during the layout, which takes no new
	// sizes: refit once it is done.
	a.onViewChanged = func(gtk.Adjustment) {
		if !a.refitting {
			a.refitting = true
			glib.IdleAdd(&a.onRefit, 0)
		}
	}
	a.onRefit = func(uintptr) bool {
		a.refitting = false
		a.refit()
		return false
	}
	a.hadj.ConnectChanged(&a.onViewChanged)
	a.vadj.ConnectChanged(&a.onViewChanged)
	a.bindInput()
	// Full screen, however it came: the picture alone.
	a.onFullscreened = func(uintptr, uintptr, uintptr) {
		full := a.win.IsFullscreen()
		a.toolbar.SetVisible(!full)
		a.detailsReveal.SetVisible(!full)
		a.fullscreenB.SetIconName(map[bool]string{true: "view-restore-symbolic", false: "view-fullscreen-symbolic"}[full])
	}
	gobject.SignalConnect(a.win.GoPointer(), "notify::fullscreened", glib.NewCallback(&a.onFullscreened))

	a.onFrame = func(gen uintptr) bool {
		if gen == a.gen && a.iter != nil {
			a.iter.Advance(nil)
			a.showFrame()
		}
		return false
	}
	a.onDisarm = func(gen uintptr) bool {
		if gen == a.trashGen {
			a.disarmTrash()
		}
		return false
	}
	a.onCalm = func(gen uintptr) bool {
		if gen == a.statusGen {
			a.status.SetText("")
		}
		return false
	}
	a.onChosen = func(_, result, _ uintptr) {
		d := a.dialog
		a.dialog = nil
		if d == nil {
			return
		}
		defer d.Unref()
		file, err := d.OpenFinish(&gio.AsyncResultBase{Ptr: result})
		if err != nil || file == nil {
			return // cancelled
		}
		path := file.GetPath()
		unrefObject(file)
		a.open([]string{path})
	}
	a.fit, a.zoom = true, 1
	return a.win
}

func (a *viewerApp) themeChanged() {}

// open shows the images of the arguments (see viewer.List).
func (a *viewerApp) open(args []string) {
	a.files, a.index = viewer.List(args)
	if len(args) > 0 && len(a.files) == 0 {
		a.showEmpty(i18n.Tf("%s is not an image the viewer knows.", filepath.Base(args[0])))
		return
	}
	a.show(a.index)
}

// bindInput: wheel and pinch zoom, drags pan, a double click switches
// between fitting and actual size, and the keys.
func (a *viewerApp) bindInput() {
	motion := gtk.NewEventControllerMotion()
	a.onMotion = func(_ gtk.EventControllerMotion, x, y float64) { a.px, a.py = x, y }
	motion.ConnectMotion(&a.onMotion)
	a.scroller.AddController(&motion.EventController)

	// A wheel zooms; a touchpad scrolls, or zooms with Ctrl.
	scroll := gtk.NewEventControllerScroll(gtk.EventControllerScrollBothAxesValue)
	scroll.SetPropagationPhase(gtk.PhaseCaptureValue)
	a.onScroll = func(s gtk.EventControllerScroll, dx, dy float64) bool {
		if a.w == 0 {
			return false
		}
		ctrl := s.GetCurrentEventState()&gdk.ControlMaskValue != 0
		if s.GetUnit() == gdk.ScrollUnitSurfaceValue && !ctrl {
			return false // the scrolled window pans
		}
		if dy == 0 {
			return true
		}
		f := math.Pow(1.2, -dy)
		if s.GetUnit() == gdk.ScrollUnitSurfaceValue {
			f = math.Exp(-dy * 0.01)
		}
		a.zoomTo(a.zoom*f, a.px, a.py)
		return true
	}
	scroll.ConnectScroll(&a.onScroll)
	a.scroller.AddController(&scroll.EventController)

	drag := gtk.NewGestureDrag()
	a.onDragBegin = func(gtk.GestureDrag, float64, float64) {
		a.dragX, a.dragY = a.hadj.GetValue(), a.vadj.GetValue()
	}
	a.onDragUpdate = func(_ gtk.GestureDrag, dx, dy float64) {
		a.hadj.SetValue(a.dragX - dx)
		a.vadj.SetValue(a.dragY - dy)
	}
	drag.ConnectDragBegin(&a.onDragBegin)
	drag.ConnectDragUpdate(&a.onDragUpdate)
	a.scroller.AddController(&drag.EventController)

	pinch := gtk.NewGestureZoom()
	a.onPinchBegin = func(gtk.Gesture, uintptr) { a.pinchZoom = a.zoom }
	a.onPinch = func(g gtk.GestureZoom, scale float64) {
		var x, y float64
		if !g.GetBoundingBoxCenter(&x, &y) {
			x, y = a.px, a.py
		}
		a.zoomTo(a.pinchZoom*scale, x, y)
	}
	pinch.ConnectBegin(&a.onPinchBegin)
	pinch.ConnectScaleChanged(&a.onPinch)
	a.scroller.AddController(&pinch.EventController)

	click := gtk.NewGestureClick()
	a.onDouble = func(_ gtk.GestureClick, n int32, x, y float64) {
		if n == 2 {
			a.toggleActual(x, y)
		}
	}
	click.ConnectPressed(&a.onDouble)
	a.scroller.AddController(&click.EventController)

	keys := gtk.NewEventControllerKey()
	a.onKey = func(_ gtk.EventControllerKey, keyval, _ uint32, state gdk.ModifierType) bool {
		return a.key(keyval, state)
	}
	keys.ConnectKeyPressed(&a.onKey)
	a.win.AddController(&keys.EventController)
}

// Keys the viewer takes.
const (
	keyBackSpace = 0xff08
	keyHome      = 0xff50
	keyLeft      = 0xff51
	keyRight     = 0xff53
	keyPageUp    = 0xff55
	keyPageDown  = 0xff56
	keyEnd       = 0xff57
	keyKPAdd     = 0xffab
	keyKPSub     = 0xffad
	keyKP0       = 0xffb0
	keyKP1       = 0xffb1
	keyF11       = 0xffc8
	keyDelete    = 0xffff
	keyEsc       = 0xff1b
)

func (a *viewerApp) key(keyval uint32, state gdk.ModifierType) bool {
	ctrl := state&gdk.ControlMaskValue != 0
	shift := state&gdk.ShiftMaskValue != 0
	cx, cy := a.hadj.GetPageSize()/2, a.vadj.GetPageSize()/2
	switch {
	case ctrl && (keyval == 'o' || keyval == 'O'):
		a.action("open")
	case ctrl && (keyval == 'c' || keyval == 'C'):
		a.action("copy")
	case ctrl:
		return false
	case keyval == keyLeft || keyval == keyPageUp || keyval == keyBackSpace:
		a.action("prev")
	case keyval == keyRight || keyval == keyPageDown || keyval == ' ':
		a.action("next")
	case keyval == keyHome:
		a.show(0)
	case keyval == keyEnd:
		a.show(len(a.files) - 1)
	case keyval == '+' || keyval == '=' || keyval == keyKPAdd:
		a.zoomTo(viewer.StepZoom(a.zoom, true), cx, cy)
	case keyval == '-' || keyval == keyKPSub:
		a.zoomTo(viewer.StepZoom(a.zoom, false), cx, cy)
	case keyval == '0' || keyval == keyKP0:
		a.fitWindow()
	case keyval == '1' || keyval == keyKP1:
		a.zoomTo(1, cx, cy)
	case keyval == 'f' || keyval == 'F' || keyval == keyF11:
		a.action("fullscreen")
	case keyval == keyEsc && a.win.IsFullscreen():
		a.action("fullscreen")
	case keyval == keyEsc: // out of full screen first, then away
		a.win.Close()
	case keyval == 'r' && !shift:
		a.turn(1)
	case keyval == 'R' || (keyval == 'r' && shift) || keyval == 'l' || keyval == 'L':
		a.turn(3)
	case keyval == 'i' || keyval == 'I':
		a.detailsToggle.SetActive(!a.detailsToggle.GetActive())
	case keyval == keyDelete:
		a.action("trash")
	default:
		return false
	}
	return true
}

func (a *viewerApp) action(name string) {
	cx, cy := a.hadj.GetPageSize()/2, a.vadj.GetPageSize()/2
	if name != "trash" {
		a.disarmTrash()
	}
	switch name {
	case "open", "open_empty":
		a.choose()
	case "prev":
		if len(a.files) > 1 {
			a.show((a.index - 1 + len(a.files)) % len(a.files))
		}
	case "next":
		if len(a.files) > 1 {
			a.show((a.index + 1) % len(a.files))
		}
	case "zoom_in":
		a.zoomTo(viewer.StepZoom(a.zoom, true), cx, cy)
	case "zoom_out":
		a.zoomTo(viewer.StepZoom(a.zoom, false), cx, cy)
	case "zoom_level":
		a.toggleActual(cx, cy)
	case "rotate":
		a.turn(1)
	case "fullscreen":
		if a.win.IsFullscreen() {
			a.win.Unfullscreen()
		} else {
			a.win.Fullscreen()
		}
	}
	path := a.current()
	if path == "" {
		return
	}
	switch name {
	case "copy":
		mime := viewer.Mime(path)
		go func() {
			f, err := os.Open(path)
			if err == nil {
				cmd := exec.Command("wl-copy", "--type", mime) // its output to /dev/null: no pipe to wait on
				cmd.Stdin = f
				err = cmd.Run()
				f.Close()
			}
			gtkx.Post(func() {
				if err != nil {
					log.Printf("viewer: copy: %v", err)
					a.flash(i18n.T("Could not copy"))
				} else {
					a.flash(i18n.T("Copied"))
				}
			})
		}()
	case "wallpaper":
		a.setWallpaper(path)
	case "folder":
		go showInFolder(path)
	case "trash":
		if !a.trashArmed {
			a.trashArmed = true
			a.trashLabel.SetVisible(true)
			a.trashGen++
			glib.TimeoutAdd(3000, &a.onDisarm, a.trashGen)
			return
		}
		a.disarmTrash()
		if _, err := gio.FileNewForPath(path).Trash(nil); err != nil {
			log.Printf("viewer: trash: %v", err)
			a.flash(i18n.T("Could not move to Trash"))
			return
		}
		if s := a.cache[path]; s != nil {
			a.drop(s)
		}
		a.files = slices.Delete(a.files, a.index, a.index+1)
		a.flash(i18n.T("Moved to Trash"))
		if len(a.files) == 0 {
			a.clearImage()
			a.showEmpty(i18n.T("No images left here."))
			return
		}
		a.show(min(a.index, len(a.files)-1))
	}
}

func (a *viewerApp) current() string {
	if a.index < 0 || a.index >= len(a.files) {
		return ""
	}
	return a.files[a.index]
}

func (a *viewerApp) disarmTrash() {
	a.trashArmed = false
	a.trashLabel.SetVisible(false)
}

// flash says something in the toolbar for a moment.
func (a *viewerApp) flash(text string) {
	a.status.SetText(text)
	a.statusGen++
	glib.TimeoutAdd(1800, &a.onCalm, a.statusGen)
}

// choose asks for an image to open.
func (a *viewerApp) choose() {
	if a.dialog != nil {
		return
	}
	d := gtk.NewFileDialog()
	d.SetTitle(i18n.T("Open an Image"))
	filter := gtk.NewFileFilter()
	filter.SetName("Images")
	for _, m := range viewer.Exts {
		filter.AddMimeType(m)
	}
	d.SetDefaultFilter(filter)
	filter.Unref()
	if p := a.current(); p != "" {
		f := gio.FileNewForPath(filepath.Dir(p))
		d.SetInitialFolder(f)
		unrefObject(f)
	} else if home, err := os.UserHomeDir(); err == nil {
		f := gio.FileNewForPath(filepath.Join(home, "Pictures"))
		d.SetInitialFolder(f)
		unrefObject(f)
	}
	a.dialog = d
	d.Open(&a.win.Window, nil, &a.onChosen, 0)
}

// show shows the i-th image: at once if it is loaded, else as soon as a
// first look at it is, and then the images beside it load.
func (a *viewerApp) show(i int) {
	if i < 0 || i >= len(a.files) {
		if len(a.files) == 0 {
			a.showEmpty(i18n.T("Open an image to view it."))
		}
		return
	}
	a.index = i
	a.gen++
	if a.iter != nil {
		a.iter.Unref()
		a.iter = nil
	}
	path := a.files[i]
	s := a.shotFor(path)
	a.cur, a.prefetched = s, false
	a.turns, a.fit, a.texW, a.texH, a.w, a.h = 0, true, 0, 0, 0, 0
	a.keep(a.neighbours()...)
	a.name.SetText(filepath.Base(path))
	a.win.SetTitle(filepath.Base(path))
	many := len(a.files) > 1
	a.prev.SetVisible(many)
	a.next.SetVisible(many)
	if s.tex == nil && s.anim == nil {
		noPaintable(&a.picture)
	}
	a.display()
	a.load(s)
}

// neighbours are the shown image and those beside it.
func (a *viewerApp) neighbours() []string {
	n := len(a.files)
	if n == 0 {
		return nil
	}
	return []string{a.files[a.index], a.files[(a.index+1)%n], a.files[(a.index-1+n)%n]}
}

// shotChanged: a load has come further.
func (a *viewerApp) shotChanged(s *shot) {
	if s == a.cur {
		a.display()
	}
}

// display draws the shown image as far as it has loaded.
func (a *viewerApp) display() {
	s := a.cur
	if s == nil {
		return
	}
	switch {
	case s.anim != nil:
		a.empty.SetVisible(false)
		if a.iter == nil {
			a.iter = s.anim.GetIter(nil)
			a.showFrame()
		}
	case s.tex != nil:
		a.empty.SetVisible(false)
		a.picture.SetPaintable(s.tex)
		a.texW, a.texH = s.texW, s.texH
		a.geometry()
	case s.err != nil:
		a.showEmpty(i18n.Tf("Could not open %s.\n%v", filepath.Base(s.path), s.err))
	}
	a.showMeta()
	a.showDetails()
	if (s.tex != nil || s.anim != nil) && !a.prefetched {
		a.prefetched = true
		for _, p := range a.neighbours()[1:] {
			a.load(a.shotFor(p))
		}
	}
}

// clearImage stops drawing an image.
func (a *viewerApp) clearImage() {
	noPaintable(&a.picture)
	if a.iter != nil {
		a.iter.Unref()
		a.iter = nil
	}
	a.cur = nil
	a.texW, a.texH, a.w, a.h = 0, 0, 0, 0
}

func (a *viewerApp) showEmpty(text string) {
	a.emptyLabel.SetText(text)
	a.empty.SetVisible(true)
	if len(a.files) == 0 {
		a.name.SetText(i18n.T("Viewer"))
		a.win.SetTitle(i18n.T("Viewer"))
		a.meta.SetText("")
		a.prev.SetVisible(false)
		a.next.SetVisible(false)
	}
}

// showFrame shows the animation's frame and waits for the next.
func (a *viewerApp) showFrame() {
	pb := a.iter.GetPixbuf()
	if pb == nil {
		return
	}
	tex := gdk.NewTextureForPixbuf(pb)
	a.picture.SetPaintable(tex)
	tex.Unref()
	if w, h := int(pb.GetWidth()), int(pb.GetHeight()); w != a.texW || h != a.texH {
		a.texW, a.texH = w, h
		a.geometry()
	}
	if d := a.iter.GetDelayTime(); d >= 0 {
		glib.TimeoutAdd(uint32(max(d, 20)), &a.onFrame, a.gen)
	}
}

// orientation is how the shown image is drawn: mirrored or not, then
// turned by its EXIF orientation and the turns asked for.
func (a *viewerApp) orientation() (turns int, mirror bool) {
	if a.cur == nil {
		return 0, false
	}
	t, mirror := viewer.Orientation(a.cur.orient)
	return (t + a.turns) % 4, mirror
}

// geometry follows the shown image's size as drawn.
func (a *viewerApp) geometry() {
	s := a.cur
	if s == nil || s.fullW == 0 || a.texW == 0 {
		return
	}
	turns, _ := a.orientation()
	a.w, a.h = s.fullW, s.fullH
	if turns%2 == 1 {
		a.w, a.h = a.h, a.w
	}
	if a.fit {
		a.fitWindow()
	} else {
		a.applyZoom()
	}
}

// turn rotates the image by quarter turns clockwise: only the transform
// changes.
func (a *viewerApp) turn(q int) {
	if a.w == 0 {
		return
	}
	a.turns = (a.turns + q) % 4
	a.geometry()
	a.showMeta()
}

// scale is the screen's pixels per logical pixel.
func (a *viewerApp) scale() float64 {
	if n := a.win.GetNative(); n != nil {
		if s := n.GetSurface(); s != nil {
			if f := s.GetScale(); f > 0 {
				return f
			}
		}
	}
	return 1
}

// fitWindow shows the whole image, at most at its actual size.
func (a *viewerApp) fitWindow() {
	if a.w == 0 {
		return
	}
	s := a.scale()
	vw, vh := a.view()
	a.viewW, a.viewH = int(vw), int(vh)
	a.fit = true
	if vw <= 0 || vh <= 0 {
		// Not laid out yet: show it small (never larger than the screen),
		// and fit it once the stage has a size (refit).
		a.zoom = viewer.FitZoom(float64(a.w), float64(a.h), 800*s, 600*s)
	} else {
		a.zoom = viewer.FitZoom(float64(a.w), float64(a.h), vw*s, vh*s)
	}
	a.applyZoom()
}

// view is the stage's size: the scroll range's page, which is right
// already while the window is being resized.
func (a *viewerApp) view() (float64, float64) {
	return a.hadj.GetPageSize(), a.vadj.GetPageSize()
}

// refit follows the stage's size while the image fits it.
func (a *viewerApp) refit() {
	if !a.fit || a.w == 0 {
		return
	}
	if vw, vh := a.view(); int(vw) != a.viewW || int(vh) != a.viewH {
		a.fitWindow()
	}
}

// shownSize is the image's size on the stage, in logical pixels.
func (a *viewerApp) shownSize(z float64) (float64, float64) {
	s := a.scale()
	return math.Max(1, math.Round(float64(a.w)*z/s)), math.Max(1, math.Round(float64(a.h)*z/s))
}

// applyZoom scales the picture as a whole, which keeps its shape, and
// mirrors and turns it as it should stand. A smaller preview is scaled up
// to the image's size.
func (a *viewerApp) applyZoom() {
	s := a.cur
	if s == nil || a.texW == 0 || s.fullW == 0 {
		return
	}
	turns, mirror := a.orientation()
	k := a.zoom / a.scale() * float64(s.fullW) / float64(a.texW)
	dx, dy := viewer.Placement(float64(a.texW), float64(a.texH), k, turns, mirror)
	fx := k
	if mirror {
		fx = -k
	}
	t := gsk.NewTransform().Translate(&graphene.Point{X: float32(dx), Y: float32(dy)}).
		Rotate(float32(90*turns)).Scale(float32(fx), float32(k))
	a.canvas.SetChildTransform(&a.picture.Widget, t)
	t.Unref()
	a.canvas.QueueResize() // its size is the scaled picture's
	label := viewer.Percent(a.zoom)
	if a.fit {
		label = i18n.Tf("Fit · %s", label)
	}
	a.zoomLevel.SetLabel(label)
	if a.fit {
		a.zoomLevel.SetTooltipText(i18n.T("Actual size (1)"))
	} else {
		a.zoomLevel.SetTooltipText(i18n.T("Fit the window (0)"))
	}
}

// zoomTo zooms to z, keeping the image's point under (x, y) on the stage
// where it is.
func (a *viewerApp) zoomTo(z float64, x, y float64) {
	if a.w == 0 {
		return
	}
	z = viewer.ClampZoom(z)
	vw, vh := a.view()
	w0, h0 := a.shownSize(a.zoom)
	// The point, as a share of the image; the image is centred while it is
	// smaller than the stage.
	fx := (a.hadj.GetValue() + x - math.Max(0, (vw-w0)/2)) / w0
	fy := (a.vadj.GetValue() + y - math.Max(0, (vh-h0)/2)) / h0
	fx, fy = math.Max(0, math.Min(fx, 1)), math.Max(0, math.Min(fy, 1))
	a.fit, a.zoom = false, z
	a.applyZoom()
	w1, h1 := a.shownSize(z)
	// Set the scroll range now, so the position holds through the layout.
	place := func(adj *gtk.Adjustment, content, view, f, at float64) {
		upper := math.Max(content, view)
		v := math.Max(0, math.Min(f*content-at+math.Max(0, (view-content)/2), upper-view))
		adj.Configure(v, 0, upper, view/10, view*0.9, view)
	}
	place(a.hadj, w1, vw, fx, x)
	place(a.vadj, h1, vh, fy, y)
}

// toggleActual switches between fitting the window and actual size.
func (a *viewerApp) toggleActual(x, y float64) {
	if a.fit && a.zoom != 1 {
		a.zoomTo(1, x, y)
	} else {
		a.fitWindow()
	}
}

func (a *viewerApp) position() string {
	if len(a.files) <= 1 {
		return ""
	}
	return fmt.Sprintf("%d / %d", a.index+1, len(a.files))
}

// showMeta: where it is in the folder, its size and its file size.
func (a *viewerApp) showMeta() {
	var parts []string
	if p := a.position(); p != "" {
		parts = append(parts, p)
	}
	if a.w > 0 {
		parts = append(parts, fmt.Sprintf("%d × %d", a.w, a.h))
	}
	if a.cur != nil && a.cur.size > 0 {
		parts = append(parts, viewer.Size(a.cur.size))
	}
	if ext := strings.TrimPrefix(filepath.Ext(a.current()), "."); ext != "" {
		parts = append(parts, strings.ToUpper(ext))
	}
	a.meta.SetText(strings.Join(parts, "  ·  "))
}

// showDetails fills the details panel: the file, then the camera.
func (a *viewerApp) showDetails() {
	gtkx.Clear(&a.details)
	path := a.current()
	section := func(title string) {
		l := gtk.NewLabel(title)
		l.AddCssClass("kicker")
		l.AddCssClass("viewer-detail-head")
		l.SetXalign(0)
		gtkx.Append(&a.details, &l.Widget)
	}
	row := func(key, value string) {
		if value == "" {
			return
		}
		box := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		box.AddCssClass("viewer-detail")
		k := gtk.NewLabel(key)
		k.AddCssClass("viewer-detail-key")
		k.SetXalign(0)
		gtkx.Append(box, &k.Widget)
		v := gtk.NewLabel(value)
		v.AddCssClass("viewer-detail-value")
		v.SetXalign(0)
		v.SetWrap(true)
		v.SetWrapMode(2) // word or character
		v.SetSelectable(true)
		gtkx.Append(box, &v.Widget)
		gtkx.Append(&a.details, &box.Widget)
	}
	section(i18n.T("FILE"))
	row(i18n.T("Name"), filepath.Base(path))
	row(i18n.T("Folder"), shortHome(filepath.Dir(path)))
	s := a.cur
	if s == nil {
		return
	}
	row(i18n.T("Size"), viewer.Size(s.size))
	ow, oh := s.fullW, s.fullH // as it stands, before any turns asked for
	if t, _ := viewer.Orientation(s.orient); t%2 == 1 {
		ow, oh = oh, ow
	}
	dims := fmt.Sprintf("%d × %d", ow, oh)
	if mp := viewer.Megapixels(ow, oh); mp != "" {
		dims += "  ·  " + mp
	}
	row(i18n.T("Dimensions"), dims)
	row(i18n.T("Type"), viewer.Mime(path))
	if !s.mod.IsZero() {
		row(i18n.T("Modified"), i18n.Time(s.mod, "Jan 2, 2006  15:04"))
	}
	if e := s.exif; !e.Empty() {
		section(i18n.T("CAMERA"))
		row(i18n.T("Camera"), e.Camera())
		row(i18n.T("Lens"), e.Lens)
		row(i18n.T("Exposure"), e.Settings())
		if !e.Taken.IsZero() {
			row(i18n.T("Taken"), i18n.Time(e.Taken, "Jan 2, 2006  15:04"))
		}
	}
}

// setWallpaper makes the image inua's wallpaper and asks the shell to read
// its settings again.
func (a *viewerApp) setWallpaper(path string) {
	cfgPath := config.DefaultPath()
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		cfg = config.Default()
	}
	cfg.Background.Enable = true
	cfg.Background.Image = shortHome(path)
	if err := config.Save(cfgPath, cfg); err != nil {
		log.Printf("viewer: wallpaper: %v", err)
		a.flash(i18n.T("Could not set the wallpaper"))
		return
	}
	go exec.Command("inua", "call", "shell", "reload").Run()
	a.flash(i18n.T("Set as wallpaper"))
}

// showInFolder opens the file manager at the file, or its folder.
func showInFolder(path string) {
	uri := "file://" + path
	err := exec.Command("gdbus", "call", "--session", "--dest", "org.freedesktop.FileManager1",
		"--object-path", "/org/freedesktop/FileManager1",
		"--method", "org.freedesktop.FileManager1.ShowItems", "['"+strings.ReplaceAll(uri, "'", "\\'")+"']", "").Run()
	if err == nil {
		return
	}
	if err := exec.Command("xdg-open", filepath.Dir(path)).Run(); err != nil {
		log.Printf("viewer: show in folder: %v", err)
	}
}

// shortHome writes the home directory as "~".
func shortHome(p string) string {
	if home, err := os.UserHomeDir(); err == nil && (p == home || strings.HasPrefix(p, home+"/")) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func unrefObject(o gobject.Ptr) { (&gobject.Object{Ptr: o.GoPointer()}).Unref() }
