package files

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/image/draw"

	"github.com/lyricat/inua/internal/apps/viewer"
)

// Thumbnails follow the freedesktop Thumbnail Managing Standard, so the
// ones Nautilus and others made are used and the ones made here serve
// them too: $XDG_CACHE_HOME/thumbnails/{normal,large}/MD5(URI).png,
// holding Thumb::URI and Thumb::MTime, which says whether it is stale.

// ThumbSize is the largest side of the thumbnails asked for: "large".
const ThumbSize = 256

// CanThumbnail says whether a file is an image Go decodes, by its name.
func CanThumbnail(name string) bool { return viewer.GoDecodes(name) }

func thumbDir() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		base = filepath.Join(Home(), ".cache")
	}
	return filepath.Join(base, "thumbnails", "large")
}

// ThumbPath is where the cache keeps the thumbnail of path.
func ThumbPath(path string) string {
	sum := md5.Sum([]byte(URIOfPath(path)))
	return filepath.Join(thumbDir(), hex.EncodeToString(sum[:])+".png")
}

// Thumbnail returns path's thumbnail, at most ThumbSize on its longer
// side, from the cache when it is there and fresh, else made from the
// image and saved there. The thumbnails folders are never made in the
// trash or the cache itself.
func Thumbnail(path string, mtime time.Time) (*image.RGBA, error) {
	cached := ThumbPath(path)
	if img, ok := readThumb(cached, mtime); ok {
		return img, nil
	}
	img, err := makeThumb(path)
	if err != nil {
		return nil, err
	}
	if !inCache(path) {
		saveThumb(cached, path, mtime, img)
	}
	return img, nil
}

func inCache(path string) bool {
	rel, err := filepath.Rel(filepath.Dir(thumbDir()), path)
	return err == nil && !filepath.IsAbs(rel) && len(rel) > 0 && rel[0] != '.'
}

// readThumb reads a cached thumbnail when its Thumb::MTime is mtime.
func readThumb(cached string, mtime time.Time) (*image.RGBA, bool) {
	data, err := os.ReadFile(cached)
	if err != nil {
		return nil, false
	}
	if pngText(data)["Thumb::MTime"] != strconv.FormatInt(mtime.Unix(), 10) {
		return nil, false
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, false
	}
	return toRGBA(src), true
}

func toRGBA(src image.Image) *image.RGBA {
	if rgba, ok := src.(*image.RGBA); ok {
		return rgba
	}
	b := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), src, b.Min, draw.Src)
	return out
}

