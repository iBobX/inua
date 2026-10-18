package gamestream

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"
)

// opusPacket is an audio RTP packet's Opus data, or nil for the error
// correction packets (payload type 127), which are not used.
func opusPacket(p []byte) []byte {
	if len(p) <= 12 || p[1]&0x7F != 97 {
		return nil
	}
	off := 12
	if p[0]&0x10 != 0 {
		off = 16
	}
	if len(p) <= off {
		return nil
	}
	return p[off:]
}

// Session is a stream from a host: its video frames and Opus packets, and
// the input sent to it.
type Session struct {
	Host   *Host
	Config Config // as streamed: the codec the host could give

	Video <-chan Frame
	Audio <-chan []byte

	ctl          *control
	video, audio *receiver
	frames       chan Frame
	sound        chan []byte

	mu      sync.Mutex
	lastIDR time.Time
	done    chan struct{}
	err     error
}

// Connect starts app on the host (or joins the session running there) and
// starts its streams. The host must be paired.
func Connect(ctx context.Context, h *Host, appID int, cfg Config) (*Session, error) {
	info, err := h.Info(ctx)
	if err != nil {
		return nil, err
	}
	if !info.Paired() {
		return nil, ErrNotPaired
	}
	if !info.Sunshine() {
		return nil, errors.New("gamestream: the host is not Sunshine; Nvidia's GameStream is not supported")
	}
	switch {
	case cfg.Codec == AV1 && info.Codecs&CodecAV1 == 0:
		cfg.Codec = HEVC
		fallthrough
	case cfg.Codec == HEVC && info.Codecs&CodecHEVC == 0:
		cfg.Codec = H264
	}
	if cfg.PacketSize == 0 {
		cfg.PacketSize = 1392
	}
	if cfg.AudioPacketMs == 0 {
		cfg.AudioPacketMs = 5
	}

	var l Launch
	rand.Read(l.Key[:])
	var id [4]byte
	rand.Read(id[:])
	l.KeyID = binary.BigEndian.Uint32(id[:])
	l.AppID, l.Width, l.Height, l.FPS = appID, cfg.Width, cfg.Height, cfg.FPS
	if info.CurrentGame != 0 {
		l.AppID, l.Resume = info.CurrentGame, true
	}
	sessionURL, err := h.Start(ctx, l)
	if err != nil {
		return nil, err
	}
	host := h.Hostname()
	hs, err := handshake(ctx, sessionURL, host, cfg)
	if err != nil {
		return nil, err
	}

	s := &Session{
		Host: h, Config: cfg, frames: make(chan Frame, 32), sound: make(chan []byte, 256),
		done: make(chan struct{}),
	}
	s.Video, s.Audio = s.frames, s.sound
	asm := &assembler{codec: cfg.Codec}
	s.video, err = dialStream(context.Background(), host, hs.VideoPort, hs.PingPayload, func(p []byte) {
		v, ok := parseVideo(p)
		if !ok {
			return
		}
		f, lost := asm.add(v)
		if lost {
			s.RequestIDR()
		}
		if f == nil {
			return
		}
		select {
		case s.frames <- *f:
		default:
			// The decoder is behind: this frame is lost to it, so the
			// next it can use is an IDR frame.
			asm.waitIDR = true
			s.RequestIDR()
		}
	})
	if err != nil {
		return nil, err
	}
	s.audio, err = dialStream(context.Background(), host, hs.AudioPort, hs.PingPayload, func(p []byte) {
		if op := opusPacket(p); op != nil {
			select {
			case s.sound <- append([]byte(nil), op...):
			default: // a gap in the sound beats a delay
			}
		}
	})
	if err != nil {
		s.video.close()
		return nil, err
	}
	ctl, err := dialControl(ctx, host, hs.ControlPort, hs.ConnectData, l.Key)
	if err != nil {
		s.video.close()
		s.audio.close()
		return nil, err
	}
	s.mu.Lock()
	s.ctl = ctl // the video's IDR requests wait for it
	s.mu.Unlock()
	go func() {
		<-ctl.done
		s.end(ctl.err)
	}()
	return s, nil
}

// Done is closed when the session ends; Err says why.
func (s *Session) Done() <-chan struct{} { return s.done }

// Err is why the session ended.
func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *Session) end(err error) {
	s.mu.Lock()
	select {
	case <-s.done:
		s.mu.Unlock()
		return
	default:
	}
	s.err = err
	close(s.done)
	s.mu.Unlock()
	s.video.close()
	s.audio.close()
}

// Close ends the stream here; what runs on the host goes on, to be joined
// again.
func (s *Session) Close() {
	s.ctl.close()
	s.end(nil)
}

// Quit ends the stream and what runs on the host.
func (s *Session) Quit(ctx context.Context) error {
	s.Close()
	return s.Host.Quit(ctx)
}

// RequestIDR asks for a frame that needs none before it, at most five
// times a second.
func (s *Session) RequestIDR() {
	s.mu.Lock()
	if time.Since(s.lastIDR) < 200*time.Millisecond {
		s.mu.Unlock()
		return
	}
	s.lastIDR = time.Now()
	ctl := s.ctl
	s.mu.Unlock()
	if ctl != nil {
		ctl.requestIDR()
	}
}

// Key presses (down) or releases a key, by its Windows virtual-key code.
func (s *Session) Key(vk uint16, down bool, modifiers uint8) error {
	return s.ctl.input(keyPacket(vk, down, modifiers), chanKeyboard)
}

// MouseTo puts the pointer at x, y of a w×h picture.
func (s *Session) MouseTo(x, y, w, h int) error {
	return s.ctl.input(mouseAbsPacket(x, y, w, h), chanMouse)
}

// MouseBy moves the pointer by dx, dy.
func (s *Session) MouseBy(dx, dy int) error {
	return s.ctl.input(mouseRelPacket(int16(dx), int16(dy)), chanMouse)
}

// Button presses or releases a mouse button.
func (s *Session) Button(button uint8, down bool) error {
	return s.ctl.input(buttonPacket(button, down), chanMouse)
}

// Scroll scrolls up (positive) or down, WheelDelta a notch.
func (s *Session) Scroll(amount int) error {
	return s.ctl.input(scrollPacket(int16(max(min(amount, 32767), -32768))), chanMouse)
}

// HScroll scrolls right (positive) or left.
func (s *Session) HScroll(amount int) error {
	return s.ctl.input(hscrollPacket(int16(max(min(amount, 32767), -32768))), chanMouse)
}

// Type types text.
func (s *Session) Type(text string) error {
	for _, p := range textPackets(text) {
		if err := s.ctl.input(p, chanUTF8); err != nil {
			return fmt.Errorf("gamestream: typing: %w", err)
		}
	}
	return nil
}
