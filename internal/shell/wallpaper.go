package shell

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"regexp"
	"strconv"
)

var defineColor = regexp.MustCompile(`@define-color\s+([\w-]+)\s+(#[0-9a-fA-F]{6})\s*;`)

// ThemeColors reads the colour tokens of a theme file (ui/themes/*.css):
// name → "#rrggbb".
func ThemeColors(css string) map[string]string {
	colors := map[string]string{}
	for _, m := range defineColor.FindAllStringSubmatch(css, -1) {
		colors[m[1]] = m[2]
	}
	return colors
}

// Wallpaper describes what the background windows show.
type Wallpaper struct {
	Image string // absolute path, "" for the texture
	Fit   string // cover, contain, tile, center
	Color string // "#rrggbb", "" for the theme's ground (@app)
	Tile  string // path of the texture tile (FieldTile), used without an image
}

// CSS styles the wallpaper windows, and the lock screen's ground.
func (w Wallpaper) CSS() string {
	ground := "@app"
	if w.Color != "" {
		ground = w.Color
	}
	if w.Image == "" {
		// The texture's tile is drawn at 4x for sharp HiDPI output.
		return fmt.Sprintf(`window.wallpaper, window.lock .lock-ground {
  background-color: %s;
  background-image: url("%s");
  background-repeat: repeat;
  background-size: %dpx %dpx;
  background-position: 0 0;
}
`, ground, fileURL(w.Tile), tileW, tileH)
	}
	size, repeat := "cover", "no-repeat"
	switch w.Fit {
	case "contain":
		size = "contain"
	case "tile":
		size, repeat = "auto", "repeat"
	case "center":
		size = "auto"
	}
	return fmt.Sprintf(`window.wallpaper, window.lock .lock-ground {
  background-color: %s;
  background-image: url("%s");
  background-repeat: %s;
  background-size: %s;
  background-position: center;
}
`, ground, fileURL(w.Image), repeat, size)
}

func fileURL(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

// The texture: a run of 3 × 1.5 px every 6 px, rows every 26 px (CSS px).
const (
	tileW, tileH = 6, 26
	tileScale    = 4
)

// FieldTile draws one tile of the texture as a PNG, the run in the given
// colour at alpha over a transparent tile; the window's colour shows
// through.
func FieldTile(field string, alpha float64) ([]byte, error) {
	c, err := parseHex(field)
	if err != nil {
		return nil, err
	}
	c.A = uint8(alpha * 255)
	img := image.NewNRGBA(image.Rect(0, 0, tileW*tileScale, tileH*tileScale))
	for y := 0; y < tileScale*3/2; y++ { // 1.5 px
		for x := 0; x < 3*tileScale; x++ { // 3 px
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func parseHex(s string) (color.NRGBA, error) {
	if len(s) != 7 || s[0] != '#' {
		return color.NRGBA{}, fmt.Errorf("colour %q: want #rrggbb", s)
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return color.NRGBA{}, fmt.Errorf("colour %q: %w", s, err)
	}
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}, nil
}
