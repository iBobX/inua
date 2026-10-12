package calc

import (
	"fmt"
	"testing"
)

func TestRemember(t *testing.T) {
	var h []Entry
	h = Remember(h, Entry{"1+1", "2"})
	h = Remember(h, Entry{"2×3", "6"})
	h = Remember(h, Entry{"1+1", "2"}) // again: to the top, once
	if len(h) != 2 || h[0].Expr != "1+1" || h[1].Expr != "2×3" {
		t.Fatalf("%v", h)
	}
	for i := range HistoryLen + 5 {
		h = Remember(h, Entry{fmt.Sprint(i), fmt.Sprint(i)})
	}
	if len(h) != HistoryLen || h[0].Expr != fmt.Sprint(HistoryLen+4) {
		t.Fatalf("kept %d, newest %q", len(h), h[0].Expr)
	}
}

func TestRecall(t *testing.T) {
	for _, c := range []struct {
		text     string
		answered bool
		want     string
	}{
		{"", false, "42"},
		{"7+1", true, "42"}, // an answer shown: start again from the result
		{"7+", false, "7+42"},
		{"2×(", false, "2×(42"},
		{"3", false, "3×42"},
	} {
		if got := Recall(c.text, "42", c.answered); got != c.want {
			t.Errorf("Recall(%q, %v) = %q, want %q", c.text, c.answered, got, c.want)
		}
	}
}
