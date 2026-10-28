package termui

import (
	"log"
	"sync"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/term/host"
	"github.com/lyricat/inua/internal/term/scheme"
	"github.com/lyricat/inua/internal/term/vt"
)

// pad is the space around a pane's cells, in pixels.
const pad = 8

// paneLook is what the settings make of a pane: its font (a Pango
// description), its cursor, whether selecting copies, its scrollback.
type paneLook struct {
	Font         string
	Cursor       vt.CursorStyle
	Blink        bool
	CopyOnSelect bool
	Scrollback   int
}

// defaultLook is the look before the settings are read.
var defaultLook = paneLook{Font: "Geist Mono, IBM Plex Mono, monospace 11", Cursor: vt.CursorBlock, CopyOnSelect: true, Scrollback: 10000}

// backend is where a pane's program is: the host, through a client.
type backend interface {
	Input(b []byte) error
	// Resize gives the size in cells, and a cell's in pixels.
	Resize(cols, rows, cellW, cellH int) error
}

// termPane is one terminal: the window's own engine for drawing, and the
// program on the host, drawn by its widget. The engine is touched on the
// main thread only.
type termPane struct {
	id     int          // the host's
	root   *gtk.Overlay // the widget, and the paste sheet over it
	widget *gtk.Widget
	term   *vt.Terminal
	be     backend
	font   *pango.FontDescription
	fonts  [4]*pango.FontDescription // by bold and italic
	fg, bg gdk.RGBA
	cursor gdk.RGBA
	warn   gdk.RGBA // search matches
	cw, ch int
	// sizeHeld keeps the grid's size while the tile moves (a zoom's
	// animation): the program is resized once, where the tile ends.
	sizeHeld bool
	cols     int
	rows     int
	exited   bool
	closed   bool
	lines    [][]vt.Run            // the screen's rows as the engine last said
	drawn    map[string]*drawnLine // the lines drawn, by what they draw (lineNode)
	cur      vt.Cursor
	mouse    bool // the program tracks the mouse
	alt      bool // the alternate screen shows
	scroll   vt.Scrollbar
	// wheelRows is the part of a row a touchpad has scrolled so far.
	wheelRows, wheelNotches float64
	// The scroll going on after the fingers left the touchpad (glide):
	// its speed in rows a second, its frame callback, its last frame.
	glideVel  float64
	glideTick uint32
	glideLast time.Time
	onGlide   gtk.TickCallback
	// pointerX, pointerY is the pointer in the widget; buttons the ones
	// held down for a program tracking the mouse, a bit per button.
	pointerX, pointerY float64
	buttons            int
	// barHover is set while the pointer is over the scrollbar, barDrag
	// while a drag moves it; barGrab is where in the thumb it was taken.
	barHover, barDrag bool
	barGrab           float64
	// selecting is set while a drag selects; dragX, dragY is where it began.
	selecting    bool
	selected     bool // there is a selection, even of blank cells
	dragX, dragY float64

	mu        sync.Mutex
	pending   []feedOp // from the host, not yet fed to the engine
	scheduled bool

	im        *gtk.IMMulticontext
	preedit   string // what the input method is composing, drawn at the cursor
	preeditAt int    // the input method's cursor in it, in characters

	// chord is the window's handler for its Ctrl+Shift chords; focused
	// tells it this pane took the focus.
	chord   func(p *termPane, base, keyval uint32, state gdk.ModifierType) bool
	focused func()
	// wrote follows the engine taking output, for a pane with no host to
	// answer its program (quick.go); nil for none.
	wrote func()
	look  paneLook
	// blinkOff hides a blinking cursor for its off half.
	blinkOff bool
	// hover is the link under the pointer while Ctrl is held.
	hover *link
	// copy is copy mode, while on.
	copy      *copyMode
	onCopyKey func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool

	sheet                             *gtk.Box // the paste sheet, while it asks
	find                              *gtk.Box // the find bar, while open
	findEntry                         *gtk.Entry
	findCount                         *gtk.Label
	onFindChanged                     func(uintptr, uintptr)
	onFindKey                         func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onFindUp, onFindDown, onFindClose func(gtk.Button)
	onSheetCancel, onSheetOneLine     func(gtk.Button)
	onSheetPaste                      func(gtk.Button)
	onSheetKey                        func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onText, onFiles                   [2]gio.AsyncReadyCallback // the clipboard's, the primary selection's
	onImage                           gio.AsyncReadyCallback    // the clipboard's image, read by GTK
	onMiddle                          func(gtk.GestureClick, int32, float64, float64)
	onDrop                            func(gtk.DropTarget, uintptr, float64, float64) bool
	onScroll                          func(gtk.EventControllerScroll, float64, float64) bool
	onDecelerate                      func(gtk.EventControllerScroll, float64, float64)
	onMotion                          func(gtk.EventControllerMotion, float64, float64)
	onLeave                           func(gtk.EventControllerMotion)
	onRaw                             func(gtk.EventControllerLegacy, uintptr) bool

	onKey                                func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onKeyUp                              func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType)
	onCommit                             func(gtk.IMContext, string)
	onPreedit                            func(gtk.IMContext)
	onFocusIn                            func(gtk.EventControllerFocus)
	onFocusOut                           func(gtk.EventControllerFocus)
	onDragBegin, onDragUpdate, onDragEnd func(gtk.GestureDrag, float64, float64)
}

