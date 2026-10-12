# Design: a desktop shell in Go

## Goal

The compositor manages windows; inua is the desktop shell around them. The
logic of the panels, the launcher, search and every flow is written in Go.
The interface is GTK4, with its layouts in markup files. inua works with
Hyprland today; this document describes that integration.

The design is about everyday work: getting into a task, finding an app or a
window, doing something, and getting back to work when the interface goes
away.

## Technology

- **Hyprland**: Wayland compositing, window management, tiling layouts,
  workspaces and monitors.
- **Go**: shell state, search, talking to Hyprland, system services, and the
  interface code.
- **GTK4 + puregotk**: the toolkit. puregotk calls GTK4 through purego, so
  there is no cgo.
- **Blueprint / GtkBuilder `.ui`**: layouts as markup files; CSS describes
  the look.
- **gtk4-layer-shell**: turns windows into Wayland layer surfaces (panels,
  overlays, exclusive keyboard).
- **Existing system services** keep providing networking, sound, Bluetooth,
  secret storage and so on.

### Toolkit

The interface is GTK4 through puregotk, with layouts in Blueprint/`.ui`
files loaded at run time and layer-shell through gtk4-layer-shell, used the
way it is meant to be. GTK4 is mature, and the cgo binding gotk4 binds the
same API should puregotk fall short. puregotk's rules for references,
callbacks and threads are kept in one place, `internal/gtkx`. The reasons are
in [toolkit.md](toolkit.md); the architecture in
[architecture.md](architecture.md).

The whole program builds without cgo (`CGO_ENABLED=0`) with Go 1.24 or
later.

### Where the software comes from

The target is Ubuntu 26.04 LTS. As of 2026-10-08, Hyprland is in the
official universe repository at 0.53.3+ds-4.

Every package needed is in the official repositories. puregotk is a Go
module without releases, pinned to a commit.

### Packages

Names and versions are those of Ubuntu 26.04's repositories (checked
2026-10-09).

**Required to run** (`make deps`):

| Package | Version | For |
| --- | --- | --- |
| `hyprland` | 0.53.3 | The compositor; the shell reads windows and workspaces over its IPC |
| `libgtk-4-1` | 4.22 | The toolkit (puregotk loads it at run time; not needed to build) |
| `libgtk4-layer-shell0` | 1.3 | Layer surfaces for the bar, launcher, control center, dock and notifications |

**Needed by shell features**: without them the shell still runs, and the
part shows "Unavailable" or hides. Ubuntu Desktop usually has the first five.

| Package | For |
| --- | --- |
| `network-manager` | Network status, the Wi-Fi switch |
| `bluez` | Bluetooth status and switch |
| `upower` | Battery |
| `power-profiles-daemon` | Power saver |
| `pipewire-bin`, `wireplumber` | Volume and mute (`pw-dump` watches for changes, `wpctl` reads and writes) |
| `libnotify-bin` | Optional: `notify-send` for test notifications. inua shows notifications itself; mako is not needed. Ubuntu's AppArmor profile does not let `notify-send` ask the server what it supports, so it cannot send buttons |
| `login` | `/usr/sbin/unix_chkpwd`, which checks the password on inua's lock screen (it is always installed) |
| `grim`, `wl-clipboard` | The screenshot tool: grim captures the screens, wl-copy puts shots on the clipboard |
| `pipewire-bin` (`pw-play`) | The alarm and timer sound |
| `wf-recorder` | Recording the screen from the screenshot tool |
| `libturbojpeg0` | Optional: Viewer decodes JPEG with it, several times faster, and at a smaller size for a quick first look; without it, Go decodes |
| `xdg-terminal-exec` | Launching `Terminal=true` apps when neither `launcher.terminal` nor `$TERMINAL` is set |
| `pass`, `gnupg` | `inua-keyring`, the Secret Service kept in the password store (`pass init` sets one up); without pass, apps keep using gnome-keyring and the assistant's keys stay in its file |

Brightness is set through systemd-logind; no extra package.

**Building and development** (`make deps`):

| Package | For |
| --- | --- |
| `golang-go` | Building (Go ≥ 1.24; 26.04 has 1.26) |
| `gcc` | `make test`'s `-race` needs cgo (the build itself is `CGO_ENABLED=0`) |
| `dbus-daemon` | Private buses in tests (`internal/dbustest`) |
| `pass`, `gnupg` | `internal/keyring`'s test on the real pass (skipped without them) |
| `blueprint-compiler` | Regenerating `.ui` after editing `ui/*.blp` (`make generate`) |
| `python3` | `scripts/spike.sh … nested`, and Pillow for `scripts/fcitx-themes.py` |

