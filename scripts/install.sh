#!/bin/sh
# Install or update inua from its latest release on GitHub:
#
#   curl -fsSL https://inua.lyric.im/install.sh | sh
#
# It puts the programs in ~/.local/bin and the light apps' launchers in
# ~/.local/share, checks the packages inua needs, writes your configuration
# with `inua init` (never replacing your files), and, with sudo, adds the
# Inua login session. Where inua replaces something you have, it asks
# first, and the answer defaults to yes: gpg's pinentry (inua-pinentry),
# the keyring apps keep secrets in (inua-keyring, in pass), and the login
# manager (GDM, SDDM, LightDM … → greetd + tuigreet). Run it again to
# update.
#
# Environment:
#   INUA_VERSION=v0.1.2   a release instead of the latest
#   PREFIX=~/.local       where the programs go (PREFIX/bin)
#   INUA_NO_SESSION=1     leave the login session alone (no sudo)
#   INUA_ARCHIVE=file     a release archive already downloaded
#   INUA_DM=gdm3          the login manager to assume, instead of systemd's
set -eu

repo=lyricat/inua
prefix=${PREFIX:-$HOME/.local}
data=${XDG_DATA_HOME:-$HOME/.local/share}

if [ -t 1 ]; then b='\033[1m' n='\033[0m'; else b= n=; fi
say() { printf "${b}inua:${n} %s\n" "$*"; }
die() { printf 'inua: %s\n' "$*" >&2; exit 1; }

# ask QUESTION y|n: true for yes. Reads the terminal, as the script itself
# comes down a pipe; without one, the default stands.
ask() {
    if [ "$2" = y ]; then hint="[Y/n]"; else hint="[y/N]"; fi
    if ! { : </dev/tty; } 2>/dev/null; then
        [ "$2" = y ]
        return
    fi
    printf '%s %s ' "$1" "$hint" >/dev/tty
    read -r answer </dev/tty || answer=
    case $answer in
        [Yy]*) return 0 ;;
        [Nn]*) return 1 ;;
        *) [ "$2" = y ] ;;
    esac
}

have() { command -v "$1" >/dev/null 2>&1; }

# --- the release ---------------------------------------------------------

[ "$(uname -s)" = Linux ] || die "inua runs on Linux"
case $(uname -m) in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) die "no release for $(uname -m); build from source: https://github.com/$repo" ;;
esac
for tool in tar sha256sum; do have "$tool" || die "$tool is needed"; done

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM
name=inua_linux_$arch

if [ -n "${INUA_ARCHIVE:-}" ]; then
    cp "$INUA_ARCHIVE" "$tmp/$name.tar.gz"
else
    have curl || die "curl is needed"
    if [ -n "${INUA_VERSION:-}" ]; then
        base=https://github.com/$repo/releases/download/$INUA_VERSION
    else
        base=https://github.com/$repo/releases/latest/download
    fi
    say "downloading ${INUA_VERSION:-the latest release} for $arch"
    curl -fsSL -o "$tmp/$name.tar.gz" "$base/$name.tar.gz" || die "could not download $base/$name.tar.gz"
    curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || die "could not download the checksums"
    (cd "$tmp" && grep " $name.tar.gz\$" checksums.txt | sha256sum -c --quiet -) || die "the download does not match its checksum"
fi
tar -xzf "$tmp/$name.tar.gz" -C "$tmp"
src=$tmp/$name

# --- the programs and launchers --------------------------------------------

for bin in inua-shell inua inua-apps inua-pinentry inua-assistant inua-keyring; do
    [ -f "$src/$bin" ] || continue # inua-assistant and inua-keyring came with v0.0.3
    install -Dm755 "$src/$bin" "$prefix/bin/$bin"
done
install -Dm644 -t "$data/applications" "$src"/config/apps/*.desktop
install -Dm644 -t "$data/icons/hicolor/scalable/apps" "$src"/config/apps/*.svg
have update-desktop-database && update-desktop-database -q "$data/applications" 2>/dev/null || true
say "installed $("$prefix/bin/inua" version) in $prefix/bin"
case :$PATH: in
    *":$prefix/bin:"*) ;;
    *) say "note: $prefix/bin is not on your PATH (the Inua session adds it)" ;;
esac

# --- what inua needs --------------------------------------------------------

missing=
have Hyprland || have hyprland || missing="$missing hyprland"
lib() { ldconfig -p 2>/dev/null | grep -q "$1"; }
lib libgtk-4.so.1 || missing="$missing libgtk-4-1"
lib libgtk4-layer-shell.so || missing="$missing libgtk4-layer-shell0"
# inua-keyring keeps apps' secrets in pass (docs/feat/keyring.md)
have pass || missing="$missing pass"
have gpg || missing="$missing gnupg"
if [ -n "$missing" ]; then
    say "inua also needs:$missing"
    if have apt-get && ask "Install them with apt (sudo)?" y; then
        # shellcheck disable=SC2086
        sudo apt-get install -y $missing
    else
        say "install them with your package manager before logging in to Inua"
    fi
fi

# --- your configuration -----------------------------------------------------

# inua init asks before replacing gpg's pinentry with inua-pinentry.
PATH="$prefix/bin:$PATH" "$prefix/bin/inua" init || say "inua init reported problems (above); fix them, then run: inua init"

# --- the keyring ------------------------------------------------------------

# inua keyring replace asks before replacing the running keyring
# (gnome-keyring …), copying its items into pass first.
if [ -x "$prefix/bin/inua-keyring" ] && have pass; then
    if [ ! -f "${PASSWORD_STORE_DIR:-$HOME/.password-store}/.gpg-id" ]; then
        say "pass has no password store yet; for inua-keyring, run: pass init YOUR-GPG-KEY-ID, then: inua keyring replace"
    elif [ -z "${DBUS_SESSION_BUS_ADDRESS:-}" ] && [ ! -S "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/bus" ]; then
        say "no desktop session here; for inua-keyring, run in one: inua keyring replace"
    else
        PATH="$prefix/bin:$PATH" "$prefix/bin/inua" keyring replace </dev/null ||
            say "the keyring was not replaced (above); try again with: inua keyring replace"
    fi
fi

# --- the login session ------------------------------------------------------

if [ -n "${INUA_NO_SESSION:-}" ]; then
    say "done; log in with inua-session from a TTY, or install the session later"
    exit 0
fi

# The login manager systemd starts: gdm3, sddm, lightdm, greetd …, or none.
current=${INUA_DM-$(basename "$(readlink -f /etc/systemd/system/display-manager.service 2>/dev/null || true)" .service)}
[ "$current" = display-manager ] && current=
switch=no
case $current in
    greetd) mode=greetd ;;
    "")
        mode=other
        ask "No login manager runs. Install greetd + tuigreet, the login screen inua recommends?" y && switch=yes
        ;;
    *)
        mode=other
        say "you log in with $current; Inua will be in its session list"
        ask "Replace $current with greetd + tuigreet, the login screen inua recommends? $current stays installed" y && switch=yes
        ;;
esac
if [ "$switch" = yes ]; then
    have apt-get || die "install greetd and tuigreet with your package manager, then run this again"
    say "installing greetd and tuigreet (sudo)"
    sudo apt-get install -y greetd tuigreet
    mode=greetd
fi

say "adding the Inua login session (sudo)"
sudo sh "$src/scripts/install-session.sh" "$mode"

if [ "$switch" = yes ]; then
    [ -n "$current" ] && sudo systemctl disable "$current.service"
    sudo systemctl enable --force greetd.service
    say "greetd + tuigreet will greet you after a restart; until then this session goes on"
else
    say "done: log out and choose Inua"
fi
