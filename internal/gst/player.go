package gst

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// Options shape a Player.
type Options struct {
	// Video draws the pictures as Frames; without it only sound plays.
	Video bool
	// Filters puts the music filters in the sound's way: balance, preamp,
	// a ten-band equalizer and a spectrum of SpectrumBands bands.
	Filters       bool
	SpectrumBands int
	// AudioSink replaces the speakers: an element's name ("fakesink" in
	// tests; $INUA_AUDIO_SINK in the apps, for trying them out silently).
	AudioSink string
}

// Kind is what an Event says.
type Kind int

const (
	EOS      Kind = iota // the end
	Error                // Err says why it stopped
	Tags                 // Info has what the stream says of itself
	Spectrum             // Bands has the spectrum, in dB
	Duration             // the duration is known, or changed
	Started              // a new stream starts playing
)

// Event is news from the player's bus.
type Event struct {
	Kind  Kind
	Err   error
	Info  Info
	Bands []float64
}

// Info is what a stream says of itself.
type Info struct {
	Duration                time.Duration
	Title, Artist, Album    string
	Bitrate, Rate, Channels int
}

// Frame is a picture, RGBA, Stride bytes a row.
type Frame struct {
	W, H, Stride int
	Pix          []byte
}

// Player plays one stream at a time with playbin.
type Player struct {
	o      Options
	pipe   uintptr
	bus    uintptr
	eq     uintptr
	pre    uintptr
	pan    uintptr
	sink   uintptr // the video's appsink
	Events chan Event
	Frames chan Frame // the newest picture; older ones are dropped

	mu     sync.Mutex
	closed bool
	rate   float64
	done   chan struct{}
	wg     sync.WaitGroup
}

// New makes a player; Close ends it.
func New(o Options) (*Player, error) {
	if err := Init(); err != nil {
		return nil, err
	}
	pipe := elementFactoryMake("playbin", "")
	if pipe == 0 {
		return nil, errors.New("gst: no playbin (gstreamer1.0-plugins-base)")
	}
	p := &Player{o: o, pipe: pipe, Events: make(chan Event, 64), Frames: make(chan Frame, 1), rate: 1, done: make(chan struct{})}
	if o.Filters {
		bands := max(o.SpectrumBands, 1)
		desc := fmt.Sprintf("audiopanorama name=pan method=simple ! volume name=pre ! equalizer-10bands name=eq ! "+
			"spectrum name=spec bands=%d interval=40000000 threshold=-80 post-messages=true message-magnitude=true", bands)
		bin, err := bin(desc)
		if err != nil {
			objectUnref(pipe)
			return nil, err
		}
		p.pan, p.pre, p.eq = binGetByName(bin, "pan"), binGetByName(bin, "pre"), binGetByName(bin, "eq")
		gObjectSetPtr(pipe, "audio-filter", bin, 0)
	}
	if o.AudioSink != "" {
		s := elementFactoryMake(o.AudioSink, "")
		if s != 0 {
			utilSetObjectArg(s, "sync", "true")
			gObjectSetPtr(pipe, "audio-sink", s, 0)
		}
	}
	if o.Video {
		vbin, err := bin("videoconvert ! videoscale add-borders=false ! video/x-raw,format=RGBA ! appsink name=vsink max-buffers=2 drop=true sync=true")
		if err != nil {
			objectUnref(pipe)
			return nil, err
		}
		p.sink = binGetByName(vbin, "vsink")
		gObjectSetPtr(pipe, "video-sink", vbin, 0)
	} else {
		// Sound only: no video, no subtitles.
		utilSetObjectArg(pipe, "flags", "audio+soft-volume")
	}
	p.bus = elementGetBus(pipe)
	p.wg.Add(1)
	go p.readBus()
	if o.Video {
		p.wg.Add(1)
		go p.readFrames()
	}
	return p, nil
}

func bin(desc string) (uintptr, error) {
	var e uintptr
	b := parseBinFromDesc(desc, true, &e)
	if err := gerror(e); err != nil || b == 0 {
		return 0, errors.Join(errors.New("gst: "+desc), err)
	}
	return b, nil
}

// URI is a file's URI, as playbin takes it.
func URI(path string) string {
	if strings.Contains(path, "://") {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return (&url.URL{Scheme: "file", Path: abs}).String()
}

// Open loads a stream (a URI), with subtitles from suburi ("" for none),
// paused at its start.
func (p *Player) Open(uri, suburi string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	elementSetState(p.pipe, stateReady)
	utilSetObjectArg(p.pipe, "uri", uri)
	utilSetObjectArg(p.pipe, "suburi", suburi)
	p.rate = 1
	elementSetState(p.pipe, statePaused)
}

func (p *Player) state(s int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		elementSetState(p.pipe, s)
	}
}

