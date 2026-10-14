package appui

import (
	"time"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// secretClearAfter is how long a copied secret stays on the clipboard.
const secretClearAfter = 45 * time.Second

// secretClip copies secrets (Pass's values, GPG's decrypted text): with
// the password manager's hint, which clipboard histories (inua's among
// them) leave out, and taken off again after a while if still there.
type secretClip struct {
	gen     int
	onClear glib.SourceFunc // outlives the copy, as every callback does
}

// copy puts value on win's clipboard.
func (c *secretClip) copy(win *gtk.Widget, value string) {
	if value == "" {
		return
	}
	clip := win.GetClipboard()
	defer clip.Unref()
	data := []byte(value)
	providers := []uintptr{
		gdk.NewContentProviderForBytes("text/plain;charset=utf-8", glib.NewBytes(data, uint(len(data)))).GoPointer(),
		gdk.NewContentProviderForBytes("text/plain", glib.NewBytes(data, uint(len(data)))).GoPointer(),
		gdk.NewContentProviderForBytes("x-kde-passwordManagerHint", glib.NewBytes([]byte("secret"), 6)).GoPointer(),
	}
	union := gdk.NewContentProviderUnion(uintptr(unsafe.Pointer(&providers[0])), uint(len(providers)))
	clip.SetContent(union)
	union.Unref()
	c.gen++
	gen := c.gen
	c.onClear = func(uintptr) bool {
		if gen == c.gen {
			cb := win.GetClipboard()
			if cb.IsLocal() { // still ours: nothing else was copied since
				cb.SetContent(&gdk.ContentProvider{})
			}
			cb.Unref()
		}
		return false
	}
	glib.TimeoutAdd(uint32(secretClearAfter/time.Millisecond), &c.onClear, 0)
}
