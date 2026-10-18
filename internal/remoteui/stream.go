package remoteui

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/remotedesk"
	"github.com/lyricat/inua/internal/gamestream"
	"github.com/lyricat/inua/internal/gst"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

func decoderFor(codec string) string { return gst.Decoder(codec) }

// stream is a machine's desktop in a window of its own: its picture, its
// sound, and this computer's keyboard and mouse sent to it.
type stream struct {
	w       *window
	m       remotedesk.Machine
	win     *gtk.ApplicationWindow
	picture *gtk.Picture
	note    *gtk.Label

	session      *gamestream.Session
	video, audio *gst.Stream
	width        int // the stream's picture
	height       int

	mu        sync.Mutex
	frame     *gst.Frame
	framePost bool

	held     map[uint16]bool // keys down here, let go of when the window loses the keys
	captured bool            // the compositor's shortcuts go to the machine
	done     bool
}

// The input handlers, one of each for every stream window (puregotk makes
// a native trampoline for each Go function): they find their stream by
// the controller.
var (
	streams     = map[uintptr]*stream{}
	onKeyDown   func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onKeyUp     func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType)
	onMotion    func(gtk.EventControllerMotion, float64, float64)
	onPress     func(gtk.GestureClick, int32, float64, float64)
	onRelease   func(gtk.GestureClick, int32, float64, float64)
	onScroll    func(gtk.EventControllerScroll, float64, float64) bool
	onFocusOut  func(gtk.EventControllerFocus)
	onStreamEnd func(gtk.Window) bool
	handlersSet bool
)

func setHandlers() {
	if handlersSet {
		return
	}
	handlersSet = true
	onKeyDown = func(c gtk.EventControllerKey, keyval, keycode uint32, state gdk.ModifierType) bool {
		if s := streams[c.GoPointer()]; s != nil {
			return s.key(keyval, keycode, state, true)
		}
		return false
	}
	onKeyUp = func(c gtk.EventControllerKey, keyval, keycode uint32, state gdk.ModifierType) {
		if s := streams[c.GoPointer()]; s != nil {
			s.key(keyval, keycode, state, false)
		}
	}
	onMotion = func(c gtk.EventControllerMotion, x, y float64) {
		if s := streams[c.GoPointer()]; s != nil {
			s.motion(x, y)
		}
	}
	onPress = func(g gtk.GestureClick, _ int32, x, y float64) {
		if s := streams[g.GoPointer()]; s != nil {
			s.motion(x, y)
			s.button(g.GetCurrentButton(), true)
		}
	}
	onRelease = func(g gtk.GestureClick, _ int32, x, y float64) {
		if s := streams[g.GoPointer()]; s != nil {
			s.button(g.GetCurrentButton(), false)
		}
	}
	onScroll = func(c gtk.EventControllerScroll, dx, dy float64) bool {
		if s := streams[c.GoPointer()]; s != nil {
			s.scroll(dx, dy, c.GetUnit() == gdk.ScrollUnitWheelValue)
			return true
		}
		return false
	}
	onFocusOut = func(c gtk.EventControllerFocus) {
		if s := streams[c.GoPointer()]; s != nil {
			s.letGo()
		}
	}
	onStreamEnd = func(win gtk.Window) bool {
		if s := streams[win.GoPointer()]; s != nil {
			s.close()
		}
		return false
	}
}

// openStream connects to m and shows its desktop.
func openStream(w *window, m remotedesk.Machine) *stream {
	setHandlers()
	s := &stream{w: w, m: m, held: map[uint16]bool{}}
	st := w.settings.Stream
	s.width, s.height = st.Width, st.Height
	if s.width == 0 {
		s.width, s.height = monitorPixels(&w.win.Widget)
	}

	s.win = gtk.NewApplicationWindow(w.c.App)
	s.win.AddCssClass("remote-stream")
	s.win.SetTitle(i18n.Tf("%s — Remote Desktop", remotedesk.Label(m)))
	s.win.SetDefaultSize(int32(min(s.width, 1600)), int32(min(s.height, 900)))
	overlay := gtk.NewOverlay()
	s.picture = gtk.NewPicture()
	s.picture.SetContentFit(gtk.ContentFitContainValue)
	s.picture.SetCanShrink(true)
	s.picture.SetCursorFromName("none") // the machine draws its own pointer
	overlay.SetChild(&s.picture.Widget)
	s.note = gtk.NewLabel(i18n.Tf("Connecting to %s…", remotedesk.Label(m)))
	s.note.AddCssClass("remote-note")
	s.note.SetHalign(gtk.AlignCenterValue)
	s.note.SetValign(gtk.AlignStartValue)
	s.note.SetWrap(true)
	overlay.AddOverlay(&s.note.Widget)
	s.win.SetChild(&overlay.Widget)

	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue)
	keys.ConnectKeyPressed(&onKeyDown)
	keys.ConnectKeyReleased(&onKeyUp)
	s.win.AddController(&keys.EventController)
	streams[keys.GoPointer()] = s
	motion := gtk.NewEventControllerMotion()
	motion.ConnectMotion(&onMotion)
	s.picture.AddController(&motion.EventController)
	streams[motion.GoPointer()] = s
	click := gtk.NewGestureClick()
	click.SetButton(0) // every button
	click.ConnectPressed(&onPress)
	click.ConnectReleased(&onRelease)
	s.picture.AddController(&click.EventController)
	streams[click.GoPointer()] = s
	scroll := gtk.NewEventControllerScroll(gtk.EventControllerScrollBothAxesValue)
	scroll.ConnectScroll(&onScroll)
	s.picture.AddController(&scroll.EventController)
	streams[scroll.GoPointer()] = s
	focus := gtk.NewEventControllerFocus()
	focus.ConnectLeave(&onFocusOut)
	s.win.AddController(&focus.EventController)
	streams[focus.GoPointer()] = s
	s.win.ConnectCloseRequest(&onStreamEnd)
	streams[s.win.GoPointer()] = s
	s.win.Present()

	cfg := gamestream.Config{Width: s.width, Height: s.height, FPS: st.FPS, BitrateKbps: st.Bitrate, Codec: st.GameCodec()}
	go s.run(cfg)
	return s
}

