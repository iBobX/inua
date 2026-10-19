#!/bin/sh
# Build libinua-vt.so: libghostty-vt at the pinned Ghostty commit, linked
# with inua's shim (internal/term/vt/shim), exporting only inua_vt_*.
# inua-term loads it with purego. Releases build it in CI; locally:
#
#   scripts/libghostty.sh [OUT_DIR]        (default: bin/)
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

(cd "$src" && "$zig" build -Demit-lib-vt -Doptimize=ReleaseFast)

shim=$root/internal/term/vt/shim
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
printf '{ global: inua_vt_*; local: *; };\n' > "$tmp/exports.map"
mkdir -p "$out"
"$zig" cc -shared -fPIC -O2 -std=c11 -Wall -Werror \
	-I "$src/zig-out/include" -I "$shim" \
	-o "$tmp/libinua-vt.so" "$shim/inua_vt.c" "$src/zig-out/lib/libghostty-vt.a" \
	-Wl,--version-script="$tmp/exports.map" -Wl,-s -Wl,-soname,libinua-vt.so
mv "$tmp/libinua-vt.so" "$out/libinua-vt.so"
echo "$out/libinua-vt.so: ghostty $GHOSTTY_COMMIT, sha256 $(sha256sum "$out/libinua-vt.so" | cut -d' ' -f1)"
