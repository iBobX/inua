package viewer

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	_ "image/png" // formats for image.Decode
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// ErrAnimated is the answer for an animation, which GDK plays.
var ErrAnimated = errors.New("animated")

// GoDecodes reports whether Decode reads a file: the common formats, which
// Go reads at once, where the system's loaders may start a helper process
// for each image. An animated WebP fails in Go and goes to GDK too.
func GoDecodes(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".jpe", ".png", ".bmp", ".tif", ".tiff", ".webp", ".gif":
		return true
	}
	return false
}

// IsJPEG reports whether a file is a JPEG, by its name.
func IsJPEG(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".jpe":
		return true
	}
	return false
}

// Format is how Pixels lays out a pixel; each has a GDK memory format.
type Format int

const (
	RGB         Format = iota // 3 bytes
	RGBA                      // 4 bytes, straight alpha
	RGBAPremult               // 4 bytes, premultiplied alpha
	Gray                      // 1 byte
)

// Pixels are decoded pixels in a buffer from an Allocator.
type Pixels struct {
	W, H, Stride int
	Format       Format
	Buf          []byte
}

// Allocator gives the buffers pixels are decoded into, so they can live
// where the caller wants them (the viewer: memory GTK takes over, with no
// copy). Free returns one that will not be used.
type Allocator interface {
	Alloc(n int) []byte
	Free(b []byte)
}

// GoAllocator allocates in Go's memory.
type GoAllocator struct{}

func (GoAllocator) Alloc(n int) []byte { return make([]byte, n) }
func (GoAllocator) Free([]byte)        {}

// Decode reads an image file into pixels, at full size, as stored: the
// EXIF orientation is left to whoever draws them. A GIF of more than one
// frame is ErrAnimated.
func Decode(path string, al Allocator) (px Pixels, err error) {
	defer recoverMalformed("image", &err)
	data, err := os.ReadFile(path)
	if err != nil {
		return Pixels{}, err
	}
	// A header claiming a huge image is refused before decoding it.
	if c, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil && int64(c.Width)*int64(c.Height) > MaxPixels {
		return Pixels{}, fmt.Errorf("image: %d×%d is too large", c.Width, c.Height)
	}
	if IsJPEG(path) && TurboAvailable() {
		if px, _, _, err := DecodeJPEGTurbo(data, 1, al); err == nil {
			return px, nil
		}
		// A JPEG libjpeg-turbo will not take (CMYK, say): Go tries.
	}
	var src image.Image
	if strings.EqualFold(filepath.Ext(path), ".gif") {
		g, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return Pixels{}, err
		}
		if len(g.Image) != 1 {
			return Pixels{}, ErrAnimated
		}
		src = g.Image[0]
	} else if src, _, err = image.Decode(bytes.NewReader(data)); err != nil {
		return Pixels{}, err
	}
	return Convert(src, al), nil
}

// DecodeThumb reads a small JPEG (an EXIF thumbnail) into pixels.
func DecodeThumb(data []byte, al Allocator) (px Pixels, err error) {
	defer recoverMalformed("thumbnail", &err)
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return Pixels{}, err
	}
	return Convert(img, al), nil
}

// Size reads an image's size from its header, without decoding it.
func ImageSize(path string) (w, h int, err error) {
	defer recoverMalformed("image", &err)
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	c, _, err := image.DecodeConfig(io.LimitReader(f, 1<<20))
	return c.Width, c.Height, err
}

// MaxPixels is the most pixels Viewer decodes: 256 megapixels, a gigabyte
// of RGBA.
const MaxPixels = 256 << 20

// recoverMalformed turns a decoder's panic on a malformed file into an
// error: Viewer opens files from anywhere.
func recoverMalformed(what string, err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("%s: malformed file (%v)", what, r)
	}
}

