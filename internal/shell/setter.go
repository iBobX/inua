package shell

import "sync"

// Setter applies the latest requested value in the background. Values asked
// for while a call runs replace each other, so dragging a slider does not
// queue up a call per pixel.
type Setter struct {
	apply func(float64)

	mu      sync.Mutex
	v       float64
	running bool
}

// NewSetter returns a Setter that calls apply on its own goroutine.
func NewSetter(apply func(float64)) *Setter { return &Setter{apply: apply} }

// Set requests v. It never blocks.
func (s *Setter) Set(v float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.v = v
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
