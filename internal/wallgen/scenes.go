package wallgen

import (
	"image"
	"math"
	"math/rand/v2"
)

// The scenes are in their own colours: dusk or night under a dark theme,
// daylight under a light one.

// sky is a vertical gradient through stops, t from 0 at the top to 1.
func sky(t float64, stops ...rgb) rgb {
	t = clamp(t, 0, 1) * float64(len(stops)-1)
	i := min(int(t), len(stops)-2)
	return mix(stops[i], stops[i+1], t-float64(i))
}

func sq(v float64) float64 { return v * v }

// sunset is the sun low over the sea: a banded sky with streaks of cloud lit
// from below, a headland on one side, and the sun's broken path on the
// waves, which shrink toward the horizon.
func sunset(img *image.NRGBA, p Palette, seed uint64) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	n := newNoise(seed)
	r := rand.New(rand.NewPCG(seed, 2))
	s := unit(img)
	type colours struct{ top, mid, low, horizon, sun, seaFar, seaNear, cloudLit, cloudDark, land rgb }
	c := colours{ // a golden afternoon
		hex("#6E9FD6"), hex("#A9C6E8"), hex("#F3D2B8"), hex("#FFD9A8"), hex("#FFF6E0"),
		hex("#9AB8D8"), hex("#3F6F9E"), hex("#FFF2E0"), hex("#B9C8DD"), hex("#4A5D70"),
	}
	if p.Dark() { // dusk
		c = colours{
			hex("#0D1330"), hex("#3B2A5C"), hex("#C0584A"), hex("#F29A5A"), hex("#FFE2B0"),
			hex("#3A2A48"), hex("#070B18"), hex("#E88A6A"), hex("#2A2040"), hex("#0A0C16"),
		}
	}
	fw, fh := float64(w), float64(h)
	hy := fh * 0.6
	sx, sy, sr := fw*(0.35+0.3*r.Float64()), hy-0.04*s, 0.055*s
	left := r.IntN(2) == 0
	// land is the headland's top at x, or the horizon where there is none.
	land := func(fx float64) float64 {
		lx := fx / fw
		if !left {
			lx = 1 - lx
		}
		if lx > 0.34 {
			return hy
		}
		return hy - fh*(0.075*smoothstep(0.34, 0.02, lx)+0.01*n.fbm(fx/s*5, 3.3, 3))
	}
	rows(h, func(y int) {
		fy := float64(y)
		for x := range w {
			fx := float64(x)
			var col rgb
			if fy < hy {
				t := fy / hy
				col = sky(t, c.top, c.mid, c.low, c.horizon)
				d := math.Hypot(fx-sx, fy-sy) / s
				col = mix(col, c.horizon, 0.5*math.Exp(-d*d/0.08))
				col = mix(col, c.sun, 0.55*math.Exp(-d*d/0.004))
				col = mix(col, c.sun, clamp(sr-d*s+0.5, 0, 1))
				// Streaks of cloud, lit the more the nearer the sun and the
				// lower they are.
				u, v := fx/s, fy/s
				a := smoothstep(0.12, 0.45, n.fbm(u*0.8, v*7, 4)) * smoothstep(0.05, 0.35, t) * (1 - smoothstep(0.75, 0.97, t))
				lit := math.Exp(-sq((fx-sx)/s)/0.2) * t
				col = mix(col, mix(c.cloudDark, c.cloudLit, 0.25+0.75*lit), a*0.85)
				// The headland, a silhouette against the sky.
				col = mix(col, c.land, clamp(fy-land(fx)+0.5, 0, 1))
			} else {
				t := (fy - hy) / (fh - hy)
				col = mix(c.seaFar, c.seaNear, math.Pow(t, 0.6))
				// Waves in perspective: far ones are small and close together.
				depth := t + 0.015
				X, Z := (fx-fw/2)/s/depth, 1/depth
				wave := n.fbm(X*0.5, Z*2.2, 3)
				col = mix(col, c.horizon, 0.12*smoothstep(0.15, 0.6, wave))
				col = mix(col, c.seaNear, 0.25*smoothstep(-0.15, -0.6, wave))
				// The headland's reflection, darker and softer.
				if my := 2*hy - fy; my > land(fx) {
					col = mix(col, c.land, 0.55*clamp(my-land(fx)+0.5, 0, 1))
				}
				// The sun's path: glints on the crests, widening toward us.
				width := 0.025 + 0.22*t
				path := math.Exp(-sq((fx-sx)/s) / sq(width))
				col = mix(col, c.sun, clamp(smoothstep(0.0, 0.35, wave)*path*(1-0.5*t), 0, 1)*0.9)
				col = mix(col, c.horizon, 0.4*math.Exp(-t*t/0.0015))
			}
			set(img, x, y, col, 2.5, seed)
		}
	})
}

