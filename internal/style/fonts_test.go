package style

import "testing"

func TestSystemFonts(t *testing.T) {
	for in, want := range map[string]string{
		`a { font-family: "Geist Mono", "IBM Plex Mono", monospace; }`:           `a { font-family: "SF Mono", Menlo, monospace; }`,
		`a { font-family: "JetBrainsMono Nerd Font Mono" }`:                      `a { font-family: "SF Mono", Menlo, monospace }`,
		`a { font-family: "IBM Plex Sans", "Zen Kaku Gothic New", sans-serif; }`: `a { font-family: ".AppleSystemUIFont", "Helvetica Neue", "Hiragino Sans", sans-serif; }`,
		`a { font-family: "Shippori Mincho", "Noto Serif CJK JP", serif; }`:      `a { font-family: "New York", "Hiragino Mincho ProN", serif; }`,
		`a { font-family: inherit; font-size: 12px; }`:                           `a { font-family: inherit; font-size: 12px; }`,
	} {
		if got := SystemFonts(in); got != want {
			t.Errorf("SystemFonts(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}
