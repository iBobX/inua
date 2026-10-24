package wallgen

import (
	"math"
	"math/rand/v2"
)

// noise is seeded gradient noise (Perlin's improved noise, in two
// dimensions): smooth, about -1 to 1, the same for the same seed.
type noise struct{ p [512]uint8 }

func newNoise(seed uint64) *noise {
	r := rand.New(rand.NewPCG(seed, 0x9E3779B97F4A7C15))
	var n noise
	for i, v := range r.Perm(256) {
		n.p[i], n.p[i+256] = uint8(v), uint8(v)
	}
	return &n
}

func fade(t float64) float64 { return t * t * t * (t*(t*6-15) + 10) }

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

func grad(h uint8, x, y float64) float64 {
	switch h & 7 {
	case 0:
		return x + y
	case 1:
		return -x + y
	case 2:
		return x - y
	case 3:
		return -x - y
	case 4:
		return x
	case 5:
		return -x
	case 6:
		return y
	}
	return -y
}

func (n *noise) at(x, y float64) float64 {
	xf, yf := math.Floor(x), math.Floor(y)
	xi, yi := int(xf)&255, int(yf)&255
	x, y = x-xf, y-yf
	u, v := fade(x), fade(y)
	a, b := int(n.p[xi]), int(n.p[xi+1])
	return lerp(
		lerp(grad(n.p[a+yi], x, y), grad(n.p[b+yi], x-1, y), u),
		lerp(grad(n.p[a+yi+1], x, y-1), grad(n.p[b+yi+1], x-1, y-1), u),
		v)
}

// fbm is octaves of noise, each twice as fine and half as strong, turned
// against the one before so the grid does not show; about -1 to 1.
func (n *noise) fbm(x, y float64, octaves int) float64 {
	const c, s = 0.8, 0.6 // a turn of about 37°
	sum, amp, total := 0.0, 1.0, 0.0
	for range octaves {
		sum += amp * n.at(x, y)
		total += amp
		x, y = 2*(c*x-s*y)+17.3, 2*(s*x+c*y)+5.1
		amp /= 2
	}
	return sum / total
}

// hash is a number from 0 to 1 for a pixel, for grain: the same each time.
func hash(x, y int, seed uint64) float64 {
	h := uint64(x)*0x9E3779B185EBCA87 ^ uint64(y)*0xC2B2AE3D27D4EB4F ^ seed
	h ^= h >> 33
	h *= 0xFF51AFD7ED558CCD
	h ^= h >> 33
	return float64(h>>11) / (1 << 53)
}
