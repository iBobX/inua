package termui

import (
	"slices"
	"testing"
)

func TestLines(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
		one  string
	}{
		{"ls", []string{"ls"}, "ls"},
		{"a\nb\n", []string{"a", "b"}, "a b"},
		{"a\r\nb\rc", []string{"a", "b", "c"}, "a b c"},
		{"cd x\n\nmake\n", []string{"cd x", "", "make"}, "cd x  make"},
	} {
		if got := lines(c.in); !slices.Equal(got, c.want) {
			t.Errorf("lines(%q) = %q, want %q", c.in, got, c.want)
		}
		if got := oneLine(c.in); got != c.one {
			t.Errorf("oneLine(%q) = %q, want %q", c.in, got, c.one)
		}
	}
}

func TestQuotePaths(t *testing.T) {
	for _, c := range []struct {
		in   []string
		want string
	}{
		{[]string{"/home/me/notes.txt"}, "/home/me/notes.txt"},
		{[]string{"/a b/c.png", "/x"}, "'/a b/c.png' /x"},
		{[]string{"/it's"}, `'/it'\''s'`},
		{[]string{"/$HOME;rm"}, "'/$HOME;rm'"},
		{[]string{"/写真/猫.jpg"}, "'/写真/猫.jpg'"},
	} {
		if got := quotePaths(c.in); got != c.want {
			t.Errorf("quotePaths(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}
