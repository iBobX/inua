package gtkx

import (
	"math"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// Wave is a recording's waveform: a bar for each level added (0 to 1),
// centred on the middle line and a pixel tall when silent, the newest at
// the right, as many as fit; the oldest scroll off to the left. It is
// drawn in the widget's CSS colour.
type Wave struct {
	Widget *gtk.Widget
	levels []float64
}

const (
	waveBar   = 2 // px
	waveStep  = 4 // px, a bar and its gap
	maxLevels = 2048
)

// NewWave makes a waveform height pixels tall.
func NewWave(height int) *Wave {
	w := &Wave{}
	w.Widget = NewCanvas(&Canvas{Snapshot: w.snapshot, Min: height})
	return w
}

// Add adds a level and draws it.
func (w *Wave) Add(level float64) {
	if len(w.levels) >= maxLevels {
		w.levels = append(w.levels[:0], w.levels[len(w.levels)-maxLevels/2:]...)
	}
	w.levels = append(w.levels, level)
	w.Widget.QueueDraw()
}

// Clear empties it, for the next recording.
func (w *Wave) Clear() {
	w.levels = w.levels[:0]
	w.Widget.QueueDraw()
}

func (w *Wave) snapshot(wd *gtk.Widget, s *gtk.Snapshot) {
	var c gdk.RGBA
	wd.GetColor(&c)
	width, h := float32(wd.GetWidth()), float32(wd.GetHeight())
	levels := w.levels[max(0, len(w.levels)-int(width/waveStep)):]
	x := width - float32(len(levels))*waveStep
	for _, l := range levels {
		bh := max(1, float32(math.Round(l*float64(h))))
		var r graphene.Rect
		r.Init(x, float32(math.Round(float64(h-bh)/2)), waveBar, bh)
		s.AppendColor(&c, &r)
		x += waveStep
	}
}
