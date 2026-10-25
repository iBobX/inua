# Removing GNOME

On Ubuntu Desktop, inua runs beside GNOME. Once you log in to Inua
through greetd (or another login manager that is not GDM), you can remove
GNOME. Package names are those of Ubuntu 26.04 (checked 2026-10-20).
Before removing anything, run the same command with `-s` (`apt-get -s
remove …`) to see the list without changing anything.

Once GNOME is gone, there is no other desktop to log in to if Hyprland or
inua does not start. Make sure you can fix things from a text console
(Ctrl+Alt+F3).

## The desktop and its apps

```sh
sudo apt remove gdm3 gnome-shell gnome-session-bin gnome-settings-daemon \
  gnome-control-center gnome-initial-setup gnome-tweaks gnome-shell-extension-manager \
  yaru-theme-gnome-shell gnome-session-canberra gnome-power-manager gnome-menus \
  xdg-desktop-portal-gnome nautilus nautilus-sendto
```

This also removes `ubuntu-session` (the "Ubuntu" login session), Ubuntu's
shell extensions and `evolution-data-server`. Only remove `gdm3` if you
already log in through another login manager: `systemctl status
display-manager` shows which one runs. The install script offers greetd.

Removing Nautilus also stops the gvfs daemons it starts. Files
(`inua-files`) replaces it. GNOME's apps (`gnome-calculator`,
`gnome-terminal`, `gnome-system-monitor`, `gnome-text-editor` …) can go
along with the rest or later. inua has its own for most of them.

None of this removes NetworkManager, BlueZ, PipeWire, polkit, GTK or the
Hyprland and GTK portals. In a Hyprland session, the portals use the
Hyprland and GTK backends first (`hyprland-portals.conf`, from Hyprland's
package), so file dialogs and screen sharing work without the GNOME
backend.

## The keyring

gnome-keyring keeps apps' secrets: browsers' keys to saved logins,
Wi-Fi passwords. Removing it without copying them signs apps out. Move the
secrets to `inua-keyring` first (see [feat/keyring.md](feat/keyring.md)):

```sh
inua keyring replace    # copy gnome-keyring's items into pass, then serve them
inua keyring status     # should say inua-keyring serves
```

Log out and in again, and check that your apps still have their logins.

Flatpak apps keep their secrets through the portal, which gnome-keyring
serves too. `inua portals enable Secret` has inua-keyring serve it
([portals](portals.md)); the secrets `inua keyring replace` copied are
theirs.

You can then leave the gnome-keyring package installed: nothing starts
it, and it is small. Removing it (`gnome-keyring`, `libpam-gnome-keyring`)
also removes any app whose package depends on it. Proton VPN is one. Check
with `apt-get -s remove gnome-keyring` first. If gnome-keyring still starts
at login, look for `pam_gnome_keyring.so auto_start` in
`/etc/pam.d/greetd`: removing `auto_start` from that line (sudo) stops PAM
from starting it.

## SSH keys

GNOME's ssh agent comes from the `gcr` package, not from gnome-keyring,
so it keeps running after the keyring is replaced. It sets
`SSH_AUTH_SOCK` to `/run/user/UID/gcr/ssh`, and the first time a program
uses a key it asks for the key's passphrase in GNOME's own dialog
(gcr-prompter), which looks like a GTK pinentry. It can do this right
after login, when something touches the key. The install script offers
to turn it off; by hand, turn it off and log out and in again:

```sh
systemctl --user disable --now gcr-ssh-agent.socket gcr-ssh-agent.service
```

ssh then uses OpenSSH's `ssh-agent` (its user socket is on by default),
or gpg-agent's when gpg-agent has `enable-ssh-support`, which asks for
passphrases through `inua-pinentry`, in inua's dialog. A program that
runs ssh without a terminal asks in inua's dialog too once
`SSH_ASKPASS` is `inua-askpass` (see [features.md](features.md#passwords)).

## Before `apt autoremove`

`apt autoremove` removes packages that were installed only as a
dependency of something else, once nothing installed needs them. Removing
GNOME leaves over a hundred of those. Most are GNOME's libraries, but a
few are things you still use: GNOME was the only package that required
them. inua is not an apt package, so apt does not know that it needs them
too.

Mark these as installed by you, before running autoremove:

```sh
sudo apt-mark manual pipewire-audio pipewire-alsa gstreamer1.0-pipewire \
  power-profiles-daemon iio-sensor-proxy switcheroo-control xdg-terminal-exec \
  wireguard-tools mobile-broadband-provider-info \
  xdg-desktop-portal-hyprland xdg-desktop-portal-gtk gnupg
```

| Package | For |
| --- | --- |
| `pipewire-audio`, `pipewire-alsa`, `gstreamer1.0-pipewire` | Sound for every app, Music and Video |
| `power-profiles-daemon` | Power saver in the control center |
| `iio-sensor-proxy` | Screen rotation and the light sensor on laptops and tablets |
| `switcheroo-control` | Running apps on the discrete graphics card |
| `xdg-terminal-exec` | Opening `Terminal=true` apps |
| `wireguard-tools` | WireGuard connections |
| `mobile-broadband-provider-info` | Mobile broadband settings |
| `xdg-desktop-portal-hyprland`, `xdg-desktop-portal-gtk` | Screen sharing and file dialogs |
| `gnupg` | pass, and through it `inua-keyring` |

Add any app you use that the list includes. For example, `papers` is
GNOME's PDF viewer, and `system-config-printer-udev` sets up USB printers.
To see the list without removing anything:

```sh
apt-get -s autoremove | grep '^Remv'
```

## Later updates

After this, `apt autoremove` and updates are as safe as on any Ubuntu
system. A package marked as installed by you is never removed
automatically.

- `apt upgrade` never removes packages.
- `apt full-upgrade` and release upgrades (`do-release-upgrade`) can
  remove packages, but list them and ask first. Read the list as you would
  autoremove's.
- Unattended upgrades, with Ubuntu's defaults, remove only old kernels
  and dependencies that the same upgrade left unused.
