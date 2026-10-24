package shell

import (
	"sync"
	"time"
)

// Setter applies the latest requested value in the background. Values asked
// for while a call runs replace each other, so dragging a slider does not
// queue up a call per pixel.
type Setter struct {
	apply func(float64)

	mu      sync.Mutex
	v       float64
	at      time.Time // when v was asked for
	running bool
}

// holdTime is how long a requested value stands for the service's state,
// whose updates lag behind the request.
const holdTime = 600 * time.Millisecond

// NewSetter returns a Setter that calls apply on its own goroutine.
func NewSetter(apply func(float64)) *Setter { return &Setter{apply: apply} }

// Set requests v. It never blocks.
func (s *Setter) Set(v float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.v, s.at = v, time.Now()
	if s.running {
		return // the running loop picks v up
	}
	s.running = true
	go s.loop(v)
}

func (s *Setter) loop(v float64) {
	for {
		s.apply(v)
		s.mu.Lock()
		if s.v == v {
			s.running = false
			s.mu.Unlock()
			return
		}
		v = s.v
		s.mu.Unlock()
	}
}

// Held returns the value asked for last, and whether that was recent enough
// that the service's state may not show it yet: a slider is not reset from
// that state, and the next step builds on the value instead.
func (s *Setter) Held() (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.v, !s.at.IsZero() && time.Since(s.at) < holdTime
}
