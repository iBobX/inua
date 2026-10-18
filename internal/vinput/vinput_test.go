package vinput

import (
	"strings"
	"testing"
)

func TestKeysym(t *testing.T) {
	for r, want := range map[rune]string{'a': "a", 'Z': "Z", '7': "7", ' ': "space", '\n': "Return", 'é': "U00E9", '中': "U4E2D", '😀': "U1F600"} {
		if got := Keysym(r); got != want {
			t.Errorf("Keysym(%q) = %q, want %q", r, got, want)
		}
	}
}

func TestKeymap(t *testing.T) {
	km := Keymap([]string{"Return", "U4E2D"})
	for _, want := range []string{"minimum = 8;", "maximum = 11;", "<K1> = 9;", "<K2> = 10;", "key <K1> {[Return]};", "key <K2> {[U4E2D]};", `include "complete"`} {
		if !strings.Contains(km, want) {
			t.Errorf("the keymap has no %q:\n%s", want, km)
		}
	}
}
