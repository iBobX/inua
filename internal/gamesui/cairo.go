package gamesui

import (
	"image/color"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"codeberg.org/puregotk/purego"
	"codeberg.org/puregotk/puregotk/v4/cairo"
	"codeberg.org/puregotk/puregotk/v4/pango"
	"codeberg.org/puregotk/puregotk/v4/pangocairo"
)

// The boards are drawn with cairo in a GtkDrawingArea: GTK scales them
// for the display, and Pango sets their text in the theme's fonts.
// puregotk binds cairo's types but none of its drawing, so the few calls
// the games use are bound here with purego, from the libcairo GTK has
// already loaded.

var cr struct {
	once                              sync.Once
	save, restore, newPath, closePath func(c uintptr)
	fill, stroke, fillPreserve, paint func(c uintptr)
	clip, newSubPath                  func(c uintptr)
	translate, scale, moveTo, lineTo  func(c uintptr, x, y float64)
	rotate, setLineWidth              func(c uintptr, v float64)
	setSourceRGBA                     func(c uintptr, r, g, b, a float64)
	arc, arcNegative                  func(c uintptr, x, y, r, a1, a2 float64)
	rectangle                         func(c uintptr, x, y, w, h float64)
	curveTo                           func(c uintptr, x1, y1, x2, y2, x3, y3 float64)
	setLineCap, setLineJoin           func(c uintptr, v int32)
	setDash                           func(c uintptr, dashes *float64, n int32, offset float64)
	radial                            func(x0, y0, r0, x1, y1, r1 float64) uintptr
	addStop                           func(p uintptr, offset, r, g, b, a float64)
	setSource                         func(c, p uintptr)
	destroyPattern                    func(p uintptr)
	linear                            func(x0, y0, x1, y1 float64) uintptr
	imageSurface                      func(format, w, h int32) uintptr
	create                            func(s uintptr) unsafe.Pointer
	destroy, destroySurface           func(p uintptr)
	flushSurface, dirtySurface        func(s uintptr)
	surfaceData                       func(s uintptr) unsafe.Pointer
	surfaceStride                     func(s uintptr) int32
	surfacePattern                    func(s uintptr) uintptr
	setExtend, setFilter              func(p uintptr, v int32)
	userToDeviceDistance              func(c uintptr, dx, dy *float64)
}

func loadCairo() {
	cr.once.Do(func() {
		lib, err := purego.Dlopen("libcairo.so.2", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			panic("gamesui: " + err.Error())
		}
		for name, f := range map[string]any{
			"cairo_save": &cr.save, "cairo_restore": &cr.restore, "cairo_new_path": &cr.newPath,
			"cairo_close_path": &cr.closePath, "cairo_fill": &cr.fill, "cairo_stroke": &cr.stroke,
			"cairo_fill_preserve": &cr.fillPreserve, "cairo_paint": &cr.paint, "cairo_clip": &cr.clip,
			"cairo_new_sub_path": &cr.newSubPath, "cairo_translate": &cr.translate, "cairo_scale": &cr.scale,
			"cairo_move_to": &cr.moveTo, "cairo_line_to": &cr.lineTo, "cairo_rotate": &cr.rotate,
			"cairo_set_line_width": &cr.setLineWidth, "cairo_set_source_rgba": &cr.setSourceRGBA,
			"cairo_arc": &cr.arc, "cairo_arc_negative": &cr.arcNegative, "cairo_rectangle": &cr.rectangle,
			"cairo_curve_to": &cr.curveTo, "cairo_set_line_cap": &cr.setLineCap,
			"cairo_set_line_join": &cr.setLineJoin, "cairo_set_dash": &cr.setDash,
			"cairo_pattern_create_radial": &cr.radial, "cairo_pattern_add_color_stop_rgba": &cr.addStop,
			"cairo_set_source": &cr.setSource, "cairo_pattern_destroy": &cr.destroyPattern,
			"cairo_pattern_create_linear": &cr.linear, "cairo_image_surface_create": &cr.imageSurface,
			"cairo_create": &cr.create, "cairo_destroy": &cr.destroy, "cairo_surface_destroy": &cr.destroySurface,
			"cairo_surface_flush": &cr.flushSurface, "cairo_surface_mark_dirty": &cr.dirtySurface,
			"cairo_image_surface_get_data": &cr.surfaceData, "cairo_image_surface_get_stride": &cr.surfaceStride,
			"cairo_pattern_create_for_surface": &cr.surfacePattern, "cairo_pattern_set_extend": &cr.setExtend,
			"cairo_pattern_set_filter": &cr.setFilter, "cairo_user_to_device_distance": &cr.userToDeviceDistance,
		} {
			purego.RegisterLibFunc(f, lib, name)
		}
	})
}