func newTermPane(be backend, cols, rows int, sc scheme.Scheme, warn string, look paneLook) (*termPane, error) {
	p := &termPane{
		look:   look,
		fg:     rgbaOf(sc.FG),
		bg:     rgbaOf(sc.BG),
		cursor: rgbaOf(sc.Cursor),
		warn:   rgba(warn),
		be:     be,
		cols:   cols,
		rows:   rows,
	}
	p.loadFont(look.Font)
	var err error
	if p.term, err = vt.New(p.cols, p.rows); err != nil {
		return nil, err
	}
	p.term.SetDefaults(look.Scrollback, look.Cursor, look.Blink)
	p.term.SetPalette(sc.Palette)
	p.widget = newPaneWidget(p)
	p.widget.SetCursorFromName("text")
	p.widget.SetFocusable(true)
	p.widget.SetHexpand(true)
	p.widget.SetVexpand(true)
	p.root = gtk.NewOverlay()
	p.root.SetChild(p.widget)
	for i, primary := range []bool{false, true} {
		p.onText[i] = func(_, result, _ uintptr) { p.clipRead(primary, result) }
		p.onFiles[i] = func(_, result, _ uintptr) { p.filesRead(primary, result) }
	}
	p.onImage = func(_, result, _ uintptr) { p.imageRead(result) }
	p.im = gtk.NewIMMulticontext()
	p.im.SetClientWidget(p.widget)
	keys := gtk.NewEventControllerKey()
	keys.SetImContext(&p.im.IMContext) // the input method sees keys first
	p.onKey = func(c gtk.EventControllerKey, keyval, keycode uint32, state gdk.ModifierType) bool {
		return p.key(&c, keyval, keycode, state, vt.Press)
	}
	p.onKeyUp = func(c gtk.EventControllerKey, keyval, keycode uint32, state gdk.ModifierType) {
		p.key(&c, keyval, keycode, state, vt.Release)
	}
	keys.ConnectKeyPressed(&p.onKey)
	keys.ConnectKeyReleased(&p.onKeyUp)
	p.widget.AddController(&keys.EventController)
	// Copy mode's keys, before the input method takes the letters.
	copyKeys := gtk.NewEventControllerKey()
	copyKeys.SetPropagationPhase(gtk.PhaseCaptureValue)
	p.onCopyKey = func(_ gtk.EventControllerKey, keyval, _ uint32, state gdk.ModifierType) bool {
		if p.copy == nil {
			return false
		}
		return p.copyKey(keyval, state)
	}
	copyKeys.ConnectKeyPressed(&p.onCopyKey)
	p.widget.AddController(&copyKeys.EventController)
	p.onCommit = func(_ gtk.IMContext, text string) {
		p.preedit, p.preeditAt = "", 0
		p.imCursor()
		p.input([]byte(text))
		p.widget.QueueDraw()
	}
	p.onPreedit = func(im gtk.IMContext) {
		p.preedit, p.preeditAt = gtkx.PreeditString(&im)
		p.imCursor()
		p.widget.QueueDraw()
	}
	p.im.ConnectCommit(&p.onCommit)
	p.im.ConnectPreeditChanged(&p.onPreedit)
	focus := gtk.NewEventControllerFocus()
	p.onFocusIn = func(gtk.EventControllerFocus) {
		p.im.FocusIn()
		if p.focused != nil {
			p.focused()
		}
	}
	p.onFocusOut = func(gtk.EventControllerFocus) { p.im.FocusOut() }
	focus.ConnectEnter(&p.onFocusIn)
	focus.ConnectLeave(&p.onFocusOut)
	p.widget.AddController(&focus.EventController)
	drag := gtk.NewGestureDrag()
	drag.SetButton(1)
	p.onDragBegin = func(g gtk.GestureDrag, x, y float64) {
		p.widget.GrabFocus()
		p.hideSheet()
		// The scrollbar's, before a program's or a selection.
		if p.inBar(x) {
			p.dragX, p.dragY = x, y
			p.barPress(y)
			return
		}
		// A program tracking the mouse gets it; Shift selects anyway.
		if p.mouse && g.GetCurrentEventState()&gdk.ShiftMaskValue == 0 {
			return
		}
		p.selecting, p.dragX, p.dragY = true, x, y
		p.pointer(&g.EventController, vt.PointerPress, x, y)
	}
	p.onDragUpdate = func(g gtk.GestureDrag, dx, dy float64) {
		if p.barDrag {
			p.barTo(p.dragY + dy)
			return
		}
		if p.selecting {
			p.pointer(&g.EventController, vt.PointerDrag, p.dragX+dx, p.dragY+dy)
		}
	}
	p.onDragEnd = func(g gtk.GestureDrag, dx, dy float64) {
		if p.barDrag {
			p.barDrag = false
			p.barHovered(p.inBar(p.dragX + dx))
			p.widget.QueueDraw()
			return
		}
		if !p.selecting {
			return
		}
		p.selecting = false
		if p.pointer(&g.EventController, vt.PointerRelease, p.dragX+dx, p.dragY+dy) {
			p.copySelection(p.look.CopyOnSelect)
		}
	}
	drag.ConnectDragBegin(&p.onDragBegin)
	drag.ConnectDragUpdate(&p.onDragUpdate)
	drag.ConnectDragEnd(&p.onDragEnd)
	p.widget.AddController(&drag.EventController)
	middle := gtk.NewGestureClick()
	middle.SetButton(2)
	p.onMiddle = func(g gtk.GestureClick, _ int32, _, _ float64) {
		if p.mouse && g.GetCurrentEventState()&gdk.ShiftMaskValue == 0 {
			return
		}
		p.widget.GrabFocus()
		p.pasteFrom(true)
	}
	middle.ConnectPressed(&p.onMiddle)
	p.widget.AddController(&middle.EventController)
	p.trackMouse()
	wheel := gtk.NewEventControllerScroll(gtk.EventControllerScrollVerticalValue | gtk.EventControllerScrollKineticValue)
	p.onScroll = func(c gtk.EventControllerScroll, _, dy float64) bool { return p.wheel(&c, dy) }
	p.onDecelerate = func(c gtk.EventControllerScroll, _, vy float64) { p.glide(&c, vy) }
	wheel.ConnectScroll(&p.onScroll)
	wheel.ConnectDecelerate(&p.onDecelerate)
	p.widget.AddController(&wheel.EventController)
	// Files dropped on the pane are pasted as their paths.
	drop := gtk.NewDropTarget(gdk.FileListGLibType(), gdk.ActionCopyValue|gdk.ActionMoveValue)
	p.onDrop = func(dt gtk.DropTarget, _ uintptr, _, _ float64) bool {
		if v := dt.GetValue(); v != nil && !p.exited {
			if paths := filePaths(v); len(paths) > 0 {
				p.widget.GrabFocus()
				p.paste(quotePaths(paths), false, false)
				return true
			}
		}
		return false
	}
	drop.ConnectDrop(&p.onDrop)
	p.widget.AddController(&drop.EventController)
	return p, nil
}

