package mailui

import (
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// hub holds one signal handler of each kind and sends each signal to the
// Go function registered for the object that emitted it: purego makes a
// native trampoline for every Go function connected, never freed, and the
// window makes rows, cards and composers again and again.
type hub struct {
	clicked  map[uintptr]func()
	keys     map[uintptr]func(keyval uint32, state gdk.ModifierType) bool
	setup    map[uintptr]func(item uintptr)
	bind     map[uintptr]func(item uintptr)
	teardown map[uintptr]func(item uintptr)
	changed  map[uintptr]func()
	entered  map[uintptr]func()
	closing  map[uintptr]func() bool
	selected map[uintptr]func()
	rows     map[uintptr]func(row uintptr)
	notified map[uintptr]func()
	dropped  map[uintptr]func(paths []string)
	async    map[uintptr]func(result uintptr)
	timers   map[uintptr]func()
	next     uintptr

	onClicked   func(gtk.Button)
	onKey       func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onSetup     func(gtk.SignalListItemFactory, uintptr)
	onBind      func(gtk.SignalListItemFactory, uintptr)
	onTeardown  func(gtk.SignalListItemFactory, uintptr)
	onChanged   func(uintptr, uintptr)
	onEntered   func(uintptr, uintptr)
	onClose     func(gtk.Window) bool
	onSelection func(uintptr, uint32, uint32, uintptr)
	onRow       func(uintptr, uintptr, uintptr)
	onNotify    func(uintptr, uintptr, uintptr)
	onDrop      func(gtk.DropTarget, uintptr, float64, float64) bool
	onAsync     gio.AsyncReadyCallback
	onTimer     glib.SourceFunc
}

func newHub() *hub {
	h := &hub{
		clicked: map[uintptr]func(){}, keys: map[uintptr]func(uint32, gdk.ModifierType) bool{},
		setup: map[uintptr]func(uintptr){}, bind: map[uintptr]func(uintptr){}, teardown: map[uintptr]func(uintptr){},
		changed: map[uintptr]func(){}, entered: map[uintptr]func(){}, closing: map[uintptr]func() bool{},
		selected: map[uintptr]func(){}, rows: map[uintptr]func(uintptr){}, notified: map[uintptr]func(){}, dropped: map[uintptr]func([]string){},
		async: map[uintptr]func(uintptr){}, timers: map[uintptr]func(){},
	}
	h.onClicked = func(b gtk.Button) {
		if f := h.clicked[b.GoPointer()]; f != nil {
			f()
		}
	}
	h.onKey = func(c gtk.EventControllerKey, keyval, _ uint32, state gdk.ModifierType) bool {
		if f := h.keys[c.GoPointer()]; f != nil {
			return f(keyval, state)
		}
		return false
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
	h.onChanged = func(obj, _ uintptr) {
		if f := h.changed[obj]; f != nil {
			f()
		}
	}
	h.onEntered = func(obj, _ uintptr) {
		if f := h.entered[obj]; f != nil {
			f()
		}
	}
	h.onClose = func(w gtk.Window) bool {
		if f := h.closing[w.GoPointer()]; f != nil {
			return f()
		}
		return false
	}
	h.onSelection = func(model uintptr, _, _ uint32, _ uintptr) {
		if f := h.selected[model]; f != nil {
			f()
		}
	}
	h.onRow = func(list, row, _ uintptr) {
		if f := h.rows[list]; f != nil {
			f(row)
		}
	}
	h.onNotify = func(obj, _, _ uintptr) {
		if f := h.notified[obj]; f != nil {
			f()
		}
	}
	h.onDrop = func(d gtk.DropTarget, _ uintptr, _, _ float64) bool {
		f := h.dropped[d.GoPointer()]
		v := d.GetValue()
		if f == nil || v == nil {
			return false
		}
		paths := droppedPaths(v)
		if len(paths) == 0 {
			return false
		}
		f(paths)
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

func (h *hub) click(b *gtk.Button, f func()) uintptr {
	h.clicked[b.GoPointer()] = f
	b.ConnectClicked(&h.onClicked)
	return b.GoPointer()
}

// key adds a key controller to w; in the capture phase when capture.
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

// edit runs f when an editable (an entry) or a text buffer changes.
func (h *hub) edit(obj gobject.Ptr, f func()) uintptr {
	h.changed[obj.GoPointer()] = f
	gobject.SignalConnect(obj.GoPointer(), "changed", glib.NewCallback(&h.onChanged))
	return obj.GoPointer()
}

// searched runs f when a search entry's text settles.
func (h *hub) searched(e *gtk.SearchEntry, f func()) uintptr {
	h.changed[e.GoPointer()] = f
	gobject.SignalConnect(e.GoPointer(), "search-changed", glib.NewCallback(&h.onChanged))
	return e.GoPointer()
}

// enter runs f when Enter is pressed in an entry.
func (h *hub) enter(e gobject.Ptr, f func()) uintptr {
	h.entered[e.GoPointer()] = f
	gobject.SignalConnect(e.GoPointer(), "activate", glib.NewCallback(&h.onEntered))
	return e.GoPointer()
}

// close runs f when a window is asked to close; true keeps it open.
func (h *hub) close(w *gtk.Window, f func() bool) uintptr {
	h.closing[w.GoPointer()] = f
	w.ConnectCloseRequest(&h.onClose)
	return w.GoPointer()
}

func (h *hub) selection(model gobject.Ptr, f func()) uintptr {
	h.selected[model.GoPointer()] = f
	gobject.SignalConnect(model.GoPointer(), "selection-changed", glib.NewCallback(&h.onSelection))
	return model.GoPointer()
}

// rowSelected runs f with the row a list box selects (0 for none).
func (h *hub) rowSelected(list *gtk.ListBox, f func(row uintptr)) uintptr {
	h.rows[list.GoPointer()] = f
	gobject.SignalConnect(list.GoPointer(), "row-selected", glib.NewCallback(&h.onRow))
	return list.GoPointer()
}

// notify runs f when obj's property changes.
func (h *hub) notify(obj gobject.Ptr, property string, f func()) uintptr {
	h.notified[obj.GoPointer()] = f
	gobject.SignalConnect(obj.GoPointer(), "notify::"+property, glib.NewCallback(&h.onNotify))
	return obj.GoPointer()
}

// drop takes files dropped on w.
func (h *hub) drop(w *gtk.Widget, f func(paths []string)) uintptr {
	d := gtk.NewDropTarget(gdk.FileListGLibType(), gdk.ActionCopyValue)
	h.dropped[d.GoPointer()] = f
	d.ConnectDrop(&h.onDrop)
	w.AddController(&d.EventController)
	return d.GoPointer()
}

// callback registers f for one async result and returns its user data.
func (h *hub) callback(f func(result uintptr)) uintptr {
	h.next++
	h.async[h.next] = f
	return h.next
}

// later runs f after ms milliseconds on the main loop.
func (h *hub) later(ms uint32, f func()) {
	h.next++
	h.timers[h.next] = f
	glib.TimeoutAdd(ms, &h.onTimer, h.next)
}

// forget drops the registrations of objects that are gone.
func (h *hub) forget(ptrs []uintptr) {
	for _, p := range ptrs {
		delete(h.clicked, p)
		delete(h.keys, p)
		delete(h.changed, p)
		delete(h.entered, p)
		delete(h.closing, p)
		delete(h.selected, p)
		delete(h.rows, p)
		delete(h.notified, p)
		delete(h.dropped, p)
	}
}

func droppedPaths(v *gobject.Value) []string {
	boxed := v.GetBoxed()
	if boxed == 0 {
		return nil
	}
	var paths []string
	list := (*gdk.FileList)(unsafe.Add(unsafe.Pointer(nil), boxed))
	for l := list.GetFiles(); l != nil; l = l.Next {
		if path := (&gio.FileBase{Ptr: l.Data}).GetPath(); path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}
