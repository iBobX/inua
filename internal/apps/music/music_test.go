package music

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func tracks(n int) []Track {
	var ts []Track
	for i := range n {
		ts = append(ts, Track{Path: "/m/" + string(rune('a'+i)) + ".mp3", Length: time.Minute})
	}
	return ts
}

func TestOrder(t *testing.T) {
	p := NewPlaylist()
	p.Add(tracks(3)...)
	if p.Next(true) != 0 {
		t.Fatal("first")
	}
	p.Advance(0)
	p.Advance(p.Next(true))
	p.Advance(p.Next(true))
	if p.Current != 2 || p.Next(true) != -1 {
		t.Errorf("the end without repeat: %d, next %d", p.Current, p.Next(true))
	}
	p.Repeat = RepeatAll
	if p.Next(true) != 0 || p.Previous() != 1 {
		t.Error("repeat all")
	}
	p.Repeat = RepeatOne
	if p.Next(true) != 2 || p.Next(false) != 0 {
		t.Error("repeat one: again at the end, on with a click")
	}
	p.Current, p.Repeat = 0, RepeatOff
	if p.Previous() != 0 {
		t.Error("previous of the first")
	}
}

// Shuffled: every track once a round, then a new round when repeating;
// Previous walks back the way it came, and Next forward again.
func TestShuffle(t *testing.T) {
	p := NewPlaylist()
	p.Add(tracks(5)...)
	p.Shuffle = true
	var seen []int
	for i := p.Next(true); i >= 0; i = p.Next(true) {
		p.Advance(i)
		seen = append(seen, i)
	}
	sorted := slices.Clone(seen)
	slices.Sort(sorted)
	if !slices.Equal(sorted, []int{0, 1, 2, 3, 4}) {
		t.Fatalf("a round: %v", seen)
	}
	if p.Previous() != seen[3] || p.Previous() != seen[2] {
		t.Error("back")
	}
	if n := p.Next(false); n != seen[3] {
		t.Errorf("forward again: %d, want %d", n, seen[3])
	}
	p.Advance(seen[3])
	p.Advance(p.Next(false))
	if p.Current != seen[4] {
		t.Errorf("forward again: %d", p.Current)
	}
	p.Repeat = RepeatAll
	if n := p.Next(true); n < 0 || n == p.Current {
		t.Errorf("a new round starts with another track: %d", n)
	}
}

func TestRemoveMove(t *testing.T) {
	p := NewPlaylist()
	p.Add(tracks(5)...)
	p.Play(3)
	p.Remove(0, 1)
	if p.Current != 1 || len(p.Tracks) != 3 || p.Tracks[1].Path != "/m/d.mp3" {
		t.Errorf("after remove: %d %v", p.Current, p.Tracks)
	}
	p.Move(1, 2)
	if p.Current != 2 || p.Tracks[2].Path != "/m/d.mp3" {
		t.Errorf("after move: %d", p.Current)
	}
	p.Move(0, 2)
	if p.Current != 1 {
		t.Errorf("after moving another past it: %d", p.Current)
	}
	p.Remove(1)
	if p.Current != -1 {
		t.Error("the current one removed")
	}
	if d, all := p.Total(); d != 2*time.Minute || !all {
		t.Errorf("total %v %v", d, all)
	}
	p.Tracks[0].Title, p.Tracks[0].Artist = "Blue", "Band"
	if got := p.Filter("band BLUE"); len(got) != 1 || got[0] != 0 {
		t.Errorf("filter %v", got)
	}
}

func TestM3U(t *testing.T) {
	in := "#EXTM3U\n#EXTINF:187,Band - Blue\nmusic/blue.flac\n#EXTINF:-1,Other\n/abs/other.mp3\nfile:///abs/x%20y.ogg\n"
	ts, err := ReadM3U(strings.NewReader(in), "/home/ada")
	if err != nil || len(ts) != 3 {
		t.Fatalf("%v %v", ts, err)
	}
	if ts[0].Path != "/home/ada/music/blue.flac" || ts[0].Length != 187*time.Second || ts[0].Artist != "Band" || ts[0].Title != "Blue" {
		t.Errorf("first %+v", ts[0])
	}
	if ts[1].Length != 0 || ts[2].Path != "/abs/x y.ogg" {
		t.Errorf("%+v %+v", ts[1], ts[2])
	}
	var b strings.Builder
	WriteM3U(&b, ts[:2])
	again, _ := ReadM3U(strings.NewReader(b.String()), "/")
	if len(again) != 2 || again[0] != ts[0] || again[1].Title != "Other" {
		t.Errorf("round trip %q → %+v", b.String(), again)
	}
}

func TestExpand(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"b/2.flac", "b/1.mp3", "b/cover.jpg", "a.ogg"} {
		os.MkdirAll(filepath.Join(dir, filepath.Dir(f)), 0o755)
		os.WriteFile(filepath.Join(dir, f), nil, 0o644)
	}
	os.WriteFile(filepath.Join(dir, "list.m3u"), []byte("a.ogg\n"), 0o644)
	ts := Expand([]string{filepath.Join(dir, "b"), filepath.Join(dir, "list.m3u"), filepath.Join(dir, "b/cover.jpg"), "/none"})
	var names []string
	for _, tr := range ts {
		names = append(names, strings.TrimPrefix(tr.Path, dir+"/"))
	}
	if strings.Join(names, " ") != "b/1.mp3 b/2.flac a.ogg" {
		t.Errorf("%v", names)
	}
}

func TestClockName(t *testing.T) {
	for d, want := range map[time.Duration]string{0: "0:00", 187 * time.Second: "3:07", 3723 * time.Second: "1:02:03", -time.Second: "0:00"} {
		if got := Clock(d); got != want {
			t.Errorf("%v: %q", d, got)
		}
	}
	if n := (Track{Path: "/m/Song One.mp3"}).Name(); n != "Song One" {
		t.Errorf("name %q", n)
	}
}
