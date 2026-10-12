package gamesui

import (
	"encoding/binary"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"sync"

	"golang.org/x/sys/unix"

	"github.com/lyricat/inua/internal/games"
)

// synth plays Space Pinball's sounds: short tones, sweeps and noise made
// here, mixed and streamed as raw samples to pw-cat (PipeWire). The pipe
// is kept small, so a sound is heard within a few hundredths of a second
// of its hit. Without pw-cat the table is silent.
type synth struct {
	mu     sync.Mutex
	voices []voice
	muted  bool
	ok     bool
}

// voice is one sound: a waveform swept from f0 to f1 over dur seconds,
// after delay, fading out.
type voice struct {
	delay, t, dur float64
	f0, f1, vol   float64
	wave          int // waveSine …
	phase         float64
}

const (
	waveSine = iota
	waveSquare
	waveTri
	waveNoise
)

const sampleRate = 48000

func newSynth() *synth {
	s := &synth{}
	path, err := exec.LookPath("pw-cat")
	if err != nil {
		return s
	}
	r, w, err := os.Pipe()
	if err != nil {
		return s
	}
	unix.FcntlInt(w.Fd(), unix.F_SETPIPE_SZ, 4096) // a few hundredths of a second
	cmd := exec.Command(path, "--playback", "--raw", "--rate", "48000", "--channels", "1", "--format", "s16",
		"--latency", "20ms", "--media-role", "Game", "-")
	cmd.Stdin = r
	if cmd.Start() != nil {
		r.Close()
		w.Close()
		return s
	}
	r.Close()
	s.ok = true
	go s.stream(w, cmd)
	return s
}

// stream mixes the voices and writes them out for as long as pw-cat
// reads; the write blocks while its pipe is full, which paces it.
func (s *synth) stream(w *os.File, cmd *exec.Cmd) {
	defer cmd.Wait()
	defer w.Close()
	const n = 240 // 5 ms
	buf := make([]byte, 2*n)
	for {
		s.mu.Lock()
		for i := range n {
			v := 0.0
			for k := range s.voices {
				v += s.voices[k].next()
			}
			v = math.Tanh(v * 0.9) // soft clipping
			binary.LittleEndian.PutUint16(buf[2*i:], uint16(int16(v*0.55*32767)))
		}
		kept := s.voices[:0]
		for _, vo := range s.voices {
			if vo.t < vo.dur {
				kept = append(kept, vo)
			}
		}
		s.voices = kept
		s.mu.Unlock()
		if _, err := w.Write(buf); err != nil {
			s.mu.Lock()
			s.ok = false
			s.mu.Unlock()
			return
		}
	}
}

func (v *voice) next() float64 {
	const dt = 1.0 / sampleRate
	if v.delay > 0 {
		v.delay -= dt
		return 0
	}
	if v.t >= v.dur {
		return 0
	}
	k := v.t / v.dur
	f := v.f0 + (v.f1-v.f0)*k
	v.phase += f * dt
	v.phase -= math.Floor(v.phase)
	var x float64
	switch v.wave {
	case waveSine:
		x = math.Sin(2 * math.Pi * v.phase)
	case waveSquare:
		x = 0.6
		if v.phase > 0.5 {
			x = -0.6
		}
	case waveTri:
		x = 4*math.Abs(v.phase-0.5) - 1
	case waveNoise:
		x = rand.Float64()*2 - 1
	}
	env := min(1, v.t/0.003) * (1 - k) * (1 - k) // a quick attack, then a fade
	v.t += dt
	return x * env * v.vol
}

func (s *synth) add(vs ...voice) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ok || s.muted || len(s.voices) > 40 {
		return
	}
	s.voices = append(s.voices, vs...)
}

// notes is a run of short notes, step seconds apart.
func notes(wave int, step, dur, vol float64, fs ...float64) []voice {
	var out []voice
	for i, f := range fs {
		out = append(out, voice{delay: float64(i) * step, dur: dur, f0: f, f1: f, vol: vol, wave: wave})
	}
	return out
}

