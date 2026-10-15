// Package music is the Music app's logic, without GTK: the playlist and
// the order it plays in, M3U playlists, the equalizer's presets and what
// is kept between runs (docs/feat/players.md).
package music

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/i18n"
)

// StoreName is the app's file in the store.
const StoreName = "music"

// Track is an entry of the playlist.
type Track struct {
	Path   string        `json:"path"`
	Title  string        `json:"title,omitempty"`
	Artist string        `json:"artist,omitempty"`
	Album  string        `json:"album,omitempty"`
	Length time.Duration `json:"length,omitempty"`
	// Rate (Hz), Channels and Bitrate (bit/s), for the display.
	Rate     int `json:"rate,omitempty"`
	Channels int `json:"channels,omitempty"`
	Bitrate  int `json:"bitrate,omitempty"`
}

// Name is how the playlist shows a track: "Artist - Title", the title, or
// the file's name.
func (t Track) Name() string {
	switch {
	case t.Title != "" && t.Artist != "":
		return t.Artist + " - " + t.Title
	case t.Title != "":
		return t.Title
	}
	base := filepath.Base(t.Path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// Repeat is what happens at the playlist's end.
type Repeat int

const (
	RepeatOff Repeat = iota // stop
	RepeatAll               // from the top again
	RepeatOne               // the same track again
)

// Playlist is the tracks and which plays, in order or shuffled.
type Playlist struct {
	Tracks  []Track
	Current int // -1 for none
	Shuffle bool
	Repeat  Repeat

	played []int // shuffled: the tracks played this round, in order
	back   int   // how far Previous went back into played
	rng    *rand.Rand
}

// NewPlaylist is an empty playlist.
func NewPlaylist() *Playlist { return &Playlist{Current: -1} }

func (p *Playlist) rand() *rand.Rand {
	if p.rng == nil {
		p.rng = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 1))
	}
	return p.rng
}

// Add puts tracks at the end.
func (p *Playlist) Add(ts ...Track) { p.Tracks = append(p.Tracks, ts...) }

// Remove takes out the tracks at idx; the current one stays current, or
// none is when it goes.
func (p *Playlist) Remove(idx ...int) {
	gone := map[int]bool{}
	for _, i := range idx {
		gone[i] = true
	}
	var keep []Track
	cur := -1
	remap := map[int]int{}
	for i, t := range p.Tracks {
		if gone[i] {
			continue
		}
		remap[i] = len(keep)
		if i == p.Current {
			cur = len(keep)
		}
		keep = append(keep, t)
	}
	p.Tracks, p.Current = keep, cur
	var played []int
	for _, i := range p.played {
		if j, ok := remap[i]; ok {
			played = append(played, j)
		}
	}
	p.played, p.back = played, min(p.back, len(played))
}

// Clear empties the playlist.
func (p *Playlist) Clear() {
	p.Tracks, p.Current, p.played, p.back = nil, -1, nil, 0
}

// Move puts the track at from at to, the current one followed.
func (p *Playlist) Move(from, to int) {
	if from < 0 || from >= len(p.Tracks) || to < 0 || to >= len(p.Tracks) || from == to {
		return
	}
	t := p.Tracks[from]
	p.Tracks = slices.Insert(slices.Delete(p.Tracks, from, from+1), to, t)
	move := func(i int) int {
		switch {
		case i == from:
			return to
		case from < to && i > from && i <= to:
			return i - 1
		case from > to && i >= to && i < from:
			return i + 1
		}
		return i
	}
	p.Current = move(p.Current)
	for k, i := range p.played {
		p.played[k] = move(i)
	}
}

// Play makes track i current (a double-click on it).
func (p *Playlist) Play(i int) bool {
	if i < 0 || i >= len(p.Tracks) {
		return false
	}
	p.Current = i
	if p.Shuffle {
		p.played = append(p.played[:len(p.played)-p.back], i)
		p.back = 0
	}
	return true
}

