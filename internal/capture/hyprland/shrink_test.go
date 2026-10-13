package hyprland

import (
	"bytes"
	"testing"
)

func TestShrinkCopiesAndFlipsWithoutResizing(t *testing.T) {
	src := Frame{
		Address: "0x1", Width: 2, Height: 2, Stride: 8, YInvert: true,
		Pixels: []byte{
			1, 2, 3, 4, 5, 6, 7, 8,
			9, 10, 11, 12, 13, 14, 15, 16,
		},
	}
	got := Shrink(src, 16, 16)
	if got.Width != 2 || got.Height != 2 || got.Stride != 8 || got.YInvert || got.Address != "0x1" {
		t.Fatalf("identity: %+v", got)
	}
	want := []byte{
		9, 10, 11, 12, 13, 14, 15, 16,
		1, 2, 3, 4, 5, 6, 7, 8,
	}
	if !bytes.Equal(got.Pixels, want) {
		t.Fatalf("pixels %v", got.Pixels)
	}
	if &got.Pixels[0] == &src.Pixels[0] {
		t.Fatal("must copy out of the capture mmap")
	}
}

func TestShrinkFitsDisplayBoxAndAverages(t *testing.T) {
	src := Frame{
		Address: "0x2", Width: 4, Height: 2, Stride: 16,
		Pixels: []byte{
			10, 0, 0, 255, 20, 0, 0, 255, 30, 0, 0, 255, 40, 0, 0, 255,
			50, 0, 0, 255, 60, 0, 0, 255, 70, 0, 0, 255, 80, 0, 0, 255,
		},
	}
	got := Shrink(src, 2, 1)
	if got.Width != 2 || got.Height != 1 {
		t.Fatalf("size %dx%d", got.Width, got.Height)
	}
	if got.Pixels[0] != 35 || got.Pixels[4] != 55 {
		t.Fatalf("averages %v", got.Pixels)
	}
}

func TestShrinkFallsBackAndCaps(t *testing.T) {
	px := make([]byte, 4000*4)
	src := Frame{Width: 4000, Height: 1, Stride: 4000 * 4, Pixels: px}
	got := Shrink(src, 0, 0)
	if got.Width != 720 {
		t.Fatalf("fallback long edge %d", got.Width)
	}
	got = Shrink(src, 8000, 8000)
	if got.Width != 1920 {
		t.Fatalf("cap %d", got.Width)
	}
}

func TestShrinkRejectsBadFrames(t *testing.T) {
	if got := Shrink(Frame{Address: "x", Width: 2, Height: 2, Stride: 8, Pixels: []byte{1}}, 8, 8); got.Pixels != nil || got.Address != "x" {
		t.Fatalf("short buffer: %+v", got)
	}
}