// pen draws on one frame's cairo context, in the board's own units.
type pen struct {
	c   uintptr
	ctx *cairo.Context
}

func newPen(ctx *cairo.Context) pen {
	loadCairo()
	return pen{ctx.GoPointer(), ctx}
}

func (p pen) Save()                                { cr.save(p.c) }
func (p pen) Restore()                             { cr.restore(p.c) }
func (p pen) Translate(x, y float64)               { cr.translate(p.c, x, y) }
func (p pen) Scale(x, y float64)                   { cr.scale(p.c, x, y) }
func (p pen) Rotate(a float64)                     { cr.rotate(p.c, a) }
func (p pen) MoveTo(x, y float64)                  { cr.moveTo(p.c, x, y) }
func (p pen) LineTo(x, y float64)                  { cr.lineTo(p.c, x, y) }
func (p pen) Close()                               { cr.closePath(p.c) }
func (p pen) NewPath()                             { cr.newPath(p.c) }
func (p pen) Fill()                                { cr.fill(p.c) }
func (p pen) FillKeep()                            { cr.fillPreserve(p.c) }
func (p pen) Stroke()                              { cr.stroke(p.c) }
func (p pen) Clip()                                { cr.clip(p.c) }
func (p pen) Paint()                               { cr.paint(p.c) }
func (p pen) Width(w float64)                      { cr.setLineWidth(p.c, w) }
func (p pen) Rect(x, y, w, h float64)              { cr.rectangle(p.c, x, y, w, h) }
func (p pen) Color(c color.NRGBA)                  { p.Alpha(c, 1) }
func (p pen) Round()                               { cr.setLineCap(p.c, 1); cr.setLineJoin(p.c, 1) }
func (p pen) Curve(x1, y1, x2, y2, x3, y3 float64) { cr.curveTo(p.c, x1, y1, x2, y2, x3, y3) }

// Alpha sets a colour at a fraction of its opacity.
func (p pen) Alpha(c color.NRGBA, a float64) {
	cr.setSourceRGBA(p.c, float64(c.R)/255, float64(c.G)/255, float64(c.B)/255, float64(c.A)/255*a)
}

// Arc adds an arc to the path, as a new piece of it.
func (p pen) Arc(x, y, r, a1, a2 float64) {
	cr.newSubPath(p.c)
	cr.arc(p.c, x, y, r, a1, a2)
}

// ArcOn continues the path along an arc, clockwise (angles growing).
func (p pen) ArcOn(x, y, r, a1, a2 float64) { cr.arc(p.c, x, y, r, a1, a2) }

// ArcBack continues the path along an arc, anticlockwise.
func (p pen) ArcBack(x, y, r, a1, a2 float64) { cr.arcNegative(p.c, x, y, r, a1, a2) }

// Circle adds a circle to the path.
func (p pen) Circle(x, y, r float64) { p.Arc(x, y, r, 0, 2*math.Pi) }

// Line strokes one segment.
func (p pen) Line(x1, y1, x2, y2 float64) {
	p.MoveTo(x1, y1)
	p.LineTo(x2, y2)
	p.Stroke()
}

// RoundRect adds a rectangle with corners of radius r to the path.
func (p pen) RoundRect(x, y, w, h, r float64) {
	r = min(r, w/2, h/2)
	cr.newSubPath(p.c)
	cr.arc(p.c, x+w-r, y+r, r, -math.Pi/2, 0)
	cr.arc(p.c, x+w-r, y+h-r, r, 0, math.Pi/2)
	cr.arc(p.c, x+r, y+h-r, r, math.Pi/2, math.Pi)
	cr.arc(p.c, x+r, y+r, r, math.Pi, 3*math.Pi/2)
	cr.closePath(p.c)
}