// cosmos is the Milky Way crossing the dark: a band of coloured nebula with
// dark lanes of dust and a warm core, and stars, crowded along the band,
// a few bright ones with a glow.
func cosmos(img *image.NRGBA, p Palette, seed uint64) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	n := newNoise(seed)
	r := rand.New(rand.NewPCG(seed, 3))
	s := unit(img)
	space0, space1 := hex("#03040A"), hex("#0B0F22")
	nebA, nebB, core := hex("#9B4DCA"), hex("#2FB3C9"), hex("#FFD7A8")
	if !p.Dark() { // a lighter night for a light theme: the band brighter, the dark bluer
		space0, space1 = hex("#0B1230"), hex("#1A2550")
		nebA, nebB = hex("#B07AE0"), hex("#5CCBDD")
	}
	fw, fh := float64(w), float64(h)
	a := (r.Float64() - 0.5) * 1.2
	ca, sa := math.Cos(a), math.Sin(a)
	ox, oy := fw*(0.4+0.2*r.Float64()), fh*(0.4+0.2*r.Float64())
	// band is how near the band's middle a point is, from 0 to 1.
	band := func(fx, fy float64) float64 {
		d := (-(fx-ox)*sa + (fy-oy)*ca) / s
		return math.Exp(-d * d / 0.07)
	}
	rows(h, func(y int) {
		fy := float64(y)
		for x := range w {
			fx := float64(x)
			u, v := fx/s, fy/s
			b := band(fx, fy)
			col := mix(space0, space1, 0.5*b+0.3*fy/fh)
			f := n.fbm(u*1.7, v*1.7, 5)*0.5 + 0.5
			// The two colours meet in a narrow seam, not a grey middle.
			hue := smoothstep(-0.08, 0.08, n.fbm(u*0.7+3, v*0.7+7, 3))
			nc := mix(nebA, nebB, hue)
			col = mix(col, nc, b*smoothstep(0.35, 0.85, f)*0.85)
			ridge := math.Pow(1-math.Abs(n.fbm(u*3+11, v*3, 4)), 5)
			col = mix(col, mix(nc, core, 0.45), ridge*b*0.45)
			col = mix(col, core, 0.5*math.Pow(b, 4)*smoothstep(0.4, 0.9, f))
			dust := smoothstep(0.55, 0.78, n.fbm(u*2.4+21, v*2.4, 4)*0.5+0.5) * b
			col = mix(col, space0, dust*0.75)
			set(img, x, y, col, 2, seed)
		}
	})
	// Stars: most faint, a few bright; more along the band.
	px := s / 1080
	for range w * h / 320 {
		fx, fy := r.Float64()*fw, r.Float64()*fh
		if r.Float64() > 0.3+0.7*band(fx, fy) {
			continue
		}
		bright := math.Pow(r.Float64(), 6)
		tint := mix(hex("#FFFFFF"), mix(hex("#A8C4FF"), hex("#FFD2A0"), r.Float64()), 0.6*r.Float64())
		star(img, fx, fy, (0.55+1.6*bright)*px, 0.25+0.9*bright, tint)
		if bright > 0.5 {
			star(img, fx, fy, 7*bright*px, 0.12*bright, tint)
		}
	}
}

// star lights a soft round spot of radius rad at x, y.
func star(img *image.NRGBA, x, y, rad, v float64, c rgb) {
	reach := int(math.Ceil(rad * 3))
	for j := -reach; j <= reach; j++ {
		for i := -reach; i <= reach; i++ {
			px, py := int(x)+i, int(y)+j
			d2 := sq(float64(px)+0.5-x) + sq(float64(py)+0.5-y)
			add(img, px, py, c, v*math.Exp(-d2/(rad*rad)))
		}
	}
}

