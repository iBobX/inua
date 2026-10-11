package ui

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"

	"github.com/lyricat/inua/internal/apps/colors"
	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/notify"
)

// Picking a colour is the screenshot tool in its "color" mode: the same
// still of every monitor, undimmed, with a loupe under the pointer. A click
// copies the colour as #rrggbb and adds it to the Color Picker app's
// history.

const (
	loupeSpan  = 11 // pixels across the loupe
	loupeScale = 11 // each shown this many times larger
	swatchSize = 14
)

// moveLoupe shows the pixels around the pointer and the colour under it,
// beside the pointer and kept on the monitor.
func (s *shellUI) moveLoupe(sf *shotSurface, x, y float64) {
	if sf.img == nil {
		return
	}
	c := sf.colorAt(x, y)
	tex := loupeTexture(sf.img, sf.pixelAt(x, y))
	sf.loupePixels.SetPaintable(tex)
	tex.Unref()
	sw := solidTexture(c, swatchSize)
	sf.loupeSwatch.SetPaintable(sw)
	sw.Unref()
	sf.loupeHex.SetText(colors.Color{R: c.R, G: c.G, B: c.B}.Hex())
	size := float64(loupeSpan*loupeScale + 24)
	lx, ly := x+24, y+24
	if lx+size > sf.w {
		lx = x - 24 - size
	}
	if ly+size+30 > sf.h {
		ly = y - 24 - size - 30
	}
	sf.marks.Move(&sf.loupe.Widget, max(0, lx), max(0, ly))
	sf.loupe.SetVisible(true)
}

// pixelAt is the capture's pixel under a logical point.
func (sf *shotSurface) pixelAt(x, y float64) image.Point {
	b := sf.img.Bounds()
	px := b.Min.X + int(x*sf.scale)
	py := b.Min.Y + int(y*sf.scale)
	return image.Pt(max(b.Min.X, min(px, b.Max.X-1)), max(b.Min.Y, min(py, b.Max.Y-1)))
}

func (sf *shotSurface) colorAt(x, y float64) color.NRGBA {
	p := sf.pixelAt(x, y)
	return color.NRGBAModel.Convert(sf.img.At(p.X, p.Y)).(color.NRGBA)
}

// loupeTexture magnifies the pixels around p, with the middle one framed.
func loupeTexture(img image.Image, p image.Point) *gdk.MemoryTexture {
	const side = loupeSpan * loupeScale
	buf := make([]byte, side*side*4)
	b := img.Bounds()
	half := loupeSpan / 2
	for j := range loupeSpan {
		for i := range loupeSpan {
			x, y := p.X+i-half, p.Y+j-half
			var c color.NRGBA
			if image.Pt(x, y).In(b) {
				c = color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				c.A = 255
			}
			for dy := range loupeScale {
				row := ((j*loupeScale+dy)*side + i*loupeScale) * 4
				for dx := range loupeScale {
					o := row + dx*4
					buf[o], buf[o+1], buf[o+2], buf[o+3] = c.R, c.G, c.B, c.A
				}
			}
		}
	}
	// Frame the middle pixel: dark then light, so it shows on any colour.
	frame := func(inset int, v byte) {
		lo, hi := half*loupeScale-inset, (half+1)*loupeScale-1+inset
		for k := lo; k <= hi; k++ {
			for _, pt := range [][2]int{{k, lo}, {k, hi}, {lo, k}, {hi, k}} {
				o := (pt[1]*side + pt[0]) * 4
				buf[o], buf[o+1], buf[o+2], buf[o+3] = v, v, v, 255
			}
		}
	}
	frame(1, 0)
	frame(2, 255)
	return memoryTexture(buf, side, side)
}

// solidTexture is a square of one colour.
func solidTexture(c color.NRGBA, side int) *gdk.MemoryTexture {
	buf := make([]byte, side*side*4)
	for o := 0; o < len(buf); o += 4 {
		buf[o], buf[o+1], buf[o+2], buf[o+3] = c.R, c.G, c.B, 255
	}
	return memoryTexture(buf, side, side)
}

func memoryTexture(rgba []byte, w, h int) *gdk.MemoryTexture {
	bytes := glib.NewBytes(rgba, uint(len(rgba)))
	defer bytes.Unref()
	return gdk.NewMemoryTexture(int32(w), int32(h), gdk.MemoryR8g8b8a8Value, bytes, uint(w*4))
}

// pickColor closes the tool, copies the colour under the pointer and adds
// it to the history.
func (s *shellUI) pickColor(sf *shotSurface) {
	if sf.img == nil {
		return
	}
	c := sf.colorAt(sf.px, sf.py)
	col := colors.Color{R: c.R, G: c.G, B: c.B}
	hex := col.Hex()
	s.closeShot("")
	var data colors.Data
	if err := store.Load(colors.StoreName, &data); err != nil {
		log.Printf("color: %v", err)
	}
	data.Add(hex)
	if err := store.Save(colors.StoreName, data); err != nil {
		log.Printf("color: %v", err)
	}
	copied := copyText(hex) == nil
	summary := "Colour copied"
	if !copied {
		summary = "Colour picked"
	}
	n := notify.Notification{
		App: "Color Picker", Entry: "io.github.lyricat.Inua.ColorPicker", Icon: "color-select-symbolic",
		Summary: summary, Body: notify.Markup(hex + "   " + col.RGB()), Time: time.Now(), Transient: true,
	}
	if p, err := swatchFile(c); err == nil {
		n.ImgPath = p
	}
	if s.notes.srv != nil {
		n.ID = s.notes.srv.NewID()
		s.postNote(n)
	}
	log.Printf("color: %s", hex)
}

// swatchFile writes a square of the colour for the notification.
func swatchFile(c color.NRGBA) (string, error) {
	dir, err := shotTempDir()
	if err != nil {
		return "", err
	}
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 255
	}
	p := filepath.Join(dir, fmt.Sprintf("swatch-%02x%02x%02x.png", c.R, c.G, c.B))
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	return p, writePNG(p, img)
}
