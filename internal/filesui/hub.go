package filesui

import (
	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
)

// hub holds one signal handler of each kind for the whole process, and
// sends each signal to the Go function registered for the object that
// emitted it. purego makes a native trampoline for every Go function
// connected, at most 2000 and never freed; the file manager runs for the
// whole session and opens windows again and again, so its windows must
// not each connect closures of their own.
type hub struct {
	clicked   map[uintptr]func()
	toggled   map[uintptr]func(bool)
	keys      map[uintptr]func(keyval uint32, state gdk.ModifierType) bool
	activated map[uintptr]func(pos uint32)
	setup     map[uintptr]func(item uintptr)
	bind      map[uintptr]func(item uintptr)
	teardown  map[uintptr]func(item uintptr)
	entered   map[uintptr]func()
	changed   map[uintptr]func()
	pressed   map[uintptr]func(button uint32, n int32, x, y float64)
	closing   map[uintptr]func() bool
	prepare   map[uintptr]func(x, y float64) uintptr
	dropped   map[uintptr]func(v *gobject.Value, x, y float64, state gdk.ModifierType) bool
	selection map[uintptr]func()
	notified  map[uintptr]func()
	scrolled  map[uintptr]func(dy float64)
	async     map[uintptr]func(result uintptr)
	timers    map[uintptr]func()
	next      uintptr

	onClicked   func(gtk.Button)
	onToggled   func(gtk.ToggleButton)
	onChecked   func(gtk.CheckButton)
	onKey       func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onListAct   func(gtk.ListView, uint32)
	onGridAct   func(gtk.GridView, uint32)
	onSetup     func(gtk.SignalListItemFactory, uintptr)
	onBind      func(gtk.SignalListItemFactory, uintptr)
	onTeardown  func(gtk.SignalListItemFactory, uintptr)
	onEntered   func(uintptr, uintptr)
	onChanged   func(uintptr, uintptr)
	onPressed   func(gtk.GestureClick, int32, float64, float64)
	onClose     func(gtk.Window) bool
	onPrepare   func(gtk.DragSource, float64, float64) gdk.ContentProvider
	onDrop      func(gtk.DropTarget, uintptr, float64, float64) bool
	onSelection func(uintptr, uint32, uint32, uintptr)
	onNotify    func(uintptr, uintptr, uintptr)
	onScroll    func(gtk.EventControllerScroll, float64, float64) bool
	onAsync     gio.AsyncReadyCallback
	onTimer     glib.SourceFunc
}

