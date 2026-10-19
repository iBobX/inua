// Package termui is inua-term's window (docs/feat/inua-term.md). This is
// the first spike: one pane, a widget of its own drawing the engine's
// screen in snapshot.
package termui

import (
	"sync"
	"unsafe"

	"codeberg.org/puregotk/purego"
	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gobject/types"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"
)

// GObject's own calls that puregotk does not bind in a form purego can
// make: g_type_query, and g_object_new without varargs.
var gobj struct {
	once      sync.Once
	typeQuery func(t types.GType, q *gobject.TypeQuery)
	newObject func(t types.GType, n uint32, names, values uintptr) uintptr
}

func loadGObject() {
	gobj.once.Do(func() {
		h, err := purego.Dlopen("libgobject-2.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			panic("termui: " + err.Error())
		}
		purego.RegisterLibFunc(&gobj.typeQuery, h, "g_type_query")
		purego.RegisterLibFunc(&gobj.newObject, h, "g_object_new_with_properties")
	})
}

// paneDrawer is what draws a pane and hears its size: a Go value, found
// by the widget's pointer.
type paneDrawer interface {
	snapshot(w *gtk.Widget, s *gtk.Snapshot)
	allocate(w *gtk.Widget, width, height int)
}

var panes struct {
	once sync.Once
	t    types.GType
	mu   sync.Mutex
	by   map[uintptr]paneDrawer
	// The class's callbacks live as long as the type.
	classInit gobject.ClassInitFunc
	instInit  gobject.InstanceInitFunc
}

// paneType registers InuaTermPane, a GtkWidget whose snapshot and
// size_allocate are its drawer's.
func paneType() types.GType {
	panes.once.Do(func() {
		loadGObject()
		panes.by = map[uintptr]paneDrawer{}
		parent := gtk.WidgetGLibType()
		var q gobject.TypeQuery
		gobj.typeQuery(parent, &q)
		panes.classInit = func(c *gobject.TypeClass, _ uintptr) {
			wc := (*gtk.WidgetClass)(unsafe.Pointer(c))
			wc.OverrideSnapshot(func(w *gtk.Widget, s *gtk.Snapshot) {
				if d := drawerOf(w); d != nil {
					d.snapshot(w, s)
				}
			})
			wc.OverrideSizeAllocate(func(w *gtk.Widget, width, height, _ int32) {
				if d := drawerOf(w); d != nil {
					d.allocate(w, int(width), int(height))
				}
			})
		}
		panes.instInit = func(*gobject.TypeInstance, *gobject.TypeClass) {}
		panes.t = gobject.TypeRegisterStaticSimple(parent, "InuaTermPane", q.ClassSize, &panes.classInit, q.InstanceSize, &panes.instInit, 0)
	})
	return panes.t
}

func drawerOf(w *gtk.Widget) paneDrawer {
	panes.mu.Lock()
	defer panes.mu.Unlock()
	return panes.by[w.GoPointer()]
}

// newPaneWidget makes an InuaTermPane drawn by d; the caller owns its
// floating reference, as with any new widget.
func newPaneWidget(d paneDrawer) *gtk.Widget {
	ptr := gobj.newObject(paneType(), 0, 0, 0)
	panes.mu.Lock()
	panes.by[ptr] = d
	panes.mu.Unlock()
	return gtk.WidgetNewFromInternalPtr(ptr)
}

func forgetPaneWidget(w *gtk.Widget) {
	panes.mu.Lock()
	delete(panes.by, w.GoPointer())
	panes.mu.Unlock()
}

// Drawing helpers: GSK takes these by pointer.
func rect(x, y, w, h float32) *graphene.Rect {
	var r graphene.Rect
	r.Init(x, y, w, h)
	return &r
}

func rgba(hex string) gdk.RGBA {
	var c gdk.RGBA
	if len(hex) == 7 && hex[0] == '#' {
		v := func(i int) float32 {
			var n int
			for _, ch := range hex[i : i+2] {
				n <<= 4
				switch {
				case ch >= '0' && ch <= '9':
					n |= int(ch - '0')
				case ch >= 'a' && ch <= 'f':
					n |= int(ch-'a') + 10
				case ch >= 'A' && ch <= 'F':
					n |= int(ch-'A') + 10
				}
			}
			return float32(n) / 255
		}
		c = gdk.RGBA{Red: v(1), Green: v(3), Blue: v(5), Alpha: 1}
	}
	return c
}

// cellSize is a monospace font's cell in pixels.
func cellSize(w *gtk.Widget, font *pango.FontDescription) (cw, ch int) {
	l := w.CreatePangoLayout("M")
	l.SetFontDescription(font)
	var x, y int32
	l.GetPixelSize(&x, &y)
	l.Unref()
	return max(int(x), 1), max(int(y), 1)
}
