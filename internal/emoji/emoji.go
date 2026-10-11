// Package emoji finds emoji and symbols by name and keyword, for the
// launcher (":heart"). The table is Unicode's: emoji-test.txt and CLDR's
// English annotations, rebuilt by scripts/emoji.py (LICENSE-UNICODE).
package emoji

import (
	_ "embed"
	"slices"
	"strings"
	"sync"
)

//go:embed emoji.tsv
var table string

// Item is one emoji or symbol.
type Item struct {
	Char     string
	Name     string // "grinning face", "right-pointing arrow"
	Keywords []string
	Symbol   bool // a symbol (→, ±), not an emoji
}

var (
	once  sync.Once
	items []Item
)

func load() {
	for line := range strings.SplitSeq(table, "\n") {
		if line == "" || line[0] == '#' {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 3 {
			continue
		}
		it := Item{Char: f[1], Name: f[2], Symbol: f[0] == "s"}
		if len(f) > 3 && f[3] != "" {
			it.Keywords = strings.Split(f[3], ", ")
		}
		items = append(items, it)
	}
}

// Search finds the items matching every word of q, best first, at most
// limit: the name itself, then names starting with q, then names whose
// words start with q's, then keywords. Ties keep Unicode's order (smileys
// first, then the symbols). An empty q lists the first items.
func Search(q string, limit int) []Item {
	once.Do(load)
	words := strings.Fields(strings.ToLower(q))
	type hit struct {
		i, score int
	}
	var hits []hit
	for i, it := range items {
		if s := score(it, strings.ToLower(it.Name), words); s > 0 {
			hits = append(hits, hit{i, s})
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int { return b.score - a.score })
	out := make([]Item, 0, min(limit, len(hits)))
	for _, h := range hits[:min(limit, len(hits))] {
		out = append(out, items[h.i])
	}
	return out
}

// everyday are the emoji most reached for; they come first among equals,
// so "heart" finds ❤️ before 💘.
var everyday = map[string]bool{}

func init() {
	for _, c := range strings.Fields("❤️ 👍 👎 😂 🤣 😊 😀 😍 🥰 😘 😭 😅 😉 🙂 🤔 🙄 😎 😢 😡 🙏 👏 🙌 👋 👀 💪 🔥 ✨ ⭐ 🎉 🎂 🎁 ✅ ❌ ⚠️ 💯 🚀 💡 📌 ☕ 🍺 🌙 ☀️ 🌧️ ❄️ 🐶 🐱 🎵 💻 📱 ✓ ✔️ → ←") {
		everyday[c] = true
	}
}

func score(it Item, name string, words []string) int {
	s := baseScore(it, name, words)
	if s > 0 && everyday[it.Char] {
		s += 10
	}
	return s
}

func baseScore(it Item, name string, words []string) int {
	if len(words) == 0 {
		return 1
	}
	q := strings.Join(words, " ")
	switch {
	case name == q:
		return 100
	case strings.HasPrefix(name, q):
		return 80
	}
	nameWords := strings.FieldsFunc(name, func(r rune) bool { return r == ' ' || r == '-' || r == ':' || r == ',' })
	inName, inKeywords := 0, 0
	for _, w := range words {
		switch {
		case slices.ContainsFunc(nameWords, func(n string) bool { return strings.HasPrefix(n, w) }):
			inName++
		case slices.ContainsFunc(it.Keywords, func(k string) bool { return strings.HasPrefix(strings.ToLower(k), w) }):
			inKeywords++
		default:
			return 0 // every word must match
		}
	}
	if inKeywords == 0 {
		if len(words) == 1 && slices.Contains(nameWords, q) {
			// A whole word of its name, more so of a short one: "heart" is
			// "red heart" more than "smiling face with heart-eyes".
			return 76 + max(0, 6-len(nameWords))
		}
		return 60
	}
	if len(words) == 1 && slices.ContainsFunc(it.Keywords, func(k string) bool { return strings.EqualFold(k, q) }) {
		return 75 // a keyword exactly: "heart" for ❤️
	}
	return 40 - inKeywords
}
