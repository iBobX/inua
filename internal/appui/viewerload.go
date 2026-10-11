package appui

import (
	"errors"
	"os"
	"time"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gdkpixbuf"
	"codeberg.org/puregotk/puregotk/v4/glib"

	"github.com/lyricat/inua/internal/apps/viewer"
	"github.com/lyricat/inua/internal/gtkx"
)

// The Viewer's loading. Each image is a shot, kept while it is shown or
// beside the one shown, so stepping back and forth shows it at once. A
// shot loads off the main thread in steps, each shown as it comes: first
// a preview (a smaller decode with libjpeg-turbo, or the photo's EXIF
// thumbnail), then the whole image. The pixels go into memory GTK takes
// over, and textures are made off the main thread too; orientation and
// turns are left to the transform that draws them.

// shot is one image, loading or loaded.
type shot struct {
	path         string
	tex          *gdk.Texture // what can be drawn; nil until something is
	texW, texH   int
	full         bool // tex is the whole image, not a preview
	anim         *gdkpixbuf.PixbufAnimation
	fullW, fullH int // its pixels as stored; 0 until known
	orient       int // its EXIF orientation, applied when drawn
	exif         viewer.Exif
	size         int64
	mod          time.Time
	err          error
	loading      bool
	dropped      bool // let go of while loading: its results are thrown away
}

// loads is how many images load at once.
const loads = 2

// shotFor is the cached shot for a file, made if there is none.
func (a *viewerApp) shotFor(path string) *shot {
	if a.cache == nil {
		a.cache = map[string]*shot{}
		a.work = make(chan struct{}, loads)
	}
	s := a.cache[path]
	if s == nil {
		s = &shot{path: path}
		a.cache[path] = s
	}
	return s
}

// keep lets go of every shot but those of these files.
func (a *viewerApp) keep(paths ...string) {
	for p, s := range a.cache {
		if !containsString(paths, p) {
			a.drop(s)
		}
	}
}

func (a *viewerApp) drop(s *shot) {
	delete(a.cache, s.path)
	s.dropped = true
	if s.loading {
		return // freed when its load ends
	}
	s.free()
}