// play makes the table's sound.
func (s *synth) play(snd games.Sound) {
	tone := func(wave int, dur, f0, f1, vol float64) voice {
		return voice{dur: dur, f0: f0, f1: f1, vol: vol, wave: wave}
	}
	switch snd {
	case games.SndFlipper:
		s.add(tone(waveNoise, 0.025, 0, 0, 0.18), tone(waveSquare, 0.03, 110, 70, 0.2))
	case games.SndBumper:
		s.add(tone(waveSquare, 0.08, 520, 940, 0.3), tone(waveSine, 0.06, 1040, 1040, 0.25))
	case games.SndSling:
		s.add(tone(waveNoise, 0.04, 0, 0, 0.25), tone(waveTri, 0.07, 320, 200, 0.35))
	case games.SndTarget:
		s.add(notes(waveSine, 0.035, 0.07, 0.3, 1250, 1870)...)
	case games.SndDrop:
		s.add(tone(waveSquare, 0.1, 640, 300, 0.25), tone(waveNoise, 0.03, 0, 0, 0.15))
	case games.SndLane:
		s.add(notes(waveTri, 0.06, 0.08, 0.35, 660, 880)...)
	case games.SndRamp:
		s.add(tone(waveSine, 0.45, 280, 1250, 0.3), tone(waveNoise, 0.3, 0, 0, 0.06))
	case games.SndHole:
		s.add(tone(waveSine, 0.8, 420, 55, 0.45), tone(waveTri, 0.8, 210, 30, 0.2))
	case games.SndKick:
		s.add(tone(waveNoise, 0.06, 0, 0, 0.25), tone(waveSquare, 0.09, 200, 420, 0.22))
	case games.SndWormhole:
		s.add(tone(waveSine, 0.5, 200, 1600, 0.3), voice{delay: 0.45, dur: 0.5, f0: 1600, f1: 200, vol: 0.3})
	case games.SndLaunch:
		s.add(tone(waveNoise, 0.12, 0, 0, 0.22), tone(waveTri, 0.16, 140, 520, 0.35))
	case games.SndDrain:
		s.add(tone(waveSquare, 0.7, 420, 90, 0.25))
	case games.SndMission:
		s.add(notes(waveSquare, 0.1, 0.12, 0.22, 523, 659, 784)...)
	case games.SndComplete:
		s.add(notes(waveTri, 0.11, 0.18, 0.38, 523, 659, 784, 1047)...)
	case games.SndPromotion:
		s.add(notes(waveSquare, 0.14, 0.2, 0.22, 392, 523, 659, 784)...)
		s.add(voice{delay: 0.56, dur: 0.7, f0: 1047, f1: 1047, vol: 0.3, wave: waveTri})
	case games.SndFail:
		s.add(notes(waveSquare, 0.22, 0.3, 0.25, 330, 247, 165)...)
	case games.SndExtraBall:
		s.add(notes(waveTri, 0.08, 0.1, 0.35, 880, 1175, 1568, 880, 1175, 1568)...)
	case games.SndMultiball:
		s.add(notes(waveSquare, 0.13, 0.12, 0.22, 880, 660, 880, 660, 880, 660, 1320)...)
	case games.SndJackpot:
		s.add(notes(waveTri, 0.09, 0.2, 0.38, 1047, 1319, 1568, 2093)...)
	case games.SndSkill:
		s.add(tone(waveSine, 0.35, 1568, 2093, 0.35), voice{delay: 0.1, dur: 0.3, f0: 784, f1: 1047, vol: 0.25, wave: waveTri})
	case games.SndNudge:
		s.add(tone(waveNoise, 0.04, 0, 0, 0.2), tone(waveSine, 0.06, 90, 60, 0.3))
	case games.SndTilt:
		s.add(tone(waveSquare, 0.9, 110, 100, 0.3))
	case games.SndSave:
		s.add(notes(waveTri, 0.09, 0.14, 0.35, 784, 1047)...)
	}
}

func (s *synth) setMuted(m bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.muted = m
	if m {
		s.voices = nil
	}
}

// available reports whether sounds can be heard (pw-cat runs).
func (s *synth) available() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ok
}