// Dash strokes the next lines dashed; no lengths, solid again.
func (p pen) Dash(lengths ...float64) {
	if len(lengths) == 0 {
		cr.setDash(p.c, nil, 0, 0)
		return
	}
	cr.setDash(p.c, &lengths[0], int32(len(lengths)), 0)
}

// Glow fills a soft disc of c, opaque at its centre and gone at r.
func (p pen) Glow(x, y, r float64, c color.NRGBA, a float64) {
	pat := cr.radial(x, y, 0, x, y, r)
	f := func(v uint8) float64 { return float64(v) / 255 }
	cr.addStop(pat, 0, f(c.R), f(c.G), f(c.B), a)
	cr.addStop(pat, 1, f(c.R), f(c.G), f(c.B), 0)
	cr.setSource(p.c, pat)
	p.Circle(x, y, r)
	p.Fill()
	cr.destroyPattern(pat)
}

// Gradient sets a linear gradient as the source, from c0 at x0, y0 to c1
// at x1, y1, for the next fill or stroke.
func (p pen) Gradient(x0, y0, x1, y1 float64, c0, c1 color.NRGBA) {
	pat := cr.linear(x0, y0, x1, y1)
	f := func(v uint8) float64 { return float64(v) / 255 }
	cr.addStop(pat, 0, f(c0.R), f(c0.G), f(c0.B), f(c0.A))
	cr.addStop(pat, 1, f(c1.R), f(c1.G), f(c1.B), f(c1.A))
	cr.setSource(p.c, pat)
	cr.destroyPattern(pat) // the context holds it now
}

// DeviceScale is how many device pixels one unit is, as the pen draws now.
func (p pen) DeviceScale() float64 {
	dx, dy := 1.0, 0.0
	cr.userToDeviceDistance(p.c, &dx, &dy)
	return math.Hypot(dx, dy)
}

// image is a picture drawn once off screen and painted from then on.
type image struct {
	s    uintptr
	w, h int
}

// newImage makes a transparent image of w by h pixels; Free frees it.
func newImage(w, h int) *image {
	loadCairo()
	return &image{cr.imageSurface(0, int32(w), int32(h)), w, h} // CAIRO_FORMAT_ARGB32
}

func (im *image) Free() { cr.destroySurface(im.s) }

// Draw gives a pen on the image to draw with, in pixels.
func (im *image) Draw(draw func(pn pen)) {
	c := cr.create(im.s)
	draw(pen{uintptr(c), (*cairo.Context)(c)})
	cr.destroy(uintptr(c))
	cr.flushSurface(im.s)
}

// Speckle scatters n dots of c over the image, each at a random fraction
// of alpha a: paper's grain.
func (im *image) Speckle(r *rand.Rand, n int, c color.NRGBA, a float64) {
	cr.flushSurface(im.s)
	stride := int(cr.surfaceStride(im.s))
	px := unsafe.Slice((*byte)(cr.surfaceData(im.s)), stride*im.h)
	for range n {
		x, y := r.IntN(im.w), r.IntN(im.h)
		al := a * (0.3 + 0.7*r.Float64())
		i := y*stride + x*4 // premultiplied BGRA, little-endian ARGB32
		over := func(dst *byte, v uint8) { *dst = uint8(float64(v)*al + float64(*dst)*(1-al)) }
		over(&px[i], c.B)
		over(&px[i+1], c.G)
		over(&px[i+2], c.R)
		px[i+3] = uint8(al*255 + float64(px[i+3])*(1-al))
	}
	cr.dirtySurface(im.s)
}

// Image paints im at x, y, scaled by k (pixels to units), smoothly.
func (p pen) Image(im *image, x, y, k float64) {
	p.Save()
	p.Translate(x, y)
	p.Scale(k, k)
	pat := cr.surfacePattern(im.s)
	cr.setFilter(pat, 1) // CAIRO_FILTER_GOOD
	cr.setSource(p.c, pat)
	cr.destroyPattern(pat)
	p.Rect(0, 0, float64(im.w), float64(im.h))
	p.Fill()
	p.Restore()
}

