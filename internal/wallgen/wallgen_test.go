package wallgen

import (
	"bytes"
	"testing"
	"time"
)

var yoru = map[string]string{"app": "#0B1012", "field": "#44504E", "accent": "#9FC3C9", "primary": "#E7E4DA"}

func testPalette(t testing.TB) Palette {
	t.Helper()
	p, err := NewPalette(yoru)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// A recipe and a seed make the same picture every time, which is what lets
// it be drawn once and kept; another seed makes another; and every recipe
// draws something, not the bare ground.
func TestRecipesAreSteadyAndDrawSomething(t *testing.T) {
	p := testPalette(t)
	for _, r := range recipes {
		a, err := Draw(r.name, 160, 90, p, 7)
		if err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
		b, _ := Draw(r.name, 160, 90, p, 7)
		if !bytes.Equal(a.Pix, b.Pix) {
			t.Errorf("%s: the same seed drew two pictures", r.name)
		}
		c, _ := Draw(r.name, 160, 90, p, 8)
		if bytes.Equal(a.Pix, c.Pix) {
			t.Errorf("%s: another seed drew the same picture", r.name)
		}
		lo, hi := 255, 0
		for i := 0; i < len(a.Pix); i += 4 {
			lo, hi = min(lo, int(a.Pix[i+1])), max(hi, int(a.Pix[i+1]))
		}
		if hi-lo < 8 {
			t.Errorf("%s: the picture is flat (green %d to %d)", r.name, lo, hi)
		}
	}
}

// A day has one picture, whatever its hour; the next days bring each of the
// others before it comes again, and then in another variation.
func TestOfDay(t *testing.T) {
	day := time.Date(2026, 10, 23, 0, 30, 0, 0, time.Local)
	name, seed := OfDay(day)
	if n, s := OfDay(day.Add(23 * time.Hour)); n != name || s != seed {
		t.Errorf("the same day has %s/%d and %s/%d", name, seed, n, s)
	}
	seen := map[string]bool{}
	for i := range len(recipes) {
		n, _ := OfDay(day.AddDate(0, 0, i))
		if seen[n] {
			t.Errorf("day %d: %s again", i, n)
		}
		seen[n] = true
	}
	if n, s := OfDay(day.AddDate(0, 0, len(recipes))); n != name || s == seed {
		t.Errorf("a week on: %s/%d, want %s in another variation than %d", n, s, name, seed)
	}
}

// Another picture is never the one shown.
func TestAnother(t *testing.T) {
	for _, r := range recipes {
		for range 20 {
			if n, _ := Another(r.name); n == r.name {
				t.Fatalf("after %s, %s again", r.name, n)
			}
		}
	}
}

func TestDrawRefuses(t *testing.T) {
	p := testPalette(t)
	if _, err := Draw("nothing", 10, 10, p, 0); err == nil {
		t.Error("an unknown picture was drawn")
	}
	if _, err := Draw("dots", 0, 10, p, 0); err == nil {
		t.Error("a picture of no size was drawn")
	}
	if _, err := NewPalette(map[string]string{"app": "#000000"}); err == nil {
		t.Error("a palette without its field was made")
	}
}

func BenchmarkDraw(b *testing.B) {
	p := testPalette(b)
	for _, r := range recipes {
		b.Run(r.name, func(b *testing.B) {
			for b.Loop() {
				Draw(r.name, 3840, 2160, p, 1)
			}
		})
	}
}