func (s *shot) free() {
	if s.tex != nil {
		s.tex.Unref()
		s.tex = nil
	}
	if s.anim != nil {
		s.anim.Unref()
		s.anim = nil
	}
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// load starts loading a shot unless it has all it needs or is loading.
func (a *viewerApp) load(s *shot) {
	if s.loading || s.full || s.anim != nil || s.err != nil {
		return
	}
	s.loading = true
	tw, th := a.target()
	havePreview := s.tex != nil
	go func() {
		a.work <- struct{}{}
		defer func() { <-a.work }()
		path := s.path

		// What the file is: its size, EXIF, and the image's size.
		var size int64
		var mod time.Time
		if st, err := os.Stat(path); err == nil {
			size, mod = st.Size(), st.ModTime()
		}
		ex, _ := viewer.ReadExif(path)
		w, h, _ := viewer.ImageSize(path)
		gtkx.Post(func() {
			s.size, s.mod, s.exif, s.orient = size, mod, ex, ex.Orientation
			if w > 0 && s.fullW == 0 {
				s.fullW, s.fullH = w, h
			}
			a.shotChanged(s)
		})

		// A preview of a big photo, quick to make.
		if !havePreview && viewer.IsJPEG(path) && w > 0 {
			fit := viewer.FitZoom(float64(w), float64(h), tw, th)
			if viewer.TurboAvailable() && fit < 0.5 {
				if data, err := os.ReadFile(path); err == nil {
					if px, _, _, err := viewer.DecodeJPEGTurbo(data, fit, cAlloc{}); err == nil {
						a.post(s, texture(px), px.W, px.H, false, nil, nil)
					}
				}
			} else if len(ex.Thumb) > 0 && fit < 1 {
				if px, err := viewer.DecodeThumb(ex.Thumb, cAlloc{}); err == nil {
					a.post(s, texture(px), px.W, px.H, false, nil, nil)
				}
			}
		}

		// The whole image.
		tex, fw, fh, anim, err := decodeFull(path)
		a.post(s, tex, fw, fh, true, anim, err)
	}()
}

// target is the stage's size in screen pixels, which a preview should
// fill: the shown image's stage, or a guess before it is laid out.
func (a *viewerApp) target() (float64, float64) {
	s := a.scale()
	vw, vh := a.view()
	if vw <= 0 || vh <= 0 {
		vw, vh = 1600, 1000
	}
	return vw * s, vh * s
}

// decodeFull reads a whole image: in Go (with libjpeg-turbo for JPEG when
// it is installed) for the common formats, else with GDK; an animation
// comes back to be played.
func decodeFull(path string) (tex *gdk.Texture, w, h int, anim *gdkpixbuf.PixbufAnimation, err error) {
	if viewer.GoDecodes(path) {
		px, err := viewer.Decode(path, cAlloc{})
		if err == nil {
			return texture(px), px.W, px.H, nil, nil
		}
		if !errors.Is(err, viewer.ErrAnimated) && !viewer.Animated(path) {
			return nil, 0, 0, nil, err
		}
	}
	if viewer.Animated(path) {
		if a, err := gdkpixbuf.NewPixbufAnimationFromFile(path); err == nil {
			if !a.IsStaticImage() {
				return nil, 0, 0, a, nil
			}
			a.Unref()
		}
	}
	pb, err := gdkpixbuf.NewPixbufFromFile(path)
	if err != nil {
		return nil, 0, 0, nil, err
	}
	defer pb.Unref()
	if o := pb.ApplyEmbeddedOrientation(); o != nil { // GDK's formats: turned here
		defer o.Unref()
		pb = o
	}
	return gdk.NewTextureForPixbuf(pb), int(pb.GetWidth()), int(pb.GetHeight()), nil, nil
}

// post hands a step's result to the main thread.
func (a *viewerApp) post(s *shot, tex *gdk.Texture, w, h int, full bool, anim *gdkpixbuf.PixbufAnimation, err error) {
	gtkx.Post(func() {
		if full {
			s.loading = false
		}
		if s.dropped {
			if tex != nil {
				tex.Unref()
			}
			if anim != nil {
				anim.Unref()
			}
			if full {
				s.free()
			}
			return
		}
		switch {
		case err != nil:
			if s.tex == nil {
				s.err = err
			}
		case anim != nil:
			s.anim = anim
			s.fullW, s.fullH, s.orient = int(anim.GetWidth()), int(anim.GetHeight()), 1
			s.full = true
		case tex != nil:
			if s.tex != nil {
				s.tex.Unref()
			}
			s.tex, s.texW, s.texH, s.full = tex, w, h, full
			if full && !viewer.GoDecodes(s.path) {
				s.fullW, s.fullH, s.orient = w, h, 1 // GDK turned it already
			} else if s.fullW == 0 {
				s.fullW, s.fullH = w, h
			}
		}
		a.shotChanged(s)
	})
}

// texture makes a texture of pixels, taking over their buffer.
func texture(px viewer.Pixels) *gdk.Texture {
	format := map[viewer.Format]gdk.MemoryFormat{
		viewer.RGB:         gdk.MemoryR8g8b8Value,
		viewer.RGBA:        gdk.MemoryR8g8b8a8Value,
		viewer.RGBAPremult: gdk.MemoryR8g8b8a8PremultipliedValue,
		viewer.Gray:        gdk.MemoryG8Value,
	}[px.Format]
	b := glib.NewBytesTake(uintptr(unsafe.Pointer(&px.Buf[0])), uint(len(px.Buf)))
	tex := gdk.NewMemoryTexture(int32(px.W), int32(px.H), format, b, uint(px.Stride))
	b.Unref()
	return &tex.Texture
}

// cAlloc allocates pixel buffers with GLib, so a texture takes one over
// without a copy.
type cAlloc struct{}

func (cAlloc) Alloc(n int) []byte {
	p := glib.Malloc(uint(n))
	return unsafe.Slice((*byte)(unsafe.Add(unsafe.Pointer(nil), p)), n)
}

func (cAlloc) Free(b []byte) {
	if len(b) > 0 {
		glib.Free(uintptr(unsafe.Pointer(&b[0])))
	}
}
