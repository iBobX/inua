package diskui

import (
	"image/color"
	"math"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gsk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/disk"
	"github.com/lyricat/inua/internal/gtkx"
)

// The map is drawn as GSK paths when GTK draws it: a slice is two arcs and
// two lines, filled; the guides are circles. While a scan runs, the frame
// clock turns the sweep, so it moves at the display's rate and costs a
// few paths a frame, not a picture.

// mapNatural is the map's side when there is the room, in pixels; it is
// mapSize at least.
const mapNatural = 620

// sweepTurn is how long the sweep takes to go round, in seconds.
const sweepTurn = 2.4

func (w *window) buildMap() *gtk.Widget {
	c := gtkx.NewCanvas(&gtkx.Canvas{Snapshot: w.snapshotMap, Min: mapSize, Natural: mapNatural})
	c.AddCssClass("disk-map")
	return c
}

// snapshotMap draws the arcs drawMap chose.
func (w *window) snapshotMap(wd *gtk.Widget, s *gtk.Snapshot) {
	g := disk.Measure(float64(wd.GetWidth()), float64(wd.GetHeight()))
	p := w.paint
	if p.Guide.A > 0 {
		stroke := gsk.NewStroke(1)
		for _, r := range g.Guides() {
			pb := gsk.NewPathBuilder()
			pb.AddCircle(&graphene.Point{X: float32(g.CX), Y: float32(g.CY)}, float32(r))
			path := pb.FreeToPath()
			col := rgbaOf(p.Guide)
			s.AppendStroke(path, stroke, &col)
			path.Unref()
		}
		stroke.Free()
	}
	if p.Middle.A > 0 {
		pb := gsk.NewPathBuilder()
		pb.AddCircle(&graphene.Point{X: float32(g.CX), Y: float32(g.CY)}, float32(g.Middle-disk.Gap/2))
		path := pb.FreeToPath()
		col := rgbaOf(p.Middle)
		s.AppendFill(path, gsk.FillRuleWindingValue, &col)
		path.Unref()
	}
	// Each slice over the ground first: the guides do not show through a
	// slice drawn lighter.
	ground := rgbaOf(hexColor(w.c.Colors["app"]))
	for _, sh := range disk.Shapes(w.drawn, g, p) {
		path, rule := slicePath(g, sh)
		col := rgbaOf(sh.Color)
		s.AppendFill(path, rule, &ground)
		s.AppendFill(path, rule, &col)
		path.Unref()
	}
}

// slicePath is a slice's outline: the outer arc clockwise, the inner one
// back; a whole ring is two circles, filled even-odd.
func slicePath(g disk.Geometry, sh disk.Shape) (*gsk.Path, gsk.FillRule) {
	pb := gsk.NewPathBuilder()
	c := graphene.Point{X: float32(g.CX), Y: float32(g.CY)}
	if sh.A1-sh.A0 >= 2*math.Pi-1e-6 {
		pb.AddCircle(&c, float32(sh.R1))
		pb.AddCircle(&c, float32(sh.R0))
		return pb.FreeToPath(), gsk.FillRuleEvenOddValue
	}
	at := func(r, a float64) (float32, float32) {
		return float32(g.CX + r*math.Sin(a)), float32(g.CY - r*math.Cos(a))
	}
	large := sh.A1-sh.A0 > math.Pi
	x, y := at(sh.R1, sh.A0)
	pb.MoveTo(x, y)
	x, y = at(sh.R1, sh.A1)
	pb.SvgArcTo(float32(sh.R1), float32(sh.R1), 0, large, true, x, y)
	x, y = at(sh.R0, sh.A1)
	pb.LineTo(x, y)
	x, y = at(sh.R0, sh.A0)
	pb.SvgArcTo(float32(sh.R0), float32(sh.R0), 0, large, false, x, y)
	pb.Close()
	return pb.FreeToPath(), gsk.FillRuleWindingValue
}

func rgbaOf(c color.NRGBA) gdk.RGBA {
	return gdk.RGBA{Red: float32(c.R) / 255, Green: float32(c.G) / 255, Blue: float32(c.B) / 255, Alpha: float32(c.A) / 255}
}

// sweep turns the scanning map with the frame clock while a scan of the
// place in view runs; it stops by itself once none does.
func (w *window) sweep(s *scanState, path string) {
	if w.ticking {
		return
	}
	w.ticking = true
	lastText := int64(0)
	w.onTick = func(_, clock, _ uintptr) bool {
		if w.closed || w.scans[path] != s || !s.running() {
			w.ticking = false
			return false
		}
		if w.page == "place" && w.place == path {
			now := gdk.FrameClockNewFromInternalPtr(clock).GetFrameTime() // µs
			s.phase = float64(now-s.started.UnixMicro()) / 1e6 / sweepTurn * 2 * math.Pi
			w.drawMap()
			if now-lastText > 100_000 { // the numbers, ten times a second
				lastText = now
				w.showProgress(s)
			}
		}
		return true
	}
	w.mapCanvas.AddTickCallback(&w.onTick, 0, &w.onTickDone)
}
