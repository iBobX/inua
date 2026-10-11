// Package colors is the Color Picker app's logic: a picked colour's forms
// (hex, RGB, HSL) and the history of picks, which the shell (it does the
// picking) and the app share in the store.
package colors

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// StoreName is the file the shell and the app share.
const StoreName = "colors"

// Keep is how many picks the history keeps.
const Keep = 30

// Data is the history of picks, newest first, as "#rrggbb".
type Data struct {
	History []string `json:"history"`
}

// Add puts a pick first in the history, once.
func (d *Data) Add(hex string) {
	d.History = slices.DeleteFunc(d.History, func(h string) bool { return strings.EqualFold(h, hex) })
	d.History = append([]string{hex}, d.History...)
	if len(d.History) > Keep {
		d.History = d.History[:Keep]
	}
}

// Color is an sRGB colour.
type Color struct{ R, G, B uint8 }

// Parse reads "#rrggbb".
func Parse(hex string) (Color, bool) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return Color{}, false
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return Color{}, false
	}
	return Color{uint8(v >> 16), uint8(v >> 8), uint8(v)}, true
}

// Hex is "#rrggbb".
func (c Color) Hex() string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// RGB is "rgb(36, 99, 174)".
func (c Color) RGB() string { return fmt.Sprintf("rgb(%d, %d, %d)", c.R, c.G, c.B) }

// HSL is "hsl(213, 66%, 41%)".
func (c Color) HSL() string {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	hi, lo := max(r, g, b), min(r, g, b)
	l := (hi + lo) / 2
	var h, s float64
	if d := hi - lo; d > 0 {
		s = d / (1 - math.Abs(2*l-1))
		switch hi {
		case r:
			h = math.Mod((g-b)/d, 6)
		case g:
			h = (b-r)/d + 2
		default:
			h = (r-g)/d + 4
		}
		h *= 60
		if h < 0 {
			h += 360
		}
	}
	return fmt.Sprintf("hsl(%.0f, %.0f%%, %.0f%%)", h, s*100, l*100)
}

// Light reports whether dark text reads better on the colour than light.
func (c Color) Light() bool {
	// Relative luminance (sRGB, approximately).
	return 0.2126*float64(c.R)+0.7152*float64(c.G)+0.0722*float64(c.B) > 150
}
