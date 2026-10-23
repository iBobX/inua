#!/bin/sh
# Build libinua-vt.so (libinua-vt.dylib for macOS): libghostty-vt at the
# pinned Ghostty commit, linked with inua's shim (internal/term/vt/shim),
# exporting only inua_vt_*. inua-term loads it with purego. Releases build
# it in CI; locally:
#
#   scripts/libghostty.sh [OUT_DIR]        (default: bin/)
#
# ZIG_TARGET builds it for another machine (aarch64-linux-gnu,
# x86_64-linux-gnu, aarch64-macos, x86_64-macos), as the releases do.
#
# Needs git and Zig 0.16 ($ZIG, else zig-0.16, else zig). The Ghostty
# checkout is kept in $GHOSTTY_SRC (default: $XDG_CACHE_HOME/inua/ghostty)
# and moved to the pinned commit. Updating the pin: change GHOSTTY_COMMIT,
# run this and `go test ./internal/term/...`, and fix the shim where the
# compiler says the API changed.
set -eu

GHOSTTY_COMMIT=765ea37ad64f7e3a48f6ac27a70ad7fb6f31b9bb # 2026-10-18
GHOSTTY_REPO=https://github.com/ghostty-org/ghostty.git

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
out=${1:-$root/bin}
src=${GHOSTTY_SRC:-${XDG_CACHE_HOME:-$HOME/.cache}/inua/ghostty}
zig=${ZIG:-}
if [ -z "$zig" ]; then
	for z in zig-0.16 zig; do
		if command -v "$z" >/dev/null 2>&1; then zig=$z; break; fi
	done
fi
[ -n "$zig" ] || { echo "libghostty.sh: Zig 0.16 is not installed (set ZIG)" >&2; exit 1; }
case "$("$zig" version)" in
	0.16.*) ;;
	*) echo "libghostty.sh: $zig is $("$zig" version); Ghostty needs 0.16 (set ZIG)" >&2; exit 1 ;;
esac

if [ ! -d "$src/.git" ]; then
	mkdir -p "$(dirname "$src")"
	git clone --quiet --filter=blob:none "$GHOSTTY_REPO" "$src"
fi
if [ "$(git -C "$src" rev-parse HEAD)" != "$GHOSTTY_COMMIT" ]; then
	git -C "$src" fetch --quiet origin "$GHOSTTY_COMMIT"
	git -C "$src" -c advice.detachedHead=false checkout --quiet "$GHOSTTY_COMMIT"
fi

target=${ZIG_TARGET:-}
(cd "$src" && "$zig" build -Demit-lib-vt -Doptimize=ReleaseFast ${target:+-Dtarget=$target})

shim=$root/internal/term/vt/shim
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
printf '{ global: inua_vt_*; local: *; };\n' > "$tmp/exports.map"
# The keyboard's keycodes (XKB, as GDK reports them) to libghostty's keys,
# from Ghostty's own table: its W3C code names to its key tags, kept to
# the names its C header has.
awk '
FILENAME == ARGV[1] {
	if (match($0, /GHOSTTY_KEY_[A-Z0-9_]+/)) names[substr($0, RSTART, RLENGTH)] = 1
	next
}
/\.\{ "[A-Za-z0-9]+", \.[a-z0-9_]+ \}/ {
	match($0, /"[A-Za-z0-9]+"/); code = substr($0, RSTART + 1, RLENGTH - 2)
	match($0, /\.[a-z0-9_]+ \}/); tags[code] = substr($0, RSTART + 1, RLENGTH - 3)
	next
}
/^ *\.\{ 0x[0-9a-f]+, 0x[0-9a-f]+, 0x[0-9a-f]+,/ {
	split($0, f, ","); xkb = f[3]; gsub(/ /, "", xkb)
	if (xkb != "0x0000" && match($0, /"[A-Za-z0-9]+"/)) pairs[++n] = xkb SUBSEP substr($0, RSTART + 1, RLENGTH - 2)
}
END {
	for (i = 1; i <= n; i++) {
		split(pairs[i], p, SUBSEP)
		if (!(p[2] in tags)) continue
		name = toupper(tags[p[2]])
		if (name ~ /^KEY_[A-Z]$/) name = substr(name, 5)
		name = "GHOSTTY_KEY_" name
		if (name in names) printf "  {%s, %s},\n", p[1], name
	}
}' "$src/include/ghostty/vt/key/event.h" "$src/src/input/keycodes.zig" > "$tmp/inua_keymap.inc"
[ -s "$tmp/inua_keymap.inc" ] || { echo "libghostty.sh: no keycodes found in Ghostty's table" >&2; exit 1; }
mkdir -p "$out"
# macOS (a macos target, or built on a Mac): a dylib. Zig's Mach-O linker
# takes no list of exports, so libghostty-vt's own symbols are exported
# too; harmless there, where each library's symbols are looked up in it
# alone (two-level namespaces). Elsewhere a shared object exporting
# inua_vt_* alone, by a version script.
case "${target:-$(uname -s)}" in
*macos* | Darwin)
	lib=libinua-vt.dylib
	link="-Wl,-install_name,@rpath/$lib"
	;;
*)
	lib=libinua-vt.so
	link="-Wl,--version-script=$tmp/exports.map -Wl,-s -Wl,-soname,$lib"
	;;
esac
# shellcheck disable=SC2086 # $link is several options
"$zig" cc ${target:+-target $target} -shared -fPIC -O2 -std=c11 -Wall -Werror \
	-I "$src/zig-out/include" -I "$shim" -I "$tmp" \
	-o "$tmp/$lib" "$shim/inua_vt.c" "$src/zig-out/lib/libghostty-vt.a" $link
mv "$tmp/$lib" "$out/$lib"
sum=$( (sha256sum "$out/$lib" 2>/dev/null || shasum -a 256 "$out/$lib") | cut -d' ' -f1)
echo "$out/$lib: ghostty $GHOSTTY_COMMIT, sha256 $sum"
