package chart

import (
	"image/color"
	"testing"
)

func TestFixedRange(t *testing.T) {
	// All points at the top of a fixed 0..100 range: the line runs along
	// the top, so the bottom row is only the soft fill.
	img := Line([]float64{100, 100, 100}, 40, 20, color.NRGBA{255, 0, 0, 255}, Options{Lo: 0, Hi: 100})
	bottom := img.Pix[(19*40+20)*4+3]
	var top uint8
	for y := 0; y < 4; y++ {
		top = max(top, img.Pix[(y*40+20)*4+3])
	}
	if top < 3*bottom || bottom > 60 {
		t.Errorf("top alpha %d, bottom alpha %d", top, bottom)
	}
	if Line([]float64{150}, 10, 10, color.NRGBA{}, Options{}).Pix[3] != 0 {
		t.Error("one point draws nothing")
	}
}

func TestFlatLineHasNoGaps(t *testing.T) {
	img := Line([]float64{50, 50, 50, 50, 50, 50}, 100, 20, color.NRGBA{0, 0, 255, 255}, Options{Lo: 0, Hi: 100})
	// Along the line's row, every pixel is drawn strongly.
	for x := 2; x < 98; x++ {
		if a := img.Pix[(10*100+x)*4+3]; a < 200 {
			t.Fatalf("gap at x=%d (alpha %d)", x, a)
		}
	}
}
