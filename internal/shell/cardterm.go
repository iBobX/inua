package shell

import (
	"cmp"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/system"
)

// The card for a terminal (`inua fetch`), laid out as neofetch lays out
// its: inua's mark on the left, the card's facts on the right, a label and
// a value a line, the theme's colours under them.

// cardLogo is inua's mark (website/favicon.svg), the stroke cut out of
// its rounded square, two pixels a cell.
var cardLogo = []string{
	"  ▄▄████████████████████▄▄  ",
	" ██████████████████████████ ",
	"███████████████████▀▀▀██████",
	"███████████████████   ██████",
	"███████▀      ▀████   ██████",
	"██████    ▄▄   ▀███   ██████",
	"██████   ███    ███   ██████",
	"██████   ███    ███   ██████",
	"██████   ███▄         ██████",
	"██████   ████▄▄     ▄███████",
	"██████   ███████████████████",
	"██████▄▄████████████████████",
	" ██████████████████████████ ",
	"  ▀▀████████████████████▀▀  ",
}

// cardValueWidth is the most columns a value takes; a longer one is cut.
const cardValueWidth = 48

// cardBar is how many columns a gauge's bar takes.
const cardBar = 12

// CardInk is how text is coloured: the escape that starts a theme token's
// colour (as the background with bg), "" for none.
type CardInk func(token string, bg bool) string

// ANSIInk colours text in the theme's colours (24-bit), its tokens
// "#rrggbb".
func ANSIInk(colors map[string]string) CardInk {
	return func(token string, bg bool) string {
		hex := strings.TrimPrefix(colors[token], "#")
		v, err := strconv.ParseUint(hex, 16, 32)
		if len(hex) != 6 || err != nil {
			return ""
		}
		layer := 38
		if bg {
			layer = 48
		}
		return fmt.Sprintf("\x1b[%d;2;%d;%d;%dm", layer, v>>16, v>>8&0xff, v&0xff)
	}
}

