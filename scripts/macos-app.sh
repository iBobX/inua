#!/bin/bash
# Build inua-term.app, the terminal window for macOS, with everything it
# loads inside it (docs/terminal.md, "On macOS"), and a .dmg of it. Run on
# a Mac, from the repository:
#
#   scripts/macos-app.sh [VERSION]        (default: git describe)
#
# Needs Go, Zig 0.16 (the engine, scripts/libghostty.sh) and Homebrew's
# gtk4, librsvg and adwaita-icon-theme. Writes build/macos/inua-term.app
# and build/macos/inua-term-VERSION-macos-ARCH.dmg.
#
# GTK and the libraries it loads are copied from Homebrew into
# Contents/Frameworks, each reference rewritten to the copy beside it;
# puregotk finds them there (third_party/puregotk/INUA.md). The image
# loaders, GSettings schemas and icons go in Contents/Resources, which
# inua-term points GTK at when it starts (cmd/inua-term/bundle_darwin.go).
#
# Signing: with APPLE_CODESIGN_IDENTITY set ("Developer ID Application: …",
# in the keychain), every file is signed with it, the hardened runtime
# and a timestamp, the libraries first; without it, ad hoc, to run on
# this Mac. Notarizing is the release job's (docs/apple_signing_linux.md).
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
version=${1:-$(git describe --tags --always --dirty | sed 's/^v//')}
arch=$(uname -m) # arm64 or x86_64
brew=$(brew --prefix)
out=build/macos
app=$out/inua-term.app
contents=$app/Contents
fw=$contents/Frameworks
res=$contents/Resources

for f in gtk4 librsvg adwaita-icon-theme; do
	brew list --versions "$f" >/dev/null || { echo "macos-app: brew install $f" >&2; exit 1; }
done

rm -rf "$app"
mkdir -p "$contents/MacOS" "$fw/loaders" "$res/share/glib-2.0/schemas" "$res/share/icons" "$res/lib/gdk-pixbuf-2.0"

# The programs, and the engine.
CGO_ENABLED=0 go build -trimpath -o "$contents/MacOS/inua-term" ./cmd/inua-term
CGO_ENABLED=0 go build -trimpath -o "$contents/MacOS/inua-termd" ./cmd/inua-termd
[ -f bin/libinua-vt.dylib ] || scripts/libghostty.sh
cp bin/libinua-vt.dylib "$fw/"