**Fonts** (optional): monospace text uses Geist Mono, then IBM Plex Mono
(`fonts-ibm-plex`); body text Zen Kaku Gothic New, then Noto Sans CJK
(`fonts-noto-cjk`); titles in the settings window IBM Plex Sans. Geist Mono
and Zen Kaku Gothic New are not in Ubuntu's repositories; install them from
Google Fonts into `~/.local/share/fonts`. Without them the system defaults
apply.

**Companions the sample config uses**
([config/hypr/hyprland.conf](../config/hypr/hyprland.conf); all optional,
started only when installed; `make deps-desktop`): `fonts-ibm-plex`,
`fonts-noto-cjk`, `kitty`, `nautilus`, `hypridle`,
`xdg-desktop-portal-hyprland`, `xdg-desktop-portal-gtk`,
`brightnessctl`, `playerctl`, `fcitx5`, and the
`grim` and `wl-clipboard` above.

Window layouts are whatever the installed Hyprland offers; pick Dwindle or
Master by trying them. Scrolling layouts are not assumed, and no plugins are
added to imitate other compositors.

## Who owns what

| Part | Source of truth | What inua does |
| --- | --- | --- |
| Windows, focus, workspaces | Hyprland IPC | Shows, searches, selects, and calls window operations |
| Panels and search | inua's Go shell | Shell state drives search and actions; layout files describe the interface; interface code fills widgets from state |
| Network, sound, Bluetooth | The system services | Shows their state and offers their controls |
| Notifications | inua (`org.freedesktop.Notifications`) | Banners, the notification center, Focus |

Hyprland is the only source of truth for window state. Go keeps a snapshot
of windows and workspaces, refreshed by events and fetched again after a
reconnect. The interface reads that snapshot and keeps no window state of
its own.

State changes from background work move to the GTK main thread before they
touch shell state or widgets. puregotk needs native references and
callbacks managed by hand; those rules live in `internal/gtkx`, not across
the interface code.

After the interface asks for something, Hyprland's reply and the state
changes that follow confirm the result; inua does not assume focus or window
positions changed.

Window arrangement and workspace structure stay Hyprland's. The shell works
through Hyprland's own configuration and IPC.

## Interaction

One interface, several modes: launching apps, switching windows and desktop
commands share search and navigation, entered with different keys or
prefixes.

| Mode | Searches | Result |
| --- | --- | --- |
| Apps | Installed applications | Launches the app |
| Windows | Titles and app IDs of open windows | Focuses the window, switching workspace |
| Commands (planned) | Defined desktop actions | Runs the action |

The interface always shows the current mode. "Launch an app" and "switch to
a window" are distinct result types with their own action labels.

## Scope of the first version

### Still to check

In a real Hyprland session: Chinese input through Fcitx 5 in the launcher,
opening over fullscreen apps, whether showing a layer surface again can hang
outside nested sessions, and long runs across Hyprland restarts.

### The everyday flow

One flow that works for real:

> A shortcut opens it → search for an app or a window → pick with the
> keyboard → launch or switch → the interface closes and hands focus back.

The first version has:

1. A search interface that opens, closes and takes keyboard input.
2. App and window modes sharing the interface and navigation.
3. Hyprland window state, and focusing by window address.
4. Clear feedback for no results, failed actions and a lost connection.
5. Defined focus behaviour for cancel, execute and opening again.

Built since: the control center, the tray, the wallpaper, the settings
window, notifications with a notification center, the app dock, a
screenshot tool (with recording and a colour picker), a lock screen, light
apps (Clock, Weather, Stocks, Calculator, Calendar, Color Picker, System
Monitor, Viewer), answers in the launcher (arithmetic, units, currencies), and
keyboard, touchpad and shortcut settings. Command mode and
clipboard history are later.

### Light apps

Clock, Weather, Stocks and Calculator are inua's own small apps, like a
phone's built-in utilities. Each is a process of its own (`inua-apps NAME`)
with its own application ID (`io.github.lyricat.Inua.Clock` …), so the
launcher, the dock and Hyprland treat it as any app: a desktop entry, an
icon, a window class to pin and match. They share the settings window's
controls and type (mono small capitals for labels, light figures for times
and prices, hairlines, one accent): Clock and Weather are one column,
Stocks keeps an index of its list beside the chosen quote. They load the
shell's theme, style and icons, follow theme changes while open, and float at their own size
(a window rule in `inua.conf`). Their data lives in `~/.local/share/inua`
as JSON. The shell, not the Clock app, rings alarms, so they go off with
the app closed. Weather reads Open-Meteo (free, no key); Stocks reads
Yahoo Finance's public but unofficial chart and search endpoints, which may
change or refuse requests without notice.

