package wallgen

import (
	"image"
	"math"
	"math/rand/v2"
)

// contours is a topographic map: the lines of equal height of a smooth,
// warped landscape, every fifth one stronger, on the ground lightly shaded
// by the height.
func contours(img *image.NRGBA, p Palette, seed uint64) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	n := newNoise(seed)
	s := unit(img)
	const levels = 16
	// The height at each pixel, in levels, then the lines where it crosses
	// a whole level: their distance in pixels is the distance in levels
	// over how fast the height changes there.
	field := make([]float32, w*h)
	rows(h, func(y int) {
		for x := range w {
			u, v := float64(x)/s*1.1, float64(y)/s*1.1
			qx, qy := n.fbm(u, v, 2), n.fbm(u+5.2, v+1.3, 2)
			field[y*w+x] = float32(n.fbm(u+0.45*qx, v+0.45*qy, 4) * levels)
		}
	})
	ground, field1, field5 := rgbOf(p.Ground), mix(rgbOf(p.Ground), rgbOf(p.Field), 0.6), mix(rgbOf(p.Ground), rgbOf(p.Field), 1)
	shade := mix(rgbOf(p.Ground), rgbOf(p.Field), 0.12)
	width := math.Max(1, s/1080) // a line's width, in pixels
	rows(h, func(y int) {
		for x := range w {
			t := float64(field[y*w+x])
			dx := float64(field[y*w+min(x+1, w-1)]-field[y*w+max(x-1, 0)]) / 2
			dy := float64(field[min(y+1, h-1)*w+x]-field[max(y-1, 0)*w+x]) / 2
			g := math.Max(math.Hypot(dx, dy), 1e-4)
			c := mix(ground, shade, smoothstep(-levels*0.5, levels*0.5, t))
			level := math.Round(t)
			d := math.Abs(t-level) / g
			line, wd := field1, width
			if int(level)%5 == 0 {
				line, wd = field5, width*1.7
			}
			c = mix(c, line, clamp(wd/2+0.5-d, 0, 1))
			set(img, x, y, c, 1.5, seed)
		}
	})
}

// dunes is hills one behind another, each nearer one deeper in the field's
// colour, under a sky with the faintest glow of the accent at the horizon.
func dunes(img *image.NRGBA, p Palette, seed uint64) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	n := newNoise(seed)
	s := unit(img)
	const layers = 6
	ridge := make([][]float64, layers)
	for i := range layers {
		ridge[i] = make([]float64, w)
		base := 0.42 + 0.1*float64(i)
		amp := 0.07 + 0.012*float64(i)
		freq := 0.9 + 0.3*float64(i)
		for x := range w {
			ridge[i][x] = float64(h) * (base + amp*n.fbm(float64(x)/s*freq, float64(i)*7.31, 4))
		}
	}
	ground, field, accent := rgbOf(p.Ground), rgbOf(p.Field), rgbOf(p.Accent)
	horizon := float64(h) * 0.45
	rows(h, func(y int) {
		fy := float64(y)
		sky := mix(ground, accent, 0.07*math.Exp(-math.Pow((fy-horizon)/(float64(h)*0.22), 2)))
		for x := range w {
			c := sky
			for i := range layers {
				cover := clamp(fy-ridge[i][x]+0.5, 0, 1)
				if cover == 0 {
					continue
				}
				depth := 0.16 + 0.13*float64(i)
				hill := mix(ground, field, depth)
				// Each hill fades back toward the ground below its crest.
				hill = mix(hill, ground, 0.35*smoothstep(0, float64(h)*0.35, fy-ridge[i][x]))
				c = mix(c, hill, cover)
			}
			set(img, x, y, c, 2, seed)
		}
	})
}

// aurora is a few long, soft bands of the accent's colour, and one of the
// primary's, warped and slanted over the ground, grained so their gradients
// do not band.
func aurora(img *image.NRGBA, p Palette, seed uint64) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	n := newNoise(seed)
	r := rand.New(rand.NewPCG(seed, 1))
	s := unit(img)
	aspect := float64(w) / s
	type band struct {
		x, y, long, wide, sin, cos, strength float64
		c                                    rgb
	}
	var bands []band
	for i, c := range []rgb{rgbOf(p.Accent), rgbOf(p.Accent), rgbOf(p.Primary)} {
		a := (r.Float64() - 0.5) * 0.9 // a slant of up to about 26°
		bands = append(bands, band{
			x: (0.25 + 0.5*r.Float64()) * aspect, y: 0.25 + 0.5*r.Float64(),
			long: 0.7 + 0.4*r.Float64(), wide: 0.12 + 0.08*r.Float64(),
			sin: math.Sin(a), cos: math.Cos(a), strength: []float64{0.34, 0.24, 0.1}[i], c: c,
		})
	}
	ground := rgbOf(p.Ground)
	rows(h, func(y int) {
		for x := range w {
			u, v := float64(x)/s, float64(y)/s
			u, v = u+0.25*n.fbm(u*1.1, v*1.1, 3), v+0.25*n.fbm(u*1.1+9.1, v*1.1+3.7, 3)
			c := ground
			for _, b := range bands {
				du, dv := u-b.x, v-b.y
				along, across := (du*b.cos+dv*b.sin)/b.long, (-du*b.sin+dv*b.cos)/b.wide
				c = mix(c, b.c, b.strength*math.Exp(-(along*along+across*across)))
			}
			set(img, x, y, c, 3, seed)
		}
	})
}

// dots is a halftone grid: a dot in every cell, as large as a smooth
// landscape is high there, the smallest a faint point.
func dots(img *image.NRGBA, p Palette, seed uint64) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	n := newNoise(seed)
	s := unit(img)
	step := math.Round(s / 34) // the grid's cell, in pixels
	cols, rws := int(float64(w)/step)+2, int(float64(h)/step)+2
	radius := make([]float64, cols*rws)
	for j := range rws {
		for i := range cols {
			cx, cy := (float64(i)+0.5)*step/s, (float64(j)+0.5)*step/s
			v := smoothstep(-0.35, 0.45, n.fbm(cx*1.3, cy*1.3, 4))
			radius[j*cols+i] = math.Max(0.8, step*0.46*math.Pow(v, 1.3))
		}
	}
	ground, dot := rgbOf(p.Ground), mix(rgbOf(p.Ground), rgbOf(p.Field), 0.95)
	rows(h, func(y int) {
		j := int(float64(y) / step)
		cy := (float64(j) + 0.5) * step
		for x := range w {
			i := int(float64(x) / step)
			cx := (float64(i) + 0.5) * step
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			c := mix(ground, dot, clamp(radius[j*cols+i]-d+0.5, 0, 1))
			set(img, x, y, c, 1.5, seed)
		}
	})
}