// Convert lays an image out as Pixels, in bands of rows on every core.
// The common layouts are copied as they are; the rest are converted.
func Convert(src image.Image, al Allocator) Pixels {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	switch img := src.(type) {
	case *image.YCbCr: // JPEG
		px := alloc(al, w, h, 3, RGB)
		bands(h, func(y0, y1 int) {
			for y := y0; y < y1; y++ {
				row := px.Buf[y*px.Stride:]
				for x := range w {
					yi := img.YOffset(b.Min.X+x, b.Min.Y+y)
					ci := img.COffset(b.Min.X+x, b.Min.Y+y)
					r, g, bl := color.YCbCrToRGB(img.Y[yi], img.Cb[ci], img.Cr[ci])
					row[x*3], row[x*3+1], row[x*3+2] = r, g, bl
				}
			}
		})
		return px
	case *image.RGBA: // PNG and others: premultiplied, as GDK takes it
		px := alloc(al, w, h, 4, RGBAPremult)
		copyRows(px, img.Pix, img.Stride, img.PixOffset(b.Min.X, b.Min.Y), w*4, h)
		return px
	case *image.NRGBA:
		px := alloc(al, w, h, 4, RGBA)
		copyRows(px, img.Pix, img.Stride, img.PixOffset(b.Min.X, b.Min.Y), w*4, h)
		return px
	case *image.Gray:
		px := alloc(al, w, h, 1, Gray)
		copyRows(px, img.Pix, img.Stride, img.PixOffset(b.Min.X, b.Min.Y), w, h)
		return px
	case *image.Paletted: // GIF, PNG-8
		pal := make([]color.NRGBA, len(img.Palette))
		for i, c := range img.Palette {
			pal[i] = color.NRGBAModel.Convert(c).(color.NRGBA)
		}
		px := alloc(al, w, h, 4, RGBA)
		bands(h, func(y0, y1 int) {
			for y := y0; y < y1; y++ {
				src := img.Pix[img.PixOffset(b.Min.X, b.Min.Y+y):]
				row := px.Buf[y*px.Stride:]
				for x := range w {
					var c color.NRGBA
					if i := int(src[x]); i < len(pal) {
						c = pal[i]
					}
					row[x*4], row[x*4+1], row[x*4+2], row[x*4+3] = c.R, c.G, c.B, c.A
				}
			}
		})
		return px
	}
	// Anything else (CMYK, 16 bits a channel): through NRGBA, by bands.
	px := alloc(al, w, h, 4, RGBA)
	bands(h, func(y0, y1 int) {
		dst := &image.NRGBA{Pix: px.Buf[y0*px.Stride : y1*px.Stride], Stride: px.Stride, Rect: image.Rect(0, y0, w, y1)}
		draw.Draw(dst, dst.Rect, src, image.Pt(b.Min.X, b.Min.Y+y0), draw.Src)
	})
	return px
}

func alloc(al Allocator, w, h, bpp int, f Format) Pixels {
	return Pixels{W: w, H: h, Stride: w * bpp, Format: f, Buf: al.Alloc(w * h * bpp)}
}

func copyRows(px Pixels, src []byte, stride, off, n, h int) {
	bands(h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			copy(px.Buf[y*px.Stride:y*px.Stride+n], src[off+y*stride:off+y*stride+n])
		}
	})
}

// bands runs f over [0, h) in a band of rows per core.
func bands(h int, f func(y0, y1 int)) {
	n := min(runtime.NumCPU(), max(1, h/64))
	if n <= 1 {
		f(0, h)
		return
	}
	var wg sync.WaitGroup
	step := (h + n - 1) / n
	for y := 0; y < h; y += step {
		wg.Add(1)
		go func(y0, y1 int) {
			defer wg.Done()
			f(y0, y1)
		}(y, min(y+step, h))
	}
	wg.Wait()
}

// Orientation is how to draw an image with an EXIF orientation so it
// stands as it was taken: mirrored left to right first, then turned by
// quarter turns clockwise.
func Orientation(o int) (turns int, mirror bool) {
	switch o {
	case 2:
		return 0, true
	case 3:
		return 2, false
	case 4:
		return 2, true
	case 5:
		return 3, true
	case 6:
		return 1, false
	case 7:
		return 1, true
	case 8:
		return 3, false
	}
	return 0, false
}

// Placement is the offset that keeps a w×h picture, scaled by k, mirrored
// when asked and turned by quarter turns clockwise (about its corner), at
// the origin: the translation to apply after those.
func Placement(w, h, k float64, turns int, mirror bool) (dx, dy float64) {
	fx := k
	if mirror {
		fx = -k
	}
	minX, minY := math64Inf, math64Inf
	for _, c := range [][2]float64{{0, 0}, {w, 0}, {0, h}, {w, h}} {
		x, y := c[0]*fx, c[1]*k
		for range ((turns % 4) + 4) % 4 {
			x, y = -y, x // a quarter turn clockwise, y down
		}
		minX, minY = min(minX, x), min(minY, y)
	}
	return -minX, -minY
}

const math64Inf = 1e308
