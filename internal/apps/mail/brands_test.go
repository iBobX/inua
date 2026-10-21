package mail

import (
	"strings"
	"testing"
)

func TestBrandPlate(t *testing.T) {
	for _, c := range []struct {
		icon        string
		dark        bool
		plate, mark string
	}{
		{"github-logo", false, "#181717", "#FFFFFF"}, // black, the mark white
		{"github-logo", true, "#FFFFFF", "#181717"},  // on a dark theme, white with the mark black
		{"spotify-logo", false, "#1ED760", "#000000"},
		{"spotify-logo", true, "#1ED760", "#000000"}, // a colour reads on both
		{"youtube-logo", true, "#FF0000", "#FFFFFF"},
		{"slack-logo", true, "#FFFFFF", "#4A154B"},
		{"amazon-logo", false, "#FF9900", "#000000"},   // white under 3:1
		{"whatsapp-logo", false, "#25D366", "#FFFFFF"}, // its own white
	} {
		plate, mark, ok := BrandPlate(c.icon, c.dark)
		if !ok || plate != c.plate || mark != c.mark {
			t.Errorf("%s dark=%v: %s on %s, want %s on %s", c.icon, c.dark, mark, plate, c.mark, c.plate)
		}
	}
	if _, _, ok := BrandPlate("envelope", false); ok {
		t.Error("a plate for no brand")
	}
	if css := BrandCSS(true); !strings.Contains(css, ".brand.brand-github-logo { background: #FFFFFF; color: #181717; }") {
		t.Errorf("dark CSS:\n%s", css)
	}
	if !Dark("#0B1012") || Dark("#F2EDE2") { // yoru, washi
		t.Error("dark or light, wrong")
	}
}