# deps lists what a file loads from Homebrew: by path, or by @rpath
# (Homebrew's lib then).
deps() {
	otool -L "$1" | awk 'NR>1 {print $1}' | grep "^$brew/\|^/opt/homebrew/\|^/usr/local/\|^@rpath/" || true
}
resolve() {
	case $1 in @rpath/*) echo "$brew/lib/${1#@rpath/}" ;; *) echo "$1" ;; esac
}

# The libraries puregotk opens (the names its packages register), then
# whatever they load from Homebrew, found with otool until none is new.
copied=" "
queue=()
for name in libgtk-4.1 libglib-2.0.0 libgobject-2.0.0 libgio-2.0.0 libgmodule-2.0.0 \
	libpango-1.0.0 libpangocairo-1.0.0 libcairo-gobject.2 libgdk_pixbuf-2.0.0 libgraphene-1.0.0; do
	queue+=("$brew/lib/$name.dylib")
done
# The image loaders: SVG (the icons) through librsvg, and gdk-pixbuf's own.
for l in "$brew"/lib/gdk-pixbuf-2.0/2.10.0/loaders/*.so; do
	[ -f "$l" ] || continue
	base=$(basename "$l")
	cp "$l" "$fw/loaders/$base"
	chmod u+w "$fw/loaders/$base"
	for d in $(deps "$l"); do queue+=("$(resolve "$d")"); done
done
while [ ${#queue[@]} -gt 0 ]; do
	lib=${queue[0]}
	queue=("${queue[@]:1}")
	base=$(basename "$lib")
	case "$copied" in *" $base "*) continue ;; esac
	copied="$copied$base "
	src=$(realpath "$lib")
	cp "$src" "$fw/$base"
	chmod u+w "$fw/$base"
	for d in $(deps "$src"); do queue+=("$(resolve "$d")"); done
done

# Each reference to Homebrew becomes the copy beside it.
relink() { # file, the path from it to Frameworks
	local f=$1 to=$2 ref
	install_name_tool -id "@loader_path/$(basename "$f")" "$f" 2>/dev/null || true
	install_name_tool -delete_rpath "$brew/lib" "$f" 2>/dev/null || true
	for ref in $(deps "$f"); do
		install_name_tool -change "$ref" "@loader_path/$to$(basename "$ref")" "$f"
	done
}
for f in "$fw"/*.dylib; do relink "$f" ""; done
for f in "$fw"/loaders/*.so; do relink "$f" "../"; done

# The loaders' cache, by path: inua-term writes it out with the bundle's
# own path in place of @CONTENTS@.
GDK_PIXBUF_MODULEDIR="$fw/loaders" gdk-pixbuf-query-loaders "$fw"/loaders/*.so |
	sed "s|$(cd "$contents" && pwd)|@CONTENTS@|g" >"$res/lib/gdk-pixbuf-2.0/loaders.cache.in"

# GTK's settings schemas, compiled.
cp "$brew"/share/glib-2.0/schemas/org.gtk.gtk4.*.gschema.xml "$res/share/glib-2.0/schemas/"
glib-compile-schemas "$res/share/glib-2.0/schemas"

# The icons: Adwaita's symbolic ones, which the apps' theme inherits for
# what the Phosphor set lacks, and hicolor's index.
mkdir -p "$res/share/icons/Adwaita" "$res/share/icons/hicolor"
cp "$brew/share/icons/Adwaita/index.theme" "$res/share/icons/Adwaita/"
cp -RL "$brew/share/icons/Adwaita/symbolic" "$res/share/icons/Adwaita/" # links as files: some lead out of what is copied
cp "$brew/share/icons/hicolor/index.theme" "$res/share/icons/hicolor/"

# The app's icon, from its SVG.
iconset=$out/inua-term.iconset
rm -rf "$iconset"
mkdir -p "$iconset"
for s in 16 32 128 256 512; do
	rsvg-convert -w $s -h $s config/apps/io.github.lyricat.Inua.Term.svg -o "$iconset/icon_${s}x${s}.png"
	rsvg-convert -w $((s * 2)) -h $((s * 2)) config/apps/io.github.lyricat.Inua.Term.svg -o "$iconset/icon_${s}x${s}@2x.png"
done
iconutil -c icns "$iconset" -o "$res/inua-term.icns"
rm -rf "$iconset"

# The oldest macOS it runs on: the newest any library was built for.
minos=$(for f in "$fw"/*.dylib "$fw"/loaders/*.so; do
	otool -l "$f" | awk '/LC_BUILD_VERSION/ {b=1} b && $1 == "minos" {print $2; exit}'
done | sort -t. -k1,1n -k2,2n | tail -1)

cat >"$contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key><string>io.github.lyricat.Inua.Term</string>
	<key>CFBundleName</key><string>inua-term</string>
	<key>CFBundleDisplayName</key><string>inua-term</string>
	<key>CFBundleExecutable</key><string>inua-term</string>
	<key>CFBundleIconFile</key><string>inua-term</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>${version%%-*}</string>
	<key>CFBundleVersion</key><string>$version</string>
	<key>LSMinimumSystemVersion</key><string>${minos:-13.0}</string>
	<key>LSApplicationCategoryType</key><string>public.app-category.developer-tools</string>
	<key>NSHighResolutionCapable</key><true/>
	<key>NSSupportsAutomaticGraphicsSwitching</key><true/>
</dict>
</plist>
EOF

# Nothing may point outside the bundle but the system's own. A library's
# own name (otool -D) is not a reference: the engine's stays @rpath/…, as
# Zig leaves no room in its header to rename it, and it is opened by path.
bad=0
for f in "$contents"/MacOS/* "$fw"/*.dylib "$fw"/loaders/*.so; do
	self=$(otool -D "$f" | awk 'NR>1 {print $1}')
	if otool -L "$f" | awk -v self="$self" 'NR>1 && $1 != self {print $1}' | grep -v '^@loader_path/\|^/usr/lib/\|^/System/'; then
		echo "macos-app: $f points outside the bundle" >&2
		bad=1
	fi
done
# Nor may a link lead nowhere: codesign refuses the bundle then.
if find -L "$app" -type l | grep .; then
	echo "macos-app: broken links in the bundle" >&2
	bad=1
fi
[ $bad = 0 ]

# Signing, inside out: the libraries, the programs, the bundle.
if [ -n "${APPLE_CODESIGN_IDENTITY:-}" ]; then
	sign=(codesign --force --options runtime --timestamp --sign "$APPLE_CODESIGN_IDENTITY")
else
	sign=(codesign --force --sign -)
fi
for f in "$fw"/loaders/*.so "$fw"/*.dylib "$contents"/MacOS/inua-termd "$contents"/MacOS/inua-term; do
	"${sign[@]}" "$f"
done
"${sign[@]}" "$app"
codesign --verify --strict --deep --verbose=4 "$app"

# The disk image: the app and a link to Applications.
dmg=$out/inua-term-$version-macos-$arch.dmg
stage=$out/dmg
rm -rf "$stage" "$dmg"
mkdir -p "$stage"
cp -R "$app" "$stage/"
ln -s /Applications "$stage/Applications"
hdiutil create -volname inua-term -srcfolder "$stage" -ov -format UDZO "$dmg" >/dev/null
rm -rf "$stage"
if [ -n "${APPLE_CODESIGN_IDENTITY:-}" ]; then
	codesign --force --timestamp --sign "$APPLE_CODESIGN_IDENTITY" "$dmg"
fi
du -sh "$app" "$dmg"
