package hyprland

import (
	"testing"
	"time"
)

// The selected workspace's windows come first and three times as often;
// the others still get their turn, and a window gone from the targets is
// never captured again.
func TestCaptureSchedule(t *testing.T) {
	var s captureSchedule
	s.set(captureTargets{addresses: []string{"a", "b", "p", "a"}, priority: []string{"p"}})
	now := time.Unix(1000, 0)
	if got := s.next(now); got != "p" {
		t.Fatalf("first: %q, want the priority window", got)
	}
	counts := map[string]int{}
	for i := 1; i <= 1000/16*3; i++ { // three seconds of 16 ms ticks
		if a := s.next(now.Add(time.Duration(i) * 16 * time.Millisecond)); a != "" {
			counts[a]++
		}
	}
	if counts["p"] < 2*counts["a"] || counts["a"] == 0 || counts["b"] == 0 {
		t.Errorf("captures in 3s: %v", counts)
	}
	if counts["p"] > 3000/120+1 {
		t.Errorf("priority window captured %d times in 3s, more than every 120 ms", counts["p"])
	}

	s.set(captureTargets{addresses: []string{"b"}})
	for i := range 100 {
		if a := s.next(now.Add(time.Hour + time.Duration(i)*time.Second)); a != "b" && a != "" {
			t.Fatalf("captured %q, which is gone", a)
		}
	}
	if len(s.last) != 1 {
		t.Errorf("deadlines kept for %d windows", len(s.last))
	}
}