func newHub() *hub {
	h := &hub{
		clicked: map[uintptr]func(){}, toggled: map[uintptr]func(bool){},
		keys:      map[uintptr]func(uint32, gdk.ModifierType) bool{},
		activated: map[uintptr]func(uint32){},
		setup:     map[uintptr]func(uintptr){}, bind: map[uintptr]func(uintptr){}, teardown: map[uintptr]func(uintptr){},
		entered: map[uintptr]func(){}, changed: map[uintptr]func(){},
		pressed:   map[uintptr]func(uint32, int32, float64, float64){},
		closing:   map[uintptr]func() bool{},
		prepare:   map[uintptr]func(float64, float64) uintptr{},
		dropped:   map[uintptr]func(*gobject.Value, float64, float64, gdk.ModifierType) bool{},
		selection: map[uintptr]func(){},
		notified:  map[uintptr]func(){},
		scrolled:  map[uintptr]func(float64){},
		async:     map[uintptr]func(uintptr){}, timers: map[uintptr]func(){},
	}
	h.onClicked = func(b gtk.Button) {
		if f := h.clicked[b.GoPointer()]; f != nil {
			f()
		}
	}
	h.onToggled = func(b gtk.ToggleButton) {
		if f := h.toggled[b.GoPointer()]; f != nil {
			f(b.GetActive())
		}
	}
	h.onChecked = func(b gtk.CheckButton) {
		if f := h.toggled[b.GoPointer()]; f != nil {
			f(b.GetActive())
		}
	}
	h.onKey = func(c gtk.EventControllerKey, keyval, _ uint32, state gdk.ModifierType) bool {
		if f := h.keys[c.GoPointer()]; f != nil {
			return f(keyval, state)
		}
		return false
	}
	h.onListAct = func(v gtk.ListView, pos uint32) {
		if f := h.activated[v.GoPointer()]; f != nil {
			f(pos)
		}
	}
	h.onGridAct = func(v gtk.GridView, pos uint32) {
		if f := h.activated[v.GoPointer()]; f != nil {
			f(pos)
		}
	}
	h.onSetup = func(f gtk.SignalListItemFactory, item uintptr) {
		if fn := h.setup[f.GoPointer()]; fn != nil {
			fn(item)
		}
	}
	h.onBind = func(f gtk.SignalListItemFactory, item uintptr) {
		if fn := h.bind[f.GoPointer()]; fn != nil {
			fn(item)
		}
	}
	h.onTeardown = func(f gtk.SignalListItemFactory, item uintptr) {
		if fn := h.teardown[f.GoPointer()]; fn != nil {
			fn(item)
		}
	}
	h.onEntered = func(obj, _ uintptr) {
		if f := h.entered[obj]; f != nil {
			f()
		}
	}
	h.onChanged = func(obj, _ uintptr) {
		if f := h.changed[obj]; f != nil {
			f()
		}
	}
	h.onPressed = func(g gtk.GestureClick, n int32, x, y float64) {
		if f := h.pressed[g.GoPointer()]; f != nil {
			f(g.GetCurrentButton(), n, x, y)
		}
	}
	h.onClose = func(w gtk.Window) bool {
		if f := h.closing[w.GoPointer()]; f != nil {
			return f()
		}
		return false
	}
	h.onPrepare = func(d gtk.DragSource, x, y float64) gdk.ContentProvider {
		var cp gdk.ContentProvider
		if f := h.prepare[d.GoPointer()]; f != nil {
			cp.Ptr = f(x, y) // transfer full: the drag takes it
		}
		return cp
	}
	h.onDrop = func(d gtk.DropTarget, _ uintptr, x, y float64) bool {
		f := h.dropped[d.GoPointer()]
		v := d.GetValue()
		if f == nil || v == nil {
			return false
		}
		return f(v, x, y, d.GetCurrentEventState())
	}
	h.onSelection = func(model uintptr, _, _ uint32, _ uintptr) {
		if f := h.selection[model]; f != nil {
			f()
		}
	}
	h.onNotify = func(obj, _, _ uintptr) {
		if f := h.notified[obj]; f != nil {
			f()
		}
	}
	h.onScroll = func(c gtk.EventControllerScroll, _, dy float64) bool {
		f := h.scrolled[c.GoPointer()]
		if f == nil || c.GetCurrentEventState()&gdk.ControlMaskValue == 0 {
			return false
		}
		f(dy)
		return true
	}
	h.onAsync = func(_, result, id uintptr) {
		if f := h.async[id]; f != nil {
			delete(h.async, id)
			f(result)
		}
	}
	h.onTimer = func(id uintptr) bool {
		if f := h.timers[id]; f != nil {
			delete(h.timers, id)
			f()
		}
		return false
	}
	return h
}

// The connections: each registers f for the object and connects the
// shared handler. forget drops the registrations of objects that are gone.

func (h *hub) click(b *gtk.Button, f func()) {
	h.clicked[b.GoPointer()] = f
	b.ConnectClicked(&h.onClicked)
}

func (h *hub) toggle(b *gtk.ToggleButton, f func(bool)) {
	h.toggled[b.GoPointer()] = f
	b.ConnectToggled(&h.onToggled)
}

// check is toggle for a check button.
func (h *hub) check(b *gtk.CheckButton, f func(bool)) {
	h.toggled[b.GoPointer()] = f
	b.ConnectToggled(&h.onChecked)
}

// key adds a key controller to w, in the capture phase when capture.
func (h *hub) key(w *gtk.Widget, capture bool, f func(uint32, gdk.ModifierType) bool) uintptr {
	c := gtk.NewEventControllerKey()
	if capture {
		c.SetPropagationPhase(gtk.PhaseCaptureValue)
	}
	h.keys[c.GoPointer()] = f
	c.ConnectKeyPressed(&h.onKey)
	w.AddController(&c.EventController)
	return c.GoPointer()
}

func (h *hub) factory(setup, bind, teardown func(uintptr)) *gtk.SignalListItemFactory {
	f := gtk.NewSignalListItemFactory()
	p := f.GoPointer()
	h.setup[p], h.bind[p], h.teardown[p] = setup, bind, teardown
	f.ConnectSetup(&h.onSetup)
	f.ConnectBind(&h.onBind)
	f.ConnectTeardown(&h.onTeardown)
	return f
}

