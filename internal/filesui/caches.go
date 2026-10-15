package filesui

import (
	"os"
	"path/filepath"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// thumbs keeps the grid's thumbnails as textures, the most recent
// maxThumbs of them, and makes the missing ones a few at a time.
type thumbs struct {
	tex     map[string]thumb
	order   []string // oldest first
	pending map[string]bool
	failed  map[string]bool
	slots   chan struct{}
}

type thumb struct {
	tex *gdk.MemoryTexture
	mod time.Time
}

const maxThumbs = 600

func newThumbs() *thumbs {
	return &thumbs{tex: map[string]thumb{}, pending: map[string]bool{}, failed: map[string]bool{}, slots: make(chan struct{}, 3)}
}

// get returns path's thumbnail when it is ready; otherwise it starts
// making it, puts it in the rows showing it when done, and returns nil.
func (t *thumbs) get(path string, e *files.Entry, p *pane) *gdk.MemoryTexture {
	if e.Dir || !files.CanThumbnail(e.Name) || t.failed[path] {
		return nil
	}
	if th, ok := t.tex[path]; ok && th.mod.Equal(e.ModTime) {
		return th.tex
	}
	if t.pending[path] {
		return nil
	}
	t.pending[path] = true
	mod := e.ModTime
	go func() {
		t.slots <- struct{}{}
		img, err := files.Thumbnail(path, mod)
		<-t.slots
		gtkx.Post(func() {
			delete(t.pending, path)
			if err != nil {
				t.failed[path] = true
				return
			}
			b := img.Bounds()
			bytes := glib.NewBytes(img.Pix, uint(len(img.Pix)))
			tex := gdk.NewMemoryTexture(int32(b.Dx()), int32(b.Dy()), gdk.MemoryR8g8b8a8PremultipliedValue, bytes, uint(img.Stride))
			bytes.Unref()
			t.put(path, thumb{tex, mod})
			p.w.app.updateRows(path, func(r *row) {
				if r.grid {
					r.icon.SetFromPaintable(tex)
				}
			})
		})
	}()
	return nil
}

func (t *thumbs) put(path string, th thumb) {
	if old, ok := t.tex[path]; ok {
		old.tex.Unref()
	} else {
		t.order = append(t.order, path)
	}
	t.tex[path] = th
	for len(t.order) > maxThumbs {
		oldest := t.order[0]
		t.order = t.order[1:]
		if old, ok := t.tex[oldest]; ok {
			old.tex.Unref() // an image showing it keeps its own reference
			delete(t.tex, oldest)
		}
	}
}

// counts are how many items folders hold, for the list's size column,
// counted on goroutines a few at a time.
type counts struct {
	n       map[string]int
	pending map[string]bool
	slots   chan struct{}
}

func newCounts() *counts {
	return &counts{n: map[string]int{}, pending: map[string]bool{}, slots: make(chan struct{}, 4)}
}

// text is the folder's count as the list shows it, or "" while it is
// counted (it is then put in the rows showing it).
func (c *counts) text(path string, p *pane) string {
	if n, ok := c.n[path]; ok {
		return countText(n)
	}
	if !c.pending[path] {
		c.pending[path] = true
		go func() {
			c.slots <- struct{}{}
			n := -1
			if f, err := os.Open(path); err == nil {
				names, _ := f.Readdirnames(-1)
				f.Close()
				n = len(names)
			}
			<-c.slots
			gtkx.Post(func() {
				delete(c.pending, path)
				c.n[path] = n
				p.w.app.updateRows(path, func(r *row) {
					if !r.grid {
						r.size.SetText(countText(n))
					}
				})
			})
		}()
	}
	return ""
}

func countText(n int) string {
	if n < 0 {
		return "—"
	}
	return i18n.Nf("%d item", "%d items", n, n)
}

// forget drops the counts of dir's folders, which a change in dir may
// have made stale.
func (c *counts) forget(dir string) {
	for k := range c.n {
		if filepath.Dir(k) == dir {
			delete(c.n, k)
		}
	}
}
