// Package record records voice input from the microphone through pw-cat
// (PipeWire), as 16 kHz mono WAV: what speech-to-text models take, 32 KB
// a second, so the longest recording (MaxLength) stays well under their
// 25 MB.
package record

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const rate = 16000 // samples a second, 16-bit, one channel

// MaxLength is the longest recording; it stops growing after it (Full).
const MaxLength = 5 * time.Minute

// ErrNoPwCat is a system without pw-cat.
var ErrNoPwCat = errors.New("voice input records with pw-cat (PipeWire), which is not installed")

// Available reports whether there is pw-cat to record with.
func Available() bool {
	_, err := exec.LookPath("pw-cat")
	return err == nil
}

// Recorder is one recording, from Start to Stop or Cancel.
type Recorder struct {
	cmd    *exec.Cmd
	stderr bytes.Buffer
	read   chan struct{} // closed when pw-cat's output ends

	mu    sync.Mutex
	pcm   []byte
	level float64
	full  bool
	err   error
}

// Start starts recording from the default microphone.
func Start() (*Recorder, error) {
	path, err := exec.LookPath("pw-cat")
	if err != nil {
		return nil, ErrNoPwCat
	}
	r := &Recorder{read: make(chan struct{})}
	r.cmd = exec.Command(path, "--record", "--raw", "--rate", "16000", "--channels", "1", "--format", "s16",
		"--media-role", "Communication", "--latency", "50ms", "-")
	r.cmd.Stderr = &r.stderr
	r.cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM} // not left recording
	out, err := r.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := r.cmd.Start(); err != nil {
		return nil, err
	}
	go r.take(out)
	return r, nil
}

// take keeps pw-cat's samples until it ends or the recording is full,
// and how loud the last tenth of a second was.
func (r *Recorder) take(out io.Reader) {
	defer close(r.read)
	limit := int(MaxLength.Seconds()) * rate * 2
	buf := make([]byte, rate/10*2)
	for {
		n, err := io.ReadFull(out, buf)
		n -= n % 2
		r.mu.Lock()
		room := limit - len(r.pcm)
		if n > room {
			n = room
		}
		r.pcm = append(r.pcm, buf[:n]...)
		if n > 0 {
			r.level = loudness(buf[:n])
		}
		if len(r.pcm) >= limit {
			r.full = true
		}
		full := r.full
		r.mu.Unlock()
		if err != nil || full {
			if full {
				io.Copy(io.Discard, out) // until Stop ends pw-cat
			}
			return
		}
	}
}

// loudness is how loud samples are, 0 to 1: their RMS on a decibel scale
// from -60 dB, as a meter shows speech.
func loudness(pcm []byte) float64 {
	if len(pcm) < 2 {
		return 0
	}
	var sum float64
	for i := 0; i+1 < len(pcm); i += 2 {
		s := float64(int16(binary.LittleEndian.Uint16(pcm[i:]))) / 32768
		sum += s * s
	}
	rms := math.Sqrt(sum / float64(len(pcm)/2))
	if rms <= 0 {
		return 0
	}
	return max(0, min(1, 1+20*math.Log10(rms)/60))
}

// Level is how loud the microphone is now, 0 to 1.
func (r *Recorder) Level() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.level
}

// Length is how long the recording is so far.
func (r *Recorder) Length() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return time.Duration(len(r.pcm)/2) * time.Second / rate
}

// Full reports whether the recording reached MaxLength, or pw-cat ended
// by itself (the microphone went away): it should be stopped.
func (r *Recorder) Full() bool {
	select {
	case <-r.read:
		return true
	default:
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.full
}

// Stop ends the recording and returns it as WAV.
func (r *Recorder) Stop() ([]byte, error) {
	r.end()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.pcm) == 0 {
		if msg := strings.TrimSpace(r.stderr.String()); msg != "" {
			return nil, errors.New("pw-cat: " + msg)
		}
		return nil, errors.New("nothing was recorded")
	}
	return WAV(r.pcm), nil
}

// Cancel ends the recording and drops it.
func (r *Recorder) Cancel() {
	r.end()
	r.mu.Lock()
	r.pcm = nil
	r.mu.Unlock()
}

func (r *Recorder) end() {
	if r.cmd.ProcessState == nil {
		r.cmd.Process.Signal(syscall.SIGINT) // pw-cat ends cleanly on it
		timer := time.AfterFunc(2*time.Second, func() { r.cmd.Process.Kill() })
		<-r.read
		r.cmd.Wait()
		timer.Stop()
	}
}

// WAV wraps 16 kHz mono 16-bit samples in a WAV header.
func WAV(pcm []byte) []byte {
	var b bytes.Buffer
	b.Grow(44 + len(pcm))
	w := func(v any) { binary.Write(&b, binary.LittleEndian, v) }
	b.WriteString("RIFF")
	w(uint32(36 + len(pcm)))
	b.WriteString("WAVEfmt ")
	w(uint32(16))       // the format chunk's size
	w(uint16(1))        // PCM
	w(uint16(1))        // channels
	w(uint32(rate))     // sample rate
	w(uint32(rate * 2)) // bytes a second
	w(uint16(2))        // bytes a frame
	w(uint16(16))       // bits a sample
	b.WriteString("data")
	w(uint32(len(pcm)))
	b.Write(pcm)
	return b.Bytes()
}