// Terminal is the card as text, coloured by ink; nil leaves it plain.
// The mark, the labels and the stamp take the theme's accent, the values
// the terminal's own colour, so it reads on whatever ground the terminal
// has. With ground, it is a block on the theme's panel colour instead, all
// in the theme's colours, as the card is on the page.
func (c Card) Terminal(ink CardInk, ground bool) string {
	groundEsc := ""
	if ink != nil && ground {
		groundEsc = ink("panel", true)
	}
	// paint colours s as token; off the theme's ground, its text is the
	// terminal's own colour, dimmed for the quieter tokens.
	paint := func(token, s string) string {
		if ink == nil || s == "" {
			return s
		}
		if groundEsc == "" {
			switch token {
			case "t0", "t1":
				return s
			case "t2", "line", "surface2":
				return "\x1b[2m" + s + "\x1b[22m"
			}
		}
		if e := ink(token, false); e != "" {
			return e + s + "\x1b[39m"
		}
		return s
	}
	bold := func(s string) string {
		if ink == nil {
			return s
		}
		return "\x1b[1m" + s + "\x1b[22m"
	}

	// The facts' side: a line each, coloured, and how many columns.
	type line struct {
		text string
		cols int
	}
	var info []line
	add := func(text string, cols int) { info = append(info, line{text, cols}) }
	stampToken := "accent"
	if !c.Status.Nominal {
		stampToken = "warn"
	}
	title, stamp := i18n.T("INUA // SYSTEM MANIFEST"), "[ "+c.Status.Stamp+" ]"
	add(bold(paint("accent", title))+"  "+paint(stampToken, stamp), termWidth(title)+2+termWidth(stamp))
	sub := "NO. " + c.Serial + " · " + c.Time
	add(paint("t2", sub), termWidth(sub))
	rule := max(info[0].cols, info[1].cols)
	add(paint("line", strings.Repeat("─", rule)), rule)

	type fact struct{ label, value, bar string }
	var facts []fact
	put := func(label, value string) {
		if value != "" {
			facts = append(facts, fact{label: label, value: value})
		}
	}
	model := c.Model
	if c.Arch != "" {
		model += " · " + c.Arch
	}
	put(i18n.T("Model"), model)
	cpu := c.CPU
	if c.Threads > 0 && cpu != "" { // the name cut short, not the threads
		threads := " · " + i18n.Tf("%d thr", c.Threads)
		cpu = termCut(cpu, cardValueWidth-termWidth(threads)) + threads
	}
	put("CPU", cpu)
	put("GPU", c.GPU)
	gauge := func(label string, u system.Usage) {
		if u.Total == 0 {
			return
		}
		used := min(cardBar, int(float64(cardBar)*float64(u.Used)/float64(u.Total)))
		facts = append(facts, fact{label: label, value: CardGiB(u),
			bar: paint("accent", strings.Repeat("━", used)) + paint("surface2", strings.Repeat("─", cardBar-used))})
	}
	gauge(i18n.T("Memory"), c.Memory)
	gauge(i18n.T("Disk"), c.Disk)
	for _, l := range c.Stack {
		v := strings.TrimSpace(l.Name + " " + l.Version)
		if l.Note != "" {
			v += " · " + l.Note
		}
		label := map[string]string{"kernel": i18n.T("Kernel"), "os": "OS", "wm": "WM", "inua": i18n.T("Desktop")}[l.Kind]
		put(cmp.Or(label, l.Name), v)
	}
	for _, sc := range c.Screens {
		put(i18n.T("Screen"), sc.String())
	}
	put(i18n.T("Theme"), c.Theme)
	put(i18n.T("Uptime"), c.Uptime)
	labels, values := 0, 0
	for _, f := range facts {
		labels = max(labels, termWidth(f.label))
		if f.bar != "" {
			values = max(values, termWidth(f.value))
		}
	}
	for _, f := range facts {
		value := termCut(f.value, cardValueWidth)
		text := bold(paint("accent", f.label)) + strings.Repeat(" ", labels-termWidth(f.label)+2) + paint("t0", value)
		cols := labels + 2 + termWidth(value)
		if f.bar != "" { // the gauges' bars in a column
			text += strings.Repeat(" ", values-termWidth(value)+2) + f.bar
			cols += values - termWidth(value) + 2 + cardBar
		}
		add(text, cols)
	}
	if !c.Status.Nominal {
		add(paint("warn", c.Status.Headline), termWidth(c.Status.Headline))
	}
	if ink != nil {
		var sw strings.Builder
		n := 0
		for _, token := range CardColors {
			if e := ink(token, true); e != "" {
				sw.WriteString(e + "   " + cmp.Or(groundEsc, "\x1b[49m"))
				n += 3
			}
		}
		if n > 0 {
			add("", 0)
			add(sw.String(), n)
		}
	}

	// The mark beside them, from the top, as neofetch has its logo.
	logoW := termWidth(cardLogo[0])
	var b strings.Builder
	for i := range max(len(cardLogo), len(info)) {
		mark := strings.Repeat(" ", logoW)
		if i < len(cardLogo) {
			mark = paint("accent", cardLogo[i])
		}
		if i >= len(info) {
			b.WriteString(strings.TrimRight(mark, " ") + "\n")
			continue
		}
		b.WriteString(mark + "   " + info[i].text + "\n")
	}
	if groundEsc == "" {
		return b.String()
	}
	// On the ground: every line as wide as the widest, two columns and a
	// line of the ground round them.
	lines := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	width := 0
	for _, l := range lines {
		width = max(width, termWidth(ansiEscape.ReplaceAllString(l, "")))
	}
	var out strings.Builder
	blank := groundEsc + strings.Repeat(" ", width+4) + "\x1b[0m\n"
	out.WriteString(blank)
	for _, l := range lines {
		pad := width - termWidth(ansiEscape.ReplaceAllString(l, ""))
		out.WriteString(groundEsc + "  " + l + strings.Repeat(" ", pad) + "  \x1b[0m\n")
	}
	out.WriteString(blank)
	return out.String()
}

// ansiEscape is a colour escape, left out when text is measured.
var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

// termWidth is how many columns s takes in a terminal: two for each wide
// character (CJK, fullwidth forms), none for a combining mark.
func termWidth(s string) int {
	n := 0
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || r == 0x200d:
		case wideRune(r):
			n += 2
		default:
			n++
		}
	}
	return n
}

// wideRune reports whether r takes two columns (East Asian Wide and
// Fullwidth, the common blocks).
func wideRune(r rune) bool {
	return r >= 0x1100 && (r <= 0x115f || // Hangul Jamo
		(r >= 0x2e80 && r <= 0x303e) || // CJK radicals, punctuation
		(r >= 0x3041 && r <= 0x33ff) || // kana, CJK symbols
		(r >= 0x3400 && r <= 0x4dbf) || (r >= 0x4e00 && r <= 0x9fff) || // CJK ideographs
		(r >= 0xa000 && r <= 0xa4cf) || (r >= 0xac00 && r <= 0xd7a3) || // Yi, Hangul
		(r >= 0xf900 && r <= 0xfaff) || (r >= 0xfe30 && r <= 0xfe4f) || // compatibility
		(r >= 0xff00 && r <= 0xff60) || (r >= 0xffe0 && r <= 0xffe6) || // fullwidth forms
		(r >= 0x1f300 && r <= 0x1f64f) || (r >= 0x1f900 && r <= 0x1f9ff) || // emoji
		(r >= 0x20000 && r <= 0x3fffd))
}

// termCut shortens s to at most cols columns, ending it with "…" when cut.
func termCut(s string, cols int) string {
	if termWidth(s) <= cols {
		return s
	}
	if cols <= 0 {
		return ""
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		rw := termWidth(string(r))
		if n+rw > cols-1 {
			break
		}
		b.WriteRune(r)
		n += rw
	}
	return b.String() + "…"
}
