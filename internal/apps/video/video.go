// Package video is the Video app's logic, without GTK: where each video
// was left, and the subtitles beside a video (docs/feat/players.md).
package video

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// StoreName is the app's file in the store.
const StoreName = "video"

// Keep is how many videos' places are kept.
const Keep = 200

// Place is where a video was left.
type Place struct {
	Path     string        `json:"path"`
	Position time.Duration `json:"position"`
	Length   time.Duration `json:"length,omitempty"`
	When     time.Time     `json:"when"`
}

// State is what the app keeps.
type State struct {
	Places  []Place `json:"places"` // newest first
	Volume  float64 `json:"volume"`
	LastDir string  `json:"last_dir,omitempty"`
}

// DefaultState is a first run's.
func DefaultState() State { return State{Volume: 1} }

// Resume is where to start a video: where it was left, unless that was
// near its start or its end (then from the start).
func (s *State) Resume(path string) time.Duration {
	for _, p := range s.Places {
		if p.Path != path {
			continue
		}
		if p.Position < 10*time.Second || (p.Length > 0 && p.Length-p.Position < 15*time.Second) {
			return 0
		}
		return p.Position
	}
	return 0
}

// Leave notes where a video was left, keeping the newest Keep.
func (s *State) Leave(path string, pos, length time.Duration, now time.Time) {
	s.Places = slices.DeleteFunc(s.Places, func(p Place) bool { return p.Path == path })
	s.Places = slices.Insert(s.Places, 0, Place{Path: path, Position: pos, Length: length, When: now})
	if len(s.Places) > Keep {
		s.Places = s.Places[:Keep]
	}
}

// Videos are the file extensions taken as video.
var Videos = []string{".mp4", ".mkv", ".webm", ".avi", ".mov", ".m4v", ".mpg", ".mpeg", ".wmv", ".flv", ".ogv", ".ts", ".m2ts", ".3gp"}

// IsVideo says whether a file's name is a video's.
func IsVideo(path string) bool { return slices.Contains(Videos, strings.ToLower(filepath.Ext(path))) }

var subtitleExts = []string{".srt", ".ass", ".ssa", ".vtt", ".sub"}

// Subtitles is a subtitle file beside a video: the same name with a
// subtitle's extension ("film.srt"), else with a language before it
// ("film.en.srt", the first by name); "" for none.
func Subtitles(video string) string {
	dir, base := filepath.Dir(video), strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))
	for _, ext := range subtitleExts {
		if p := filepath.Join(dir, base+ext); isFile(p) {
			return p
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasPrefix(name, base+".") && slices.Contains(subtitleExts, strings.ToLower(filepath.Ext(name))) {
			return filepath.Join(dir, name)
		}
	}
	return ""
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// Speeds are the speeds offered, in order.
var Speeds = []float64{0.5, 0.75, 1, 1.25, 1.5, 2}

// Faster is the next speed up (or down, when !up) from speed.
func Faster(speed float64, up bool) float64 {
	i := slices.Index(Speeds, speed)
	if i < 0 {
		i = slices.Index(Speeds, 1)
	}
	if up {
		i = min(i+1, len(Speeds)-1)
	} else {
		i = max(i-1, 0)
	}
	return Speeds[i]
}
