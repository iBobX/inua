// Package viewer is the Viewer's logic without GTK: which files are
// images, the images of a folder in order, decoding them (in Go, or with
// libjpeg-turbo when it is installed), how to draw them standing, zoom
// steps, and what a photo's EXIF says.
package viewer

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

// Exts are the file extensions the viewer opens, with their MIME types
// (for the clipboard and the desktop entry).
var Exts = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".jpe": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".svg": "image/svg+xml", ".svgz": "image/svg+xml",
	".tif": "image/tiff", ".tiff": "image/tiff", ".bmp": "image/bmp", ".ico": "image/vnd.microsoft.icon",
	".avif": "image/avif", ".heic": "image/heic", ".heif": "image/heif", ".jxl": "image/jxl",
	".tga": "image/x-tga", ".xpm": "image/x-xpixmap", ".pnm": "image/x-portable-anymap",
	".ppm": "image/x-portable-pixmap", ".pgm": "image/x-portable-graymap", ".pbm": "image/x-portable-bitmap",
}

// IsImage reports whether a file is one the viewer opens, by its name.
func IsImage(path string) bool {
	_, ok := Exts[strings.ToLower(filepath.Ext(path))]
	return ok
}

// Mime is a file's image MIME type, "" if it is not an image.
func Mime(path string) string { return Exts[strings.ToLower(filepath.Ext(path))] }

// Animated reports whether a file may be animated (GIF, WebP).
func Animated(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".gif", ".webp":
		return true
	}
	return false
}

// List is what the viewer steps through for its arguments: the images of
// the folder of a single file (or of a single folder), else the image files
// given, in the order given. Index is the first to show.
func List(args []string) (files []string, index int) {
	if len(args) == 1 {
		p, err := filepath.Abs(args[0])
		if err != nil {
			return nil, 0
		}
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return Folder(p), 0
		}
		files = Folder(filepath.Dir(p))
		if i := slices.Index(files, p); i >= 0 {
			return files, i
		}
		return []string{p}, 0 // not listed (hidden, or an unknown extension)
	}
	for _, a := range args {
		if p, err := filepath.Abs(a); err == nil && IsImage(p) {
			files = append(files, p)
		}
	}
	return files, 0
}

// Folder is a folder's images, hidden ones left out, in natural order.
func Folder(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !IsImage(e.Name()) {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	slices.SortFunc(files, func(a, b string) int {
		if NaturalLess(filepath.Base(a), filepath.Base(b)) {
			return -1
		}
		if NaturalLess(filepath.Base(b), filepath.Base(a)) {
			return 1
		}
		return 0
	})
	return files
}

// NaturalLess orders names as people do: case aside, and runs of digits by
// their value ("img2" before "img10").
func NaturalLess(a, b string) bool {
	ra, rb := []rune(strings.ToLower(a)), []rune(strings.ToLower(b))
	i, j := 0, 0
	for i < len(ra) && j < len(rb) {
		if unicode.IsDigit(ra[i]) && unicode.IsDigit(rb[j]) {
			si, sj := i, j
			for i < len(ra) && unicode.IsDigit(ra[i]) {
				i++
			}
			for j < len(rb) && unicode.IsDigit(rb[j]) {
				j++
			}
			na := strings.TrimLeft(string(ra[si:i]), "0")
			nb := strings.TrimLeft(string(rb[sj:j]), "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			continue
		}
		if ra[i] != rb[j] {
			return ra[i] < rb[j]
		}
		i++
		j++
	}
	if len(ra)-i != len(rb)-j {
		return len(ra)-i < len(rb)-j
	}
	return a < b // equal but for case or leading zeros: still a fixed order
}

// Zoom limits, as image pixels per screen pixel.
const (
	MinZoom = 0.02
	MaxZoom = 32
)

// zoomSteps are where the zoom buttons and keys stop.
var zoomSteps = []float64{0.02, 0.05, 0.1, 0.125, 0.25, 1.0 / 3, 0.5, 2.0 / 3, 1, 1.5, 2, 3, 4, 6, 8, 12, 16, 24, 32}

// StepZoom is the next stop above (up) or below z.
func StepZoom(z float64, up bool) float64 {
	if up {
		for _, s := range zoomSteps {
			if s > z*1.001 {
				return s
			}
		}
		return MaxZoom
	}
	for i := len(zoomSteps) - 1; i >= 0; i-- {
		if zoomSteps[i] < z*0.999 {
			return zoomSteps[i]
		}
	}
	return MinZoom
}

// ClampZoom keeps z within the limits.
func ClampZoom(z float64) float64 { return max(MinZoom, min(z, MaxZoom)) }

// FitZoom is the zoom that shows a w×h image whole in a view of vw×vh
// screen pixels, never above 1 (a small image is not blown up).
func FitZoom(w, h, vw, vh float64) float64 {
	if w <= 0 || h <= 0 || vw <= 0 || vh <= 0 {
		return 1
	}
	return min(1, vw/w, vh/h)
}

// Percent is a zoom as the toolbar shows it: "100%", "12.5%".
func Percent(z float64) string {
	p := z * 100
	if math.Abs(p-math.Round(p)) < 0.05 || p >= 100 {
		return fmt.Sprintf("%.0f%%", p)
	}
	return fmt.Sprintf("%.1f%%", p)
}

// Size is a file size: "812 KB", "3.4 MB".
func Size(n int64) string {
	const k = 1000.0
	f := float64(n)
	switch {
	case f < k:
		return fmt.Sprintf("%d B", n)
	case f < k*k:
		return fmt.Sprintf("%.0f KB", f/k)
	case f < k*k*k:
		return fmt.Sprintf("%.1f MB", f/(k*k))
	}
	return fmt.Sprintf("%.2f GB", f/(k*k*k))
}

// Megapixels is "12.2 MP" for a photo's size, "" for small images.
func Megapixels(w, h int) string {
	mp := float64(w) * float64(h) / 1e6
	if mp < 1 {
		return ""
	}
	return fmt.Sprintf("%.1f MP", mp)
}
