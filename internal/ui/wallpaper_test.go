package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/wallgen"
)

// Another picture picked stands for the day's own until the day is over.
func TestPickedPictureLastsTheDay(t *testing.T) {
	dir := t.TempDir()
	day := time.Date(2026, 10, 23, 9, 0, 0, 0, time.Local)
	own, ownSeed := wallgen.OfDay(day)
	if n, s := todaysPicture(dir, day); n != own || s != ownSeed {
		t.Fatalf("without a pick: %s/%d, want the day's %s/%d", n, s, own, ownSeed)
	}
	if err := os.WriteFile(filepath.Join(dir, "pick"), []byte("2026-10-23 cat 42\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, s := todaysPicture(dir, day.Add(14*time.Hour)); n != "cat" || s != 42 {
		t.Errorf("late that day: %s/%d, want cat/42", n, s)
	}
	next := day.AddDate(0, 0, 1)
	want, wantSeed := wallgen.OfDay(next)
	if n, s := todaysPicture(dir, next); n != want || s != wantSeed {
		t.Errorf("the next day: %s/%d, want its own %s/%d", n, s, want, wantSeed)
	}
}

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
	pick := filepath.Join(dir, "pick")
	for _, f := range []string{old, drawing, pick} {
		if err := os.WriteFile(f, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prunePictures(dir, path)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("the picture from before was kept")
	}
	for _, f := range []string{path, drawing, pick} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("%s went: %v", filepath.Base(f), err)
		}
	}
}
