package gtkx

import (
	"time"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
)

// SecretClearAfter is how long a copied secret stays on the clipboard.
const SecretClearAfter = 45 * time.Second

// SecretClip copies secrets (Pass's values, GPG's decrypted text): with
// the password manager's hint, which clipboard histories (inua's among
// them) leave out, and taken off again after a while if still there. The
// clipboard is the display's, so a window closed since does not matter.
type SecretClip struct {
	gen     uintptr
	onClear glib.SourceFunc // one for every copy: see the package comment
}

// Copy puts value on the clipboard.
func (c *SecretClip) Copy(value string) {
	if value == "" {
		return
	}
	clip := gdk.DisplayGetDefault().GetClipboard()
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
	if c.onClear == nil {
		c.onClear = func(gen uintptr) bool {
			if gen == c.gen {
				cb := gdk.DisplayGetDefault().GetClipboard()
				if cb.IsLocal() { // still ours: nothing else was copied since
					cb.SetContent(&gdk.ContentProvider{})
				}
				cb.Unref()
			}
			return false
		}
	}
	glib.TimeoutAdd(uint32(SecretClearAfter/time.Millisecond), &c.onClear, c.gen)
}