// pointer passes a pointer event at x, y (the widget's pixels) to the
// engine's selection, and redraws; it reports whether there is a
// selection.
func (p *termPane) pointer(c *gtk.EventController, kind vt.PointerKind, x, y float64) bool {
	if kind == vt.PointerPress {
		p.stopGlide()
	}
	if p.cw == 0 || p.ch == 0 {
		return false
	}
	px, py := x-pad, y-pad
	cx := min(max(int(px)/p.cw, 0), p.cols-1)
	cy := min(max(int(py)/p.ch, 0), p.rows-1)
	ms, px2 := doubleClick()
	ok, err := p.term.Select(vt.Pointer{
		Kind: kind, X: cx, Y: cy, PX: max(px, 0), PY: max(py, 0),
		Time: time.Duration(c.GetCurrentEventTime()) * time.Millisecond, Repeat: ms, RepeatPx: px2,
		CellW: p.cw, Height: p.rows * p.ch,
	})
	if err != nil {
		log.Printf("term: select: %v", err)
	}
	p.selected = ok
	p.update(false)
	return ok
}

// doubleClick is GTK's: how long and how far apart two clicks are one.
// Read as ints: puregotk's getters for these read them as longs.
func doubleClick() (time.Duration, float64) {
	s := gtk.SettingsGetDefault()
	if s == nil {
		return 400 * time.Millisecond, 5
	}
	get := func(name string) int32 {
		var v gobject.Value
		v.Init(gobject.TypeIntVal)
		s.GetProperty(name, &v)
		n := v.GetInt()
		v.Unset()
		return n
	}
	return time.Duration(get("gtk-double-click-time")) * time.Millisecond, float64(get("gtk-double-click-distance"))
}