func (p *Player) Play()  { p.state(statePlaying) }
func (p *Player) Pause() { p.state(statePaused) }

// Stop ends playing and goes back to the start, unloading nothing.
func (p *Player) Stop() {
	p.state(stateReady)
}

// Playing says whether it plays (or is about to).
func (p *Player) Playing() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	var cur, pending int32
	elementGetState(p.pipe, &cur, &pending, 0)
	return pending == statePlaying || (cur == statePlaying && pending <= 0)
}

// Position is where it plays; false while nothing is loaded.
func (p *Player) Position() (time.Duration, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var v int64
	if p.closed || !elementQueryPosition(p.pipe, formatTime, &v) || v < 0 {
		return 0, false
	}
	return time.Duration(v), true
}

// Length is the stream's duration; false while not known.
func (p *Player) Length() (time.Duration, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var v int64
	if p.closed || !elementQueryDuration(p.pipe, formatTime, &v) || v <= 0 {
		return 0, false
	}
	return time.Duration(v), true
}

// Seek goes to a place in the stream.
func (p *Player) Seek(to time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	elementSeek(p.pipe, p.rate, formatTime, seekFlagFlush|seekFlagKeyUnit, seekTypeSet, int64(max(to, 0)), seekTypeNone, -1)
}

// SetRate plays faster or slower (0.25 to 4), where it is.
func (p *Player) SetRate(rate float64) {
	pos, ok := p.Position()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || !ok {
		return
	}
	p.rate = min(max(rate, 0.25), 4)
	elementSeek(p.pipe, p.rate, formatTime, seekFlagFlush|seekFlagAccurate, seekTypeSet, int64(pos), seekTypeNone, -1)
}

func (p *Player) set(obj uintptr, name, value string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed && obj != 0 {
		utilSetObjectArg(obj, name, value)
	}
}

// SetVolume sets the volume, 0 to 1 (on a cubic scale, as ears hear).
func (p *Player) SetVolume(v float64) {
	v = min(max(v, 0), 1)
	p.set(p.pipe, "volume", fmt.Sprint(v*v*v))
}

func (p *Player) SetMute(on bool) { p.set(p.pipe, "mute", fmt.Sprint(on)) }

// SetBalance pans the sound, -1 (left) to 1 (right).
func (p *Player) SetBalance(b float64) { p.set(p.pan, "panorama", fmt.Sprint(min(max(b, -1), 1))) }

// SetEQ sets the preamp and the ten bands (60 Hz … 16 kHz), in dB.
func (p *Player) SetEQ(preamp float64, bands [10]float64) {
	p.set(p.pre, "volume", fmt.Sprint(math.Pow(10, preamp/20)))
	for i, b := range bands {
		p.set(p.eq, fmt.Sprintf("band%d", i), fmt.Sprint(min(max(b, -24), 12)))
	}
}

// Tracks are how many sound and subtitle tracks the stream has, and
// which play (-1: none).
func (p *Player) Tracks() (audio, text, curAudio, curText int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0, 0, -1, -1
	}
	var a, t, ca, ct, flags int32
	gObjectGetInt(p.pipe, "n-audio", &a, 0)
	gObjectGetInt(p.pipe, "n-text", &t, 0)
	gObjectGetInt(p.pipe, "current-audio", &ca, 0)
	gObjectGetInt(p.pipe, "current-text", &ct, 0)
	gObjectGetInt(p.pipe, "flags", &flags, 0)
	if flags&playFlagText == 0 {
		ct = -1
	}
	return int(a), int(t), int(ca), int(ct)
}

// playbin's flags.
const playFlagText = 1 << 2

// SetAudioTrack plays another sound track.
func (p *Player) SetAudioTrack(i int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		gObjectSetInt(p.pipe, "current-audio", int32(i), 0)
	}
}

// SetSubtitles shows a subtitle track, or none (-1).
func (p *Player) SetSubtitles(i int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	var flags int32
	gObjectGetInt(p.pipe, "flags", &flags, 0)
	if i < 0 {
		flags &^= playFlagText
	} else {
		flags |= playFlagText
		gObjectSetInt(p.pipe, "current-text", int32(i), 0)
	}
	gObjectSetInt(p.pipe, "flags", flags, 0)
}

// Close stops and frees the player.
func (p *Player) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	close(p.done)
	elementSetState(p.pipe, stateNull)
	p.mu.Unlock()
	p.wg.Wait()
	for _, o := range []uintptr{p.eq, p.pre, p.pan, p.sink} {
		if o != 0 {
			objectUnref(o)
		}
	}
	objectUnref(p.bus)
	objectUnref(p.pipe)
}

func (p *Player) send(e Event) {
	select {
	case p.Events <- e:
	default: // nobody reads: dropped
	}
}

