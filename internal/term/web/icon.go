package web

import (
	"image"
	"image/color"
	"math"
)

// drawIcon is the web app's icon: a prompt, >_, in the accent on the
// ground, inside the middle 60% (what a maskable icon keeps).
func drawIcon(n int, ground, ink uint32) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	g, k := rgba(ground), rgba(ink)
	f := float64(n)
	strokes := [][4]float64{
		{0.32, 0.36, 0.47, 0.50}, {0.47, 0.50, 0.32, 0.64}, // >
		{0.53, 0.64, 0.68, 0.64}, // _
	}
	half := 0.045 * f
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			c := g
			px, py := float64(x)+0.5, float64(y)+0.5
			for _, s := range strokes {
				// Covered by the stroke, with a pixel of soft edge.
				if a := half - segDist(px, py, s[0]*f, s[1]*f, s[2]*f, s[3]*f); a > 0 {
					c = mix(c, k, math.Min(1, a))
				}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func mix(a, b color.RGBA, t float64) color.RGBA {
	m := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t) }
	return color.RGBA{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), 255}
}

// segDist is the distance from (px, py) to the segment from (ax, ay) to
// (bx, by).
func segDist(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	t := math.Max(0, math.Min(1, ((px-ax)*dx+(py-ay)*dy)/(dx*dx+dy*dy)))
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
}