// copySelection puts the selection on the primary selection, and on the
// clipboard when toClipboard (selecting copies; Ctrl+Shift+C does too).
func (p *termPane) copySelection(toClipboard bool) {
	text, err := p.term.SelectionText()
	if err != nil || text == "" {
		return
	}
	for _, primary := range []bool{true, false} {
		if primary || toClipboard {
			clip := p.clipboard(primary)
			clip.SetText(text)
			clip.Unref()
		}
	}
}

// key sends a key the input method left, encoded by the engine in the
// modes the program set.
func (p *termPane) key(c *gtk.EventControllerKey, keyval, keycode uint32, state gdk.ModifierType, action vt.KeyAction) bool {
	if action == vt.Press {
		p.stopGlide()
	}
	state = keyMods(state)
	var layout uint32
	var consumed gdk.ModifierType
	if ev := gobj.currentEvent(c.GoPointer()); ev != 0 { // borrowed, no reference taken
		ke := gdk.KeyEventNewFromInternalPtr(ev)
		consumed = keyMods(ke.GetConsumedModifiers())
		layout = ke.GetLayout()
	}
	// The key without modifiers: the window's chords match it, and the
	// kitty protocol sends its character.
	var base uint32
	if !p.widget.GetDisplay().TranslateKey(keycode, 0, int32(layout), &base, nil, nil, nil) {
		base = keyval
	}
	if action == vt.Press && p.chord != nil && p.chord(p, base, keyval, state) {
		return true
	}
	p.blinkOff = false // typing shows the cursor
	if p.exited {
		return false
	}
	if action == vt.Press {
		chord := state & (gdk.ControlMaskValue | gdk.ShiftMaskValue | gdk.AltMaskValue | gdk.SuperMaskValue)
		switch {
		case chord == gdk.ShiftMaskValue && (int32(keyval) == gdk.KEY_Insert || int32(keyval) == gdk.KEY_KP_Insert):
			p.pasteFrom(true)
			return true
		case p.scrollKey(keyval, chord):
			return true
		}
	}
	k := vt.KeyEvent{Keyval: keyval, Keycode: keycode, Mods: mods(state), Action: action}
	if r := gdk.KeyvalToUnicode(keyval); r >= 0x20 && r != 0x7f {
		k.Text = string(rune(r))
	}
	k.Consumed = mods(consumed)
	k.Unshifted = rune(gdk.KeyvalToUnicode(base))
	b, err := p.term.Key(k)
	if err != nil {
		log.Printf("term: key: %v", err)
		return false
	}
	if len(b) == 0 {
		return false
	}
	p.input(b)
	return true
}