// readBus turns the bus's messages into Events until Close.
func (p *Player) readBus() {
	defer p.wg.Done()
	for {
		select {
		case <-p.done:
			return
		default:
		}
		msg := busTimedPopFiltered(p.bus, uint64(100*time.Millisecond), msgEOS|msgError|msgTag|msgElement|msgDurationChanged|msgStreamStart|msgAsyncDone)
		if msg == 0 {
			continue
		}
		switch messageType(msg) {
		case msgEOS:
			p.send(Event{Kind: EOS})
		case msgError:
			p.send(Event{Kind: Error, Err: parseError(msg)})
		case msgTag:
			var list uintptr
			messageParseTag(msg, &list)
			if list != 0 {
				p.send(Event{Kind: Tags, Info: tags(list)})
				miniObjectUnref(list)
			}
		case msgElement:
			if s := messageGetStructure(msg); s != 0 && structureGetName(s) == "spectrum" {
				p.send(Event{Kind: Spectrum, Bands: floats(cstring(structureToString(s), true), "magnitude")})
			}
		case msgDurationChanged, msgAsyncDone:
			p.send(Event{Kind: Duration})
		case msgStreamStart:
			p.send(Event{Kind: Started})
		}
		miniObjectUnref(msg)
	}
}

// tags reads a tag list.
func tags(list uintptr) Info {
	var in Info
	str := func(tag string) string {
		var v uintptr
		if tagListGetString(list, tag, &v) {
			return cstring(v, true)
		}
		return ""
	}
	in.Title, in.Artist, in.Album = str("title"), str("artist"), str("album")
	var u uint32
	if tagListGetUint(list, "bitrate", &u) || tagListGetUint(list, "nominal-bitrate", &u) {
		in.Bitrate = int(u)
	}
	return in
}

// readFrames pulls the video's pictures until Close, keeping the newest.
func (p *Player) readFrames() {
	defer p.wg.Done()
	for {
		select {
		case <-p.done:
			return
		default:
		}
		sample := appSinkTryPullSample(p.sink, uint64(100*time.Millisecond))
		if sample == 0 {
			continue
		}
		f, ok := frame(sample)
		miniObjectUnref(sample)
		if !ok {
			continue
		}
		select { // drop the one not shown yet
		case <-p.Frames:
		default:
		}
		p.Frames <- f
	}
}

func frame(sample uintptr) (Frame, bool) {
	caps, buf := sampleGetCaps(sample), sampleGetBuffer(sample)
	if caps == 0 || buf == 0 {
		return Frame{}, false
	}
	s := capsGetStructure(caps, 0)
	var w, h int32
	if !structureGetInt(s, "width", &w) || !structureGetInt(s, "height", &h) || w <= 0 || h <= 0 {
		return Frame{}, false
	}
	var info mapInfo
	if !bufferMap(buf, &info, mapRead) {
		return Frame{}, false
	}
	defer bufferUnmap(buf, &info)
	stride := int(w) * 4
	if int(info.size) < stride*int(h) {
		return Frame{}, false
	}
	pix := make([]byte, stride*int(h))
	copy(pix, unsafe.Slice((*byte)(cmem(info.data)), len(pix)))
	return Frame{W: int(w), H: int(h), Stride: stride, Pix: pix}, true
}

// Discover reads a stream's duration and tags without playing it.
func Discover(uri string) (Info, error) {
	if err := Init(); err != nil {
		return Info{}, err
	}
	var e uintptr
	d := discovererNew(uint64(5*time.Second), &e)
	if err := gerror(e); err != nil || d == 0 {
		return Info{}, errors.Join(errors.New("gst: no discoverer"), err)
	}
	defer gObjectUnref(d)
	info := discovererDiscoverURI(d, uri, &e)
	if info == 0 {
		return Info{}, errors.Join(errors.New("gst: cannot read "+uri), gerror(e))
	}
	defer gObjectUnref(info)
	if err := gerror(e); discovererInfoGetResult(info) != 0 { // GST_DISCOVERER_OK
		return Info{}, errors.Join(errors.New("gst: cannot read "+uri), err)
	}
	var in Info
	if list := discovererInfoGetTags(info); list != 0 {
		in = tags(list)
	}
	if dur := discovererInfoGetDuration(info); dur != math.MaxUint64 {
		in.Duration = time.Duration(dur)
	}
	// The first sound stream: GList {data, next, prev}.
	if list := discovererInfoGetAudio(info); list != 0 {
		ai := *(*uintptr)(cmem(list))
		in.Rate, in.Channels = int(audioInfoGetSampleRate(ai)), int(audioInfoGetChannels(ai))
		if b := int(audioInfoGetBitrate(ai)); in.Bitrate == 0 {
			in.Bitrate = b
		}
		streamInfoListFree(list)
	}
	return in, nil
}