// Next is the track after the current one: the next in order, or one not
// played yet this round when shuffled; at the end, the first again when
// repeating, else none (-1). ended says the current track came to its
// end, where RepeatOne plays it again; a click on Next goes on anyway.
func (p *Playlist) Next(ended bool) int {
	n := len(p.Tracks)
	if n == 0 {
		return -1
	}
	if ended && p.Repeat == RepeatOne && p.Current >= 0 {
		return p.Current
	}
	if !p.Shuffle {
		switch {
		case p.Current+1 < n:
			return p.Current + 1
		case p.Repeat == RepeatOff:
			return -1
		}
		return 0
	}
	if p.back > 0 { // went back with Previous: forward again the same way
		return p.played[len(p.played)-p.back]
	}
	seen := map[int]bool{}
	for _, i := range p.played {
		seen[i] = true
	}
	var left []int
	for i := range n {
		if !seen[i] {
			left = append(left, i)
		}
	}
	if len(left) == 0 {
		if p.Repeat == RepeatOff {
			return -1
		}
		p.played = nil // a new round
		for i := range n {
			if i != p.Current || n == 1 {
				left = append(left, i)
			}
		}
	}
	return left[p.rand().IntN(len(left))]
}

// Advance makes the track Next gave current.
func (p *Playlist) Advance(i int) {
	if i < 0 {
		p.Current = -1
		return
	}
	if p.Shuffle && p.back > 0 && p.played[len(p.played)-p.back] == i {
		p.back--
		p.Current = i
		return
	}
	p.Play(i)
}

// Previous is the track before: the one before in order (the first stays
// the first, or the last when repeating), or the one played before when
// shuffled.
func (p *Playlist) Previous() int {
	n := len(p.Tracks)
	if n == 0 {
		return -1
	}
	if p.Shuffle {
		if k := len(p.played) - p.back - 2; k >= 0 {
			p.back++
			p.Current = p.played[k]
			return p.Current
		}
		return max(p.Current, 0)
	}
	switch {
	case p.Current > 0:
		return p.Current - 1
	case p.Repeat == RepeatAll:
		return n - 1
	}
	return 0
}

// Total is the playlist's length, and whether every track's is known.
func (p *Playlist) Total() (time.Duration, bool) {
	var d time.Duration
	all := true
	for _, t := range p.Tracks {
		if t.Length <= 0 {
			all = false
		}
		d += t.Length
	}
	return d, all
}

// Filter is the tracks whose name has the words, by index.
func (p *Playlist) Filter(q string) []int {
	words := strings.Fields(strings.ToLower(q))
	var out []int
	for i, t := range p.Tracks {
		hay := strings.ToLower(t.Name() + " " + t.Album + " " + filepath.Base(t.Path))
		if !slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(hay, w) }) {
			out = append(out, i)
		}
	}
	return out
}

// Audio are the file extensions taken as music.
var Audio = []string{".mp3", ".flac", ".ogg", ".oga", ".opus", ".m4a", ".aac", ".wav", ".wma", ".ape", ".wv", ".mka", ".aiff", ".aif", ".mpc"}

// IsAudio says whether a file's name is music's.
func IsAudio(path string) bool { return slices.Contains(Audio, strings.ToLower(filepath.Ext(path))) }

// IsPlaylist says whether a file is an M3U playlist.
func IsPlaylist(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".m3u" || ext == ".m3u8"
}

// Expand turns files, folders (their music, all the way down, sorted)
// and playlists into tracks.
func Expand(paths []string) []Track {
	var out []Track
	for _, p := range paths {
		info, err := os.Stat(p)
		switch {
		case err != nil:
			continue
		case info.IsDir():
			var files []string
			filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() && IsAudio(path) {
					files = append(files, path)
				}
				return nil
			})
			slices.Sort(files)
			for _, f := range files {
				out = append(out, Track{Path: f})
			}
		case IsPlaylist(p):
			if f, err := os.Open(p); err == nil {
				ts, _ := ReadM3U(f, filepath.Dir(p))
				f.Close()
				out = append(out, ts...)
			}
		case IsAudio(p):
			out = append(out, Track{Path: p})
		}
	}
	return out
}