// input sends what was typed (keys, the input method's text); typing
// ends the selection, as in Ghostty.
func (p *termPane) input(b []byte) {
	if p.exited || len(b) == 0 {
		return
	}
	if p.selected {
		p.term.ClearSelection()
		p.selected = false
		p.update(false)
	}
	// Typing shows where it goes.
	if !p.scroll.AtBottom() {
		p.scrollBy(vt.ScrollBottom, 0)
	}
	p.send(b)
}

// send gives the program bytes, as they are.
func (p *termPane) send(b []byte) {
	if p.exited || len(b) == 0 {
		return
	}
	if err := p.be.Input(b); err != nil {
		log.Printf("term: input: %v", err)
	}
}

// mods is GDK's modifiers as the engine's.
func mods(state gdk.ModifierType) uint32 {
	var m uint32
	for _, b := range []struct {
		gdk gdk.ModifierType
		vt  uint32
	}{
		{gdk.ShiftMaskValue, vt.ModShift}, {gdk.ControlMaskValue, vt.ModCtrl}, {gdk.AltMaskValue, vt.ModAlt},
		{gdk.SuperMaskValue, vt.ModSuper}, {gdk.LockMaskValue, vt.ModCaps},
	} {
		if state&b.gdk != 0 {
			m |= b.vt
		}
	}
	return m
}

// feed takes the program's output on the client's goroutine and hands it
// to the main thread, a burst at a time.
func (p *termPane) feed(b []byte) {
	p.mu.Lock()
	if n := len(p.pending); n > 0 && p.pending[n-1].cols == 0 {
		p.pending[n-1].b = append(p.pending[n-1].b, b...)
	} else {
		p.pending = append(p.pending, feedOp{b: append([]byte(nil), b...)})
	}
	p.schedule()
}

// feedSize gives the engine a size the host took, after the output that
// came before it: the engine stays as the host's, which a size taken at
// once, ahead of the output on its way, would not.
func (p *termPane) feedSize(cols, rows int) {
	p.mu.Lock()
	p.pending = append(p.pending, feedOp{cols: cols, rows: rows})
	p.schedule()
}

// schedule drains on the main thread; p.mu is held, and let go.
func (p *termPane) schedule() {
	post := !p.scheduled
	p.scheduled = true
	p.mu.Unlock()
	if post {
		gtkx.Post(p.drain)
	}
}

// feedOp is output, or a size (cols > 0).
type feedOp struct {
	b          []byte
	cols, rows int
}

// drain feeds what came to the engine, in order, and redraws once.
func (p *termPane) drain() {
	p.mu.Lock()
	ops := p.pending
	p.pending, p.scheduled = nil, false
	p.mu.Unlock()
	if p.closed { // its engine is freed; this was posted before the close
		return
	}
	all := false
	for _, op := range ops {
		if op.cols == 0 {
			p.term.Write(op.b)
			continue
		}
		if err := p.term.Resize(op.cols, op.rows); err != nil {
			log.Printf("term: %v", err)
		}
		all = true
	}
	if p.wrote != nil {
		p.wrote()
	}
	p.update(all)
}

// update takes the rows that changed from the engine and redraws.
func (p *termPane) update(all bool) {
	f, err := p.term.Frame(all)
	if err != nil {
		log.Printf("term: %v", err)
		return
	}
	if len(p.lines) != f.Rows {
		p.lines = make([][]vt.Run, f.Rows)
	}
	for _, l := range f.Lines {
		if l.Y < len(p.lines) {
			p.lines[l.Y] = l.Runs
		}
	}
	p.cur = f.Cursor
	p.mouse = f.Mouse
	p.alt = f.Alt
	p.scroll = f.Scroll
	p.imCursor()
	p.showCount()
	p.widget.QueueDraw()
}

