package gst

import (
	"testing"
	"time"
	"unsafe"
)

// encoded makes a short stream with GStreamer's own encoder: its buffers,
// one access unit each, as a remote desktop's host sends them.
func encoded(t *testing.T, desc string) [][]byte {
	t.Helper()
	var e uintptr
	pipe := parseLaunch(desc+" ! appsink name=out sync=false", &e)
	if err := gerror(e); err != nil || pipe == 0 {
		t.Skip("cannot encode a test stream: ", err)
	}
	defer objectUnref(pipe)
	out := binGetByName(pipe, "out")
	defer objectUnref(out)
	elementSetState(pipe, statePlaying)
	defer elementSetState(pipe, stateNull)
	var bufs [][]byte
	for {
		sample := appSinkTryPullSample(out, uint64(2*time.Second))
		if sample == 0 {
			break
		}
		buf := sampleGetBuffer(sample)
		var info mapInfo
		if bufferMap(buf, &info, mapRead) {
			bufs = append(bufs, append([]byte(nil), unsafe.Slice((*byte)(cmem(info.data)), int(info.size))...))
			bufferUnmap(buf, &info)
		}
		miniObjectUnref(sample)
	}
	if len(bufs) == 0 {
		t.Skip("the encoder gave nothing")
	}
	return bufs
}

func TestAVideoStreamDecodesWhatIsPushed(t *testing.T) {
	if err := Init(); err != nil {
		t.Skip(err)
	}
	if !has("x264enc") {
		t.Skip("no x264enc")
	}
	// 8-bit 4:2:0, main profile: as a host's encoder makes it.
	aus := encoded(t, "videotestsrc num-buffers=20 ! video/x-raw,format=I420,width=320,height=240,framerate=30/1 ! "+
		"x264enc tune=zerolatency key-int-max=10 ! video/x-h264,profile=main,stream-format=byte-stream,alignment=au")
	s, err := NewVideo("h264")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, au := range aus {
		if err := s.Push(au); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case f := <-s.Frames:
		if f.W != 320 || f.H != 240 || len(f.Pix) != 320*240*4 {
			t.Errorf("frame %dx%d, %d bytes", f.W, f.H, len(f.Pix))
		}
	case err := <-s.Errors:
		t.Fatal(err)
	case <-time.After(10 * time.Second):
		t.Fatal("no frame came out")
	}
}

func TestAnAudioStreamPlaysOpus(t *testing.T) {
	if err := Init(); err != nil {
		t.Skip(err)
	}
	packets := encoded(t, "audiotestsrc num-buffers=20 ! audio/x-raw,channels=2,rate=48000 ! opusenc frame-size=5")
	s, err := NewAudio("fakesink")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, p := range packets {
		if err := s.Push(p); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case err := <-s.Errors:
		t.Fatal(err)
	case <-time.After(500 * time.Millisecond):
	}
}

func TestDecoderIsOneThisComputerHas(t *testing.T) {
	if err := Init(); err != nil {
		t.Skip(err)
	}
	if d := Decoder("h264"); d != "" && !has(d) {
		t.Errorf("%s named, but not there", d)
	}
	if Decoder("nonsense") != "" {
		t.Error("a decoder for nothing")
	}
}
