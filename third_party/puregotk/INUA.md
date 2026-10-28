# puregotk, as inua carries it

This is codeberg.org/puregotk/puregotk at
v0.0.0-20261003054014-d92056b0aa41 (LGPL-2.1, see LICENSE and
LICENSE.LGPL), trimmed and patched for inua's start-up time. inua's go.mod
points at it with a `replace`.

## Trimmed

Only the packages inua's programs import are here: cairo, gdk, gdkpixbuf,
gio, glib, gmodule, gobject, graphene, gsk, gtk, pango and pangocairo
under v4, with internal/core and pkg/core. Each package's documentation variant (the
files built with the `puregotkdocs` tag) is left out; the code is the same
without the comments. The generator (gen.go, templates) is not here: to
take a new puregotk, copy its packages over these again (the list above)
and put the patch below back.

## Patched: internal/core/core.go

- `PuregoSafeRegister` binds a function on its first call instead of at
  start. Every generated file's init registers each of its library's
  functions, 27,054 in all; looking each up with dlsym and making its
  trampoline took about 165 ms of every program's start, though a program
  calls a few hundred of them. Now each is a stub (reflect.MakeFunc, under
  1 µs) that, on its first call, looks the symbol up, makes the
  trampoline, puts it in the stub's place and calls it. A function missing
  from the libraries panics when called, as calling the nil function did.
- `GetPaths` remembers each library's paths: every generated file's init
  asked again, each time a handful of stat calls.
- `GetPaths` looks in a macOS app bundle's `Contents/Frameworks` first,
  when the program runs from its `Contents/MacOS` (`bundleFrameworks`):
  inua-term.app carries GTK there (docs/terminal.md, "On macOS"), and the
  packages' init asks for the libraries before `main` could set
  `PUREGOTK_LIB_FOLDER`.

Measured (docs/performance.md): the packages' init went from ~385 ms to
~220 ms; what is left is loading GTK and its libraries (~100 ms) and the
stubs.

The generated files are kept as upstream wrote them, some not in gofmt's
layout, so a new copy compares cleanly; CI's format check leaves
third_party out.
