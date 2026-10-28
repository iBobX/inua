package gtkx

import "testing"

func TestLibDarwin(t *testing.T) {
	for in, want := range map[string]string{
		"libgtk-4.so.1":       "libgtk-4.1.dylib",
		"libgobject-2.0.so.0": "libgobject-2.0.0.dylib",
		"libcairo.so.2":       "libcairo.2.dylib",
	} {
		if got := libDarwin(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}