// Blit lays im at the origin, k units to its pixel: without smoothing,
// for an image drawn at the scale it is laid at, or else smoothed simply.
func (p pen) Blit(im *image, k float64, smooth bool) {
	p.Save()
	p.Scale(k, k)
	pat := cr.surfacePattern(im.s)
	cr.setFilter(pat, map[bool]int32{false: 0, true: 4}[smooth]) // CAIRO_FILTER_FAST, _BILINEAR
	cr.setSource(p.c, pat)
	cr.destroyPattern(pat)
	p.Rect(0, 0, float64(im.w), float64(im.h))
	p.Fill()
	p.Restore()
}

// Texture fills the current path with im repeated, k units to its pixel.
func (p pen) Texture(im *image, k float64) {
	p.Save()
	p.Scale(k, k)
	pat := cr.surfacePattern(im.s)
	cr.setExtend(pat, 1) // CAIRO_EXTEND_REPEAT
	cr.setSource(p.c, pat)
	cr.destroyPattern(pat)
	p.Fill() // the path keeps the place it was given; the scale is the texture's
	p.Restore()
}

// Text sets s in font (a Pango description: "IBM Plex Sans 12") at x, y,
// placed by ax and ay: 0 left or top, 0.5 centre, 1 right or bottom; c's
// alpha fades it. It returns the text's width and height.
func (p pen) Text(s, font string, x, y, ax, ay float64, c color.NRGBA) (float64, float64) {
	l := pangocairo.CreateLayout(p.ctx)
	defer l.Unref()
	d := pango.FontDescriptionFromString(font)
	l.SetFontDescription(d)
	d.Free()
	l.SetText(s, -1)
	var w, h int32
	l.GetPixelSize(&w, &h)
	p.Color(c)
	p.MoveTo(x-float64(w)*ax, y-float64(h)*ay)
	pangocairo.ShowLayout(p.ctx, l)
	p.NewPath()
	return float64(w), float64(h)
}

// TextBox sets s wrapped to width w, centred, with its top centre at
// x, y; it returns the height it took.
func (p pen) TextBox(s, font string, x, y, w float64, c color.NRGBA) float64 {
	l := pangocairo.CreateLayout(p.ctx)
	defer l.Unref()
	d := pango.FontDescriptionFromString(font)
	l.SetFontDescription(d)
	d.Free()
	l.SetWidth(int32(w * float64(pango.SCALE)))
	l.SetWrap(pango.WrapWordValue)
	l.SetAlignment(pango.AlignCenterValue)
	l.SetText(s, -1)
	var lw, lh int32
	l.GetPixelSize(&lw, &lh)
	p.Color(c)
	p.MoveTo(x-w/2, y)
	pangocairo.ShowLayout(p.ctx, l)
	p.NewPath()
	return float64(lh)
}

// TextWrap sets s wrapped to width w, left aligned, with its top left at
// x, y; it returns the height it took.
func (p pen) TextWrap(s, font string, x, y, w float64, c color.NRGBA) float64 {
	l := pangocairo.CreateLayout(p.ctx)
	defer l.Unref()
	d := pango.FontDescriptionFromString(font)
	l.SetFontDescription(d)
	d.Free()
	l.SetWidth(int32(w * float64(pango.SCALE)))
	l.SetWrap(pango.WrapWordValue)
	l.SetText(s, -1)
	var lw, lh int32
	l.GetPixelSize(&lw, &lh)
	p.Color(c)
	p.MoveTo(x, y)
	pangocairo.ShowLayout(p.ctx, l)
	p.NewPath()
	return float64(lh)
}

// hexColor is a theme colour, "#rrggbb".
func hexColor(s string) color.NRGBA {
	v, err := strconv.ParseUint(strings.TrimPrefix(s, "#"), 16, 32)
	if err != nil || len(s) != 7 {
		return color.NRGBA{128, 128, 128, 255}
	}
	return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}
}

// mix is a between b and c: 0 is b, 1 is c.
func mix(b, c color.NRGBA, t float64) color.NRGBA {
	f := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return color.NRGBA{f(b.R, c.R), f(b.G, c.G), f(b.B, c.B), 255}
}