// monitorPixels is the size, in pixels, of the monitor w is on.
func monitorPixels(w *gtk.Widget) (int, int) {
	native := w.GetNative()
	if native == nil {
		return 1920, 1080
	}
	surface := native.GetSurface()
	if surface == nil {
		return 1920, 1080
	}
	mon := gdk.DisplayGetDefault().GetMonitorAtSurface(surface)
	if mon == nil {
		return 1920, 1080
	}
	var r gdk.Rectangle
	mon.GetGeometry(&r)
	scale := mon.GetScale()
	if scale <= 0 {
		scale = 1
	}
	// Even sizes: encoders want them.
	return int(float64(r.Width)*scale) &^ 1, int(float64(r.Height)*scale) &^ 1
}

// run connects, then feeds the decoders until the session ends.
func (s *stream) run(cfg gamestream.Config) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	session, err := gamestream.Connect(ctx, s.w.host(s.m), 0, cfg)
	cancel()
	if err == nil {
		codec := map[gamestream.Codec]string{gamestream.H264: "h264", gamestream.HEVC: "h265", gamestream.AV1: "av1"}[session.Config.Codec]
		var video, audio *gst.Stream
		video, err = gst.NewVideo(codec)
		if err == nil {
			audio, err = gst.NewAudio("")
			if err != nil { // no sound beats no picture
				log.Printf("remote: sound: %v", err)
				err = nil
			}
		}
		if err != nil {
			session.Close()
		} else {
			s.session, s.video, s.audio = session, video, audio
		}
	}
	gtkx.Post(func() {
		if err != nil || s.done {
			if s.session != nil {
				s.session.Close()
			}
			s.end(err)
			return
		}
		s.note.SetText(i18n.T("Ctrl+Alt+Shift+Q disconnects · F: full screen · Z: this desktop's shortcuts"))
		s.capture(true)
		go func() {
			time.Sleep(5 * time.Second)
			gtkx.Post(func() {
				if !s.done {
					s.note.SetVisible(false)
				}
			})
		}()
	})
	if err != nil {
		return
	}
	go func() {
		for f := range s.video.Frames {
			s.mu.Lock()
			s.frame = &f
			post := !s.framePost
			s.framePost = true
			s.mu.Unlock()
			if post {
				gtkx.Post(s.draw)
			}
		}
	}()
	go func() {
		for p := range s.session.Audio {
			if s.audio != nil {
				s.audio.Push(p)
			}
		}
	}()
	go func() {
		for {
			select {
			case f := <-s.session.Video:
				if err := s.video.Push(f.Data); err != nil {
					return
				}
			case err := <-s.video.Errors:
				log.Printf("remote: decoding: %v", err)
				s.session.RequestIDR()
			case <-s.session.Done():
				err := s.session.Err()
				gtkx.Post(func() { s.end(err) })
				return
			}
		}
	}()
}

func (s *stream) draw() {
	s.mu.Lock()
	f := s.frame
	s.frame, s.framePost = nil, false
	s.mu.Unlock()
	if f == nil || s.done {
		return
	}
	// An encoder may pad the picture past the size asked for (AMD's AV1:
	// 1080 rows come as 1082); the rows past it are dropped, which costs
	// nothing, as they come last.
	if f.W == s.width && f.H > s.height && s.height > 0 {
		f.H = s.height
		f.Pix = f.Pix[:f.Stride*f.H]
	}
	b := glib.NewBytes(f.Pix, uint(len(f.Pix)))
	tex := gdk.NewMemoryTexture(int32(f.W), int32(f.H), gdk.MemoryR8g8b8a8Value, b, uint(f.Stride))
	b.Unref()
	s.picture.SetPaintable(tex)
	tex.Unref()
}

