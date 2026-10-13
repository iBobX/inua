package hyprland

import "time"

type captureTargets struct {
	addresses, priority []string
}

type captureSchedule struct {
	targets  []string
	priority map[string]bool
	last     map[string]time.Time
}

func (s *captureSchedule) set(t captureTargets) {
	s.targets = unique(t.addresses)
	s.priority = make(map[string]bool, len(t.priority))
	for _, address := range t.priority {
		s.priority[address] = true
	}
	// Retain deadlines across snapshots, but release disappeared windows.
	last := make(map[string]time.Time, len(s.targets))
	for _, address := range s.targets {
		last[address] = s.last[address]
	}
	s.last = last
}

// next chooses the most overdue window. Priority windows have a shorter
// interval; normalized lateness lets background windows catch up under load.
// Mark requests when dispatched so a failed client cannot monopolize capture.
func (s *captureSchedule) next(now time.Time) string {
	chosen := ""
	best := -1.0
	for _, address := range s.targets {
		interval := 120 * time.Millisecond
		if len(s.priority) > 0 && !s.priority[address] {
			interval = 360 * time.Millisecond
		}
		last := s.last[address]
		score := float64(now.Sub(last)) / float64(interval)
		if last.IsZero() {
			score = 1
			if s.priority[address] {
				score *= 2
			}
		}
		if score >= 1 && score > best {
			chosen, best = address, score
		}
	}
	if chosen != "" {
		s.last[chosen] = now
	}
	return chosen
}
