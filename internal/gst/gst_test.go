package gst

import (
	"path/filepath"
	"testing"
	"time"
)

func needGst(t *testing.T) {
	t.Helper()
	if err := Init(); err != nil {
		t.Skip(err)
	}
}

// tone makes a 2-second tone, tagged, as an Ogg Vorbis file.
func tone(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tone.ogg")
	err := Launch(`audiotestsrc num-buffers=86 freq=440 ! audio/x-raw,rate=44100,channels=2 ! audioconvert ! ` +
		`taginject tags="title=Tone,artist=Test,album=Tests" ! vorbisenc ! oggmux ! filesink location=` + path)
	if err != nil {
		t.Skip("cannot make a tone: ", err)
	}
	return path
}

func TestMessageLayout(t *testing.T) {
	needGst(t)
	for v, name := range map[uint32]string{msgEOS: "eos", msgError: "error", msgTag: "tag", msgElement: "element",
		msgDurationChanged: "duration-changed", msgAsyncDone: "async-done", msgStreamStart: "stream-start", msgStateChanged: "state-changed"} {
		if got := messageTypeGetName(v); got != name {
			t.Errorf("%#x: %q, want %q", v, got, name)
		}
	}
}

func TestDiscover(t *testing.T) {
	needGst(t)
	in, err := Discover(URI(tone(t)))
	if err != nil {
		t.Fatal(err)
	}
	if in.Duration < 1900*time.Millisecond || in.Duration > 2100*time.Millisecond || in.Title != "Tone" || in.Artist != "Test" {
		t.Errorf("%+v", in)
	}
	if in.Rate != 44100 || in.Channels != 2 {
		t.Errorf("rate %d, channels %d", in.Rate, in.Channels)
	}
	if _, err := Discover(URI(filepath.Join(t.TempDir(), "none.ogg"))); err == nil {
		t.Error("a file that is not there")
	}
}

// The video player: pictures of the video's size, and the sound tracks.
func TestPlayerVideo(t *testing.T) {
	needGst(t)
	path := filepath.Join(t.TempDir(), "clip.webm")
	if err := Launch(`webmmux name=m ! filesink location=` + path + ` ` +
		`videotestsrc num-buffers=30 ! video/x-raw,width=320,height=180,framerate=30/1 ! vp8enc deadline=1 ! m. ` +
		`audiotestsrc num-buffers=43 ! audioconvert ! vorbisenc ! m.`); err != nil {
		t.Skip("cannot make a video: ", err)
	}
	p, err := New(Options{Video: true, AudioSink: "fakesink"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.Open(URI(path), "")
	p.Play()
	select {
	case f := <-p.Frames:
		if f.W != 320 || f.H != 180 || len(f.Pix) != 320*180*4 {
			t.Errorf("frame %dx%d, %d bytes", f.W, f.H, len(f.Pix))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no picture")
	}
	if a, _, ca, _ := p.Tracks(); a != 1 || ca != 0 {
		t.Errorf("audio tracks %d, current %d", a, ca)
	}
	p.SetRate(2)
	for {
		select {
		case e := <-p.Events:
			if e.Kind == Error {
				t.Fatal(e.Err)
			}
			if e.Kind == EOS {
				return
			}
		case <-time.After(10 * time.Second):
			t.Fatal("no end")
		}
	}
}

func TestFloats(t *testing.T) {
	got := floats(`spectrum, endtime=(guint64)40000000, magnitude=(float){ -60, -12.5, 0 };`, "magnitude")
	if len(got) != 3 || got[1] != -12.5 {
		t.Errorf("%v", got)
	}
}