// capture sends the compositor's shortcuts (Super and the rest) to the
// machine, or gives them back to this desktop.
func (s *stream) capture(on bool) {
	native := s.win.GetNative()
	if native == nil {
		return
	}
	surface := native.GetSurface()
	if surface == nil {
		return
	}
	top := &gdk.ToplevelBase{Ptr: surface.GoPointer()}
	if on {
		top.InhibitSystemShortcuts(&gdk.Event{})
	} else {
		top.RestoreSystemShortcuts()
	}
	s.captured = on
}

// key sends a key to the machine, but for this window's own: with
// Ctrl+Alt+Shift, Q disconnects, F fills the screen, Z gives this
// desktop its shortcuts back (or takes them again).
func (s *stream) key(keyval, keycode uint32, state gdk.ModifierType, down bool) bool {
	if s.session == nil {
		return false
	}
	const combo = gdk.ControlMaskValue | gdk.AltMaskValue | gdk.ShiftMaskValue
	if down && state&combo == combo {
		switch gdk.KeyvalToLower(keyval) {
		case uint32(gdk.KEY_q):
			s.letGo()
			s.win.Close()
			return true
		case uint32(gdk.KEY_f):
			s.letGo()
			if s.win.IsFullscreen() {
				s.win.Unfullscreen()
			} else {
				s.win.Fullscreen()
			}
			return true
		case uint32(gdk.KEY_z):
			s.letGo()
			s.capture(!s.captured)
			return true
		}
	}
	vk := remotedesk.VirtualKey(keycode - 8)
	if vk == 0 {
		return false
	}
	if down {
		s.held[vk] = true
	} else if !s.held[vk] {
		return true // its press went to this desktop
	} else {
		delete(s.held, vk)
	}
	s.session.Key(vk, down, modifiers(state))
	return true
}

func modifiers(state gdk.ModifierType) uint8 {
	var m uint8
	if state&gdk.ShiftMaskValue != 0 {
		m |= gamestream.ModShift
	}
	if state&gdk.ControlMaskValue != 0 {
		m |= gamestream.ModCtrl
	}
	if state&gdk.AltMaskValue != 0 {
		m |= gamestream.ModAlt
	}
	if state&gdk.SuperMaskValue != 0 {
		m |= gamestream.ModMeta
	}
	return m
}

// letGo lets go of the keys held, so none stays down on the machine.
func (s *stream) letGo() {
	if s.session == nil {
		return
	}
	for vk := range s.held {
		s.session.Key(vk, false, 0)
	}
	s.held = map[uint16]bool{}
}

func (s *stream) motion(x, y float64) {
	if s.session == nil {
		return
	}
	px, py, inside := remotedesk.StreamPoint(x, y, float64(s.picture.GetWidth()), float64(s.picture.GetHeight()), s.width, s.height)
	if inside {
		s.session.MouseTo(px, py, s.width, s.height)
	}
}

func (s *stream) button(b uint32, down bool) {
	if s.session == nil {
		return
	}
	button := map[uint32]uint8{1: gamestream.ButtonLeft, 2: gamestream.ButtonMiddle, 3: gamestream.ButtonRight,
		8: gamestream.ButtonX1, 9: gamestream.ButtonX2}[b]
	if button != 0 {
		s.session.Button(button, down)
	}
}

func (s *stream) scroll(dx, dy float64, wheel bool) {
	if s.session == nil {
		return
	}
	if dy != 0 {
		s.session.Scroll(remotedesk.ScrollAmount(dy, wheel))
	}
	if dx != 0 {
		s.session.HScroll(-remotedesk.ScrollAmount(dx, wheel)) // right is positive in both
	}
}

// close ends the stream from this side: the window closes, the session
// on the machine goes on, to be joined again.
func (s *stream) close() {
	if s.done {
		return
	}
	if s.session != nil {
		s.letGo()
		s.session.Close()
	}
	s.end(nil)
	s.win.Close()
}

// end lets the stream go: its decoders, its handlers, and the main
// window's note of it. An error the host ended with is shown.
func (s *stream) end(err error) {
	if s.done {
		return
	}
	s.done = true
	if s.captured {
		s.capture(false)
	}
	if s.video != nil {
		s.video.Close()
	}
	if s.audio != nil {
		s.audio.Close()
	}
	for k, v := range streams {
		if v == s {
			delete(streams, k)
		}
	}
	var ended *gamestream.ErrHostEnded
	var status *gamestream.StatusError
	switch {
	case err == nil:
	case errors.As(err, &ended):
		err = errors.New(i18n.Tf("%s ended the session.", remotedesk.Label(s.m)))
	case errors.As(err, &status) && status.Code == 503:
		// Sunshine's answer when it has no screen to capture.
		err = errors.New(i18n.Tf("%s cannot show its screen: no monitor is connected, or its desktop session is not running. A computer without a monitor can share a virtual screen (Remote Desktop, Sharing).", remotedesk.Label(s.m)))
	default:
		err = errors.New(i18n.Tf("%s: %v", remotedesk.Label(s.m), err))
	}
	if s.win.GetVisible() {
		s.win.Close()
	}
	s.w.ended(s, err)
}
