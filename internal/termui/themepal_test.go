package termui

import "testing"

func TestThemePalette(t *testing.T) {
	yoru := map[string]string{"app": "#0B1012", "surface2": "#172124", "t0": "#E7E4DA", "t1": "#B3B6AF", "t2": "#7F8F8C",
		"danger": "#E0694F", "ok": "#7FB89E", "warn": "#D9A441", "accent": "#9FC3C9"}
	p := themePalette(yoru)
	if p[1] != 0xE0694F || p[2] != 0x7FB89E || p[4] != 0x9FC3C9 || p[15] != 0xE7E4DA || p[7] != 0xB3B6AF {
		t.Errorf("dark %06x", p)
	}
	if rgb(parseHex("#000000")).luma() != 0 || parseHex("#zz") != (rgb{}) {
		t.Error("parse")
	}
	light := map[string]string{"app": "#F4F1EA", "surface2": "#E6E1D6", "t0": "#2A2622", "t1": "#5B554D", "t2": "#8A8278",
		"danger": "#B84A32", "ok": "#4E7D63", "warn": "#9A6A12", "accent": "#3F6E7A"}
	p = themePalette(light)
	if p[0] != 0x2A2622 || p[15] != 0xF4F1EA {
		t.Errorf("light: black %06x, bright white %06x", p[0], p[15])
	}
}
