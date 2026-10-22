#!/bin/sh
# Copy what inua-term serve's page loads into internal/term/web/static/vendor,
# at pinned versions, so the page asks nothing of the internet: xterm.js
# (MIT) and its addons, and the fonts of inua's look (SIL OFL 1.1, the
# Latin subsets; CJK text takes the phone's own font). The files are
# committed; change a version and run this again to update them.
#
#   scripts/term-web.sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
dest=$root/internal/term/web/static/vendor
cdn=https://cdn.jsdelivr.net/npm

get() { # PACKAGE@VERSION PATH [NAME]
	curl -fsSL "$cdn/$1/$2" -o "$dest/${3:-$(basename "$2")}"
}

rm -rf "$dest"
mkdir -p "$dest"

xterm=@xterm/xterm@6.0.0
get $xterm lib/xterm.js
get $xterm css/xterm.css
get $xterm LICENSE xterm.LICENSE
get @xterm/addon-webgl@0.19.0 lib/addon-webgl.js
get @xterm/addon-unicode-graphemes@0.4.0 lib/addon-unicode-graphemes.js

# FAMILY-DIR WEIGHTS: the fonts' Latin subsets as woff2.
for font in "geist-mono 400 600" "ibm-plex-sans 400 600" "ibm-plex-mono 400 600" "jetbrains-mono 400 600"; do
	set -- $font
	name=$1
	shift
	for w in "$@"; do
		get @fontsource/$name@5.3.0 files/$name-latin-$w-normal.woff2
	done
	get @fontsource/$name@5.3.0 LICENSE $name.LICENSE
done
