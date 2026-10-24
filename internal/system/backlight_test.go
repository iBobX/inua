package system

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fakeBacklight(t *testing.T) *Backlight {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "panel")
	os.MkdirAll(dir, 0o755)
	for name, v := range map[string]string{"max_brightness": "255\n", "brightness": "64\n", "actual_brightness": "64\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, err := FindBacklight(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBacklightWatchEnds(t *testing.T) {
	b := fakeBacklight(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Watch(ctx, func(float64) { t.Error("a file nobody wrote changed") }) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Watch = %v after ctx ended", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Watch did not return after ctx ended")
	}
}

func TestBacklightWatchMissing(t *testing.T) {
	b := fakeBacklight(t)
	os.Remove(filepath.Join(b.dir, "actual_brightness"))
	if err := b.Watch(context.Background(), func(float64) {}); err == nil {
		t.Fatal("Watch of a device without actual_brightness did not fail")
	}
}
