package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/lyricat/inua/internal/wallgen"
)

// A built-in picture is drawn to its file whole, at its size; the pictures
// kept from before go when another is shown, but not one being drawn.
func TestPictureIsDrawnAndKeptAlone(t *testing.T) {
	dir := t.TempDir()
	p, err := wallgen.NewPalette(map[string]string{"app": "#0B1012", "field": "#44504E", "accent": "#9FC3C9", "primary": "#E7E4DA"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, wallgen.Key("cat", 64, 36, p, 1)+".png")
	if err := drawPicture(dir, path, "cat", 64, 36, p, 1); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(f)
	f.Close()
	if err != nil || cfg.Width != 64 || cfg.Height != 36 {
		t.Fatalf("the picture is %dx%d (%v), want 64x36", cfg.Width, cfg.Height, err)
	}
	if _, err := os.Stat(path + ".part"); !os.IsNotExist(err) {
		t.Errorf("the part file was left: %v", err)
	}

	old := filepath.Join(dir, "old.png")
	drawing := filepath.Join(dir, "next.png.part")
	for _, f := range []string{old, drawing} {
		if err := os.WriteFile(f, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prunePictures(dir, path)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("the picture from before was kept")
	}
	for _, f := range []string{path, drawing} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("%s went: %v", filepath.Base(f), err)
		}
	}
}