// enter runs f when Enter is pressed in an entry (a GtkEntry, a
// GtkPasswordEntry: any widget with an activate signal).
func (h *hub) enter(e gobject.Ptr, f func()) {
	h.entered[e.GoPointer()] = f
	gobject.SignalConnect(e.GoPointer(), "activate", glib.NewCallback(&h.onEntered))
}

func (h *hub) edit(editable gobject.Ptr, f func()) {
	h.changed[editable.GoPointer()] = f
	gobject.SignalConnect(editable.GoPointer(), "changed", glib.NewCallback(&h.onChanged))
}

// press adds a click gesture for any button to w; in the capture phase
// when capture, so it sees clicks its children take.
func (h *hub) press(w *gtk.Widget, capture bool, f func(button uint32, n int32, x, y float64)) uintptr {
	g := gtk.NewGestureClick()
	g.SetButton(0)
	if capture {
		g.SetPropagationPhase(gtk.PhaseCaptureValue)
	}
	h.pressed[g.GoPointer()] = f
	g.ConnectPressed(&h.onPressed)
	w.AddController(&g.EventController)
	return g.GoPointer()
}

// dragSource lets w be dragged from; f gives the content for a drag
// starting at x, y, or 0 for none.
func (h *hub) dragSource(w *gtk.Widget, f func(x, y float64) uintptr) uintptr {
	d := gtk.NewDragSource()
	d.SetActions(gdk.ActionCopyValue | gdk.ActionMoveValue | gdk.ActionLinkValue)
	h.prepare[d.GoPointer()] = f
	d.ConnectPrepare(&h.onPrepare)
	w.AddController(&d.EventController)
	return d.GoPointer()
}

// dropTarget takes files dropped on w.
func (h *hub) dropTarget(w *gtk.Widget, f func(v *gobject.Value, x, y float64, state gdk.ModifierType) bool) uintptr {
	d := gtk.NewDropTarget(gdk.FileListGLibType(), gdk.ActionCopyValue|gdk.ActionMoveValue)
	h.dropped[d.GoPointer()] = f
	d.ConnectDrop(&h.onDrop)
	w.AddController(&d.EventController)
	return d.GoPointer()
}

// notify runs f when obj's property changes; one property an object.
func (h *hub) notify(obj gobject.Ptr, property string, f func()) {
	h.notified[obj.GoPointer()] = f
	gtkx.ConnectNotify(obj, property, &h.onNotify)
}

// ctrlScroll runs f for each step of the wheel turned over w with Ctrl
// held (dy below zero is up); without Ctrl the view scrolls as usual.
func (h *hub) ctrlScroll(w *gtk.Widget, f func(dy float64)) uintptr {
	c := gtk.NewEventControllerScroll(gtk.EventControllerScrollVerticalValue | gtk.EventControllerScrollDiscreteValue)
	h.scrolled[c.GoPointer()] = f
	c.ConnectScroll(&h.onScroll)
	w.AddController(&c.EventController)
	return c.GoPointer()
}

func (h *hub) onSelectionChanged(model gobject.Ptr, f func()) {
	h.selection[model.GoPointer()] = f
	gobject.SignalConnect(model.GoPointer(), "selection-changed", glib.NewCallback(&h.onSelection))
}

// later runs f after ms milliseconds on the main loop.
func (h *hub) later(ms uint32, f func()) {
	h.next++
	h.timers[h.next] = f
	glib.TimeoutAdd(ms, &h.onTimer, h.next)
}

// callback registers f for one async result and returns its user data.
func (h *hub) callback(f func(result uintptr)) uintptr {
	h.next++
	h.async[h.next] = f
	return h.next
}

func (h *hub) forget(ptrs []uintptr) {
	for _, p := range ptrs {
		delete(h.clicked, p)
		delete(h.toggled, p)
		delete(h.keys, p)
		delete(h.activated, p)
		delete(h.setup, p)
		delete(h.bind, p)
		delete(h.teardown, p)
		delete(h.entered, p)
		delete(h.changed, p)
		delete(h.pressed, p)
		delete(h.closing, p)
		delete(h.prepare, p)
		delete(h.dropped, p)
		delete(h.selection, p)
		delete(h.notified, p)
		delete(h.scrolled, p)
	}
}
