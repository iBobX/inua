package shell

import (
	"testing"
	"time"
)

func TestSetterHeld(t *testing.T) {
	s := NewSetter(func(float64) {})
	if _, held := s.Held(); held {
		t.Fatal("a setter never set holds a value")
	}
	s.Set(0.4)
	if v, held := s.Held(); !held || v != 0.4 {
		t.Fatalf("Held() = %v, %v right after Set(0.4)", v, held)
	}
	s.mu.Lock()
	s.at = time.Now().Add(-holdTime)
	s.mu.Unlock()
	if _, held := s.Held(); held {
		t.Fatal("the value is still held after holdTime")
	}
}
