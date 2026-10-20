package pass

import (
	"slices"
	"testing"
)

func TestUsername(t *testing.T) {
	for name, want := range map[string]string{
		"corp/google.com/abc@corp.com": "abc@corp.com",
		"github.com/ann":               "ann",
		"google.com":                   "",
	} {
		if got := Username(name); got != want {
			t.Errorf("Username(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestSearch(t *testing.T) {
	entries := []string{
		"corp/google.com/abc@corp.com",
		"corp/google.com/xyz@corp.com",
		"personal/google.com/ann@gmail.com",
		"personal/github.com/ann",
		"bank/abcbank",
		"wifi/home",
	}
	for _, c := range []struct {
		query string
		want  []string
	}{
		// Words in any order, each anywhere in the path.
		{"google abc", []string{"corp/google.com/abc@corp.com"}},
		{"abc google", []string{"corp/google.com/abc@corp.com"}},
		{"GOOGLE corp", []string{"corp/google.com/abc@corp.com", "corp/google.com/xyz@corp.com"}},
		// A match in the entry's own name first: abcbank before the folder.
		{"abc", []string{"corp/google.com/abc@corp.com", "bank/abcbank"}},
		// A word with its folder.
		{"corp/goo", []string{"corp/google.com/abc@corp.com", "corp/google.com/xyz@corp.com"}},
		// Letters in order within a segment.
		{"gthb", []string{"personal/github.com/ann"}},
		{"ann gmail", []string{"personal/google.com/ann@gmail.com"}},
		{"nothing", nil},
		{"  ", nil},
	} {
		if got := Search(entries, c.query); !slices.Equal(got, c.want) {
			t.Errorf("Search(%q) = %q, want %q", c.query, got, c.want)
		}
	}
	// The best match first: the whole word before its start.
	if got := Search([]string{"x/homebrew", "x/home"}, "home"); !slices.Equal(got, []string{"x/home", "x/homebrew"}) {
		t.Errorf("ranking: %q", got)
	}
}
