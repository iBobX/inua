package mail

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// brandColors are the services' own colours, by the Phosphor logo Face
// gives (Pass's brands): from Simple Icons (CC0), and from the brand's own
// guidelines for those it no longer lists (LinkedIn, Microsoft's).
var brandColors = map[string]string{
	"amazon-logo":            "#FF9900",
	"android-logo":           "#3DDC84",
	"apple-logo":             "#000000",
	"discord-logo":           "#5865F2",
	"dropbox-logo":           "#0061FF",
	"facebook-logo":          "#0866FF",
	"figma-logo":             "#F24E1E",
	"github-logo":            "#181717",
	"gitlab-logo":            "#FC6D26",
	"google-logo":            "#4285F4",
	"instagram-logo":         "#FF0069",
	"linkedin-logo":          "#0A66C2",
	"mastodon-logo":          "#6364FF",
	"microsoft-outlook-logo": "#0078D4",
	"microsoft-teams-logo":   "#6264A7",
	"notion-logo":            "#000000",
	"paypal-logo":            "#003087",
	"pinterest-logo":         "#BD081C",
	"reddit-logo":            "#FF4500",
	"slack-logo":             "#4A154B",
	"spotify-logo":           "#1ED760",
	"steam-logo":             "#000000",
	"stripe-logo":            "#635BFF",
	"telegram-logo":          "#26A5E4",
	"tiktok-logo":            "#000000",
	"twitch-logo":            "#9146FF",
	"whatsapp-logo":          "#25D366",
	"windows-logo":           "#0078D4",
	"x-logo":                 "#000000",
	"youtube-logo":           "#FF0000",
}

// whiteMarks are the brands whose own logo is white on their colour,
// though white reads less there than black would.
var whiteMarks = map[string]bool{"whatsapp-logo": true, "telegram-logo": true}

// BrandPlate is how a service's logo is drawn: the round plate in its
// colour and the mark in white, as most brands draw theirs, unless white
// is under 3:1 on it (WCAG's least for a graphic): black then. On a dark
// theme a plate near black would sink into the ground: it is white then,
// with the mark in the brand's colour (GitHub black on white).
func BrandPlate(icon string, dark bool) (plate, mark string, ok bool) {
	c, ok := brandColors[icon]
	if !ok {
		return "", "", false
	}
	if dark && luminance(c) < 0.06 {
		return "#FFFFFF", c, true
	}
	if whiteMarks[icon] || contrast(c, "#FFFFFF") >= 3 {
		return c, "#FFFFFF", true
	}
	return c, "#000000", true
}

// BrandCSS styles the faces of the services with a colour, for a light or
// a dark theme. The colours are the brands', not the theme's: the one
// place a mark leaves the tokens (design/ui.md).
func BrandCSS(dark bool) string {
	icons := make([]string, 0, len(brandColors))
	for icon := range brandColors {
		icons = append(icons, icon)
	}
	slices.Sort(icons)
	var b strings.Builder
	for _, icon := range icons {
		plate, mark, _ := BrandPlate(icon, dark)
		fmt.Fprintf(&b, "window.mail .mail-sender-mark.brand.brand-%s { background: %s; color: %s; }\n", icon, plate, mark)
	}
	return b.String()
}

// Dark says whether a theme is dark, from its ground (@app).
func Dark(ground string) bool { return luminance(ground) < 0.4 }

// luminance is a colour's relative luminance (WCAG), 0 for black to 1 for
// white; "#rrggbb", else 1.
func luminance(hex string) float64 {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 1
	}
	var l float64
	for i, w := range []float64{0.2126, 0.7152, 0.0722} {
		v, err := strconv.ParseUint(hex[2*i:2*i+2], 16, 8)
		if err != nil {
			return 1
		}
		c := float64(v) / 255
		if c <= 0.03928 {
			c /= 12.92
		} else {
			c = math.Pow((c+0.055)/1.055, 2.4)
		}
		l += w * c
	}
	return l
}

// contrast is two colours' contrast ratio (WCAG), 1 to 21.
func contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}
