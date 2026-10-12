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

# The output: a heading for each step, ✓ for what is done, ! for what
# needs your attention; in colour on a terminal.
if [ -t 1 ]; then
    bold='\033[1m' dim='\033[2m' green='\033[32m' yellow='\033[33m' red='\033[31m' blue='\033[34m' off='\033[0m'
else
    bold= dim= green= yellow= red= blue= off=
fi
steps=6
[ -n "${INUA_NO_SESSION:-}" ] && steps=5
step=0
heading() { step=$((step + 1)); printf "\n${bold}${blue}[%d/%d]${off} ${bold}%s${off}\n" "$step" "$steps" "$*"; }
say() { printf "      %s\n" "$*"; }
done_() { printf "  ${green}✓${off}   %s\n" "$*"; }
note() { printf "  ${yellow}!${off}   %s\n" "$*"; }
die() { printf "  ${red}✗   %s${off}\n" "$*" >&2; exit 1; }

# ask QUESTION y|n: true for yes. Reads the terminal, as the script itself
# comes down a pipe; without one, the default stands.
ask() {
    if [ "$2" = y ]; then hint="[Y/n]"; else hint="[y/N]"; fi
    if ! (: </dev/tty) 2>/dev/null; then
        [ "$2" = y ]
        return
    fi
    printf "  ${bold}?${off}   %s ${dim}%s${off} " "$1" "$hint" >/dev/tty
    read -r answer </dev/tty || answer=
    case $answer in
        [Yy]*) return 0 ;;
        [Nn]*) return 1 ;;
        *) [ "$2" = y ] ;;
    esac
}

have() { command -v "$1" >/dev/null 2>&1; }

# The package manager here, and what it calls the packages inua needs:
# pkg NAME gives the name for this system's manager.
manager=
for m in apt-get pacman dnf zypper; do
    if have "$m"; then manager=$m; break; fi
done
pkg() {
    case $manager:$1 in
        apt-get:gtk4) echo libgtk-4-1 ;;
        apt-get:gtk4-layer-shell) echo libgtk4-layer-shell0 ;;
        zypper:gtk4) echo libgtk-4-1 ;;
        zypper:gtk4-layer-shell) echo libgtk4-layer-shell0 ;;
        zypper:pass) echo password-store ;;
        zypper:gnupg) echo gpg2 ;;
        dnf:gnupg) echo gnupg2 ;;
        pacman:tuigreet) echo greetd-tuigreet ;;
        *) echo "$1" ;;
    esac
}
# pkg_install NAMES…: installs packages with this system's manager (sudo).
pkg_install() {
    case $manager in
        apt-get) sudo apt-get install -y "$@" ;;
        pacman) sudo pacman -S --needed --noconfirm "$@" ;;
        dnf) sudo dnf install -y "$@" ;;
        zypper) sudo zypper --non-interactive install "$@" ;;
        *) return 1 ;;
    esac
}

# The version of inua installed now, if any: "0.0.6".
version_of() { "$prefix/bin/inua" version 2>/dev/null | sed -n 's/^inua //p'; }

# --- the release ---------------------------------------------------------

heading "Downloading inua"
before=$(version_of || true)
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
    done_ "using $INUA_ARCHIVE"
else
    have curl || die "curl is needed"
    if [ -n "${INUA_VERSION:-}" ]; then
        base=https://github.com/$repo/releases/download/$INUA_VERSION
    else
        base=https://github.com/$repo/releases/latest/download
    fi
    say "${INUA_VERSION:-the latest release}, for $arch"
    curl -fsSL -o "$tmp/$name.tar.gz" "$base/$name.tar.gz" || die "could not download $base/$name.tar.gz"
    curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || die "could not download the checksums"
    (cd "$tmp" && grep " $name.tar.gz\$" checksums.txt | sha256sum -c --quiet -) || die "the download does not match its checksum"
    done_ "downloaded, and its checksum matches"
fi
tar -xzf "$tmp/$name.tar.gz" -C "$tmp"
src=$tmp/$name

# --- the programs and launchers --------------------------------------------

heading "Installing the programs"
for bin in inua-shell inua inua-apps inua-games inua-pinentry inua-assistant inua-keyring; do
    [ -f "$src/$bin" ] || continue # inua-games, inua-assistant and inua-keyring came after v0.0.2
    install -Dm755 "$src/$bin" "$prefix/bin/$bin"