## Focus and display rules

- When it opens, the search interface takes keyboard input.
- On cancel, focus returns to the previous window if it still exists.
- After picking a window, focus goes to it.
- After launching an app the interface closes, and Hyprland places the new
  window by its rules; focus is not pulled back to the old window.
- If the previous window has closed, Hyprland's focus stands; a stale window
  address is never restored.
- With several monitors, the launcher and overlays open on the focused one.
- The search interface uses a layer that shows above fullscreen apps and
  receives input there.

Panels, notifications and the control center follow the same rules for
opening, cancelling and handing focus back.

## Using the interfaces

- Extra queries use hyprctl's JSON output, never its human-readable output;
  actions go through dispatchers.
- Direct IPC uses the request socket for commands and the event socket for
  events. Hyprland's raw events are `EVENT>>DATA` lines, not JSON.
- The event stream has no initial snapshot: query the initial state
  explicitly, then refresh on events, and handle queries and events
  interleaving during start and reconnects so no change is lost.
- Prefer events; merge the queries a burst of events triggers; avoid
  frequent polling.
- Window operations name a window address rather than relying on the
  current focus. Addresses are only for live windows in this session, never
  stored.
- Showing state and the result of an action are separate; a window may close
  between being picked and the action.
- Use existing interfaces directly; no wrappers that only rename or forward.

## Companion services

| Function | Component |
| --- | --- |
| Login and session choice | greetd + tuigreet (recommended); GDM or SDDM also work |
| Network and VPN | NetworkManager |
| Sound | PipeWire and WirePlumber |
| Bluetooth | BlueZ |
| Screen sharing and file choosers | xdg-desktop-portal; the Hyprland backend shares screens, the GTK backend provides file choosers |
| Passwords and app secrets | gnome-keyring |
| Administrator prompts | inua itself (the session's Polkit agent) |
| gpg passphrases | inua itself (`inua-pinentry`) |
| Input methods | Fcitx 5, with inua's own themes |
| X11 apps | Xwayland, managed by Hyprland |
| Notifications | inua itself |

This is a list of responsibilities: check what the system already has before
installing, so nothing runs twice.

## Testing

Layer-shell integration is checked with `scripts/spike.sh shell nested` and
`stress`: layers, reserved space, keyboard focus, hangs when opening and
closing repeatedly, memory, GTK warnings. Shell state, search and focus rules
live in packages without GTK and are unit tested; services are tested
against fake D-Bus services on private buses.

The interface also needs to feel right: opening, searching, picking,
cancelling, executing and handing focus back, plus Chinese input, fullscreen
and several monitors. Look and interaction are checked by using it.

Code with logic gets its tests written or updated first, then the
implementation, until they pass. The focus is event-driven state, rebuilding
state after disconnects, search matching and failure handling. Documentation,
wording and purely visual changes get no tests.

## References

- [Ubuntu 26.04 Hyprland package](https://packages.ubuntu.com/resolute/hyprland)
- [Hyprland IPC (0.53.0)](https://wiki.hypr.land/0.53.0/IPC/)
- [hyprctl (0.53.0)](https://wiki.hypr.land/0.53.0/Configuring/Using-hyprctl/)
- [Hyprland Dwindle layout (0.53.0)](https://wiki.hypr.land/0.53.0/Configuring/Dwindle-Layout/)
- [Hyprland Master layout (0.53.0)](https://wiki.hypr.land/0.53.0/Configuring/Master-Layout/)
- [puregotk](https://codeberg.org/puregotk/puregotk)
- [gtk4-layer-shell](https://github.com/wmww/gtk4-layer-shell)
- [Blueprint](https://gnome.pages.gitlab.gnome.org/blueprint-compiler/)
- [GtkBuilder `.ui` format](https://docs.gtk.org/gtk4/class.Builder.html)
- [purego](https://github.com/ebitengine/purego)
- [Desktop Notifications Specification](https://specifications.freedesktop.org/notification-spec/latest/)
- [StatusNotifierItem](https://www.freedesktop.org/wiki/Specifications/StatusNotifierItem/)

Hyprland's documentation is that of the installed 0.53; puregotk has no
releases, so its source at the pinned commit is the reference.