// imCursor tells the input method where the cursor is, in the preedit
// when composing, so its candidates open there.
func (p *termPane) imCursor() {
	if p.cw == 0 {
		return
	}
	x := p.cur.X
	if p.preedit != "" {
		cs, _ := cellClusters(p.preedit, 0)
		_, at := preeditCells(cs, p.preeditAt)
		x += at
	}
	p.im.SetCursorLocation(&gdk.Rectangle{X: int32(pad + x*p.cw), Y: int32(pad + p.cur.Y*p.ch), Width: int32(p.cw), Height: int32(p.ch)})
}

func (p *termPane) exit(code int) {
	if p.exited {
		return
	}
	p.drain()
	p.exited = true
	p.term.Write([]byte("\r\n\x1b[2m[exited " + itoa(code) + "]\x1b[0m"))
	p.update(false)
}

func (p *termPane) allocate(w *gtk.Widget, width, height int) {
	p.cw, p.ch = cellSize(w, p.font)
	cols, rows := max((width-2*pad)/p.cw, 2), max((height-2*pad)/p.ch, 1)
	if p.sizeHeld || cols == p.cols && rows == p.rows {
		return
	}
	p.cols, p.rows = cols, rows
	// The engine takes the size when the host has (feedSize); without a
	// host that tells it, now.
	if err := p.be.Resize(cols, rows, p.cw, p.ch); err != nil {
		if err := p.term.Resize(cols, rows); err != nil {
			log.Printf("term: %v", err)
		}
		p.update(true)
	}
}

func (p *termPane) snapshot(w *gtk.Widget, s *gtk.Snapshot) {
	s.AppendColor(&p.bg, rect(0, 0, float32(w.GetWidth()), float32(w.GetHeight())))
	if p.cw == 0 {
		p.cw, p.ch = cellSize(w, p.font)
	}
	cw, ch := float32(p.cw), float32(p.ch)
	s.Save()
	s.Translate(&graphene.Point{X: pad, Y: pad})
	for _, l := range p.drawn {
		l.used = false
	}
	for y, runs := range p.lines {
		if len(runs) == 0 {
			continue
		}
		if n := p.lineNode(w, runs, cw, ch); n != nil {
			s.Save()
			s.Translate(&graphene.Point{Y: float32(y) * ch})
			s.AppendNode(n)
			s.Restore()
		}
	}
	for key, l := range p.drawn {
		if !l.used {
			l.free()
			delete(p.drawn, key)
		}
	}
	if h := p.hover; h != nil {
		c := p.cursor
		s.AppendColor(&c, rect(float32(h.X0)*cw, float32(h.Y+1)*ch-1, float32(h.X1-h.X0+1)*cw, 1))
	}
	if p.preedit != "" {
		// What the input method is composing, over the cells at the cursor.
		// On the cells it will take, its own cursor where the input
		// method has it.
		cs, _ := cellClusters(p.preedit, 0)
		n, at := preeditCells(cs, p.preeditAt)
		x, y := float32(p.cur.X)*cw, float32(p.cur.Y)*ch
		s.AppendColor(&p.bg, rect(x, y, float32(n)*cw, ch))
		s.Save()
		s.Translate(&graphene.Point{X: x, Y: y})
		p.drawText(w, s, p.preedit, n, p.font, &p.fg, cw)
		s.AppendColor(&p.cursor, rect(0, ch-2, float32(n)*cw, 1))
		s.AppendColor(&p.cursor, rect(float32(at)*cw, 0, 2, ch))
		s.Restore()
	}
	if c := p.copy; c != nil {
		// The copy cursor: a hollow block in the accent.
		x, y := float32(c.x)*cw, float32(c.y)*ch
		col := p.cursor
		s.AppendColor(&col, rect(x, y, cw, 1))
		s.AppendColor(&col, rect(x, y+ch-1, cw, 1))
		s.AppendColor(&col, rect(x, y, 1, ch))
		s.AppendColor(&col, rect(x+cw-1, y, 1, ch))
	}
	if !p.exited && p.cur.Visible && p.preedit == "" && !(p.cur.Blink && p.blinkOff) && p.copy == nil {
		c := p.cursor
		x, y := float32(p.cur.X)*cw, float32(p.cur.Y)*ch
		switch p.cur.Style {
		case vt.CursorBar:
			s.AppendColor(&c, rect(x, y, 2, ch))
		case vt.CursorUnderline:
			s.AppendColor(&c, rect(x, y+ch-2, cw, 2))
		default:
			c.Alpha = 0.55
			s.AppendColor(&c, rect(x, y, cw, ch))
		}
	}
	s.Restore()
	p.drawScrollbar(w, s)
}

