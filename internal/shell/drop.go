package shell

import "math"

// A panel that drops from the bar comes down from the bar's lower edge
// the way a drop of water leaves a branch, and is as soft: it gathers a
// moment, falls faster and faster, stretching as it goes, and on landing
// wobbles, long and narrow then short and wide, until it is still. Its top
// edge stays on the bar throughout; closing, the bar draws it back up,
// stretched by the pull. DropAt gives the panel's shape at a moment.

// DropOpenMS and DropCloseMS are how long the drop takes each way.
const (
	DropOpenMS  = 620
	DropCloseMS = 300
)

// Drop is the panel at a moment: the share of its height out of the bar,
// and how it is stretched, about where its top edge meets the bar.
type Drop struct {
	Shown          float64 // 0 to 1
	ScaleX, ScaleY float64
}

const (
	dropGather  = 0.14 // it swells a little first
	dropLand    = 0.52 // then falls, and lands here
	dropSwell   = 0.05
	dropStretch = 0.06 // the most it stretches, as a share of its size
)

// DropAt is the panel at u (0 to 1) of the way through opening (or
// closing, open false).
func DropAt(u float64, open bool) Drop {
	u = clamp01(u)
	var shown, sy float64
	switch {
	case !open:
		shown = 1 - u*u
		sy = 1 + dropStretch*0.8*math.Sin(math.Pi*u)
	case u < dropGather:
		shown = dropSwell * easeOut(u/dropGather)
		sy = 1
	case u < dropLand:
		t := (u - dropGather) / (dropLand - dropGather)
		shown = dropSwell + (1-dropSwell)*t*t
		sy = 1 + dropStretch*t // longer the faster it falls
	default:
		// Landed: the stretch it came down with swings to a squash and
		// back, smaller each time, and is gone at the end.
		t := (u - dropLand) / (1 - dropLand)
		shown = 1
		sy = 1 + dropStretch*math.Exp(-3*t)*math.Cos(2.5*math.Pi*t)
	}
	// Narrower as it gets longer, wider as it gets shorter.
	return Drop{Shown: shown, ScaleX: 1 - 0.5*(sy-1), ScaleY: sy}
}

func clamp01(v float64) float64 { return max(0, min(v, 1)) }

func easeOut(t float64) float64 { return 1 - (1-t)*(1-t)*(1-t) }
