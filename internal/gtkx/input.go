package gtkx

import (
	"sync"

	"codeberg.org/puregotk/purego"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// The calls an input region needs; puregotk has no cairo_region_create.
var inputRegion struct {
	once    sync.Once
	create  func() uintptr
	rect    func(r *regionRect) uintptr
	destroy func(r uintptr)
	set     func(surface, region uintptr)
}

// regionRect is cairo_rectangle_int_t.
type regionRect struct{ X, Y, Width, Height int32 }

// PassInput lets the pointer through a window to what is under it. The
// window must be realized; GTK keeps the region until it is set again.
func PassInput(w *gtk.Window) { passInput(w, nil) }

// PassInputBut is PassInput but for a rectangle of the window, in its
// coordinates, which keeps the pointer: a card's, around which is only
// its shadow.
func PassInputBut(w *gtk.Window, x, y, width, height int) {
	passInput(w, &regionRect{int32(x), int32(y), int32(width), int32(height)})
}

func passInput(w *gtk.Window, keep *regionRect) {
	p := &inputRegion
	p.once.Do(func() {
		cairo, err := Open("libcairo.so.2")
		if err != nil {
			panic("gtkx: " + err.Error())
		}
		gtk4, err := Open("libgtk-4.so.1")
		if err != nil {
			panic("gtkx: " + err.Error())
		}
		purego.RegisterLibFunc(&p.create, cairo, "cairo_region_create")
		purego.RegisterLibFunc(&p.rect, cairo, "cairo_region_create_rectangle")
		purego.RegisterLibFunc(&p.destroy, cairo, "cairo_region_destroy")
		purego.RegisterLibFunc(&p.set, gtk4, "gdk_surface_set_input_region")
	})
	surface := w.GetSurface()
	if surface == nil {
		return
	}
	var r uintptr
	if keep != nil {
		r = p.rect(keep)
	} else {
		r = p.create()
	}
	p.set(surface.GoPointer(), r)
	p.destroy(r)
}
