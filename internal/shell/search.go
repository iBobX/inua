package shell

import (
	"strings"
	"unicode"
)

// Match tiers, best first. A result's score is the best tier any of its
// fields reaches; the primary field (app name, window title) counts double
// so "fire" ranks Firefox above an app that merely mentions fire.
const (
	noMatch     = 0
	subsequence = 1 // "ffx" in "firefox"
	contains    = 2
	wordStart   = 3 // "code" in "Visual Studio Code"
	prefix      = 4
	exact       = 5
)

// match rates how well query q (lower case, trimmed) matches text.
func match(text, q string) int {
	t := strings.ToLower(text)
	switch {
	case q == "" || t == "":
		return noMatch
	case t == q:
		return exact
	case strings.HasPrefix(t, q):
		return prefix
	}
	if i := strings.Index(t, q); i >= 0 {
		for ; i >= 0; i = nextIndex(t, q, i) {
			if isWordStart(t, i) {
				return wordStart
			}
		}
		return contains
	}
	if isSubsequence(t, q) {
		return subsequence
	}
	return noMatch
}

func nextIndex(t, q string, after int) int {
	j := strings.Index(t[after+1:], q)
	if j < 0 {
		return -1
	}
	return after + 1 + j
}

func isWordStart(t string, i int) bool {
	if i == 0 {
		return true
	}
	prev := rune(t[i-1])
	return prev == ' ' || prev == '-' || prev == '_' || prev == '.' || prev == '/' || prev == '(' || prev == ':'
}

// isSubsequence reports whether q's runes appear in t in order. Spaces in q
// are skipped, so "vs code" still matches "Visual Studio Code".
func isSubsequence(t, q string) bool {
	rest := []rune(t)
	for _, r := range q {
		if unicode.IsSpace(r) {
			continue
		}
		i := indexRune(rest, r)
		if i < 0 {
			return false
		}
		rest = rest[i+1:]
	}
	return true
}

func indexRune(rs []rune, r rune) int {
	for i, x := range rs {
		if x == r {
			return i
		}
	}
	return -1
}

// rank scores a result from its primary field and secondary fields.
// Subsequence matches only count on the primary field: on long comments and
// keyword lists they match almost anything.
func rank(q, primary string, secondary ...string) int {
	best := 2 * match(primary, q)
	for _, s := range secondary {
		if m := match(s, q); m > subsequence {
			best = max(best, m)
		}
	}
	return best
}
