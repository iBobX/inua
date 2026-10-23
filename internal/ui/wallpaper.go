package ui

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/wallgen"
)

// With background.generate inua draws the wallpaper itself: the day's
// picture (wallgen.OfDay) for the theme, at the largest monitor's size, off
// the main thread. It is kept in pictureDir under a name that says what was
// drawn (wallgen.Key), so a later start shows the file without drawing it
// again; at midnight the next day's is drawn.

// picturePath is the file of the day's picture, or "" while it is being
// drawn; loadWallpaper runs again when it is done.
func (s *shellUI) picturePath() string {
	s.nextPicture()
	p, err := wallgen.NewPalette(shellThemeColors(s, s.theme()))
	if err != nil {
		log.Printf("wallpaper: %s: %v", s.theme(), err)
		return ""
	}
	w, h := largestMonitor()
	if w == 0 {
		return ""
	}
	name, seed := wallgen.OfDay(time.Now())
	key := wallgen.Key(name, w, h, p, seed)
	dir := pictureDir()
	path := filepath.Join(dir, key+".png")
	if _, err := os.Stat(path); err == nil {
		go prunePictures(dir, path)
		return path
	}
	if s.styles.drawing == key {
		return ""
	}
	s.styles.drawing = key
	go func() {
		start := time.Now()
		err := drawPicture(dir, path, name, w, h, p, seed)
		gtkx.Post(func() {
			if s.styles.drawing == key {
				s.styles.drawing = ""
			}
			if err != nil {
				log.Printf("wallpaper: %s: %v", name, err)
				return
			}
			log.Printf("wallpaper: drew %s in %v", key, time.Since(start).Round(time.Millisecond))
			s.loadWallpaper() // shows it, unless the settings moved on meanwhile
		})
	}()
	return ""
}

// nextPicture loads the wallpaper again just after midnight, for the next
// day's picture.
func (s *shellUI) nextPicture() {
	if s.styles.midnight != nil {
		s.styles.midnight.Stop()
	}
	now := time.Now()
	y, m, d := now.Date()
	next := time.Date(y, m, d+1, 0, 0, 5, 0, now.Location())
	s.styles.midnight = time.AfterFunc(next.Sub(now), func() {
		gtkx.Post(func() {
			if s.cfg.Background.Generate {
				s.loadWallpaper()
			}
		})
	})
}

// pictureDir is where the pictures are kept: $XDG_CACHE_HOME/inua/wallpapers.
func pictureDir() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "inua", "wallpapers")
}

// drawPicture draws a picture to path.
func drawPicture(dir, path, name string, w, h int, p wallgen.Palette, seed uint64) error {
	img, err := wallgen.Draw(name, w, h, p, seed)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Whole or not at all: a file half written by a shell that went away is
	// never shown.
	part := path + ".part"
	if err := writePNG(part, img); err != nil {
		os.Remove(part)
		return err
	}
	return os.Rename(part, path)
}

// prunePictures removes the pictures kept but for keep, the one shown: only
// it is worth its room. One being drawn is left to finish.
func prunePictures(dir, keep string) {
	old, _ := filepath.Glob(filepath.Join(dir, "*"))
	for _, f := range old {
		if f != keep && !strings.HasSuffix(f, ".part") {
			os.Remove(f)
		}
	}
}

// largestMonitor is the size in pixels of the largest monitor, the one a
// picture is drawn for; the others show it scaled down. 0, 0 without one.
func largestMonitor() (w, h int) {
	list := gdk.DisplayGetDefault().GetMonitors()
	for i := range list.GetNItems() {
		mon := gdk.MonitorNewFromInternalPtr(list.GetItem(i))
		var geo gdk.Rectangle
		mon.GetGeometry(&geo)
		scale := mon.GetScale()
		mon.Unref()
		mw, mh := int(float64(geo.Width)*scale+0.5), int(float64(geo.Height)*scale+0.5)
		if mw*mh > w*h {
			w, h = mw, mh
		}
	}
	return w, h
}
