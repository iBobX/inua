package shell

import (
	"image"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestSpanRect(t *testing.T) {
	got := SpanRect(300, 200, 100, -50, 1000, 800)
	if want := (Rect{X: 100, Y: 0, W: 200, H: 200}); got != want {
		t.Fatalf("SpanRect = %+v, want %+v", got, want)
	}
	if !SpanRect(5, 5, 6, 5, 100, 100).Empty() {
		t.Fatal("a 1×0 drag should be empty")
	}
}

func TestRectPixels(t *testing.T) {
	bounds := image.Rect(0, 0, 3840, 2160) // 2560×1440 at scale 1.5
	got := Rect{X: 10.2, Y: 20, W: 100, H: 50.5}.Pixels(1.5, bounds)
	if want := image.Rect(15, 30, 166, 106); got != want {
		t.Fatalf("Pixels = %v, want %v", got, want)
	}
	if got := (Rect{X: 2500, Y: 1400, W: 200, H: 200}).Pixels(1.5, bounds); got != image.Rect(3750, 2100, 3840, 2160) {
		t.Fatalf("Pixels past the edge = %v", got)
	}
}

const shotClients = `[
 {"address":"0xA","title":"tiled old","class":"a","at":[2256,30],"size":[1000,800],"mapped":true,"hidden":false,"floating":false,"fullscreen":0,"focusHistoryID":2,"workspace":{"id":1}},
 {"address":"0xB","title":"tiled new","class":"b","at":[3300,30],"size":[1000,800],"mapped":true,"hidden":false,"floating":false,"fullscreen":0,"focusHistoryID":0,"workspace":{"id":1}},
 {"address":"0xC","title":"float","class":"c","at":[2400,100],"size":[300,200],"mapped":true,"hidden":false,"floating":true,"fullscreen":0,"focusHistoryID":1,"workspace":{"id":1}},
 {"address":"0xD","title":"elsewhere","class":"d","at":[0,0],"size":[500,500],"mapped":true,"hidden":false,"floating":false,"fullscreen":0,"focusHistoryID":3,"workspace":{"id":2}},
 {"address":"0xE","title":"hidden","class":"e","at":[2256,30],"size":[500,500],"mapped":true,"hidden":true,"floating":false,"fullscreen":0,"focusHistoryID":4,"workspace":{"id":1}}
]`

func TestShotWindows(t *testing.T) {
	ws, err := ShotWindows([]byte(shotClients), 2256, 0, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, w := range ws {
		order = append(order, w.Title)
	}
	if got, want := len(order), 3; got != want {
		t.Fatalf("windows = %v, want 3", order)
	}
	if order[0] != "float" || order[1] != "tiled new" || order[2] != "tiled old" {
		t.Fatalf("stacking = %v", order)
	}
	if ws[2].Rect != (Rect{X: 0, Y: 30, W: 1000, H: 800}) {
		t.Fatalf("rect on the monitor = %+v", ws[2].Rect)
	}
	if i := WindowAt(ws, 200, 150); ws[i].Title != "float" {
		t.Fatalf("WindowAt over the floating window = %q", ws[i].Title)
	}
	if i := WindowAt(ws, 50, 900); i != -1 {
		t.Fatalf("WindowAt below every window = %d", i)
	}
}

func TestShotName(t *testing.T) {
	got := ShotName(time.Date(2026, 10, 10, 9, 4, 5, 0, time.Local))
	if got != "Screenshot_2026-10-10_09-04-05.png" {
		t.Fatal(got)
	}
}

func TestRecentShots(t *testing.T) {
	shots, videos := t.TempDir(), t.TempDir()
	now := time.Now()
	write := func(dir, name string, age time.Duration, data string) {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(data), 0o644)
		os.Chtimes(p, now.Add(-age), now.Add(-age))
	}
	write(shots, "Screenshot_a.png", 3*time.Minute, "x")
	write(shots, "Screenshot_b.png", 1*time.Minute, "x")
	write(shots, "Screenshot_empty.png", 0, "") // being written
	write(shots, "frozen-eDP-1.png", 0, "x")    // the tool's own capture
	write(shots, "holiday.png", 0, "x")
	write(videos, "Recording_c.mp4", 2*time.Minute, "x")
	write(videos, "Recording_d.mkv", 0, "x")
	got := RecentShots([]string{shots, videos, shots, "", filepath.Join(shots, "missing")}, 10)
	var names []string
	for _, it := range got {
		names = append(names, filepath.Base(it.Path))
	}
	if want := []string{"Screenshot_b.png", "Recording_c.mp4", "Screenshot_a.png"}; !slices.Equal(names, want) {
		t.Fatalf("RecentShots = %v, want %v", names, want)
	}
	if !got[1].Video || got[0].Video {
		t.Errorf("videos %+v", got)
	}
	if n := len(RecentShots([]string{shots, videos}, 2)); n != 2 {
		t.Errorf("limit: %d", n)
	}
}

func TestThumbSize(t *testing.T) {
	for _, c := range []struct{ w, h, mw, mh, ww, wh int }{
		{3840, 2160, 240, 150, 240, 135},
		{1000, 3000, 240, 150, 50, 150},
		{100, 50, 240, 150, 100, 50}, // never enlarged
		{5000, 1, 240, 150, 240, 1},
		{0, 10, 240, 150, 0, 0},
	} {
		if w, h := ThumbSize(c.w, c.h, c.mw, c.mh); w != c.ww || h != c.wh {
			t.Errorf("ThumbSize(%d,%d) = %d×%d, want %d×%d", c.w, c.h, w, h, c.ww, c.wh)
		}
	}
}
