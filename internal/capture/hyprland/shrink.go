package hyprland

// Shrink copies a compositor frame into a buffer that fits maxW×maxH and
// never enlarges it. Area-average downscale keeps text readable without
// holding the full capture. maxW or maxH <= 0 uses a 720px long-edge
// fallback, enough for the first frame before layout is known. A 1920 cap
// stops one fullscreen card from storing a 4K buffer.
func Shrink(f Frame, maxW, maxH int) Frame {
	const (
		fallbackEdge = 720
		capEdge      = 1920
	)
	if f.Width <= 0 || f.Height <= 0 || f.Stride < f.Width*4 || len(f.Pixels) < f.Stride*f.Height {
		return Frame{Address: f.Address}
	}
	if maxW <= 0 || maxH <= 0 {
		maxW, maxH = fallbackEdge, fallbackEdge
	}
	maxW, maxH = min(maxW, capEdge), min(maxH, capEdge)
	w, h := fit(f.Width, f.Height, maxW, maxH)
	pixels := make([]byte, w*h*4)
	if w == f.Width && h == f.Height {
		for y := 0; y < h; y++ {
			srcY := y
			if f.YInvert {
				srcY = f.Height - 1 - y
			}
			copy(pixels[y*w*4:(y+1)*w*4], f.Pixels[srcY*f.Stride:srcY*f.Stride+w*4])
		}
		f.Width, f.Height, f.Stride, f.Pixels, f.YInvert = w, h, w*4, pixels, false
		return f
	}
	for y := 0; y < h; y++ {
		sy0 := y * f.Height / h
		sy1 := min(f.Height, (y+1)*f.Height/h)
		if sy1 <= sy0 {
			sy1 = min(f.Height, sy0+1)
		}
		if f.YInvert {
			sy0, sy1 = f.Height-sy1, f.Height-sy0
		}
		for x := 0; x < w; x++ {
			sx0 := x * f.Width / w
			sx1 := min(f.Width, (x+1)*f.Width/w)
			if sx1 <= sx0 {
				sx1 = min(f.Width, sx0+1)
			}
			var c0, c1, c2, c3, n int
			for sy := sy0; sy < sy1; sy++ {
				row := sy * f.Stride
				for sx := sx0; sx < sx1; sx++ {
					src := row + sx*4
					c0 += int(f.Pixels[src])
					c1 += int(f.Pixels[src+1])
					c2 += int(f.Pixels[src+2])
					c3 += int(f.Pixels[src+3])
					n++
				}
			}
			if n == 0 {
				continue
			}
			dst := (y*w + x) * 4
			pixels[dst] = byte(c0 / n)
			pixels[dst+1] = byte(c1 / n)
			pixels[dst+2] = byte(c2 / n)
			pixels[dst+3] = byte(c3 / n)
		}
	}
	f.Width, f.Height, f.Stride, f.Pixels, f.YInvert = w, h, w*4, pixels, false
	return f
}

func fit(w, h, maxW, maxH int) (int, int) {
	if w <= 0 || h <= 0 || maxW <= 0 || maxH <= 0 {
		return 1, 1
	}
	if w <= maxW && h <= maxH {
		return w, h
	}
	fw, fh := float64(maxW)/float64(w), float64(maxH)/float64(h)
	s := min(fw, fh)
	return max(1, int(float64(w)*s)), max(1, int(float64(h)*s))
}