// ReadM3U reads a playlist; relative paths are from dir, and #EXTINF
// lines give lengths and names.
func ReadM3U(r io.Reader, dir string) ([]Track, error) {
	var out []Track
	var pending Track
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\ufeff"))
		switch {
		case line == "":
		case strings.HasPrefix(line, "#EXTINF:"):
			secs, name, _ := strings.Cut(strings.TrimPrefix(line, "#EXTINF:"), ",")
			if n, err := strconv.Atoi(strings.TrimSpace(strings.Fields(secs + " ")[0])); err == nil && n > 0 {
				pending.Length = time.Duration(n) * time.Second
			}
			if a, t, ok := strings.Cut(name, " - "); ok {
				pending.Artist, pending.Title = strings.TrimSpace(a), strings.TrimSpace(t)
			} else {
				pending.Title = strings.TrimSpace(name)
			}
		case strings.HasPrefix(line, "#"):
		default:
			path := line
			if u, err := url.Parse(line); err == nil && u.Scheme == "file" {
				path = u.Path
			}
			if !filepath.IsAbs(path) {
				path = filepath.Join(dir, path)
			}
			pending.Path = path
			out = append(out, pending)
			pending = Track{}
		}
	}
	return out, sc.Err()
}

// WriteM3U writes an extended M3U playlist.
func WriteM3U(w io.Writer, ts []Track) error {
	bw := bufio.NewWriter(w)
	fmt.Fprintln(bw, "#EXTM3U")
	for _, t := range ts {
		secs := -1
		if t.Length > 0 {
			secs = int(t.Length.Round(time.Second) / time.Second)
		}
		fmt.Fprintf(bw, "#EXTINF:%d,%s\n%s\n", secs, t.Name(), t.Path)
	}
	return bw.Flush()
}

// Clock is a time as the display shows it: 3:07, or 1:02:03.
func Clock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d / time.Second)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// Bands are the equalizer's ten bands, as Winamp's.
var Bands = []string{"60", "170", "310", "600", "1K", "3K", "6K", "12K", "14K", "16K"}

// Preset is a named equalizer setting: the preamp and the bands, in dB.
type Preset struct {
	Name   string
	Preamp float64
	Bands  [10]float64
}

// Presets are the equalizer's presets; their names are translated where
// shown (i18n.T).
var Presets = []Preset{
	{i18n.M("Flat"), 0, [10]float64{}},
	{i18n.M("Rock"), 0, [10]float64{5, 3, -3, -5, -2, 2, 5, 7, 7, 7}},
	{i18n.M("Pop"), 0, [10]float64{-1, 3, 5, 5, 3, -1, -2, -2, -1, -1}},
	{i18n.M("Jazz"), 0, [10]float64{3, 2, 1, 2, -1, -1, 0, 1, 2, 3}},
	{i18n.M("Classical"), 0, [10]float64{0, 0, 0, 0, 0, 0, -4, -4, -4, -6}},
	{i18n.M("Dance"), 0, [10]float64{6, 4, 1, 0, 0, -3, -4, -4, 0, 0}},
	{i18n.M("Bass"), -2, [10]float64{7, 7, 5, 3, 1, -1, -2, -3, -3, -3}},
	{i18n.M("Treble"), -2, [10]float64{-4, -4, -4, -2, 1, 4, 7, 9, 9, 10}},
	{i18n.M("Vocal"), 0, [10]float64{-2, -3, -2, 1, 4, 4, 3, 1, 0, -2}},
	{i18n.M("Laptop speakers"), 0, [10]float64{3, 6, 3, -2, -1, 1, 3, 6, 8, 9}},
}

// State is what the app keeps between runs.
type State struct {
	Tracks   []Track       `json:"tracks"`
	Current  int           `json:"current"`
	Position time.Duration `json:"position,omitempty"`
	Volume   float64       `json:"volume"`
	Balance  float64       `json:"balance,omitempty"`
	Shuffle  bool          `json:"shuffle,omitempty"`
	Repeat   Repeat        `json:"repeat,omitempty"`
	EQOn     bool          `json:"eq_on"`
	Preamp   float64       `json:"preamp,omitempty"`
	EQ       [10]float64   `json:"eq"`
	ShowEQ   bool          `json:"show_eq"`
	ShowList bool          `json:"show_playlist"`
	TimeLeft bool          `json:"time_left,omitempty"`
	LastDir  string        `json:"last_dir,omitempty"`
}

// DefaultState is a first run's.
func DefaultState() State {
	return State{Current: -1, Volume: 0.8, EQOn: true, ShowEQ: true, ShowList: true}
}
