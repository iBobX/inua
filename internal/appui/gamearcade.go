package appui

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"

	"github.com/lyricat/inua/internal/apps/games"
)

// The arcade field is rasterized in Go and uploaded directly as RGBA,
// without PNG encoding or a new drawing dependency. GTK scales the texture
// to the available window size, keeping the physics coordinates fixed.
func (a *gamesApp) drawArcade() {
	w, h := 600, 400
	if a.current == "space-pinball" {
		w, h = 420, 560
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	bg, fg := hexColor(a.c.Colors["surface"]), hexColor(a.c.Colors["t0"])
	line, accent := hexColor(a.c.Colors["line"]), hexColor(a.c.Colors["accent"])
	muted, ok := hexColor(a.c.Colors["surface2"]), hexColor(a.c.Colors["ok"])
	draw.Draw(img, img.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	if a.current == "harbor" {
		for y := 20; y < 400; y += 30 {
			for x := 20; x < 600; x += 40 {
				gameLine(img, games.Vec{X: float64(x), Y: float64(y)}, games.Vec{X: float64(x + 9), Y: float64(y)}, 1, line)
			}
		}
		for _, r := range a.boat.Rocks {
			gameCircle(img, r.X, r.Y, r.Radius+5, line)
			gameCircle(img, r.X, r.Y, r.Radius, muted)
			gameLine(img, games.Vec{X: r.X - 8, Y: r.Y + 4}, games.Vec{X: r.X + 4, Y: r.Y - 7}, 2, line)
		}
		for x := 505; x < 580; x += 8 {
			gameLine(img, games.Vec{X: float64(x), Y: 52}, games.Vec{X: float64(x), Y: 108}, 2, ok)
		}
		draw.Draw(img, image.Rect(505, 62, 575, 98), image.NewUniform(bg), image.Point{}, draw.Src)
		gameText(img, 511, 84, "DOCK  >", ok)
		p, angle := a.boat.Position, a.boat.Angle
		var hull []games.Vec
		for _, v := range []games.Vec{{X: 16, Y: 0}, {X: -11, Y: -8}, {X: -8, Y: 0}, {X: -11, Y: 8}} {
			hull = append(hull, games.Vec{X: p.X + v.X*math.Cos(angle) - v.Y*math.Sin(angle), Y: p.Y + v.X*math.Sin(angle) + v.Y*math.Cos(angle)})
		}
		gamePolygon(img, hull, accent)
		gameCircle(img, p.X, p.Y, 3, bg)
	} else {
		// A restrained star chart, with orbit rings around the bumpers.
		for i := 0; i < 45; i++ {
			x, y := (i*73+31)%380+20, (i*97+43)%410+25
			gameCircle(img, float64(x), float64(y), 1, line)
		}
		gameText(img, 130, 57, "O R B I T A L   0 1", fg)
		for i, b := range games.Bumpers {
			gameCircle(img, b.X, b.Y, b.Radius+8, line)
			gameCircle(img, b.X, b.Y, b.Radius+6, bg)
			gameCircle(img, b.X, b.Y, b.Radius, accent)
			if a.pinball.Cooldown[i] > 0 {
				gameCircle(img, b.X, b.Y, b.Radius, ok)
			}
			gameText(img, int(b.X)-10, int(b.Y)+5, "100", bg)
		}
		gameLine(img, games.Vec{X: 10, Y: 550}, games.Vec{X: 10, Y: 10}, 4, line)
		gameLine(img, games.Vec{X: 10, Y: 10}, games.Vec{X: 410, Y: 10}, 4, line)
		gameLine(img, games.Vec{X: 410, Y: 10}, games.Vec{X: 410, Y: 550}, 4, line)
		for _, rail := range games.PinballRails {
			gameLine(img, rail[0], rail[1], 4, accent)
		}
		for _, f := range a.pinball.Flippers() {
			gameLine(img, f[0], f[1], 12, accent)
			gameCircle(img, f[0].X, f[0].Y, 6, fg)
			gameCircle(img, f[1].X, f[1].Y, 6, accent)
		}
		gameText(img, 110, 530, "A / LEFT       D / RIGHT", fg)
		p := a.pinball.Ball
		gameCircle(img, p.X+2, p.Y+3, 9, line)
		gameCircle(img, p.X, p.Y, 8, fg)
		gameCircle(img, p.X-2, p.Y-2, 3, bg)
	}
	bytes := glib.NewBytes(img.Pix, uint(len(img.Pix)))
	tex := gdk.NewMemoryTexture(int32(w), int32(h), gdk.MemoryR8g8b8a8PremultipliedValue, bytes, uint(img.Stride))
	bytes.Unref()
	a.field.SetPaintable(tex)
	tex.Unref()
}

func gameCircle(img *image.RGBA, x, y, r float64, c color.NRGBA) {
	for py := int(y - r - 1); py <= int(y+r+1); py++ {
		for px := int(x - r - 1); px <= int(x+r+1); px++ {
			d := math.Hypot(float64(px)+0.5-x, float64(py)+0.5-y)
			if d < r && image.Pt(px, py).In(img.Bounds()) {
				coverage := min(1, r-d)
				old := img.RGBAAt(px, py)
				img.SetRGBA(px, py, color.RGBA{R: uint8(float64(c.R)*coverage + float64(old.R)*(1-coverage)), G: uint8(float64(c.G)*coverage + float64(old.G)*(1-coverage)), B: uint8(float64(c.B)*coverage + float64(old.B)*(1-coverage)), A: 255})
			}
		}
	}
}

func gameLine(img *image.RGBA, a, b games.Vec, width float64, c color.NRGBA) {
	dx, dy := b.X-a.X, b.Y-a.Y
	d := math.Hypot(dx, dy)
	if d == 0 {
		return
	}
	x, y := -dy/d*width/2, dx/d*width/2
	gamePolygon(img, []games.Vec{{X: a.X + x, Y: a.Y + y}, {X: b.X + x, Y: b.Y + y}, {X: b.X - x, Y: b.Y - y}, {X: a.X - x, Y: a.Y - y}}, c)
}

func gamePolygon(img *image.RGBA, points []games.Vec, c color.NRGBA) {
	minX, minY, maxX, maxY := points[0].X, points[0].Y, points[0].X, points[0].Y
	for _, p := range points {
		minX, minY, maxX, maxY = min(minX, p.X), min(minY, p.Y), max(maxX, p.X), max(maxY, p.Y)
	}
	bounds := image.Rect(int(math.Floor(minX))-1, int(math.Floor(minY))-1, int(math.Ceil(maxX))+1, int(math.Ceil(maxY))+1).Intersect(img.Bounds())
	if bounds.Empty() {
		return
	}
	x, y := float64(bounds.Min.X), float64(bounds.Min.Y)
	r := vector.NewRasterizer(bounds.Dx(), bounds.Dy())
	r.MoveTo(float32(points[0].X-x), float32(points[0].Y-y))
	for _, p := range points[1:] {
		r.LineTo(float32(p.X-x), float32(p.Y-y))
	}
	r.ClosePath()
	r.Draw(img, bounds, image.NewUniform(c), image.Point{})
}

func gameText(img *image.RGBA, x, y int, text string, c color.NRGBA) {
	d := font.Drawer{Dst: img, Src: image.NewUniform(c), Face: basicfont.Face7x13, Dot: fixed.P(x, y)}
	d.DrawString(text)
}
