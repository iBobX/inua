package termui

import (
	"slices"
	"testing"

	"codeberg.org/puregotk/puregotk/v4/gdk"
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

// The image a clipboard offers, asked of formats with no MIME type at all
// too: their list is NULL, which once crashed the window on Ctrl+Shift+V.
func TestImageMime(t *testing.T) {
	for _, c := range []struct {
		mimes []string
		want  string
	}{
		{nil, ""},
		{[]string{"image/jpeg", "image/png"}, "image/png"},
		{[]string{"image/webp"}, "image/webp"},
		{[]string{"image/png", "text/plain;charset=utf-8"}, ""},
		{[]string{"text/html", "image/png", "UTF8_STRING"}, ""},
	} {
		f := gdk.NewContentFormats(c.mimes, uint32(len(c.mimes)))
		if got := imageMime(f); got != c.want {
			t.Errorf("imageMime(%q) = %q, want %q", c.mimes, got, c.want)
		}
	}
}