// colors are the pane's default colours, for programs that ask.
func (p *termPane) colors() host.Colors {
	v := func(c gdk.RGBA) uint32 {
		b := func(f float32) uint32 { return uint32(f*255+0.5) & 0xff }
		return b(c.Red)<<16 | b(c.Green)<<8 | b(c.Blue)
	}
	return host.Colors{FG: v(p.fg), BG: v(p.bg), Cursor: v(p.cursor)}
}

// color is a run's colour, or the pane's for the terminal's default.
func (p *termPane) color(v uint32, def gdk.RGBA) gdk.RGBA {
	if v == vt.DefaultColor {
		return def
	}
	return gdk.RGBA{Red: float32(v>>16&0xff) / 255, Green: float32(v>>8&0xff) / 255, Blue: float32(v&0xff) / 255, Alpha: 1}
}

// loadFont takes a Pango font description (family, then size) and its
// bold and italic faces; the cells are measured again at the next layout.
func (p *termPane) loadFont(spec string) {
	p.dropLines()
	p.font = pango.FontDescriptionFromString(spec)
	for i := range p.fonts {
		f := pango.FontDescriptionFromString(spec)
		if i&1 != 0 {
			f.SetWeight(pango.WeightBoldValue)
		}
		if i&2 != 0 {
			f.SetStyle(pango.StyleItalicValue)
		}
		p.fonts[i] = f
	}
}

// setFont changes the font of a pane shown: its cells and size follow.
func (p *termPane) setFont(spec string) {
	p.look.Font = spec
	p.loadFont(spec)
	p.cols, p.rows = 0, 0 // the next layout sizes the terminal anew
	p.widget.QueueAllocate()
	p.widget.QueueDraw()
}

// busy says a sheet or the find bar has the keys.
func (p *termPane) busy() bool { return p.sheet != nil || p.find != nil }

// setScheme takes the terminals' colours, and the theme's warn (the
// search's matches).
func (p *termPane) setScheme(sc scheme.Scheme, warn string) {
	p.fg, p.bg, p.cursor = rgbaOf(sc.FG), rgbaOf(sc.BG), rgbaOf(sc.Cursor)
	p.warn = rgba(warn)
	p.dropLines()
	p.term.SetPalette(sc.Palette)
	p.update(true)
}

// callbacks are the pane's callbacks, to free once it is gone (one never
// connected frees nothing).
func (p *termPane) callbacks() []any {
	return []any{
		&p.onKey, &p.onKeyUp, &p.onCopyKey, &p.onCommit, &p.onPreedit,
		&p.onFocusIn, &p.onFocusOut, &p.onDragBegin, &p.onDragUpdate, &p.onDragEnd,
		&p.onMiddle, &p.onScroll, &p.onDecelerate, &p.onDrop, &p.onMotion, &p.onLeave, &p.onRaw,
		&p.onGlide, &p.onFindChanged, &p.onFindKey, &p.onFindUp, &p.onFindDown,
		&p.onFindClose, &p.onSheetKey, &p.onSheetCancel, &p.onSheetOneLine,
		&p.onSheetPaste, &p.onText[0], &p.onText[1], &p.onFiles[0], &p.onFiles[1], &p.onImage,
	}
}

func (p *termPane) close() {
	p.stopGlide()
	p.closed = true
	p.im.SetClientWidget(nil)
	p.term.Close()
	p.dropLines()
	forgetPaneWidget(p.widget)
}

func itoa(n int) string {
	if n < 0 {
		return "-" + itoa(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
