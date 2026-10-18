package remotedesk

import "testing"

func TestKeysAreSentByTheirPlace(t *testing.T) {
	for evdev, want := range map[uint32]uint16{
		1: 0x1B, 2: 0x31, 11: 0x30, 16: 'Q', 30: 'A', 44: 'Z', 28: 0x0D, 57: 0x20, 59: 0x70, 88: 0x7B,
		42: 0xA0, 54: 0xA1, 29: 0xA2, 97: 0xA3, 56: 0xA4, 100: 0xA5, 125: 0x5B, 105: 0x25, 111: 0x2E, 183: 0x7C, 194: 0x87,
	} {
		if got := VirtualKey(evdev); got != want {
			t.Errorf("evdev %d = %#x, want %#x", evdev, got, want)
		}
	}
	if VirtualKey(0) != 0 || VirtualKey(240) != 0 || VirtualKey(100000) != 0 {
		t.Error("a key with no code got one")
	}
}

func TestThePointerFallsOnTheStreamsPixels(t *testing.T) {
	// A 1920×1080 picture in a 1000×1000 view: bars above and below.
	for _, c := range []struct {
		x, y   float64
		sx, sy int
		inside bool
	}{
		{500, 500, 960, 540, true},
		{0, 218.75, 0, 0, true},
		{999, 781, 1918, 1079, true},
		{500, 100, 960, 0, false}, // on the bar above
	} {
		sx, sy, in := StreamPoint(c.x, c.y, 1000, 1000, 1920, 1080)
		if sx != c.sx || sy != c.sy || in != c.inside {
			t.Errorf("(%v,%v) = (%d,%d,%v), want (%d,%d,%v)", c.x, c.y, sx, sy, in, c.sx, c.sy, c.inside)
		}
	}
	if _, _, in := StreamPoint(1, 1, 0, 0, 1920, 1080); in {
		t.Error("a point in an empty view")
	}
	if ScrollAmount(1, true) != -120 || ScrollAmount(-40, false) != 120 {
		t.Error("scroll amounts")
	}
}
