package termui

import "strconv"

// The terminal's 16 colours follow inua's theme (section 6): red, green,
// yellow and blue are the theme's danger, ok, warn and accent; magenta
// and cyan mixes of them; black and white its surfaces and text, the
// right way round for a dark theme or a light one. The bright eight are
// each a step toward the text colour.

type rgb struct{ r, g, b float64 }

func parseHex(s string) rgb {
	if len(s) != 7 || s[0] != '#' {
		return rgb{}
	}
	v, _ := strconv.ParseUint(s[1:], 16, 32)
	return rgb{float64(v >> 16 & 0xff), float64(v >> 8 & 0xff), float64(v & 0xff)}
}

func (c rgb) mix(o rgb, t float64) rgb {
	return rgb{c.r + (o.r-c.r)*t, c.g + (o.g-c.g)*t, c.b + (o.b-c.b)*t}
}

func (c rgb) value() uint32 {
	b := func(f float64) uint32 { return uint32(min(max(f+0.5, 0), 255)) }
	return b(c.r)<<16 | b(c.g)<<8 | b(c.b)
}

func (c rgb) luma() float64 { return (0.2126*c.r + 0.7152*c.g + 0.0722*c.b) / 255 }

// themePalette is the palette for a theme's tokens.
func themePalette(t map[string]string) [16]uint32 {
	get := func(n string) rgb { return parseHex(t[n]) }
	app, t0, t1, t2 := get("app"), get("t0"), get("t1"), get("t2")
	red, green, yellow, blue := get("danger"), get("ok"), get("warn"), get("accent")
	magenta, cyan := red.mix(blue, 0.5), blue.mix(green, 0.5)
	black, white := get("surface2").mix(t2, 0.35), t1
	brightBlack, brightWhite := t2, t0
	if app.luma() >= 0.5 {
		// A light theme: black is the text's, white the paper's.
		black, white = t0, get("surface2").mix(t2, 0.35)
		brightBlack, brightWhite = t1, app
	}
	base := [8]rgb{black, red, green, yellow, blue, magenta, cyan, white}
	var p [16]uint32
	for i, c := range base {
		p[i] = c.value()
		if i > 0 && i < 7 {
			p[i+8] = c.mix(t0, 0.25).value()
		}
	}
	p[8], p[15] = brightBlack.value(), brightWhite.value()
	return p
}
