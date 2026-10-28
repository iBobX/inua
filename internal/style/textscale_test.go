package style

import "testing"

func TestScaleText(t *testing.T) {
	cases := []struct {
		css     string
		percent int
		want    string
	}{
		{".a { font-size: 13px; }", 100, ".a { font-size: 13px; }"},
		{".a { font-size: 12px; }", 125, ".a { font-size: 15px; }"},
		{".a { font-size: 10.5px; }", 110, ".a { font-size: 11.55px; }"},
		{".a { font-size:11px }", 90, ".a { font-size:9.9px }"},
		{".a { font-size: 13px; padding: 4px; min-height: 13px; }", 150, ".a { font-size: 19.5px; padding: 4px; min-height: 13px; }"},
		{".a { font-size: 0; } .b { font-size: 1.1em; }", 150, ".a { font-size: 0; } .b { font-size: 1.1em; }"},
		{".a { font-size: 112px; } .b { font-size: .5px; }", 80, ".a { font-size: 89.6px; } .b { font-size: 0.4px; }"},
	}
	for _, c := range cases {
		if got := ScaleText(c.css, c.percent); got != c.want {
			t.Errorf("ScaleText(%q, %d) = %q, want %q", c.css, c.percent, got, c.want)
		}
	}
}
