package gtkx

import (
	"sync"
	"unsafe"

	"codeberg.org/puregotk/purego"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gobject/types"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// Canvas is what draws a widget of its own (InuaCanvas) and hears its
// size: Snapshot draws it with GSK nodes, Allocate (may be nil) is told
// its new size. GTK has no plain widget taking a snapshot function; this
// subclasses GtkWidget once, from Go.
type Canvas struct {
	Snapshot func(w *gtk.Widget, s *gtk.Snapshot)
	Allocate func(w *gtk.Widget, width, height int)
	// Min and Natural are its size each way, in pixels, when set: a
	// canvas wants none of its own otherwise.
	Min, Natural int
}

var canvases struct {
	once sync.Once
	t    types.GType
	mu   sync.Mutex
	by   map[uintptr]*Canvas
	// GObject's calls puregotk does not bind in a form purego can make.
	typeQuery func(t types.GType, q *gobject.TypeQuery)
	newObject func(t types.GType, n uint32, names, values uintptr) uintptr
	// The class's callbacks live as long as the type.
	classInit gobject.ClassInitFunc
	instInit  gobject.InstanceInitFunc
}

func canvasType() types.GType {
	c := &canvases
	c.once.Do(func() {
		h, err := Open("libgobject-2.0.so.0")
		if err != nil {
			panic("gtkx: " + err.Error())
		}
		purego.RegisterLibFunc(&c.typeQuery, h, "g_type_query")
		purego.RegisterLibFunc(&c.newObject, h, "g_object_new_with_properties")
		c.by = map[uintptr]*Canvas{}
		parent := gtk.WidgetGLibType()
		var q gobject.TypeQuery
		c.typeQuery(parent, &q)
		c.classInit = func(tc *gobject.TypeClass, _ uintptr) {
			wc := (*gtk.WidgetClass)(unsafe.Pointer(tc))
			wc.OverrideSnapshot(func(w *gtk.Widget, s *gtk.Snapshot) {
				if d := canvasOf(w); d != nil && d.Snapshot != nil {
					d.Snapshot(w, s)
				}
			})
			wc.OverrideMeasure(func(w *gtk.Widget, _ gtk.Orientation, _ int32, min, nat, minBase, natBase *int32) {
				*min, *nat, *minBase, *natBase = 0, 0, -1, -1
				if d := canvasOf(w); d != nil {
					*min, *nat = int32(d.Min), int32(max(d.Natural, d.Min))
				}
			})
			wc.OverrideSizeAllocate(func(w *gtk.Widget, width, height, _ int32) {
				if d := canvasOf(w); d != nil && d.Allocate != nil {
					d.Allocate(w, int(width), int(height))
				}
			})
		}
		c.instInit = func(*gobject.TypeInstance, *gobject.TypeClass) {}
		c.t = gobject.TypeRegisterStaticSimple(parent, "InuaCanvas", q.ClassSize, &c.classInit, q.InstanceSize, &c.instInit, 0)
	})
	return c.t
}

func canvasOf(w *gtk.Widget) *Canvas {
	canvases.mu.Lock()
	defer canvases.mu.Unlock()
	return canvases.by[w.GoPointer()]
}

// NewCanvas makes a widget drawn by d; the caller owns its floating
// reference, as with any new widget, and calls ForgetCanvas when done.
func NewCanvas(d *Canvas) *gtk.Widget {
	ptr := canvases.newObject(canvasType(), 0, 0, 0)
	canvases.mu.Lock()
	canvases.by[ptr] = d
	canvases.mu.Unlock()
	return gtk.WidgetNewFromInternalPtr(ptr)
}

// ForgetCanvas lets a canvas's drawer go.
func ForgetCanvas(w *gtk.Widget) {
	canvases.mu.Lock()
	delete(canvases.by, w.GoPointer())
	canvases.mu.Unlock()
}
