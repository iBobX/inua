// Package wallgen draws the wallpapers inua makes itself: pictures made by
// code at the screen's size, another each day (OfDay). Patterns (contours,
// dots …) are drawn in the theme's colours; scenes (a sunset, the cosmos …)
// in their own, at dusk under a dark theme and in daylight under a light
// one. A recipe and a seed make the same picture every time, so it can be
// drawn once and kept.
package wallgen

import (
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Palette is the colours a picture is drawn in, from the theme's tokens.
type Palette struct {
	Ground  color.NRGBA // @app: what most of the picture is
	Field   color.NRGBA // @field: lines, dots and hills
	Accent  color.NRGBA // @accent
	Primary color.NRGBA // @primary
}

// NewPalette reads a palette from a theme's colour tokens.
func NewPalette(tokens map[string]string) (Palette, error) {
	var p Palette
	for _, t := range []struct {
		name string
		c    *color.NRGBA
	}{{"app", &p.Ground}, {"field", &p.Field}, {"accent", &p.Accent}, {"primary", &p.Primary}} {
		c, err := parseHex(tokens[t.name])
		if err != nil {
			return Palette{}, fmt.Errorf("@%s: %w", t.name, err)
		}
		*t.c = c
	}
	return p, nil
}

// Dark is whether the theme is a dark one: its ground is.
func (p Palette) Dark() bool { return luma(rgbOf(p.Ground)) < 0.5 }

// recipe is one kind of picture; a seed picks one of its variations.
type recipe struct {
	name string
	draw func(img *image.NRGBA, p Palette, seed uint64)
}

// recipes are the pictures there are, in the order the days bring them: a
// scene, then a pattern.
var recipes = []recipe{
	{"sunset", sunset},
	{"contours", contours},
	{"cosmos", cosmos},
	{"dunes", dunes},
	{"cat", cat},
	{"aurora", aurora},
	{"dots", dots},
}

// OfDay is the picture of t's day: each day the next recipe, and each time
// round another variation.
func OfDay(t time.Time) (name string, seed uint64) {
	y, m, d := t.Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400
	n := int64(len(recipes))
	return recipes[day%n].name, uint64(day / n)
}

// Another is a picture chosen at random, of another recipe than name.
func Another(name string) (string, uint64) {
	i := slices.IndexFunc(recipes, func(r recipe) bool { return r.name == name })
	j := rand.IntN(len(recipes) - 1)
	if i >= 0 && j >= i {
		j++
	}
	return recipes[j].name, rand.Uint64()
}

// Draw makes the picture name, in its variation seed, at w×h pixels.
func Draw(name string, w, h int, p Palette, seed uint64) (*image.NRGBA, error) {
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("size %d×%d", w, h)
	}
	i := slices.IndexFunc(recipes, func(r recipe) bool { return r.name == name })
	if i < 0 {
		return nil, fmt.Errorf("no picture %q", name)
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	recipes[i].draw(img, p, seed)
	return img, nil
}

// Key names a picture as drawn: the recipe, the seed, the colours and the
// size. Two pictures with one key are the same, so a file named by it can be
// kept and shown again.
func Key(name string, w, h int, p Palette, seed uint64) string {
	sum := fnv.New32a()
	for _, c := range []color.NRGBA{p.Ground, p.Field, p.Accent, p.Primary} {
		sum.Write([]byte{c.R, c.G, c.B})
	}
	return fmt.Sprintf("%s-%d-%08x-%dx%d-v%d", name, seed, sum.Sum32(), w, h, version)
}

// version changes when a recipe draws differently, so kept pictures are
// drawn again.
const version = 1

// --- what the recipes share ---

// rgb is a colour with channels from 0 to 1, to mix in.
type rgb [3]float64

func rgbOf(c color.NRGBA) rgb {
	return rgb{float64(c.R) / 255, float64(c.G) / 255, float64(c.B) / 255}
}

func mix(a, b rgb, t float64) rgb {
	return rgb{lerp(a[0], b[0], t), lerp(a[1], b[1], t), lerp(a[2], b[2], t)}
}

// luma is a colour's lightness, from 0 to 1.
func luma(c rgb) float64 { return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2] }

// set writes c at x, y with grain: noise of ±grain/255 that also keeps a
// gradient from showing bands.
func set(img *image.NRGBA, x, y int, c rgb, grain float64, seed uint64) {
	g := (hash(x, y, seed) - 0.5) * grain / 255
	i := img.PixOffset(x, y)
	for k := range 3 {
		img.Pix[i+k] = uint8(clamp(c[k]+g, 0, 1)*255 + 0.5)
	}
	img.Pix[i+3] = 255
}

// add lights the pixel at x, y by c×v, as a star or a glow does.
func add(img *image.NRGBA, x, y int, c rgb, v float64) {
	if !image.Pt(x, y).In(img.Bounds()) {
		return
	}
	i := img.PixOffset(x, y)
	for k := range 3 {
		img.Pix[i+k] = uint8(clamp(float64(img.Pix[i+k])/255+c[k]*v, 0, 1)*255 + 0.5)
	}
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func smoothstep(a, b, v float64) float64 {
	t := clamp((v-a)/(b-a), 0, 1)
	return t * t * (3 - 2*t)
}

// rows runs f for every row, on every core.
func rows(h int, f func(y int)) {
	var next atomic.Int64
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for {
				y := int(next.Add(1) - 1)
				if y >= h {
					return
				}
				f(y)
			}
		})
	}
	wg.Wait()
}

// unit is the picture's measure: its short side, so a picture looks the
// same at every size.
func unit(img *image.NRGBA) float64 {
	b := img.Bounds()
	return float64(min(b.Dx(), b.Dy()))
}

// hex is a colour written in the code.
func hex(s string) rgb {
	c, err := parseHex(s)
	if err != nil {
		panic(err)
	}
	return rgbOf(c)
}

func parseHex(s string) (color.NRGBA, error) {
	if len(s) != 7 || s[0] != '#' {
		return color.NRGBA{}, fmt.Errorf("colour %q: want #rrggbb", s)
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return color.NRGBA{}, fmt.Errorf("colour %q: %w", s, err)
	}
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}, nil
}
