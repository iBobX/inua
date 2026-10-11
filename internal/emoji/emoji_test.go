package emoji

import "testing"

func TestSearch(t *testing.T) {
	first := func(q string) string {
		if r := Search(q, 1); len(r) > 0 {
			return r[0].Char
		}
		return ""
	}
	for q, want := range map[string]string{
		"grinning face":  "😀", // the name
		"thumbs up":      "👍",
		"red heart":      "❤️",
		"right arrow":    "➡️", // an emoji's own name
		"right-pointing": "→",  // a symbol's
		"plus minus":     "±",
		"rocket":         "🚀",
		"heart":          "❤️", // everyday, by a keyword, before 💘 by its name
		"cheery":         "😀",  // a keyword
		"xyzzy":          "",
	} {
		if got := first(q); got != want {
			t.Errorf("Search(%q) = %q, want %q", q, got, want)
		}
	}
	if n := len(Search("heart", 40)); n < 10 {
		t.Errorf("heart: %d items", n)
	}
	if got := Search("", 5); len(got) != 5 || got[0].Char != "😀" {
		t.Errorf("empty query: %v", got)
	}
	for _, it := range Search("face", 3000) {
		if it.Char == "" || it.Name == "" {
			t.Fatalf("bad item %+v", it)
		}
	}
}
