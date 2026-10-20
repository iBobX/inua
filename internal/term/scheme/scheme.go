// Package scheme is inua-term's colour schemes for the terminals: the
// default colours, the cursor and the 16 colours programs use. Without
// one, the terminals take their colours from the window's theme
// (internal/termui).
package scheme

import (
	"slices"

	"github.com/lyricat/inua/internal/i18n"
)

// Scheme is a terminal's colours as 0xRRGGBB.
type Scheme struct {
	Name    string // as term.toml writes it
	Label   string
	Brief   string // what it looks like, in a few words
	Dark    bool
	FG, BG  uint32
	Cursor  uint32
	Palette [16]uint32 // black, red, green, yellow, blue, magenta, cyan, white, then the bright eight
}

// All are the schemes to choose from, the dark ones first.
var All = []Scheme{
	{"tokyo-night", "Tokyo Night", i18n.M("Cool blues and violets on a deep navy"), true, 0xc0caf5, 0x1a1b26, 0xc0caf5, [16]uint32{
		0x15161e, 0xf7768e, 0x9ece6a, 0xe0af68, 0x7aa2f7, 0xbb9af7, 0x7dcfff, 0xa9b1d6,
		0x414868, 0xf7768e, 0x9ece6a, 0xe0af68, 0x7aa2f7, 0xbb9af7, 0x7dcfff, 0xc0caf5}},
	{"catppuccin-mocha", "Catppuccin Mocha", i18n.M("Soft pastels on a warm dark ground"), true, 0xcdd6f4, 0x1e1e2e, 0xf5e0dc, [16]uint32{
		0x45475a, 0xf38ba8, 0xa6e3a1, 0xf9e2af, 0x89b4fa, 0xf5c2e7, 0x94e2d5, 0xbac2de,
		0x585b70, 0xf38ba8, 0xa6e3a1, 0xf9e2af, 0x89b4fa, 0xf5c2e7, 0x94e2d5, 0xa6adc8}},
	{"dracula", "Dracula", i18n.M("Vivid pinks, greens and purples, high contrast"), true, 0xf8f8f2, 0x282a36, 0xf8f8f2, [16]uint32{
		0x21222c, 0xff5555, 0x50fa7b, 0xf1fa8c, 0xbd93f9, 0xff79c6, 0x8be9fd, 0xf8f8f2,
		0x6272a4, 0xff6e6e, 0x69ff94, 0xffffa5, 0xd6acff, 0xff92df, 0xa4ffff, 0xffffff}},
	{"gruvbox-dark", "Gruvbox Dark", i18n.M("Earthy, retro, easy on the eyes"), true, 0xebdbb2, 0x282828, 0xebdbb2, [16]uint32{
		0x282828, 0xcc241d, 0x98971a, 0xd79921, 0x458588, 0xb16286, 0x689d6a, 0xa89984,
		0x928374, 0xfb4934, 0xb8bb26, 0xfabd2f, 0x83a598, 0xd3869b, 0x8ec07c, 0xebdbb2}},
	{"nord", "Nord", i18n.M("Calm arctic greys and frost blues"), true, 0xd8dee9, 0x2e3440, 0xd8dee9, [16]uint32{
		0x3b4252, 0xbf616a, 0xa3be8c, 0xebcb8b, 0x81a1c1, 0xb48ead, 0x88c0d0, 0xe5e9f0,
		0x4c566a, 0xbf616a, 0xa3be8c, 0xebcb8b, 0x81a1c1, 0xb48ead, 0x8fbcbb, 0xeceff4}},
	{"one-dark", "One Dark", i18n.M("Atom's balanced dark, muted and clear"), true, 0xabb2bf, 0x282c34, 0x528bff, [16]uint32{
		0x282c34, 0xe06c75, 0x98c379, 0xe5c07b, 0x61afef, 0xc678dd, 0x56b6c2, 0xabb2bf,
		0x5c6370, 0xe06c75, 0x98c379, 0xe5c07b, 0x61afef, 0xc678dd, 0x56b6c2, 0xffffff}},
	{"rose-pine", "Rosé Pine", i18n.M("Muted roses and pines at dusk"), true, 0xe0def4, 0x191724, 0x524f67, [16]uint32{
		0x26233a, 0xeb6f92, 0x31748f, 0xf6c177, 0x9ccfd8, 0xc4a7e7, 0xebbcba, 0xe0def4,
		0x6e6a86, 0xeb6f92, 0x31748f, 0xf6c177, 0x9ccfd8, 0xc4a7e7, 0xebbcba, 0xe0def4}},
	{"solarized-dark", "Solarized Dark", i18n.M("The classic: low contrast on blue-green"), true, 0x839496, 0x002b36, 0x93a1a1, solarized},
	{"catppuccin-latte", "Catppuccin Latte", i18n.M("Pastels on a light, milky ground"), false, 0x4c4f69, 0xeff1f5, 0xdc8a78, [16]uint32{
		0x5c5f77, 0xd20f39, 0x40a02b, 0xdf8e1d, 0x1e66f5, 0xea76cb, 0x179299, 0xacb0be,
		0x6c6f85, 0xd20f39, 0x40a02b, 0xdf8e1d, 0x1e66f5, 0xea76cb, 0x179299, 0xbcc0cc}},
	{"gruvbox-light", "Gruvbox Light", i18n.M("Earthy tones on warm paper"), false, 0x3c3836, 0xfbf1c7, 0x3c3836, [16]uint32{
		0xfbf1c7, 0xcc241d, 0x98971a, 0xd79921, 0x458588, 0xb16286, 0x689d6a, 0x7c6f64,
		0x928374, 0x9d0006, 0x79740e, 0xb57614, 0x076678, 0x8f3f71, 0x427b58, 0x3c3836}},
	{"solarized-light", "Solarized Light", i18n.M("The classic, on cream"), false, 0x657b83, 0xfdf6e3, 0x586e75, solarized},
}

var solarized = [16]uint32{
	0x073642, 0xdc322f, 0x859900, 0xb58900, 0x268bd2, 0xd33682, 0x2aa198, 0xeee8d5,
	0x002b36, 0xcb4b16, 0x586e75, 0x657b83, 0x839496, 0x6c71c4, 0x93a1a1, 0xfdf6e3,
}

// Find is the scheme of a name.
func Find(name string) (Scheme, bool) {
	i := slices.IndexFunc(All, func(s Scheme) bool { return s.Name == name })
	if i < 0 {
		return Scheme{}, false
	}
	return All[i], true
}
