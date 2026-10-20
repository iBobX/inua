package mail

import "testing"

func TestFace(t *testing.T) {
	for _, c := range []struct{ addr, icon string }{
		{"notifications@github.com", "github-logo"},
		{"no-reply@accounts.google.com", "google-logo"},
		{"info@x.com", "x-logo"},
		{"ann@gmail.com", ""},
		{"bob@outlook.com", ""},
		{"carol@example.org", ""},
		{"nobody", ""},
	} {
		icon, tone := Face(Address{Addr: c.addr})
		if icon != c.icon {
			t.Errorf("Face(%q) icon = %q, want %q", c.addr, icon, c.icon)
		}
		if tone < 0 || tone >= Tones {
			t.Errorf("Face(%q) tone = %d", c.addr, tone)
		}
	}
	_, a := Face(Address{Addr: "Ann@Example.org"})
	_, b := Face(Address{Name: "Ann", Addr: "ann@example.org "})
	if a != b {
		t.Errorf("tone changes with case or name: %d, %d", a, b)
	}
}
