package termui

import (
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/width"
)

// A cluster is what one cell (two for a wide character) shows: its text,
// its column from the start of the run and its width in cells.
type cluster struct {
	text  string
	x, w  int
	ascii bool
}

// cellClusters splits a run's text into its cells' characters, as the
// engine wrote them: a character with the marks, selectors and joined
// parts that follow it. Drawn whole, text in a font other than the
// terminal's (Japanese, say) has its own advance and drifts from the
// cells the engine counts; drawn a cluster at a time it keeps to them.
// ok is false when the widths found do not add up to cells.
func cellClusters(text string, cells int) (cs []cluster, ok bool) {
	joined := false // the last rune was a zero-width joiner
	for i, r := range text {
		size := utf8.RuneLen(r)
		if n := len(cs); n > 0 && (joined || extends(r) || flagPair(cs[n-1].text, r)) {
			c := &cs[n-1]
			c.text = text[i-len(c.text) : i+size]
			c.ascii = false
			if (r == 0xfe0f || regional(r)) && c.w == 1 {
				c.w = 2 // shown as an emoji
			}
			joined = r == 0x200d
			continue
		}
		joined = false
		w := 1
		if k := width.LookupRune(r).Kind(); k == width.EastAsianWide || k == width.EastAsianFullwidth {
			w = 2
		}
		x := 0
		if n := len(cs); n > 0 {
			x = cs[n-1].x + cs[n-1].w
		}
		cs = append(cs, cluster{text: text[i : i+size], x: x, w: w, ascii: r < utf8.RuneSelf})
	}
	n := 0
	if len(cs) > 0 {
		n = cs[len(cs)-1].x + cs[len(cs)-1].w
	}
	return cs, n == cells
}

// extends says a rune belongs to the character before it: a combining
// mark, a variation selector, a joiner, a skin tone, a tag.
func extends(r rune) bool {
	switch {
	case unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc):
		return true
	case r == 0x200d, r >= 0xfe00 && r <= 0xfe0f, r >= 0xe0100 && r <= 0xe01ef:
		return true
	case r >= 0x1f3fb && r <= 0x1f3ff, r >= 0xe0020 && r <= 0xe007f:
		return true
	}
	return false
}

// regional says a rune is a regional indicator, two of which are a flag.
func regional(r rune) bool { return r >= 0x1f1e6 && r <= 0x1f1ff }

// flagPair says r ends a flag that prev, one regional indicator, began.
func flagPair(prev string, r rune) bool {
	p, size := utf8.DecodeRuneInString(prev)
	return regional(r) && regional(p) && size == len(prev)
}

// isASCII says text is all ASCII, drawn in the terminal's own font a cell
// a character.
func isASCII(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// preeditCells is how many cells the input method's text takes, and the
// cell its cursor, at characters in, is at.
func preeditCells(cs []cluster, at int) (cells, cursor int) {
	cursor = -1
	for _, c := range cs {
		if at <= 0 && cursor < 0 {
			cursor = c.x
		}
		at -= utf8.RuneCountInString(c.text)
		cells = c.x + c.w
	}
	if cursor < 0 {
		cursor = cells
	}
	return cells, cursor
}
