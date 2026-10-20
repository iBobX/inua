package pass

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
)

// Username is what an entry's name says its user is: the last segment of
// its path ("abc@corp.com" of "corp/google.com/abc@corp.com"), as pass
// users name their entries site/user; "" for an entry at the top level,
// whose name is the site's.
func Username(name string) string {
	i := strings.LastIndexByte(name, '/')
	if i < 0 {
		return ""
	}
	return name[i+1:]
}

// Search finds the entries a query names, best first. Each word of the
// query, in any order, has to be in the entry's path: as a whole part of
// it ("google" of "google.com"), the start of one, anywhere in it, or,
// for a word of three letters or more, its letters in order within one
// segment ("ggl"). Matches in the entry's own name, at the start of a
// part, and shorter paths come first. An empty query finds nothing.
func Search(entries []string, query string) []string {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil
	}
	type hit struct {
		name  string
		score int
	}
	var hits []hit
	for _, e := range entries {
		total := 0
		for _, w := range words {
			s := matchWord(strings.ToLower(e), w)
			if s == 0 {
				total = -1
				break
			}
			total += s
		}
		if total > 0 {
			hits = append(hits, hit{e, total})
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int {
		return cmp.Or(
			cmp.Compare(b.score, a.score),
			cmp.Compare(strings.Count(a.name, "/"), strings.Count(b.name, "/")),
			cmp.Compare(len(a.name), len(b.name)),
			strings.Compare(a.name, b.name),
		)
	})
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.name
	}
	return out
}

// matchWord scores how well a word of the query matches an entry's
// lower-cased path; 0 for not at all.
func matchWord(path, w string) int {
	segs := strings.Split(path, "/")
	best := 0
	for i, seg := range segs {
		s := 0
		switch {
		case slices.Contains(tokens(seg), w):
			s = 100
		case slices.ContainsFunc(tokens(seg), func(t string) bool { return strings.HasPrefix(t, w) }):
			s = 80
		case strings.HasPrefix(seg, w):
			s = 70
		case strings.Contains(seg, w):
			s = 40
		case len([]rune(w)) >= 3 && subsequence(seg, w):
			s = 10
		}
		if s > 0 && i == len(segs)-1 {
			s += 15 // the entry's own name
		}
		best = max(best, s)
	}
	if best == 0 && strings.Contains(w, "/") && strings.Contains(path, w) {
		best = 40 // a word with its folder ("corp/goo")
	}
	return best
}

// tokens are a segment's words: "abc@corp.com" is abc, corp, com.
func tokens(seg string) []string {
	return strings.FieldsFunc(seg, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
}

// subsequence reports whether w's letters are in s in order.
func subsequence(s, w string) bool {
	r := []rune(w)
	for _, c := range s {
		if len(r) > 0 && c == r[0] {
			r = r[1:]
		}
	}
	return len(r) == 0
}
