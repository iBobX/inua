package gtkx

import (
	"sync"

	"codeberg.org/puregotk/purego"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// GDK's Wayland calls, which puregotk does not bind.
var wl struct {
	once  sync.Once
	ok    bool
	typ   func() uintptr
	isA   func(instance, typ uintptr) bool
	setID func(toplevel uintptr, id string) bool
}

// SetAppID gives a window an application ID of its own on Wayland (the
// compositor's class for it), in place of its application's: Hyprland's
// rules then tell it from the app's other windows. GDK passes it on only
// once the window's toplevel is made, so call it right after Present:
// the compositor maps the window, and applies its rules, at its first
// frame. Elsewhere (X11, Broadway) it does nothing.
func SetAppID(w *gtk.Window, id string) {
	wl.once.Do(func() {
		gtk, err := purego.Dlopen("libgtk-4.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		gobj, err := purego.Dlopen("libgobject-2.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		if _, err := purego.Dlsym(gtk, "gdk_wayland_toplevel_set_application_id"); err != nil {
			return // built without Wayland
		}
		purego.RegisterLibFunc(&wl.typ, gtk, "gdk_wayland_toplevel_get_type")
		purego.RegisterLibFunc(&wl.setID, gtk, "gdk_wayland_toplevel_set_application_id")
		purego.RegisterLibFunc(&wl.isA, gobj, "g_type_check_instance_is_a")
		wl.ok = true
	})
	if !wl.ok {
		return
	}
	w.Widget.Realize() // the widget's: it makes the surface (Window.Realize is GtkNative's)
	s := w.GetSurface()
	if s == nil {
		return
	}
	defer s.Unref() // GetSurface takes a reference
	if wl.isA(s.GoPointer(), wl.typ()) {
		wl.setID(s.GoPointer(), id)
	}
}