// cat is a cat seen from behind, sitting on the crest of a hill and looking
// at the moon over the hills beyond; at the sun, under a light theme.
func cat(img *image.NRGBA, p Palette, seed uint64) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	n := newNoise(seed)
	r := rand.New(rand.NewPCG(seed, 4))
	s := unit(img)
	type colours struct{ top, low, moon, glow, far, near, ground rgb }
	c := colours{ // a late afternoon
		hex("#7FB0DE"), hex("#F4D9BD"), hex("#FFF3D9"), hex("#FFE2B8"), hex("#A7B9BC"), hex("#7A9089"), hex("#26302E"),
	}
	dark := p.Dark()
	if dark { // night
		c = colours{
			hex("#070B1D"), hex("#26365E"), hex("#EFE9D4"), hex("#8EA2C8"), hex("#18223F"), hex("#0F162D"), hex("#04060D"),
		}
	}
	fw, fh := float64(w), float64(h)
	mx, my, mr := fw*(0.22+0.16*r.Float64()), fh*0.32, 0.11*s
	catX, H := fw*(0.6+0.1*r.Float64()), 0.3*s
	ground := func(fx float64) float64 {
		return fh*0.84 - fh*0.06*math.Exp(-sq((fx-catX)/(0.3*fw))) + fh*0.008*n.fbm(fx/s*3, 9.1, 3)
	}
	baseY := ground(catX) + 0.02*H
	hill := func(fx float64, i int) float64 {
		return fh*(0.62+0.08*float64(i)) + fh*0.06*n.fbm(fx/s*(1+0.6*float64(i)), float64(i)*5.7, 4)
	}
	rows(h, func(y int) {
		fy := float64(y)
		for x := range w {
			fx := float64(x)
			col := sky(fy/(fh*0.75), c.top, mix(c.top, c.low, 0.55), c.low)
			d := math.Hypot(fx-mx, fy-my)
			col = mix(col, c.glow, 0.45*math.Exp(-sq(d/s)/0.03))
			if dark && hash(x, y, seed^0x5A) > 0.9994 {
				col = mix(col, c.moon, 0.4+0.5*hash(y, x, seed))
			}
			disc := clamp(mr-d+0.5, 0, 1)
			if disc > 0 {
				// The moon's seas, faint; the sun has none.
				face := c.moon
				if dark {
					face = mix(c.moon, c.glow, 0.2*smoothstep(0, 0.45, n.fbm(fx/s*5+40, fy/s*5, 2)))
				}
				col = mix(col, face, disc)
			}
			col = mix(col, c.far, clamp(fy-hill(fx, 0)+0.5, 0, 1))
			col = mix(col, c.near, clamp(fy-hill(fx, 1)+0.5, 0, 1))
			cover := clamp(fy-ground(fx)+0.5, 0, 1)
			if cx, cy := (fx-catX)/H, (fy-baseY)/H; cx > -0.45 && cx < 0.75 && cy > -1.15 && cy < 0.1 {
				cover = math.Max(cover, clamp(0.5-catShape(cx, cy)*H, 0, 1))
			}
			col = mix(col, c.ground, cover)
			set(img, x, y, col, 2, seed)
		}
	})
}

// catShape is the cat's signed distance at x, y, in its heights from where
// it sits (y up is negative): a body on its haunches, a round head with two
// ears, and a tail curled round to the right.
func catShape(x, y float64) float64 {
	d := smin(ellipse(x, y+0.36, 0.2, 0.32), ellipse(x, y+0.13, 0.26, 0.14), 0.08)
	d = smin(d, math.Hypot(x-0.02, y+0.8)-0.15, 0.08)
	// The ears lean in a little, their outer edges going on from the head's.
	d = smin(d, triangle(x, y, -0.14, -0.85, -0.015, -0.935, -0.095, -1.04), 0.035)
	d = smin(d, triangle(x, y, 0.055, -0.935, 0.17, -0.85, 0.135, -1.04), 0.035)
	// The tail lies along the ground and curls a little up the side.
	tail := [][2]float64{{0.14, -0.03}, {0.3, 0}, {0.43, -0.03}, {0.5, -0.1}, {0.5, -0.18}}
	for i := 1; i < len(tail); i++ {
		r := 0.042 - 0.003*float64(i)
		d = smin(d, segment(x, y, tail[i-1], tail[i])-r, 0.03)
	}
	return d
}

// smin joins two shapes with a fillet of size k.
func smin(a, b, k float64) float64 {
	h := clamp(0.5+0.5*(b-a)/k, 0, 1)
	return lerp(b, a, h) - k*h*(1-h)
}

// ellipse is the distance to an ellipse of radii rx, ry, nearly.
func ellipse(x, y, rx, ry float64) float64 {
	k0 := math.Hypot(x/rx, y/ry)
	k1 := math.Hypot(x/(rx*rx), y/(ry*ry))
	if k1 == 0 {
		return -math.Min(rx, ry)
	}
	return k0 * (k0 - 1) / k1
}

func segment(x, y float64, a, b [2]float64) float64 {
	px, py, bx, by := x-a[0], y-a[1], b[0]-a[0], b[1]-a[1]
	t := clamp((px*bx+py*by)/(bx*bx+by*by), 0, 1)
	return math.Hypot(px-bx*t, py-by*t)
}

// triangle is the signed distance to the triangle a, b, c.
func triangle(x, y, ax, ay, bx, by, cx, cy float64) float64 {
	pts := [3][2]float64{{ax, ay}, {bx, by}, {cx, cy}}
	d := math.Inf(1)
	inside := true
	for i := range 3 {
		a, b := pts[i], pts[(i+1)%3]
		d = math.Min(d, segment(x, y, a, b))
		// Which side of the edge the point is on, against the third corner.
		o := pts[(i+2)%3]
		side := (b[0]-a[0])*(y-a[1]) - (b[1]-a[1])*(x-a[0])
		ref := (b[0]-a[0])*(o[1]-a[1]) - (b[1]-a[1])*(o[0]-a[0])
		if side*ref < 0 {
			inside = false
		}
	}
	if inside {
		return -d
	}
	return d
}
