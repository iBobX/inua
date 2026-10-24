package gtkx

import (
	"sync"

	"codeberg.org/puregotk/purego"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// The calls an empty input region needs; puregotk has no cairo_region_create.
var inputRegion struct {
	once    sync.Once
	create  func() uintptr
	destroy func(r uintptr)
	set     func(surface, region uintptr)
}

// PassInput lets the pointer through a window to what is under it. The
// window must be realized; GTK keeps the region until it is set again.
func PassInput(w *gtk.Window) {
	p := &inputRegion
	p.once.Do(func() {
		cairo, err := purego.Dlopen("libcairo.so.2", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			panic("gtkx: " + err.Error())
		}
		gtk4, err := purego.Dlopen("libgtk-4.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			panic("gtkx: " + err.Error())
		}
		purego.RegisterLibFunc(&p.create, cairo, "cairo_region_create")
		purego.RegisterLibFunc(&p.destroy, cairo, "cairo_region_destroy")
		purego.RegisterLibFunc(&p.set, gtk4, "gdk_surface_set_input_region")
	})
	surface := w.GetSurface()
	if surface == nil {
		return
	}
	r := p.create()
	p.set(surface.GoPointer(), r)
	p.destroy(r)
}
