package gtkx

import "codeberg.org/puregotk/purego"

// Open loads one of GTK's libraries by its Linux name (libgtk-4.so.1),
// for the calls puregotk does not bind; on macOS, the library of that
// name beside the app or in Homebrew's (libDarwin).
func Open(name string) (uintptr, error) {
	return purego.Dlopen(libPath(name), purego.RTLD_NOW|purego.RTLD_GLOBAL)
}