done
install -Dm644 -t "$data/applications" "$src"/config/apps/*.desktop
install -Dm644 -t "$data/icons/hicolor/scalable/apps" "$src"/config/apps/*.svg
have update-desktop-database && update-desktop-database -q "$data/applications" 2>/dev/null || true
after=$(version_of)
if [ -z "$before" ]; then
    done_ "installed inua $after in $prefix/bin"
elif [ "$before" = "$after" ]; then
    done_ "inua $after is already the latest; reinstalled it in $prefix/bin"
else
    done_ "updated inua $before → $after in $prefix/bin"
fi
done_ "the light apps and games are in your app launcher"
case :$PATH: in
    *":$prefix/bin:"*) ;;
    *) note "$prefix/bin is not on your PATH (the Inua session adds it)" ;;
esac

# --- what inua needs --------------------------------------------------------

heading "Checking what inua needs"
missing=
have Hyprland || have hyprland || missing="$missing $(pkg hyprland)"
lib() { ldconfig -p 2>/dev/null | grep -q "$1"; }
lib libgtk-4.so.1 || missing="$missing $(pkg gtk4)"
lib libgtk4-layer-shell.so || missing="$missing $(pkg gtk4-layer-shell)"
# inua-keyring keeps apps' secrets in pass (docs/feat/keyring.md)
have pass || missing="$missing $(pkg pass)"
have gpg || missing="$missing $(pkg gnupg)"
if [ -z "$missing" ]; then
    done_ "Hyprland, GTK 4, gtk4-layer-shell, pass and gpg are all here"
else
    note "inua also needs:$missing"
    # shellcheck disable=SC2086
    if [ -n "$manager" ] && ask "Install them with ${manager%-get} (sudo)?" y && pkg_install $missing; then
        done_ "installed$missing"
    else
        note "install them${manager:+ with ${manager%-get}} before logging in to Inua"
    fi
fi

# --- your configuration -----------------------------------------------------

heading "Writing your configuration"
# inua init asks before replacing gpg's pinentry with inua-pinentry.
if PATH="$prefix/bin:$PATH" "$prefix/bin/inua" init; then
    done_ "configuration written (your own files are kept)"
else
    note "inua init reported problems (above); fix them, then run: inua init"
fi

# --- the keyring ------------------------------------------------------------

heading "Setting up the keyring"
# inua keyring replace asks before replacing the running keyring
# (gnome-keyring …), copying its items into pass first.
if [ ! -x "$prefix/bin/inua-keyring" ] || ! have pass; then
    note "inua-keyring needs pass; once it is installed, run: inua keyring replace"
elif [ ! -f "${PASSWORD_STORE_DIR:-$HOME/.password-store}/.gpg-id" ]; then
    note "pass has no password store yet; run: pass init YOUR-GPG-KEY-ID, then: inua keyring replace"
elif [ -z "${DBUS_SESSION_BUS_ADDRESS:-}" ] && [ ! -S "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/bus" ]; then
    note "no desktop session here; in one, run: inua keyring replace"
elif PATH="$prefix/bin:$PATH" "$prefix/bin/inua" keyring replace </dev/null; then
    done_ "inua-keyring keeps apps' secrets in pass"
else
    note "the keyring was not replaced (above); try again with: inua keyring replace"
fi

# --- the login session ------------------------------------------------------

if [ -n "${INUA_NO_SESSION:-}" ]; then
    printf "\n${bold}${green}Done.${off} Log in with inua-session from a TTY, or install the session later.\n"
    exit 0
fi

heading "Adding the Inua login session"

# The login manager systemd starts: gdm3, sddm, lightdm, greetd …, or none.
current=${INUA_DM-$(basename "$(readlink -f /etc/systemd/system/display-manager.service 2>/dev/null || true)" .service)}
[ "$current" = display-manager ] && current=
switch=no
case $current in
    greetd) mode=greetd ;;
    "")
        mode=other
        [ -n "$manager" ] && ask "No login manager runs. Install greetd + tuigreet, the login screen inua recommends?" y && switch=yes
        ;;
    *)
        mode=other
        say "you log in with $current; Inua will be in its list of sessions"
        [ -n "$manager" ] && ask "Replace $current with greetd + tuigreet, the login screen inua recommends? $current stays installed" y && switch=yes
        ;;
esac
if [ "$switch" = yes ]; then
    say "installing greetd and $(pkg tuigreet) (sudo)"
    pkg_install greetd "$(pkg tuigreet)" || die "install greetd and $(pkg tuigreet) with your package manager, then run this again"
    mode=greetd
fi

say "adding the session (sudo)"
sudo sh "$src/scripts/install-session.sh" "$mode"
done_ "Inua is a login session"

if [ "$switch" = yes ]; then
    [ -n "$current" ] && sudo systemctl disable "$current.service"
    sudo systemctl enable --force greetd.service
    done_ "greetd + tuigreet greet you after a restart; until then this session goes on"
    printf "\n${bold}${green}Done.${off} Restart, then log in to Inua.\n"
else
    printf "\n${bold}${green}Done.${off} Log out and choose Inua.\n"
fi
