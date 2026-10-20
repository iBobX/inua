package gtkx

import (
	"sync"
	"unsafe"

	"codeberg.org/puregotk/purego"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// GTK's calls puregotk binds in a form purego cannot fill: a char** out
// argument as a Go *string comes back empty.
var preedit struct {
	once sync.Once
	get  func(im uintptr, str *unsafe.Pointer, attrs uintptr, cursor *int32)
	free func(p unsafe.Pointer)
}

// PreeditString is what an input method is composing, and its cursor in
// it, in characters.
func PreeditString(im *gtk.IMContext) (string, int) {
	p := &preedit
	p.once.Do(func() {
		h, err := purego.Dlopen("libgtk-4.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			panic("gtkx: " + err.Error())
		}
		purego.RegisterLibFunc(&p.get, h, "gtk_im_context_get_preedit_string")
		purego.RegisterLibFunc(&p.free, h, "g_free")
	})
	var str unsafe.Pointer
	var cursor int32
	p.get(im.GoPointer(), &str, 0, &cursor)
	if str == nil {
		return "", 0
	}
	defer p.free(str)
	n := 0
	for *(*byte)(unsafe.Add(str, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(str), n)), int(cursor)
}
