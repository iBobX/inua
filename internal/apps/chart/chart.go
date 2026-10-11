// Package chart draws the light apps' line charts in Go: a price history,
// a meter's last minute.
package chart

import (
	"image"
	"image/color"
	"math"

	"golang.org/x/image/vector"
)

// Options shape a chart.
type Options struct {
	// Base draws a dotted line at this value (0 for none), and keeps it in
	// range.
	Base float64
	// Lo and Hi fix the range when Hi > Lo; otherwise it follows the
	// points.
	Lo, Hi float64
}

// Line draws points as a line over a soft fill, w×h pixels.
func Line(points []float64, w, h int, line color.NRGBA, o Options) *image.NRGBA {
	base := o.Base
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	if len(points) < 2 {
		return img
	}
	lo, hi := points[0], points[0]
	for _, p := range points {
		lo, hi = min(lo, p), max(hi, p)
	}
	if base > 0 {
		lo, hi = min(lo, base), max(hi, base)
	}
	if o.Hi > o.Lo {
		lo, hi = o.Lo, o.Hi
	}
	if hi == lo {
		hi, lo = hi+1, lo-1
	}
	pad := float32(h) * 0.08
	x := func(i int) float32 { return float32(i) * float32(w-1) / float32(len(points)-1) }
	y := func(v float64) float32 {
		v = max(lo, min(v, hi))
		return pad + (float32(h)-2*pad)*float32((hi-v)/(hi-lo))
	}

	fill := vector.NewRasterizer(w, h)
	fill.MoveTo(0, float32(h))
	for i, p := range points {
		fill.LineTo(x(i), y(p))
	}
	fill.LineTo(float32(w-1), float32(h))
	fill.ClosePath()
	paint(img, fill, color.NRGBA{line.R, line.G, line.B, 34})

	if base > 0 {
		by := y(base)
		dots := vector.NewRasterizer(w, h)
		for x0 := float32(0); x0 < float32(w); x0 += 6 {
			rect(dots, x0, by-0.5, x0+2, by+0.5)
		}
		paint(img, dots, color.NRGBA{line.R, line.G, line.B, 110})
	}

	stroke := vector.NewRasterizer(w, h)
	const half = 1.0 // a 2px line
	for i := 1; i < len(points); i++ {
		segment(stroke, x(i-1), y(points[i-1]), x(i), y(points[i]), half)
	}
	paint(img, stroke, line)
	return img
}

// segment adds a thick line from (x0, y0) to (x1, y1): a quadrilateral
// reaching half its width past each end, so neighbours overlap at the
// joins. Every segment winds the same way, so overlaps add and never cancel.
func segment(r *vector.Rasterizer, x0, y0, x1, y1, half float32) {
	dx, dy := x1-x0, y1-y0
	l := float32(math.Hypot(float64(dx), float64(dy)))
	if l == 0 {
		return
	}
	ux, uy := dx/l*half, dy/l*half // along the line
	nx, ny := -uy, ux              // across it
	x0, y0, x1, y1 = x0-ux, y0-uy, x1+ux, y1+uy
	r.MoveTo(x0+nx, y0+ny)
	r.LineTo(x1+nx, y1+ny)
	r.LineTo(x1-nx, y1-ny)
	r.LineTo(x0-nx, y0-ny)
	r.ClosePath()
}

func rect(r *vector.Rasterizer, x0, y0, x1, y1 float32) {
	r.MoveTo(x0, y0)
	r.LineTo(x1, y0)
	r.LineTo(x1, y1)
	r.LineTo(x0, y1)
	r.ClosePath()
}

// paint fills the rasterized shape with c over img. The rasterizer works
// with non-zero winding, so overlapping pieces do not darken.
func paint(img *image.NRGBA, r *vector.Rasterizer, c color.NRGBA) {
	mask := image.NewAlpha(img.Rect)
	r.Draw(mask, mask.Rect, image.Opaque, image.Point{})
	for i, a := range mask.Pix {
		if a == 0 {
			continue
		}
		alpha := uint32(a) * uint32(c.A) / 255
		px := img.Pix[i*4 : i*4+4]
		// Source over, in non-premultiplied form.
		da := uint32(px[3])
		oa := alpha + da*(255-alpha)/255
		if oa == 0 {
			continue
		}
		for k, sc := range []uint8{c.R, c.G, c.B} {
			px[k] = uint8((uint32(sc)*alpha + uint32(px[k])*da*(255-alpha)/255) / oa)
		}
		px[3] = uint8(oa)
	}
}
