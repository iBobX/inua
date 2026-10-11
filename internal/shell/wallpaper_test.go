package shell

import (
	"bytes"
	"image/png"
	"os"
	"strings"
	"testing"
)

func TestThemeColors(t *testing.T) {
	css, err := os.ReadFile("../../ui/themes/yoru.css")
	if err != nil {
		t.Fatal(err)
	}
	c := ThemeColors(string(css))
	if c["app"] != "#0B1012" || c["field"] != "#44504E" || c["accent"] != "#9FC3C9" {
		t.Errorf("yoru: %v", c)
	}
}

func TestWallpaperCSS(t *testing.T) {
	img := Wallpaper{Image: "/home/u/Pictures/my wall\"paper.png", Fit: "contain", Color: "#102030"}.CSS()
	for _, s := range []string{
		`url("file:///home/u/Pictures/my%20wall%22paper.png")`,
		"background-size: contain;", "background-repeat: no-repeat;", "background-color: #102030;",
	} {
		if !strings.Contains(img, s) {
			t.Errorf("image css lacks %q:\n%s", s, img)
		}
	}
	tile := Wallpaper{Image: "/a.png", Fit: "tile"}.CSS()
	if !strings.Contains(tile, "background-repeat: repeat;") || !strings.Contains(tile, "background-size: auto;") || !strings.Contains(tile, "@app") {
		t.Errorf("tile css:\n%s", tile)
	}
	center := Wallpaper{Image: "/a.png", Fit: "center"}.CSS()
	if !strings.Contains(center, "background-size: auto;") || !strings.Contains(center, "no-repeat") {
		t.Errorf("center css:\n%s", center)
	}
	field := Wallpaper{Tile: "/run/user/1000/inua/field.png"}.CSS()
	if !strings.Contains(field, "background-size: 6px 26px;") || !strings.Contains(field, "repeat;") || !strings.Contains(field, "file:///run/user/1000/inua/field.png") {
		t.Errorf("texture css:\n%s", field)
	}
}

func TestFieldTile(t *testing.T) {
	data, err := FieldTile("#44504E", 0.5)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 24 || b.Dy() != 104 {
		t.Fatalf("size %v", b)
	}
	// The run covers 12×6 device px at the top left; the rest is clear.
	if r, g, b, a := img.At(0, 0).RGBA(); a == 0 || r == 0 && g == 0 && b == 0 {
		t.Errorf("run pixel %v", img.At(0, 0))
	}
	for _, p := range [][2]int{{12, 0}, {0, 6}, {23, 103}} {
		if _, _, _, a := img.At(p[0], p[1]).RGBA(); a != 0 {
			t.Errorf("pixel %v should be clear", p)
		}
	}
	if _, err := FieldTile("teal", 1); err == nil {
		t.Error("bad colour accepted")
	}
}
