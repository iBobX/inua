package shell

import (
	"regexp"
	"strconv"
)

var fontSizePx = regexp.MustCompile(`(font-size\s*:\s*)([0-9]*\.?[0-9]+)px`)

// ScaleText multiplies the pixel font sizes of a stylesheet by percent/100
// (style.text_scale), leaving every other size as it is.
func ScaleText(css string, percent int) string {
	if percent == 100 {
		return css
	}
	return fontSizePx.ReplaceAllStringFunc(css, func(m string) string {
		sub := fontSizePx.FindStringSubmatch(m)
		px, err := strconv.ParseFloat(sub[2], 64)
		if err != nil {
			return m
		}
		px = float64(int(px*float64(percent)+0.5)) / 100 // to 0.01px
		return sub[1] + strconv.FormatFloat(px, 'f', -1, 64) + "px"
	})
}
