package video

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPlaces(t *testing.T) {
	s := DefaultState()
	now := time.Now()
	s.Leave("/v/a.mkv", 5*time.Minute, time.Hour, now)
	s.Leave("/v/b.mkv", 5*time.Second, time.Hour, now)
	s.Leave("/v/c.mkv", 59*time.Minute+50*time.Second, time.Hour, now)
	s.Leave("/v/a.mkv", 6*time.Minute, time.Hour, now)
	if s.Resume("/v/a.mkv") != 6*time.Minute || s.Resume("/v/b.mkv") != 0 || s.Resume("/v/c.mkv") != 0 || s.Resume("/v/d.mkv") != 0 {
		t.Errorf("resume: %v", s.Places)
	}
	if len(s.Places) != 3 || s.Places[0].Path != "/v/a.mkv" {
		t.Errorf("places %v", s.Places)
	}
	for i := range Keep + 5 {
		s.Leave(filepath.Join("/v", string(rune('a'+i%26)), "x.mp4", string(rune(i))), time.Minute, 0, now)
	}
	if len(s.Places) != Keep {
		t.Errorf("%d kept", len(s.Places))
	}
}

func TestSubtitles(t *testing.T) {
	dir := t.TempDir()
	touch := func(n string) { os.WriteFile(filepath.Join(dir, n), nil, 0o644) }
	touch("film.mkv")
	if Subtitles(filepath.Join(dir, "film.mkv")) != "" {
		t.Error("none")
	}
	touch("film.en.srt")
	touch("film.zh.ass")
	touch("filmmaker.srt")
	if got := Subtitles(filepath.Join(dir, "film.mkv")); got != filepath.Join(dir, "film.en.srt") {
		t.Errorf("with a language: %q", got)
	}
	touch("film.srt")
	if got := Subtitles(filepath.Join(dir, "film.mkv")); got != filepath.Join(dir, "film.srt") {
		t.Errorf("same name: %q", got)
	}
}

func TestSpeeds(t *testing.T) {
	if Faster(1, true) != 1.25 || Faster(2, true) != 2 || Faster(0.5, false) != 0.5 || Faster(3, false) != 0.75 {
		t.Error("speeds")
	}
	if !IsVideo("/a/B.MKV") || IsVideo("/a/b.srt") {
		t.Error("IsVideo")
	}
}
