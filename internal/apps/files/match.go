package files

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Score says how well a folded query matches a folded name: 0 is no
// match; higher is better. Every character of the query must appear in
// order (fuzzy, as fzf); a match at the start, after a separator, in one
// run, or of the whole name scores more, and a shorter name wins a tie.
func Score(query, name string) int {
	if query == "" {
		return 1
	}
	if query == name {
		return 1 << 20
	}
	if strings.HasPrefix(name, query) {
		return 1<<19 - len(name)
	}
	if i := strings.Index(name, query); i >= 0 {
		s := 1<<18 - len(name) - i
		if boundary(name, i) {
			s += 1 << 17
		}
		return s
	}
	// Fuzzy: walk the name once, taking each query character as soon as it
	// comes.
	score, qi, run, last := 0, 0, 0, -2
	for ni := 0; ni < len(name) && qi < len(query); {
		qr, qn := utf8.DecodeRuneInString(query[qi:])
		nr, nn := utf8.DecodeRuneInString(name[ni:])
		if qr == nr {
			score += 8
			if boundary(name, ni) {
				score += 16
			}
			if last == ni-1 {
				run++
				score += 4 * run
			} else {
				run = 0
			}
			last = ni + nn - 1
			qi += qn
		}
		ni += nn
	}
	if qi < len(query) {
		return 0
	}
	return max(1, score*64-len(name))
}

// boundary says whether name[i] starts a word: the name's start, or after
// a space, dot, dash or underscore.
func boundary(name string, i int) bool {
	if i == 0 {
		return true
	}
	switch name[i-1] {
	case ' ', '.', '-', '_', '/':
		return true
	}
	return false
}

// Filter returns the indexes of the entries whose names match query, in
// their order, and the place in that list of the best match (-1 when
// none match). The order stays the folder's, so rows do not jump about
// while one types; the best match is where the cursor goes.
func Filter(es []Entry, query string) (idx []int, best int) {
	q := fold(strings.TrimSpace(query))
	idx = make([]int, 0, len(es))
	best, top := -1, 0
	for i := range es {
		if s := Score(q, es[i].key); s > 0 {
			if s > top {
				best, top = len(idx), s
			}
			idx = append(idx, i)
		}
	}
	return idx, best
}

// Rank orders candidates by how well they match query, best first,
// keeping the order of equals; those that do not match are left out.
func Rank[T any](items []T, query string, key func(T) string) []T {
	q := fold(strings.TrimSpace(query))
	type scored struct {
		item  T
		score int
	}
	var ss []scored
	for _, it := range items {
		if s := Score(q, fold(key(it))); s > 0 {
			ss = append(ss, scored{it, s})
		}
	}
	slices.SortStableFunc(ss, func(a, b scored) int { return cmp.Compare(b.score, a.score) })
	out := make([]T, len(ss))
	for i, s := range ss {
		out[i] = s.item
	}
	return out
}