// makeThumb decodes an image (a JPEG at the smallest size that will do)
// and scales it down, turned as its EXIF says.
func makeThumb(path string) (*image.RGBA, error) {
	if !viewer.GoDecodes(path) {
		return nil, errors.New("not an image Go decodes")
	}
	var px viewer.Pixels
	var err error
	if viewer.IsJPEG(path) && viewer.TurboAvailable() {
		if w, h, e := viewer.ImageSize(path); e == nil && w > 0 && h > 0 {
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil, rerr
			}
			px, _, _, err = viewer.DecodeJPEGTurbo(data, float64(ThumbSize)/float64(max(w, h)), viewer.GoAllocator{})
		}
	}
	if px.W == 0 || err != nil {
		if px, err = viewer.Decode(path, viewer.GoAllocator{}); err != nil {
			return nil, err
		}
	}
	src := pixelsImage(px)
	w, h := px.W, px.H
	if w > ThumbSize || h > ThumbSize {
		if w >= h {
			w, h = ThumbSize, max(1, h*ThumbSize/w)
		} else {
			w, h = max(1, w*ThumbSize/h), ThumbSize
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(out, out.Bounds(), src, src.Bounds(), draw.Src, nil)
	if ex, err := viewer.ReadExif(path); err == nil {
		out = orient(out, ex.Orientation)
	}
	return out, nil
}

// pixelsImage wraps decoded pixels as an image without copying them.
func pixelsImage(px viewer.Pixels) image.Image {
	r := image.Rect(0, 0, px.W, px.H)
	switch px.Format {
	case viewer.Gray:
		return &image.Gray{Pix: px.Buf, Stride: px.Stride, Rect: r}
	case viewer.RGBA:
		return &image.NRGBA{Pix: px.Buf, Stride: px.Stride, Rect: r}
	case viewer.RGBAPremult:
		return &image.RGBA{Pix: px.Buf, Stride: px.Stride, Rect: r}
	}
	return rgbImage{px}
}

// rgbImage is 3-byte RGB pixels as an image.Image.
type rgbImage struct{ px viewer.Pixels }

func (m rgbImage) ColorModel() color.Model { return color.RGBAModel }
func (m rgbImage) Bounds() image.Rectangle { return image.Rect(0, 0, m.px.W, m.px.H) }
func (m rgbImage) At(x, y int) color.Color {
	i := y*m.px.Stride + x*3
	return color.RGBA{m.px.Buf[i], m.px.Buf[i+1], m.px.Buf[i+2], 0xff}
}

// orient turns a thumbnail as EXIF orientation o says (1–8).
func orient(src *image.RGBA, o int) *image.RGBA {
	if o <= 1 || o > 8 {
		return src
	}
	turns, mirror := viewer.Orientation(o)
	w, h := src.Rect.Dx(), src.Rect.Dy()
	dw, dh := w, h
	if turns%2 == 1 {
		dw, dh = h, w
	}
	out := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range h {
		for x := range w {
			sx := x
			if mirror {
				sx = w - 1 - x
			}
			var dx, dy int
			switch turns % 4 {
			case 0:
				dx, dy = x, y
			case 1: // clockwise
				dx, dy = h-1-y, x
			case 2:
				dx, dy = w-1-x, h-1-y
			case 3:
				dx, dy = y, w-1-x
			}
			si, di := src.PixOffset(sx, y), out.PixOffset(dx, dy)
			copy(out.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return out
}

// saveThumb writes a thumbnail with its Thumb::URI and Thumb::MTime, as
// the standard asks: readable by the user only, written whole.
func saveThumb(cached, path string, mtime time.Time, img *image.RGBA) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return
	}
	data := withText(buf.Bytes(), map[string]string{
		"Thumb::URI":   URIOfPath(path),
		"Thumb::MTime": strconv.FormatInt(mtime.Unix(), 10),
		"Software":     "inua-files",
	})
	if err := os.MkdirAll(filepath.Dir(cached), 0o700); err != nil {
		return
	}
	writeFileAtomic(cached, data, 0o600)
}

// withText adds tEXt chunks to a PNG before its IEND; Go's encoder
// writes none.
func withText(p []byte, text map[string]string) []byte {
	const iendLen = 12
	if len(p) < 8+iendLen {
		return p
	}
	var chunks bytes.Buffer
	for _, k := range []string{"Thumb::URI", "Thumb::MTime", "Software"} {
		v, ok := text[k]
		if !ok {
			continue
		}
		body := append(append([]byte(k), 0), v...)
		binary.Write(&chunks, binary.BigEndian, uint32(len(body)))
		typed := append([]byte("tEXt"), body...)
		chunks.Write(typed)
		binary.Write(&chunks, binary.BigEndian, crc32.ChecksumIEEE(typed))
	}
	out := make([]byte, 0, len(p)+chunks.Len())
	out = append(out, p[:len(p)-iendLen]...)
	out = append(out, chunks.Bytes()...)
	return append(out, p[len(p)-iendLen:]...)
}

// pngText reads a PNG's tEXt chunks.
func pngText(p []byte) map[string]string {
	out := map[string]string{}
	r := bytes.NewReader(p)
	var sig [8]byte
	if _, err := io.ReadFull(r, sig[:]); err != nil {
		return out
	}
	for {
		var n uint32
		var typ [4]byte
		if binary.Read(r, binary.BigEndian, &n) != nil {
			return out
		}
		if _, err := io.ReadFull(r, typ[:]); err != nil || n > 1<<20 {
			return out
		}
		switch string(typ[:]) {
		case "tEXt":
			body := make([]byte, n)
			if _, err := io.ReadFull(r, body); err != nil {
				return out
			}
			if k, v, ok := bytes.Cut(body, []byte{0}); ok {
				out[string(k)] = string(v)
			}
		case "IEND":
			return out
		default: // the pixels too: text may come before or after them
			if _, err := r.Seek(int64(n), io.SeekCurrent); err != nil {
				return out
			}
		}
		r.Seek(4, io.SeekCurrent) // the CRC
	}
}
