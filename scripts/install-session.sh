#!/bin/sh
# Install the shared session entry. An optional staging root avoids system writes.
# Build/install binaries and initialize user config first (make install-session).
set -eu

if [ "$#" -gt 2 ]; then
    echo "usage: $0 [greetd|gdm|sddm|other] [staging-root]" >&2
    exit 2
fi
dm=${1:-greetd}
case "$dm" in
    greetd|gdm|sddm|other) ;; # other: any login manager reading wayland-sessions
    *) echo "inua: unsupported display manager: $dm (use greetd, gdm, sddm or other)" >&2; exit 2 ;;
esac
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
dest=${2:-/}

install -Dm755 "$root/scripts/inua-session" "$dest/usr/local/bin/inua-session"
install -Dm644 "$root/config/session/inua.desktop" "$dest/usr/share/wayland-sessions/inua.desktop"
if [ "$dm" = greetd ]; then
    config="$dest/etc/greetd/config.toml"
    if ! cmp -s "$root/config/session/greetd.toml" "$config"; then
        install -Dm644 --backup=numbered "$root/config/session/greetd.toml" "$config"
        echo "tuigreet configured in $config; any previous config is saved as $config.~N~."
    fi
    echo "The greetd template targets Ubuntu/Debian (_greetd, tty7). See config/README.md before enabling greetd."
fi
echo "Inua session installed. Select Inua in your login screen, or run inua-session from a TTY."
