package gst

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// Stream decodes a live stream pushed into it, as it comes: a remote
// desktop's video, its frames out as Frames, or its sound, played. It
// keeps no more than it must: late frames are dropped, not waited for.
type Stream struct {
	pipe, src, sink uintptr
	Frames          chan Frame // the newest picture; older ones are dropped
	Errors          chan error

	mu     sync.Mutex
	closed bool
	done   chan struct{}
	wg     sync.WaitGroup
}

// The decoders for each codec, the hardware's first (VA-API), then the
// software's.
var decoders = map[string][]string{
	"h264": {"vah264dec", "vaapih264dec", "nvh264dec", "avdec_h264"},
	"h265": {"vah265dec", "vaapih265dec", "nvh265dec", "avdec_h265"},
	"av1":  {"vaav1dec", "nvav1dec", "dav1ddec", "av1dec"},
}

var caps = map[string]string{
	"h264": "video/x-h264,stream-format=byte-stream,alignment=au",
	"h265": "video/x-h265,stream-format=byte-stream,alignment=au",
	"av1":  "video/x-av1,stream-format=obu-stream,alignment=tu",
}

var parsers = map[string]string{"h264": "h264parse", "h265": "h265parse", "av1": "av1parse"}

// has says whether GStreamer has the element.
func has(element string) bool {
	f := elementFactoryFind(element)
	if f == 0 {
		return false
	}
	objectUnref(f)
	return true
}

// Decoder is the decoder this computer has for codec (h264, h265, av1),
// or "".
func Decoder(codec string) string {
	if err := Init(); err != nil {
		return ""
	}
	for _, d := range decoders[codec] {
		if has(d) {
			return d
		}
	}
	return ""
}

// NewVideo decodes codec (h264, h265, av1) to RGBA Frames, with the
// hardware's decoder when there is one.
func NewVideo(codec string) (*Stream, error) {
	if err := Init(); err != nil {
		return nil, err
	}
	dec := Decoder(codec)
	if dec == "" {
		return nil, fmt.Errorf("gst: no %s decoder (gstreamer1.0-libav, or the VA-API plugins of gstreamer1.0-plugins-bad)", codec)
	}
	convert := "videoconvert"
	if strings.HasPrefix(dec, "va") && has("vapostproc") {
		convert = "vapostproc" // converts on the GPU, then gives the picture to memory
	}
	desc := fmt.Sprintf("appsrc name=src is-live=true do-timestamp=true format=time caps=%s ! %s ! %s ! %s ! "+
		"video/x-raw,format=RGBA ! appsink name=sink max-buffers=2 drop=true sync=false",
		caps[codec], parsers[codec], dec, convert)
	return newStream(desc, true)
}

// NewAudio plays Opus, two channels at 48 kHz, on the default output.
func NewAudio(sink string) (*Stream, error) {
	if err := Init(); err != nil {
		return nil, err
	}
	if sink == "" {
		sink = "autoaudiosink"
	}
	desc := "appsrc name=src is-live=true do-timestamp=true format=time " +
		"caps=audio/x-opus,channels=2,rate=48000,channel-mapping-family=0 ! opusdec plc=true ! " +
		"audioconvert ! audioresample ! " + sink + " sync=false"
	return newStream(desc, false)
}

func newStream(desc string, video bool) (*Stream, error) {
	var e uintptr
	pipe := parseLaunch(desc, &e)
	if err := gerror(e); err != nil || pipe == 0 {
		return nil, errors.Join(errors.New("gst: "+desc), err)
	}
	s := &Stream{pipe: pipe, src: binGetByName(pipe, "src"), Errors: make(chan error, 4), done: make(chan struct{})}
	if video {
		s.sink = binGetByName(pipe, "sink")
		s.Frames = make(chan Frame, 1)
	}
	if elementSetState(pipe, statePlaying) == 0 { // GST_STATE_CHANGE_FAILURE
		objectUnref(pipe)
		return nil, errors.New("gst: the pipeline would not play: " + desc)
	}
	bus := elementGetBus(pipe)
	s.wg.Add(1)
	go s.readBus(bus)
	if video {
		s.wg.Add(1)
		go s.readFrames()
	}
	return s, nil
}

// Push gives the stream data: a frame, or an audio packet.
func (s *Stream) Push(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("gst: the stream is closed")
	}
	buf := bufferNewAllocate(0, uintptr(len(data)), 0)
	if buf == 0 {
		return errors.New("gst: no buffer")
	}
	bufferFill(buf, 0, unsafe.Pointer(&data[0]), uintptr(len(data)))
	if r := appSrcPushBuffer(s.src, buf); r != 0 { // takes the buffer, whatever it answers
		return fmt.Errorf("gst: the stream refused data (%d)", r)
	}
	return nil
}

func (s *Stream) readFrames() {
	defer s.wg.Done()
	for {
		select {
		case <-s.done:
			return
		default:
		}
		sample := appSinkTryPullSample(s.sink, uint64(100*time.Millisecond))
		if sample == 0 {
			continue
		}
		f, ok := frame(sample)
		miniObjectUnref(sample)
		if !ok {
			continue
		}
		select { // drop the one not shown yet
		case <-s.Frames:
		default:
		}
		s.Frames <- f
	}
}

func (s *Stream) readBus(bus uintptr) {
	defer s.wg.Done()
	defer objectUnref(bus)
	for {
		select {
		case <-s.done:
			return
		default:
		}
		msg := busTimedPopFiltered(bus, uint64(100*time.Millisecond), msgError)
		if msg == 0 {
			continue
		}
		var gerr, debug uintptr
		messageParseError(msg, &gerr, &debug)
		err := gerror(gerr)
		if debug != 0 {
			gFree(debug)
		}
		miniObjectUnref(msg)
		select {
		case s.Errors <- err:
		default:
		}
	}
}

// Close stops the stream.
func (s *Stream) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	close(s.done)
	s.mu.Unlock()
	s.wg.Wait()
	elementSetState(s.pipe, stateNull)
	if s.sink != 0 {
		objectUnref(s.sink)
	}
	objectUnref(s.src)
	objectUnref(s.pipe)
}
